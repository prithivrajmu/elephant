//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

// updatesHarness drives the controller with a synthetic store and fake
// installer/probe; no network, dialog, PATH lookup or real binary is touched.
type updatesHarness struct {
	t        *testing.T
	u        *menuBarUpdates
	c        menuControlContext
	dir      string
	target   string
	mu       sync.Mutex
	onDisk   map[string]string
	failing  map[string]bool
	choice   string
	dialogs  []menubar.Dialog
	informed []string
	installs []string
	opened   []string
}

func newUpdatesHarness(t *testing.T, cached memory.UpdateStatus) *updatesHarness {
	t.Helper()
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := memory.Store{Path: filepath.Join(dir, "memory.sqlite")}
	cached.Installed = memory.Version
	b, _ := json.Marshal(cached)
	if err := os.WriteFile(store.Path+".updates.json", b, 0o600); err != nil {
		t.Fatal(err)
	}
	h := &updatesHarness{t: t, dir: dir, onDisk: map[string]string{}, failing: map[string]bool{}}
	h.target = h.binary("elephant", "1.2.0")
	link := filepath.Join(dir, "elephant-link")
	if err := os.Symlink(h.target, link); err != nil {
		t.Fatal(err)
	}
	h.u = newMenuBarUpdates()
	h.u.self = func() (string, error) { return "", errors.New("not in this test") }
	h.u.lookPath = func(string) (string, error) { return "", errors.New("not in this test") }
	h.u.home = func() (string, error) { return dir, nil }
	h.u.check = func(memory.Store) (memory.UpdateStatus, error) {
		t.Error("unexpected network check")
		return memory.UpdateStatus{}, nil
	}
	h.u.probe = func(_ context.Context, p string) (string, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		v, ok := h.onDisk[p]
		if !ok {
			t.Error("probed unresolved path", p)
		}
		return v, nil
	}
	h.u.install = func(_ context.Context, p, v string) (memory.UpdateInstallResult, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.installs = append(h.installs, p+"@"+v)
		if h.failing[p] {
			return memory.UpdateInstallResult{}, errors.New("the downloaded archive does not match the release checksum; nothing was changed")
		}
		prev := h.onDisk[p]
		h.onDisk[p] = v
		os.WriteFile(p, []byte("synthetic "+v), 0o755)
		return memory.UpdateInstallResult{Target: p, Previous: prev, Version: v}, nil
	}
	h.c = menuControlContext{
		Context:     context.Background(),
		Service:     memory.Service{Store: store},
		Executables: []string{link},
		Prompt: func(d menubar.Dialog) (menubar.Action, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.dialogs = append(h.dialogs, d)
			return menubar.Action{ID: d.ID, Choice: h.choice}, nil
		},
		Inform: func(title, message string) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.informed = append(h.informed, title+": "+message)
		},
		Refresh: func() {},
		OpenURL: func(u string) error { h.opened = append(h.opened, u); return nil },
	}
	return h
}

// binary creates a synthetic Elephant file reporting version.
func (h *updatesHarness) binary(rel, version string) string {
	h.t.Helper()
	p := filepath.Join(h.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("synthetic "+version), 0o755); err != nil {
		h.t.Fatal(err)
	}
	h.mu.Lock()
	h.onDisk[p] = version
	h.mu.Unlock()
	return p
}

// items waits for every background version probe, then returns the rows.
func (h *updatesHarness) items() ([]menubar.Item, bool) {
	h.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		h.u.Items(h.c)
		targets, _ := h.u.targets(h.c)
		ready := true
		h.u.mu.Lock()
		for _, target := range targets {
			if v := h.u.versions[target]; v == nil || v.probing {
				ready = false
			}
		}
		h.u.mu.Unlock()
		if ready {
			return h.u.Items(h.c)
		}
	}
	h.t.Fatal("version probe never settled")
	return nil, false
}

func titles(items []menubar.Item) string {
	var s []string
	for _, it := range items {
		s = append(s, it.ID+"="+it.Title)
	}
	return strings.Join(s, "\n")
}

func availableStatus(version string) memory.UpdateStatus {
	return memory.UpdateStatus{Enabled: true, State: "available", Checked: time.Now().UTC(), Latest: &memory.ReleaseUpdate{Version: version, Summary: "Smaller status", URL: "https://github.com/prithivrajmu/elephant/releases/tag/v" + version}}
}

func TestMenuBarUpdatesInstallNeedsConfirmationAndReportsRestart(t *testing.T) {
	h := newUpdatesHarness(t, availableStatus("1.3.0"))
	rows, attention := h.items()
	if got := titles(rows); !attention || !strings.Contains(got, "Installed: Elephant 1.2.0") || !strings.Contains(got, "updates/install=Install Elephant 1.3.0 Everywhere…") || !strings.Contains(got, "Last checked:") {
		t.Fatal(got)
	}

	h.choice = "cancel"
	if !h.u.Handle(h.c, "updates/install") || len(h.installs) != 0 || len(h.dialogs) != 1 {
		t.Fatal("cancelled confirmation installed", h.installs)
	}
	d := h.dialogs[0]
	for _, want := range []string{h.target, "Elephant 1.2.0", "Elephant 1.3.0", "SHA256SUMS", "not signed", "Nothing restarts automatically"} {
		if !strings.Contains(d.Message, want) {
			t.Fatalf("dialog lacks %q: %s", want, d.Message)
		}
	}
	if d.Default != "cancel" || d.Cancel != "cancel" {
		t.Fatal("install is not the safe default", d)
	}

	h.choice = "install"
	h.u.Handle(h.c, "updates/install")
	if len(h.installs) != 1 || h.installs[0] != h.target+"@1.3.0" {
		t.Fatal(h.installs)
	}
	if last := h.informed[len(h.informed)-1]; !strings.HasPrefix(last, "Restart Needed") || !strings.Contains(last, "Nothing was restarted") {
		t.Fatal(last)
	}
	// The cached status still names 1.3.0 for the running version; the
	// on-disk binary now has it, so the menu must not offer it again.
	rows, attention = h.items()
	if got := titles(rows); !attention || strings.Contains(got, "updates/install") || !strings.Contains(got, "Restart needed to use Elephant 1.3.0") || !strings.Contains(got, "Installed: Elephant 1.3.0") {
		t.Fatal(got)
	}
	h.u.Handle(h.c, "updates/install")
	if len(h.installs) != 1 || len(h.dialogs) != 2 || !strings.Contains(h.informed[len(h.informed)-1], "Nothing was installed") {
		t.Fatal("stale cache repeated the install", h.installs, h.informed)
	}
}

func TestMenuBarUpdatesInstallsEveryBinaryButLauncherCopies(t *testing.T) {
	h := newUpdatesHarness(t, availableStatus("1.3.0"))
	other := h.binary("opt/elephant", "1.2.0")
	current := h.binary("current/elephant", "1.3.0")
	launcher := h.binary(".elephant/bin/v1.2.0/elephant", "1.2.0")
	onPath := h.binary("bin/elephant", "1.2.0")
	h.u.lookPath = func(string) (string, error) { return onPath, nil }
	// Two connections share one binary; it must be replaced once.
	h.c.Executables = append(h.c.Executables, h.target, other, current, launcher)

	rows, _ := h.items()
	if got := titles(rows); !strings.Contains(got, "Installed: Elephant 1.2.0 · 3 binaries") || !strings.Contains(got, "Installed: Elephant 1.3.0 · 1 binary") || !strings.Contains(got, "npm launcher: 1 copy") || !strings.Contains(got, "Install Elephant 1.3.0 Everywhere…") {
		t.Fatal(got)
	}

	h.choice = "install"
	h.u.Handle(h.c, "updates/install")
	want := map[string]bool{h.target + "@1.3.0": true, other + "@1.3.0": true, onPath + "@1.3.0": true}
	if len(h.installs) != len(want) {
		t.Fatal(h.installs)
	}
	for _, got := range h.installs {
		if !want[got] {
			t.Fatal("unexpected install", got, h.installs)
		}
	}
	d := h.dialogs[0].Message
	for _, s := range []string{current, "Already current", launcher, "npm launcher", "npx --yes elephant-memory@1.3.0"} {
		if !strings.Contains(d, s) {
			t.Fatalf("dialog lacks %q: %s", s, d)
		}
	}
	if last := h.informed[len(h.informed)-1]; !strings.HasPrefix(last, "Restart Needed") || !strings.Contains(last, other) || !strings.Contains(last, onPath) {
		t.Fatal(last)
	}
}

func TestMenuBarUpdatesPartialFailureKeepsOfferForFailedBinary(t *testing.T) {
	h := newUpdatesHarness(t, availableStatus("1.3.0"))
	other := h.binary("opt/elephant", "1.2.0")
	h.c.Executables = append(h.c.Executables, other)
	h.failing[other] = true
	h.choice = "install"
	h.u.Handle(h.c, "updates/install")
	last := h.informed[len(h.informed)-1]
	if !strings.HasPrefix(last, "Restart Needed") || !strings.Contains(last, "Installed Elephant 1.3.0 at:\n"+h.target) || !strings.Contains(last, "was not installed at:\n"+other) || !strings.Contains(last, "checksum") || !strings.Contains(last, "left in place") {
		t.Fatal(last)
	}
	rows, _ := h.items()
	if got := titles(rows); !strings.Contains(got, "Restart needed to use Elephant 1.3.0") || !strings.Contains(got, "Install Elephant 1.3.0 Everywhere…") {
		t.Fatal(got)
	}
	h.failing[other] = false
	h.u.Handle(h.c, "updates/install")
	if got := h.installs[len(h.installs)-1]; got != other+"@1.3.0" || len(h.installs) != 3 {
		t.Fatal("retry did not target only the failed binary", h.installs)
	}
}

func TestMenuBarUpdatesFailedInstallKeepsOffer(t *testing.T) {
	h := newUpdatesHarness(t, availableStatus("1.3.0"))
	h.choice = "install"
	h.failing[h.target] = true
	h.u.Handle(h.c, "updates/install")
	if last := h.informed[len(h.informed)-1]; !strings.Contains(last, "was not installed") || !strings.Contains(last, "checksum") {
		t.Fatal(last)
	}
	rows, _ := h.items()
	if got := titles(rows); strings.Contains(got, "Restart needed") || !strings.Contains(got, "updates/install=Install Elephant 1.3.0 Everywhere…") {
		t.Fatal(got)
	}
}

func TestMenuBarUpdatesStatusIsOfflineAndHonest(t *testing.T) {
	h := newUpdatesHarness(t, memory.UpdateStatus{Enabled: true, State: "unchecked"})
	rows, attention := h.items()
	if got := titles(rows); attention || !strings.Contains(got, "Not checked yet") || !strings.Contains(got, "updates/check=Check for Updates") || strings.Contains(got, "updates/install") || strings.Contains(got, "updates/notes") {
		t.Fatal(got)
	}
	if h.u.Handle(h.c, "updates/unknown") {
		t.Fatal("unknown action claimed")
	}

	t.Setenv("ELEPHANT_UPDATE_CHECKS", "0")
	rows, _ = h.items()
	for _, it := range rows {
		if it.ID == "updates/check" && it.Enabled {
			t.Fatal("check enabled while disabled")
		}
	}
	if got := titles(rows); !strings.Contains(got, "Update checks are disabled") {
		t.Fatal(got)
	}
}

func TestMenuBarUpdatesCheckAndNotes(t *testing.T) {
	h := newUpdatesHarness(t, memory.UpdateStatus{Enabled: true, State: "unchecked"})
	h.u.check = func(memory.Store) (memory.UpdateStatus, error) { return availableStatus("1.3.0"), nil }
	if !h.u.Handle(h.c, "updates/check") {
		t.Fatal("check not handled")
	}
	if last := h.informed[len(h.informed)-1]; !strings.Contains(last, "Elephant 1.3.0 is available") || !strings.Contains(last, "update 1 binary") {
		t.Fatal(last)
	}
	h = newUpdatesHarness(t, availableStatus("1.3.0"))
	h.u.Handle(h.c, "updates/notes")
	if len(h.opened) != 1 || h.opened[0] != "https://github.com/prithivrajmu/elephant/releases/tag/v1.3.0" {
		t.Fatal(h.opened)
	}
}
