package memory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setIdleState(t *testing.T, c AutomationConfig, change func(*IdleDreamState)) {
	t.Helper()
	if err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error { change(state); return nil }); err != nil {
		t.Fatal(err)
	}
}

// quietFixture has no pending work and an active cooldown, so only an
// explicit Dream Now request can start a review. Its live lease prevents any
// detached watcher from starting.
func quietFixture(t *testing.T) (AutomationConfig, string, Memory, string) {
	t.Helper()
	c, path, m, lease := idleFixture(t)
	setIdleState(t, c, func(s *IdleDreamState) { s.Reviewed, s.LastAttempt = s.Generation, time.Now() })
	return c, path, m, lease
}

func TestDreamNowSkipsQuietGatesButKeepsSafeguards(t *testing.T) {
	c, path, old, lease := quietFixture(t)
	calls := 0
	runner := func(_ context.Context, _ AutomationConfig, _ DreamBatch) (IdleDreamOutput, error) {
		calls++
		status, err := DreamScheduleStatus(path)
		if err != nil || !status.Reviewing || status.ManualBlocker != "reviewing" {
			t.Errorf("running review not reported: %+v %v", status, err)
		}
		if _, err := c.Service().RequestDreamNow(); err == nil {
			t.Error("overlapping Dream Now accepted")
		}
		return IdleDreamOutput{Replacements: []IdleDreamProposal{idleProposal(old)}}, nil
	}
	if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil || calls != 0 {
		t.Fatalf("schedule gates ignored: calls=%d %v", calls, err)
	}
	id, err := c.Service().RequestDreamNow()
	if err != nil || id == "" {
		t.Fatalf("request refused: %v", err)
	}
	if _, err := c.Service().RequestDreamNow(); err == nil {
		t.Fatal("duplicate request accepted")
	}
	if status, _ := DreamScheduleStatus(path); status.ManualBlocker != "requested" || status.Ready {
		t.Fatalf("pending request not reported: %+v", status)
	}
	if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil || calls != 1 {
		t.Fatalf("request not run: calls=%d %v", calls, err)
	}
	status, err := DreamScheduleStatus(path)
	if err != nil || status.State.LastResult != "saved" || len(status.State.LastSaved) != 1 || status.State.RunRequest != id || status.State.ManualRequest != "" || status.State.Attempts != 1 || status.State.LastFinished.IsZero() || status.Reviewing {
		t.Fatalf("bad manual receipt: %+v %v", status, err)
	}
	if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil || calls != 1 {
		t.Fatal("a request ran twice", calls, err)
	}
}

func TestDreamNowRefusesForegroundPauseAndLimits(t *testing.T) {
	for _, mode := range []string{"foreground", "paused", "automation_disabled", "not_configured", "daily_limit"} {
		t.Run(mode, func(t *testing.T) {
			c, path, _, _ := quietFixture(t)
			switch mode {
			case "foreground":
				if _, err := touchIdleDream(c, "foreground", true, time.Now()); err != nil {
					t.Fatal(err)
				}
			case "paused":
				settings := *c.IdleDream
				settings.Enabled = false
				c.IdleDream = &settings
			case "automation_disabled":
				c.Enabled = false
			case "not_configured":
				c.IdleDream = nil
			case "daily_limit":
				setIdleState(t, c, func(s *IdleDreamState) {
					s.Day, s.Attempts = time.Now().UTC().Format("2006-01-02"), c.IdleDream.MaxRunsPerDay
				})
			}
			writeIdleConfig(t, path, c)
			if _, err := c.Service().RequestDreamNow(); err == nil {
				t.Fatal("unsafe Dream Now accepted")
			}
			status, err := DreamScheduleStatus(path)
			if err != nil || status.State.ManualRequest != "" || status.ManualBlocker == "" {
				t.Fatalf("refusal left a request: %+v %v", status, err)
			}
		})
	}
}

func TestDreamNowWaitsForForegroundWorkThatStartsLater(t *testing.T) {
	c, path, _, lease := quietFixture(t)
	if _, err := c.Service().RequestDreamNow(); err != nil {
		t.Fatal(err)
	}
	if _, err := touchIdleDream(c, "foreground", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	runner := func(context.Context, AutomationConfig, DreamBatch) (IdleDreamOutput, error) {
		t.Fatal("reviewer ran during foreground work")
		return IdleDreamOutput{}, nil
	}
	if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil {
		t.Fatal(err)
	}
}

func TestCancelDreamReviewStopsRunAndKeepsSchedule(t *testing.T) {
	c, path, old, lease := idleFixture(t)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- runIdleDream(context.Background(), path, c, lease, func(ctx context.Context, _ AutomationConfig, _ DreamBatch) (IdleDreamOutput, error) {
			close(started)
			<-ctx.Done()
			return IdleDreamOutput{Replacements: []IdleDreamProposal{idleProposal(old)}}, ctx.Err()
		})
	}()
	<-started
	if ok, err := c.Service().CancelDreamReview(); err != nil || !ok {
		t.Fatalf("running review not cancelled: %v %v", ok, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reviewer kept running after cancellation")
	}
	status, err := DreamScheduleStatus(path)
	if err != nil || status.State.LastResult != "cancelled_by_user" || status.State.CancelRun != "" || status.State.Reviewed == status.State.Generation || !status.Config.Enabled || status.WaitReason != "cooldown" || status.Reviewing {
		t.Fatalf("cancellation not truthful or disabled the schedule: %+v %v", status, err)
	}
	if all, _ := c.Service().Store.All(); len(all) != 1 || all[0].Retired {
		t.Fatal("cancelled review changed memory")
	}
	if ok, err := c.Service().CancelDreamReview(); err != nil || ok {
		t.Fatalf("nothing to cancel, got %v %v", ok, err)
	}
}

func TestCancelDreamReviewBlocksCommitAfterReviewerFinished(t *testing.T) {
	c, path, old, lease := idleFixture(t)
	runner := func(context.Context, AutomationConfig, DreamBatch) (IdleDreamOutput, error) {
		if ok, err := c.Service().CancelDreamReview(); err != nil || !ok {
			t.Errorf("cancel: %v %v", ok, err)
		}
		return IdleDreamOutput{Replacements: []IdleDreamProposal{idleProposal(old)}}, nil
	}
	if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil {
		t.Fatal(err)
	}
	status, _ := DreamScheduleStatus(path)
	if status.State.LastResult != "cancelled_by_user" || len(status.State.LastSaved) != 0 {
		t.Fatalf("late commit not cancelled: %+v", status.State)
	}
	if all, _ := c.Service().Store.All(); len(all) != 1 || all[0].Retired {
		t.Fatal("cancelled review saved a replacement")
	}
}

func TestCancelDreamReviewWithdrawsWaitingRequest(t *testing.T) {
	c, path, _, lease := quietFixture(t)
	if _, err := c.Service().RequestDreamNow(); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.Service().CancelDreamReview(); err != nil || !ok {
		t.Fatalf("waiting request not withdrawn: %v %v", ok, err)
	}
	runner := func(context.Context, AutomationConfig, DreamBatch) (IdleDreamOutput, error) {
		t.Fatal("withdrawn request ran")
		return IdleDreamOutput{}, nil
	}
	if err := runIdleDream(context.Background(), path, c, lease, runner); err != nil {
		t.Fatal(err)
	}
	if status, _ := DreamScheduleStatus(path); status.ManualBlocker != "" || status.State.ManualRequest != "" {
		t.Fatalf("withdrawn request still reported: %+v", status)
	}
}

func TestIdleWatcherStartsDreamNow(t *testing.T) {
	c, path, _, _ := quietFixture(t)
	binary, _ := os.Executable()
	c.IdleDream.Adapter = "command"
	c.IdleDream.Runner = []string{binary, "-test.run=^TestIdleRunnerProcessHelper$", "--"}
	writeIdleConfig(t, path, c)
	setIdleState(t, c, func(s *IdleDreamState) { s.Lease, s.LeaseUntil = "", time.Time{} })
	t.Setenv("ELEPHANT_DREAM_TEST_HELPER", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- WatchIdleDream(ctx, path) }()
	deadline := time.After(8 * time.Second)
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	id := ""
	for {
		status, err := DreamScheduleStatus(path)
		if err != nil {
			t.Fatal(err)
		}
		// Request only after the in-process watcher holds the lease, so no
		// detached worker starts.
		if id == "" && status.Running {
			if id, err = c.Service().RequestDreamNow(); err != nil {
				t.Fatal(err)
			}
		}
		if id != "" && status.State.RunRequest == id && status.State.LastResult == "no_change" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("watcher did not start Dream Now: %+v", status)
		case <-poll.C:
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDreamControlsActOnlyOnTheirOwnService(t *testing.T) {
	c, path, _, _ := quietFixture(t)
	if status, found, err := c.Service().DreamStatus(); err != nil || !found || status.Config == nil {
		t.Fatalf("own setup not found: %+v %v %v", status, found, err)
	}
	sub := filepath.Join(c.Root, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	paused := *c.IdleDream
	paused.Enabled = false
	for name, change := range map[string]func(*Service){
		"store":        func(s *Service) { s.Store.Path = filepath.Join(t.TempDir(), "other.sqlite") },
		"identity":     func(s *Service) { s.Identity.User = "other" },
		"project":      func(s *Service) { s.Project = "other" },
		"conversation": func(s *Service) { s.Conversation = "other" },
		"nested root":  func(s *Service) { s.Root = sub },
	} {
		s := c.Service()
		change(&s)
		if _, found, err := s.DreamStatus(); !errors.Is(err, ErrDreamSetupMismatch) || !found {
			t.Fatalf("%s: status used another setup: %v %v", name, found, err)
		}
		if _, err := s.RequestDreamNow(); err == nil {
			t.Fatalf("%s: Dream Now used another setup", name)
		}
		if _, err := s.CancelDreamReview(); err == nil {
			t.Fatalf("%s: cancel used another setup", name)
		}
		if _, err := s.ConfigureDream(paused); err == nil {
			t.Fatalf("%s: configure used another setup", name)
		}
	}
	if status, _, _ := c.Service().DreamStatus(); status.State.ManualRequest != "" || !status.Config.Enabled {
		t.Fatalf("a foreign session changed this setup: %+v", status)
	}
	s := c.Service()
	s.Root = t.TempDir()
	if _, found, err := s.DreamStatus(); err != nil || found {
		t.Fatalf("unconfigured root: %v %v", found, err)
	}
	if _, err := s.RequestDreamNow(); err == nil {
		t.Fatal("Dream Now without automation accepted")
	}
	// Another client replaces the automation after this session read it.
	replaced := c
	replaced.Store = filepath.Join(t.TempDir(), "replaced.sqlite")
	writeIdleConfig(t, path, replaced)
	if _, err := c.Service().ConfigureDream(paused); !errors.Is(err, ErrDreamSetupMismatch) {
		t.Fatalf("configured a replaced setup: %v", err)
	}
	if current, _ := ReadAutomation(path); current.Store != replaced.Store || !current.IdleDream.Enabled {
		t.Fatal("replaced setup was modified")
	}
}

func TestDreamConfigurationKeepsLastReviewResult(t *testing.T) {
	c, path, _, _ := idleFixture(t)
	c.Enabled = false // No detached process is needed for configuration checks.
	writeIdleConfig(t, path, c)
	setIdleState(t, c, func(s *IdleDreamState) { s.LastResult, s.LastSaved = "saved", []string{"m1"} })
	settings := *c.IdleDream
	settings.Enabled = false
	status, err := c.Service().ConfigureDream(settings)
	if err != nil || status.Config.Enabled || status.State.LastResult != "saved" || len(status.State.LastSaved) != 1 {
		t.Fatalf("configuration replaced the review result: %+v %v", status, err)
	}
}
