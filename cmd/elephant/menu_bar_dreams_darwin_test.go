//go:build darwin

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

// dreamUI answers native dialogs from a script and records what was shown.
type dreamUI struct {
	t        *testing.T
	mu       sync.Mutex
	answers  []menubar.Action
	dialogs  []menubar.Dialog
	informed []string
}

func (u *dreamUI) answer(answers ...menubar.Action) {
	u.mu.Lock()
	u.answers = append(u.answers, answers...)
	u.mu.Unlock()
}

func (u *dreamUI) prompt(d menubar.Dialog) (menubar.Action, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.dialogs = append(u.dialogs, d)
	if len(u.answers) == 0 {
		u.t.Errorf("unexpected dialog %q", d.Title)
		return menubar.Action{}, context.Canceled
	}
	a := u.answers[0]
	u.answers = u.answers[1:]
	found := false
	for _, b := range d.Buttons {
		found = found || b.ID == a.Choice
	}
	if !found {
		u.t.Errorf("dialog %q has no button %q", d.Title, a.Choice)
	}
	a.ID = d.ID
	return a, nil
}

func (u *dreamUI) inform(title, message string) {
	u.mu.Lock()
	u.informed = append(u.informed, title+": "+message)
	u.mu.Unlock()
}

func (u *dreamUI) shown() ([]menubar.Dialog, []string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]menubar.Dialog(nil), u.dialogs...), append([]string(nil), u.informed...)
}

func (u *dreamUI) context(svc memory.Service, executable string) menuControlContext {
	return menuControlContext{Context: context.Background(), Service: svc, Executable: executable, InstanceID: "test", Prompt: u.prompt, Inform: u.inform, Refresh: func() {}, OpenURL: func(string) error {
		u.t.Error("dreaming opened a URL")
		return nil
	}}
}

func writeDreamScript(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

// dreamMenuFixture is a temp project with one project memory. Its executable
// is an inert script, so any detached dream-worker start exits at once.
func dreamMenuFixture(t *testing.T) (memory.Service, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := memory.Service{Store: memory.Store{Path: filepath.Join(root, "memory.sqlite")}, Identity: memory.Identity{Tenant: "local", User: "me"}, Root: root, Project: root}
	now := time.Now().UTC()
	if _, err = svc.Store.Record(memory.Memory{ID: "a", Tenant: "local", Owner: "me", Project: root, Scope: "project", Outcome: "worst", Incident: "Connections exhausted", Lesson: "Bound query concurrency to available pool capacity", Source: "incident-42", Created: now, Updated: now}); err != nil {
		t.Fatal(err)
	}
	return svc, writeDreamScript(t, "elephant", "exit 0")
}

func installDreaming(t *testing.T, svc memory.Service, executable, reviewer string) string {
	t.Helper()
	if _, err := memory.InitAutomation(svc, executable, "codex", 4000); err != nil {
		t.Fatal(err)
	}
	settings := memory.DefaultIdleDreamConfig()
	settings.Adapter, settings.Runner, settings.TimeoutSeconds = "command", []string{reviewer}, 60
	if _, err := svc.ConfigureDream(settings); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(svc.Root, ".elephant", "automation.json")
}

const dreamNoChangeReviewer = "cat >/dev/null\nprintf '{\"replacements\":[]}'"

func findDreamItem(items []menubar.Item, id string) *menubar.Item {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}

// watchDreamsAfterRequest runs the real watcher in-process once a Dream Now
// request is waiting, in place of the detached worker.
func watchDreamsAfterRequest(ctx context.Context, svc memory.Service, path string) <-chan error {
	errs := make(chan error, 1)
	go func() {
		for ctx.Err() == nil {
			if st, _, err := svc.DreamStatus(); err == nil && st.State.ManualRequest != "" {
				errs <- memory.WatchIdleDream(ctx, path)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		errs <- nil
	}()
	return errs
}

func waitForDream(t *testing.T, timeout time.Duration, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMenuBarDreamsSetupAsksBeforeInstallingHooks(t *testing.T) {
	svc, executable := dreamMenuFixture(t)
	d := newMenuBarDreams()
	ui := &dreamUI{t: t}
	c := ui.context(svc, executable)
	items, attention := d.Items(c)
	if setup := findDreamItem(items, "dream/setup"); setup == nil || !setup.Enabled || attention || items[0].Title != "Status: Not configured" {
		t.Fatalf("unconfigured state is not actionable: %+v", items)
	}
	ui.answer(menubar.Action{Choice: "cancel"})
	d.Handle(c, "dream/setup")
	config := filepath.Join(svc.Root, ".elephant", "automation.json")
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatal("hooks installed without consent")
	}
	dialogs, _ := ui.shown()
	if len(dialogs) != 1 || !strings.Contains(dialogs[0].Message, dreamDisplayPath(svc.Store.Path)) || dialogs[0].Cancel != "cancel" {
		t.Fatalf("setup consent did not name the store: %+v", dialogs)
	}
	ui.answer(menubar.Action{Choice: "codex"}, menubar.Action{Choice: "cancel"})
	if !d.Handle(c, "dream/setup") {
		t.Fatal("setup not handled")
	}
	installed, err := memory.ReadAutomation(config)
	if err != nil || len(installed.Agents) != 1 || installed.Agents[0] != "codex" || installed.IdleDream != nil || installed.Store != svc.Store.Path {
		t.Fatalf("wrong hooks installed: %+v %v", installed, err)
	}
	if _, err = os.Stat(filepath.Join(svc.Root, ".claude")); !os.IsNotExist(err) {
		t.Fatal("installed hooks for an unchosen agent")
	}
	if _, informed := ui.shown(); len(informed) == 0 || !strings.HasPrefix(informed[0], "Hooks Installed: ") {
		t.Fatalf("host approval steps not shown: %v", informed)
	}
	if items, _ = d.Items(c); findDreamItem(items, "dream/configure") == nil {
		t.Fatalf("configured hooks without schedule are not actionable: %+v", items)
	}
}

func TestMenuBarDreamsConfigureRunNowAndShowResult(t *testing.T) {
	svc, executable := dreamMenuFixture(t)
	if _, err := memory.InitAutomation(svc, executable, "codex", 4000); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(svc.Root, ".elephant", "automation.json")
	reviewer := writeDreamScript(t, "reviewer", dreamNoChangeReviewer)
	d := newMenuBarDreams()
	ui := &dreamUI{t: t}
	c := ui.context(svc, executable)
	ui.answer(menubar.Action{Choice: "command"}, menubar.Action{Choice: "ok", Text: `["` + reviewer + `"]`}, menubar.Action{Choice: "ok", Text: "idle=5m cooldown=1h timeout=30s runs=2 memories=5 bytes=8000 changes=1"}, menubar.Action{Choice: "enable"})
	d.Handle(c, "dream/configure")
	dialogs, _ := ui.shown()
	if len(dialogs) != 4 || !strings.Contains(dialogs[3].Message, dreamDisplayPath(reviewer)) || !strings.Contains(dialogs[3].Message, dreamDisplayPath(svc.Store.Path)) {
		t.Fatalf("consent did not name reviewer and store: %+v", dialogs)
	}
	st, _, err := svc.DreamStatus()
	if err != nil || st.Config == nil || !st.Config.Enabled || st.Config.Adapter != "command" || st.Config.Runner[0] != reviewer || st.Config.IdleSeconds != 300 || st.Config.CooldownSeconds != 3600 || st.Config.TimeoutSeconds != 30 || st.Config.MaxRunsPerDay != 2 || st.Config.Limit != 5 || st.Config.ByteBudget != 8000 || st.Config.MaxChanges != 1 {
		t.Fatalf("settings not saved: %+v %v", st.Config, err)
	}
	items, _ := d.Items(c)
	if now := findDreamItem(items, "dream/now"); now == nil || !now.Enabled || items[0].Title != "Status: Scheduled" {
		t.Fatalf("Dream Now unavailable: %+v", items)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := watchDreamsAfterRequest(ctx, svc, path)
	ui.answer(menubar.Action{Choice: "now"})
	done := make(chan struct{})
	go func() { d.Handle(c, "dream/now"); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("Dream Now did not finish")
	}
	st, _, err = svc.DreamStatus()
	if err != nil || st.State.LastResult != "no_change" || st.State.RunRequest == "" || st.State.Attempts != 1 || st.Reviewing {
		t.Fatalf("manual review not recorded: %+v %v", st.State, err)
	}
	if _, informed := ui.shown(); !strings.HasPrefix(informed[len(informed)-1], "Review Finished: Result: reviewed, no change justified") {
		t.Fatalf("result not reported: %v", informed)
	}
	d.Handle(c, "dream/last")
	if _, informed := ui.shown(); !strings.Contains(informed[len(informed)-1], "Started by: Dream Now") {
		t.Fatalf("last result not shown: %v", informed)
	}
	cancel()
	if err = <-watcher; err != nil {
		t.Fatal(err)
	}
}

func TestMenuBarDreamsCancelStopsReviewAndKeepsSchedule(t *testing.T) {
	svc, executable := dreamMenuFixture(t)
	path := installDreaming(t, svc, executable, writeDreamScript(t, "slow-reviewer", "cat >/dev/null\nexec sleep 30"))
	d := newMenuBarDreams()
	ui := &dreamUI{t: t}
	c := ui.context(svc, executable)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := watchDreamsAfterRequest(ctx, svc, path)
	ui.answer(menubar.Action{Choice: "now"})
	done := make(chan struct{})
	go func() { d.Handle(c, "dream/now"); close(done) }()
	waitForDream(t, 10*time.Second, "the review to start", func() bool {
		st, _, err := svc.DreamStatus()
		return err == nil && st.Reviewing
	})
	items, _ := d.Items(c)
	if stop := findDreamItem(items, "dream/cancel"); stop == nil || !stop.Enabled || items[0].Title != "Status: Reviewing" {
		t.Fatalf("running review not cancellable: %+v", items)
	}
	if !d.Handle(c, "dream/cancel") {
		t.Fatal("cancel not handled")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled review kept running")
	}
	st, _, err := svc.DreamStatus()
	if err != nil || st.State.LastResult != "cancelled_by_user" || st.Reviewing || !st.Config.Enabled {
		t.Fatalf("cancellation not truthful or disabled the schedule: %+v %v", st, err)
	}
	if _, informed := ui.shown(); !strings.Contains(informed[len(informed)-1], "cancelled by you") {
		t.Fatalf("cancellation not reported: %v", informed)
	}
	items, _ = d.Items(c)
	if items[0].Title != "Status: Scheduled" || findDreamItem(items, "dream/pause") == nil {
		t.Fatalf("schedule not kept after cancel: %+v", items)
	}
	cancel()
	if err = <-watcher; err != nil {
		t.Fatal(err)
	}
}

func TestMenuBarDreamsPauseAndResumeAskBeforeProviderUse(t *testing.T) {
	svc, executable := dreamMenuFixture(t)
	installDreaming(t, svc, executable, writeDreamScript(t, "reviewer", dreamNoChangeReviewer))
	d := newMenuBarDreams()
	ui := &dreamUI{t: t}
	c := ui.context(svc, executable)
	d.Handle(c, "dream/pause")
	items, _ := d.Items(c)
	if st, _, _ := svc.DreamStatus(); st.Config.Enabled || items[0].Title != "Status: Paused" || findDreamItem(items, "dream/resume") == nil {
		t.Fatalf("pause not applied: %+v", items)
	}
	if now := findDreamItem(items, "dream/now"); now == nil || now.Enabled {
		t.Fatal("Dream Now offered while paused")
	}
	ui.answer(menubar.Action{Choice: "cancel"})
	d.Handle(c, "dream/resume")
	if st, _, _ := svc.DreamStatus(); st.Config.Enabled {
		t.Fatal("resumed without consent")
	}
	ui.answer(menubar.Action{Choice: "resume"})
	d.Handle(c, "dream/resume")
	if st, _, _ := svc.DreamStatus(); !st.Config.Enabled {
		t.Fatal("resume not applied")
	}
}

func TestMenuBarDreamsRefuseForeignSetupAndForegroundWork(t *testing.T) {
	svc, executable := dreamMenuFixture(t)
	path := installDreaming(t, svc, executable, writeDreamScript(t, "reviewer", dreamNoChangeReviewer))
	d := newMenuBarDreams()
	ui := &dreamUI{t: t}
	other := svc
	other.Store.Path = filepath.Join(t.TempDir(), "other.sqlite")
	oc := ui.context(other, executable)
	items, _ := d.Items(oc)
	for _, item := range items {
		if item.Enabled && item.ID != "" {
			t.Fatalf("foreign setup offered %q", item.ID)
		}
	}
	for _, action := range []string{"dream/now", "dream/pause", "dream/configure", "dream/cancel"} {
		if !d.Handle(oc, action) {
			t.Fatalf("%s not handled", action)
		}
	}
	st, _, err := svc.DreamStatus()
	if err != nil || !st.Config.Enabled || st.State.ManualRequest != "" {
		t.Fatalf("foreign session changed this setup: %+v %v", st, err)
	}
	if dialogs, informed := ui.shown(); len(dialogs) != 0 || len(informed) != 4 {
		t.Fatalf("foreign actions prompted or stayed silent: %v %v", dialogs, informed)
	}
	if _, err = memory.RunHook(path, "codex", strings.NewReader(`{"hook_event_name":"PostToolUse","session_id":"foreground","cwd":"`+svc.Root+`","tool_name":"Bash"}`)); err != nil {
		t.Fatal(err)
	}
	c := ui.context(svc, executable)
	items, _ = d.Items(c)
	if now := findDreamItem(items, "dream/now"); now == nil || now.Enabled || !strings.Contains(now.Title, "foreground work active") {
		t.Fatalf("Dream Now offered during foreground work: %+v", items)
	}
	d.Handle(c, "dream/now")
	if st, _, _ = svc.DreamStatus(); st.State.ManualRequest != "" {
		t.Fatal("request filed during foreground work")
	}
	if dialogs, _ := ui.shown(); len(dialogs) != 0 {
		t.Fatal("asked for provider consent during foreground work")
	}
}

func TestParseDreamLimitsRejectsAmbiguousInput(t *testing.T) {
	base := memory.DefaultIdleDreamConfig()
	if got, err := parseDreamLimits(formatDreamLimits(base), base); err != nil || got.IdleSeconds != base.IdleSeconds || got.CooldownSeconds != base.CooldownSeconds || got.TimeoutSeconds != base.TimeoutSeconds {
		t.Fatalf("formatted limits did not round-trip: %+v %v", got, err)
	}
	for _, text := range []string{"idle=10m idle=5m", "idle", "idle=1.5s", "runs=two", "speed=1", "idle=-1m"} {
		if _, err := parseDreamLimits(text, base); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
}
