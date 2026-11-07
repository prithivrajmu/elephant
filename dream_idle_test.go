package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func idleFixture(t *testing.T) (AutomationConfig, string, Memory, string) {
	t.Helper()
	s, m := dreamFixture(t)
	root, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		t.Fatal(err)
	}
	s.Root = root
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	settings := DefaultIdleDreamConfig()
	c := AutomationConfig{Version: 1, Enabled: true, Root: s.Root, Project: s.Project, Store: s.Store.Path, Identity: s.Identity, Binary: binary, Budget: 4000, Agents: []string{"codex"}, IdleDream: &settings}
	path := automationPath(c.Root)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	writeIdleConfig(t, path, c)
	if err = idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		state.Generation, state.LastActivity = 2, time.Now().Add(-11*time.Minute)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lease, err := claimIdleWatcher(c, time.Now())
	if err != nil || lease == "" {
		t.Fatalf("claim: %s %v", lease, err)
	}
	return c, path, m, lease
}

func writeIdleConfig(t *testing.T, path string, c AutomationConfig) {
	t.Helper()
	b, _ := json.Marshal(c)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func idleProposal(m Memory) IdleDreamProposal {
	r := dreamReplacement(m)
	return IdleDreamProposal{Kind: r.Dream.Kind, Checks: r.Dream.Checks, Incident: r.Incident, Lesson: r.Lesson, Source: r.Source}
}

func TestIdleReadinessBudgetsAndDayRollover(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	config := DefaultIdleDreamConfig()
	base := IdleDreamState{Generation: 2, Reviewed: 1, LastActivity: now.Add(-time.Hour), Active: map[string]bool{}}
	if !idleReady(base, config, now) {
		t.Fatal("completed quiet task was not eligible")
	}
	for _, mutate := range []func(*IdleDreamState){
		func(s *IdleDreamState) { s.Active = map[string]bool{"busy": true} },
		func(s *IdleDreamState) { s.LastActivity = now },
		func(s *IdleDreamState) { s.Reviewed = s.Generation },
		func(s *IdleDreamState) { s.LastAttempt = now.Add(-time.Minute) },
		func(s *IdleDreamState) { s.Day = now.Format("2006-01-02"); s.Attempts = config.MaxRunsPerDay },
	} {
		state := base
		mutate(&state)
		if idleReady(state, config, now) {
			t.Fatalf("unsafe state eligible: %+v", state)
		}
	}
	base.Day, base.Attempts = "2026-11-06", 999
	if !idleReady(base, config, now) {
		t.Fatal("UTC daily budget did not reset")
	}
	config.Enabled = false
	if idleReady(base, config, now) {
		t.Fatal("paused schedule eligible")
	}
}

func TestIdleLeaseSingleFlightAndCrashRecovery(t *testing.T) {
	c, _, _, first := idleFixture(t)
	second, err := claimIdleWatcher(c, time.Now())
	if err != nil || second != "" {
		t.Fatalf("overlapping watcher: %s %v", second, err)
	}
	second, err = claimIdleWatcher(c, time.Now().Add(time.Minute))
	if err != nil || second == "" || second == first {
		t.Fatalf("expired lease not recovered: %s %v", second, err)
	}
}

func TestIdlePassCommitsThroughNormalLifecycle(t *testing.T) {
	c, path, old, lease := idleFixture(t)
	calls := 0
	runner := func(_ context.Context, _ AutomationConfig, batch DreamBatch) (IdleDreamOutput, error) {
		calls++
		if len(batch.Candidates) != 1 || batch.Candidates[0].Memory.ID != old.ID {
			t.Fatal("wrong batch")
		}
		return IdleDreamOutput{Replacements: []IdleDreamProposal{idleProposal(old)}}, nil
	}
	if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil {
		t.Fatal(err)
	}
	status, err := DreamScheduleStatus(path)
	if err != nil || status.State.LastResult != "saved" || len(status.State.LastSaved) != 1 || status.State.Attempts != 1 || status.State.Reviewed != status.State.Generation {
		t.Fatalf("missing receipt: %+v %v", status, err)
	}
	if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil || calls != 1 {
		t.Fatal("ran twice without new work", err, calls)
	}
	all, _ := c.Service().Store.All()
	for _, m := range all {
		if m.ID == old.ID && !m.Retired {
			t.Fatal("old lesson stayed active")
		}
		if m.ID != old.ID && (m.Dream == nil || m.Helpful != 0) {
			t.Fatal("derived provenance or feedback was wrong")
		}
	}
}

func TestIdleActivityAndPausePreventLateSaves(t *testing.T) {
	for _, mode := range []string{"activity", "pause", "lease", "reconfigure"} {
		t.Run(mode, func(t *testing.T) {
			c, path, old, lease := idleFixture(t)
			runner := func(_ context.Context, _ AutomationConfig, _ DreamBatch) (IdleDreamOutput, error) {
				switch mode {
				case "activity":
					if _, err := touchIdleDream(c, "foreground", true, time.Now()); err != nil {
						t.Fatal(err)
					}
				case "pause":
					paused := c
					paused.Enabled = false
					writeIdleConfig(t, path, paused)
				case "lease":
					if _, err := claimIdleWatcher(c, time.Now().Add(time.Minute)); err != nil {
						t.Fatal(err)
					}
				case "reconfigure":
					changed := c
					s := *c.IdleDream
					s.MaxChanges = 1
					changed.IdleDream = &s
					writeIdleConfig(t, path, changed)
				}
				return IdleDreamOutput{Replacements: []IdleDreamProposal{idleProposal(old)}}, nil
			}
			if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil {
				t.Fatal(err)
			}
			all, _ := c.Service().Store.All()
			if len(all) != 1 || all[0].Retired {
				t.Fatal("late idle result changed memory")
			}
		})
	}
}

func TestIdleCancelsRunningReviewerOnActivity(t *testing.T) {
	c, path, _, lease := idleFixture(t)
	started := make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	go func() {
		done <- runIdleDream(ctx, path, c, lease, func(ctx context.Context, _ AutomationConfig, _ DreamBatch) (IdleDreamOutput, error) {
			close(started)
			<-ctx.Done()
			return IdleDreamOutput{}, ctx.Err()
		})
	}()
	<-started
	if _, err := touchIdleDream(c, "new-task", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	status, _ := DreamScheduleStatus(path)
	if ctx.Err() != nil || status.State.LastResult != "cancelled" || status.State.Reviewed == status.State.Generation {
		t.Fatalf("did not cancel on activity: %+v", status)
	}
}

func TestIdleNoChangeFailureAndExcessOutput(t *testing.T) {
	for _, mode := range []string{"no_change", "runner_failed", "excess"} {
		t.Run(mode, func(t *testing.T) {
			c, path, old, lease := idleFixture(t)
			runner := func(_ context.Context, _ AutomationConfig, _ DreamBatch) (IdleDreamOutput, error) {
				if mode == "runner_failed" {
					return IdleDreamOutput{}, fmt.Errorf("private runner diagnostic")
				}
				if mode == "excess" {
					p := idleProposal(old)
					return IdleDreamOutput{Replacements: []IdleDreamProposal{p, p, p, p}}, nil
				}
				return IdleDreamOutput{Replacements: []IdleDreamProposal{}}, nil
			}
			if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil {
				t.Fatal(err)
			}
			status, _ := DreamScheduleStatus(path)
			b, _ := json.Marshal(status)
			if status.State.Attempts != 1 || strings.Contains(string(b), "private runner diagnostic") || len(status.State.LastSaved) != 0 {
				t.Fatalf("bad failure receipt: %+v", status)
			}
		})
	}
}

func TestIdleTracksAllSessionsAndIgnoresWorkerHooks(t *testing.T) {
	c, _, _, _ := idleFixture(t)
	for _, session := range []string{"first", "second"} {
		if _, err := touchIdleDream(c, session, true, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := touchIdleDream(c, "first", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		if len(state.Active) != 1 || !state.Active["second"] {
			t.Fatal("stopping one session cleared another")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ELEPHANT_DREAM_RUN", "1")
	if out, err := RunHook("missing-config", "codex", strings.NewReader("invalid")); err != nil || len(out) != 0 {
		t.Fatal("worker hook recursed", out, err)
	}
}

func TestIdleHookWaitsForBlockedReviewToFinish(t *testing.T) {
	c, path, _, _ := idleFixture(t) // The fixture's lease prevents detached startup.
	call := func(event string, extra map[string]any) map[string]any {
		t.Helper()
		h := map[string]any{"hook_event_name": event, "session_id": "foreground", "cwd": c.Root}
		for key, value := range extra {
			h[key] = value
		}
		b, _ := json.Marshal(h)
		out, err := RunHook(path, "codex", strings.NewReader(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	call("UserPromptSubmit", map[string]any{"prompt": "Check the pool limit"})
	call("PostToolUse", map[string]any{"tool_name": "Edit", "tool_use_id": "edit-one"})
	if out := call("Stop", nil); out["decision"] != "block" {
		t.Fatal("lesson review did not block Stop")
	}
	status, _ := DreamScheduleStatus(path)
	if len(status.State.Active) != 1 {
		t.Fatal("blocked review declared idle")
	}
	b, _ := json.Marshal([]string{c.Identity.Tenant, c.Identity.User, c.Project, "codex", "foreground"})
	if _, err := TaskSummary(path, digest(string(b)), true); err != nil {
		t.Fatal(err)
	}
	call("Stop", map[string]any{"stop_hook_active": true})
	status, _ = DreamScheduleStatus(path)
	if len(status.State.Active) != 0 {
		t.Fatal("completed review remained active")
	}
}

func TestIdleProposalCannotEscapePreparedBatch(t *testing.T) {
	_, _, m, _ := idleFixture(t)
	p := idleProposal(m)
	batch := DreamBatch{Candidates: []DreamCandidate{{Memory: m, Revision: dreamRevision(m)}}}
	p.Checks[0].ID = "another-owned-memory"
	if _, err := idleReplacement(p, batch); err == nil {
		t.Fatal("proposal escaped its prepared batch")
	}
}

func TestIdleRejectsInvalidProposalSetBeforeSaving(t *testing.T) {
	for _, mode := range []string{"outside-batch", "overlapping-input", "empty-lesson", "unchecked-source", "changed-scope", "secret", "writing"} {
		t.Run(mode, func(t *testing.T) {
			c, path, old, lease := idleFixture(t)
			second := old
			second.Incident, second.Lesson, second.Source = "A retry repeated a completed write.", "Use an idempotency key for each write.", "test:idempotency"
			if mode == "changed-scope" {
				second.Scope = "personal"
			}
			second, err := c.Service().Store.Record(second)
			if err != nil {
				t.Fatal(err)
			}
			first, invalid := idleProposal(old), idleProposal(second)
			switch mode {
			case "outside-batch":
				invalid.Checks[0].ID = "not-in-batch"
			case "overlapping-input":
				invalid = idleProposal(old)
				invalid.Lesson = "Check query concurrency against the pool capacity."
			case "empty-lesson":
				invalid.Lesson = ""
			case "unchecked-source":
				invalid.Source = "unchecked"
			case "changed-scope":
				invalid.Kind = "consolidate"
				invalid.Checks = append(invalid.Checks, first.Checks...)
			case "secret":
				invalid.Lesson = "Use ghp_" + strings.Repeat("A", 40)
			case "writing":
				if _, err := c.Service().Store.SetLanguagePolicy(100); err != nil {
					t.Fatal(err)
				}
				invalid.Lesson = strings.Repeat("word ", 30) + "."
			}
			err = runIdleDream(context.Background(), path, c, lease, func(context.Context, AutomationConfig, DreamBatch) (IdleDreamOutput, error) {
				return IdleDreamOutput{Replacements: []IdleDreamProposal{first, invalid}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			all, err := c.Service().Store.All()
			if err != nil || len(all) != 2 || all[0].Retired || all[1].Retired {
				t.Fatalf("invalid proposal set partially saved: %+v %v", all, err)
			}
			status, err := DreamScheduleStatus(path)
			if err != nil || status.State.LastResult != "invalid_output" || status.State.Reviewed == status.State.Generation {
				t.Fatalf("bad rejection receipt: %+v %v", status, err)
			}
		})
	}
}

func TestIdleOversizedMemoryDoesNotBlockQueue(t *testing.T) {
	c, path, old, lease := idleFixture(t)
	c.IdleDream.ByteBudget = 2048
	writeIdleConfig(t, path, c)
	large := old
	large.ID, large.Lesson = "000-oversized", strings.Repeat("large memory ", 300)
	if err := c.Service().Store.transact(func([]Memory, map[string]bool) (*Event, error) {
		return &Event{Kind: "put", Memory: &large}, nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := runIdleDream(context.Background(), path, c, lease, func(_ context.Context, _ AutomationConfig, batch DreamBatch) (IdleDreamOutput, error) {
		calls++
		if len(batch.Candidates) != 1 || batch.Candidates[0].Memory.ID != old.ID {
			t.Fatalf("wrong review batch: %+v", batch)
		}
		return IdleDreamOutput{Replacements: []IdleDreamProposal{}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	status, err := DreamScheduleStatus(path)
	if err != nil || calls != 1 || status.State.LastResult != "no_change" || status.State.Reviewed != status.State.Generation {
		t.Fatalf("oversized memory blocked the queue: %+v, calls=%d, err=%v", status, calls, err)
	}
	if len(status.State.LastSkipped) != 1 || status.State.LastSkipped[0].ID != large.ID || status.State.LastSkipped[0].RequiredBytes <= c.IdleDream.ByteBudget {
		t.Fatalf("skip was not reported: %+v", status.State.LastSkipped)
	}
}

func TestIdleSkippedPageAdvancesWithoutRunningReviewer(t *testing.T) {
	c, path, old, lease := idleFixture(t)
	c.IdleDream.ByteBudget, c.IdleDream.Limit = 2048, 1
	writeIdleConfig(t, path, c)
	large := old
	large.ID, large.Lesson = "000-oversized", strings.Repeat("large memory ", 300)
	if err := c.Service().Store.transact(func([]Memory, map[string]bool) (*Event, error) {
		return &Event{Kind: "put", Memory: &large}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := runIdleDream(context.Background(), path, c, lease, func(context.Context, AutomationConfig, DreamBatch) (IdleDreamOutput, error) {
		t.Fatal("started reviewer for an oversized-only page")
		return IdleDreamOutput{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	status, err := DreamScheduleStatus(path)
	if err != nil || status.State.LastResult != "skipped" || status.State.Cursor != large.ID || status.State.Reviewed != status.State.Generation || len(status.State.LastSkipped) != 1 {
		t.Fatalf("skip did not advance the queue: %+v %v", status, err)
	}
	// A later completed task continues at the next memory. No wall-clock wait.
	if err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		state.Generation++
		state.LastAttempt = time.Now().Add(-24 * time.Hour)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := runIdleDream(context.Background(), path, c, lease, func(_ context.Context, _ AutomationConfig, batch DreamBatch) (IdleDreamOutput, error) {
		calls++
		if len(batch.Candidates) != 1 || batch.Candidates[0].Memory.ID != old.ID {
			t.Fatal("did not continue after the skipped memory")
		}
		return IdleDreamOutput{Replacements: []IdleDreamProposal{}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	status, err = DreamScheduleStatus(path)
	if err != nil || calls != 1 || status.State.LastResult != "no_change" || status.State.Cursor != "" || len(status.State.LastSkipped) != 0 {
		t.Fatalf("next task did not complete the pass: %+v %v", status, err)
	}
}

func TestIdleAcceptsDisjointProposalSet(t *testing.T) {
	c, path, old, lease := idleFixture(t)
	second := old
	second.Incident, second.Lesson, second.Source = "A retry repeated a completed write.", "Use an idempotency key for each write.", "test:idempotency"
	second, err := c.Service().Store.Record(second)
	if err != nil {
		t.Fatal(err)
	}
	p := idleProposal(second)
	p.Incident, p.Lesson = "A source check confirmed the retry contract.", "Reuse the same idempotency key when retrying a write."
	if err := runIdleDream(context.Background(), path, c, lease, func(context.Context, AutomationConfig, DreamBatch) (IdleDreamOutput, error) {
		return IdleDreamOutput{Replacements: []IdleDreamProposal{idleProposal(old), p}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	status, err := DreamScheduleStatus(path)
	if err != nil || status.State.LastResult != "saved" || len(status.State.LastSaved) != 2 {
		t.Fatalf("disjoint proposals were not saved: %+v %v", status, err)
	}
	all, err := c.Service().Store.All()
	if err != nil || len(all) != 4 {
		t.Fatalf("missing history: count=%d err=%v", len(all), err)
	}
	for _, m := range all {
		if (m.ID == old.ID || m.ID == second.ID) != m.Retired {
			t.Fatal("wrong retirement state")
		}
	}
}

func TestIdleRunnerProcessHelper(t *testing.T) {
	if os.Getenv("ELEPHANT_DREAM_TEST_HELPER") != "1" {
		return
	}
	if os.Getenv("ELEPHANT_DREAM_TEST_MODE") == "wait" {
		time.Sleep(30 * time.Second)
		os.Exit(1)
	}
	var request map[string]json.RawMessage
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil || os.Getenv("ELEPHANT_DREAM_RUN") != "1" || len(request["batch"]) == 0 || len(request["output_schema"]) == 0 {
		os.Exit(2)
	}
	for i, arg := range os.Args {
		if arg == "--output-last-message" && i+1 < len(os.Args) {
			if err := os.WriteFile(os.Args[i+1], []byte(`{"replacements":[]}`), 0600); err != nil {
				os.Exit(3)
			}
			os.Exit(0)
		}
	}
	_, _ = io.WriteString(os.Stdout, `{"replacements":[]}`)
	os.Exit(0)
}

func TestIdleExecutesCommandAndCodexAdaptersWithoutModelCalls(t *testing.T) {
	c, _, m, _ := idleFixture(t)
	binary, _ := os.Executable()
	t.Setenv("ELEPHANT_DREAM_TEST_HELPER", "1")
	for _, adapter := range []string{"command", "codex"} {
		settings := *c.IdleDream
		settings.Adapter, settings.Runner = adapter, []string{binary, "-test.run=^TestIdleRunnerProcessHelper$", "--"}
		c.IdleDream = &settings
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := executeDreamRunner(ctx, c, DreamBatch{Candidates: []DreamCandidate{{Memory: m, Revision: dreamRevision(m)}}})
		cancel()
		if err != nil || out.Replacements == nil || len(out.Replacements) != 0 {
			t.Fatalf("%s adapter: %+v %v", adapter, out, err)
		}
	}
	t.Setenv("ELEPHANT_DREAM_TEST_MODE", "wait")
	c.IdleDream.Adapter = "command"
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := executeDreamRunner(ctx, c, DreamBatch{}); err == nil || ctx.Err() == nil {
		t.Fatal("runner timeout was not enforced", err)
	}
}

func TestIdleConfigurationPauseAndReset(t *testing.T) {
	c, path, _, _ := idleFixture(t)
	c.Enabled = false // No detached process is needed for configuration checks.
	writeIdleConfig(t, path, c)
	if _, err := touchIdleDream(c, "abandoned", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	settings := *c.IdleDream
	settings.Enabled = false
	status, err := ConfigureIdleDream(path, settings, true)
	if err != nil || status.Config.Enabled || len(status.State.Active) != 0 || status.State.Reviewed != status.State.Generation {
		t.Fatalf("pause/reset failed: %+v %v", status, err)
	}
	settings.IdleSeconds = 0
	if _, err := ConfigureIdleDream(path, settings, false); err == nil {
		t.Fatal("unsafe scheduler limits accepted")
	}
	settings = DefaultIdleDreamConfig()
	settings.Runner = []string{filepath.Join(t.TempDir(), "missing-runner")}
	if _, err := ConfigureIdleDream(path, settings, false); err == nil {
		t.Fatal("missing runner accepted")
	}
}

func TestIdleConfigurationUsesTheInitializationLock(t *testing.T) {
	c, path, _, _ := idleFixture(t)
	lock := filepath.Join(c.Root, ".elephant", "init.lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lock)
	settings := *c.IdleDream
	settings.Enabled = false
	if _, err := ConfigureIdleDream(path, settings, false); err == nil {
		t.Fatal("idle settings bypassed an active initializer")
	}
	paused := false
	if _, err := Automation(c.Root, &paused); err == nil {
		t.Fatal("automation pause bypassed an active initializer")
	}
	current, _ := ReadAutomation(path)
	if !current.Enabled || !current.IdleDream.Enabled {
		t.Fatal("contended configuration was overwritten")
	}
}

func TestIdleWatcherRunsAndReleasesItsLease(t *testing.T) {
	c, path, _, _ := idleFixture(t)
	binary, _ := os.Executable()
	c.IdleDream.Adapter = "command"
	c.IdleDream.Runner = []string{binary, "-test.run=^TestIdleRunnerProcessHelper$", "--"}
	writeIdleConfig(t, path, c)
	if err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		state.Lease, state.LeaseUntil = "", time.Time{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ELEPHANT_DREAM_TEST_HELPER", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- WatchIdleDream(ctx, path) }()
	deadline := time.After(6 * time.Second)
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	for {
		status, err := DreamScheduleStatus(path)
		if err != nil {
			t.Fatal(err)
		}
		if status.State.LastResult == "no_change" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("watcher did not run: %+v", status)
		case <-poll.C:
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	status, _ := DreamScheduleStatus(path)
	if status.Running {
		t.Fatal("watcher leaked its lease")
	}
}
