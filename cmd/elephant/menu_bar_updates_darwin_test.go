//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	fetches  [][]string
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
	h.u.releases = func(baselines []string) (map[string]*memory.ReleaseUpdate, error) {
		t.Error("unexpected network check", baselines)
		return nil, errors.New("no network in tests")
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

// items waits for every background version probe and release fetch, then
// returns the rows.
func (h *updatesHarness) items() ([]menubar.Item, bool) {
	h.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		h.u.Items(h.c)
		targets, _ := h.u.targets(h.c)
		h.u.mu.Lock()
		ready := !h.u.checking
		for _, target := range targets {
			if v := h.u.versions[target]; v == nil || v.probing {
				ready = false
			}
		}
		product := h.u.product
		h.u.mu.Unlock()
		if !ready {
			continue
		}
		// The final read may itself start a release fetch; settle that too.
		rows, attention := h.u.Items(h.c)
		h.u.mu.Lock()
		settled := !h.u.checking && h.u.product == product
		h.u.mu.Unlock()
		if settled {
			return rows, attention
		}
	}
	h.t.Fatal("version probe or release fetch never settled")
	return nil, false
}

// stubReleases answers release fetches from want and records each request.
func (h *updatesHarness) stubReleases(want map[string]string) {
	h.u.releases = func(baselines []string) (map[string]*memory.ReleaseUpdate, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.fetches = append(h.fetches, append([]string(nil), baselines...))
		out := map[string]*memory.ReleaseUpdate{}
		for _, b := range baselines {
			if v := want[b]; v != "" {
				out[b] = &memory.ReleaseUpdate{Version: v, Summary: "Release " + v, URL: "https://github.com/prithivrajmu/elephant/releases/tag/v" + v}
			}
		}
		return out, nil
	}
}

func (h *updatesHarness) fetchCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.fetches)
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
	h.stubReleases(map[string]string{"1.2.0": "1.3.0"})
	before, _ := os.ReadFile(h.c.Service.Store.Path + ".updates.json")
	if !h.u.Handle(h.c, "updates/check") {
		t.Fatal("check not handled")
	}
	if last := h.informed[len(h.informed)-1]; !strings.Contains(last, "Elephant 1.3.0 is available") || !strings.Contains(last, "update 1 binary") {
		t.Fatal(last)
	}
	if h.fetchCount() != 1 || strings.Join(h.fetches[0], ",") != "1.2.0" {
		t.Fatal("check did not fetch once for the on-disk version", h.fetches)
	}
	if after, _ := os.ReadFile(h.c.Service.Store.Path + ".updates.json"); string(after) != string(before) {
		t.Fatal("product check wrote the store cache", string(after))
	}
	rows, _ := h.items()
	if got := titles(rows); !strings.Contains(got, "updates/install=Install Elephant 1.3.0 Everywhere…") || !strings.Contains(got, "updates/notes=Release Notes for Elephant 1.3.0") {
		t.Fatal(got)
	}
	h = newUpdatesHarness(t, availableStatus("1.3.0"))
	h.u.Handle(h.c, "updates/notes")
	if len(h.opened) != 1 || h.opened[0] != "https://github.com/prithivrajmu/elephant/releases/tag/v1.3.0" {
		t.Fatal(h.opened)
	}
}

// olderThanVersion returns a release in memory.Version's major and channel
// that memory.Version updates.
func olderThanVersion(t *testing.T) string {
	t.Helper()
	core, channel, _ := strings.Cut(memory.Version, "-")
	var major, minor, patch int
	if _, err := fmt.Sscanf(core, "%d.%d.%d", &major, &minor, &patch); err != nil {
		t.Fatalf("cannot derive an older release from Version %q", memory.Version)
	}
	switch {
	case patch > 0:
		patch--
	case minor > 0:
		minor--
	default:
		t.Fatalf("cannot derive an older release from Version %q", memory.Version)
	}
	older := fmt.Sprintf("%d.%d.%d", major, minor, patch)
	if channel != "" {
		older += "-" + channel
	}
	if !memory.UpdateApplies(older, memory.Version) {
		t.Fatalf("derived %q does not update to Version %q", older, memory.Version)
	}
	return older
}

func TestMenuBarUpdatesCurrentCompanionStillOffersOlderBinaries(t *testing.T) {
	older := olderThanVersion(t)
	h := newUpdatesHarness(t, memory.UpdateStatus{Enabled: true, State: "current", Checked: time.Now().UTC()})
	h.binary("elephant", older)
	h.stubReleases(map[string]string{older: memory.Version})
	rows, attention := h.items()
	got := titles(rows)
	if !attention || strings.Contains(got, "Up to date") || !strings.Contains(got, "updates/install=Install Elephant "+memory.Version+" Everywhere…") || !strings.Contains(got, "Available: Elephant "+memory.Version) {
		t.Fatal(got)
	}
	if h.fetchCount() != 1 || strings.Join(h.fetches[0], ",") != older {
		t.Fatal("expected one background fetch for the older binary", h.fetches)
	}
	// The companion runs the replaced binary, so the result names Hide Menu Bar.
	h.u.self = func() (string, error) { return h.target, nil }
	h.choice = "install"
	h.u.Handle(h.c, "updates/install")
	if len(h.dialogs) != 1 || len(h.installs) != 1 || h.installs[0] != h.target+"@"+memory.Version {
		t.Fatal(h.installs, len(h.dialogs))
	}
	if last := h.informed[len(h.informed)-1]; !strings.HasPrefix(last, "Restart Needed") || !strings.Contains(last, "Hide Menu Bar") {
		t.Fatal(last)
	}
}

func TestMenuBarUpdatesAnyStoreOptOutDisablesChecks(t *testing.T) {
	h := newUpdatesHarness(t, availableStatus("1.3.0"))
	missing := memory.Store{Path: filepath.Join(h.dir, "missing.sqlite")}
	h.c.Stores = []memory.Store{h.c.Service.Store, missing}
	rows, _ := h.items()
	if got := titles(rows); !strings.Contains(got, "updates/install=Install Elephant 1.3.0 Everywhere…") {
		t.Fatal("a store without updates.json opted out", got)
	}

	optedOut := memory.Store{Path: filepath.Join(h.dir, "opted-out.sqlite")}
	b, _ := json.Marshal(memory.UpdateStatus{Enabled: false, State: "disabled", Installed: memory.Version})
	if err := os.WriteFile(optedOut.Path+".updates.json", b, 0o600); err != nil {
		t.Fatal(err)
	}
	h.c.Stores = []memory.Store{h.c.Service.Store, optedOut, missing}
	rows, attention := h.items()
	got := titles(rows)
	if attention || !strings.Contains(got, "Update checks are disabled") || strings.Contains(got, "updates/install") {
		t.Fatal(got)
	}
	for _, it := range rows {
		if it.ID == "updates/check" && it.Enabled {
			t.Fatal("check enabled while one store opted out")
		}
	}
	h.u.Handle(h.c, "updates/check")
	h.choice = "install"
	h.u.Handle(h.c, "updates/install")
	if h.fetchCount() != 0 || len(h.installs) != 0 || len(h.dialogs) != 0 {
		t.Fatal("opt-out ignored", h.fetches, h.installs)
	}
	if after, _ := os.ReadFile(optedOut.Path + ".updates.json"); string(after) != string(b) {
		t.Fatal("menu rewrote a store's update preference", string(after))
	}

	h.u.cached = func(s memory.Store) (memory.UpdateStatus, error) {
		if s.Path == missing.Path {
			return memory.UpdateStatus{}, errors.New("unreadable")
		}
		return s.CachedUpdate()
	}
	h.c.Stores = []memory.Store{h.c.Service.Store, missing}
	rows, _ = h.items()
	if got := titles(rows); !strings.Contains(got, "Update status unavailable") || strings.Contains(got, "updates/install") {
		t.Fatal(got)
	}
}

func TestMenuBarUpdatesUnionsReportedPaths(t *testing.T) {
	h := newUpdatesHarness(t, availableStatus("1.3.0"))
	second := h.binary("session-path/elephant", "1.2.0")
	relative := h.binary("relative/elephant", "1.2.0")
	t.Chdir(h.dir)
	h.c.ReportedExecutables = []string{second, second, "relative/elephant", filepath.Join(h.dir, "elephant-link")}
	targets, _ := h.u.targets(h.c)
	if strings.Join(targets, ",") != strings.Join([]string{h.target, second}, ",") {
		t.Fatal("reported paths not unioned, deduped, or a relative path was used", targets, relative)
	}
	rows, _ := h.items()
	if got := titles(rows); !strings.Contains(got, "Installed: Elephant 1.2.0 · 2 binaries") || !strings.Contains(got, "Install Elephant 1.3.0 Everywhere…") {
		t.Fatal(got)
	}
}

func TestMenuBarUpdatesSessionOptOutDisablesChecks(t *testing.T) {
	h := newUpdatesHarness(t, availableStatus("1.3.0"))
	h.c.UpdateChecksDisabled = true
	rows, _ := h.items()
	if got := titles(rows); !strings.Contains(got, "Update checks are disabled") || strings.Contains(got, "updates/install") {
		t.Fatal(got)
	}
	h.u.Handle(h.c, "updates/check")
	if h.fetchCount() != 0 || !strings.Contains(h.informed[len(h.informed)-1], "disabled") {
		t.Fatal("session opt-out ignored", h.informed)
	}
}

func TestMenuBarUpdatesOneFetchAtATime(t *testing.T) {
	h := newUpdatesHarness(t, memory.UpdateStatus{Enabled: true, State: "unchecked"})
	release := make(chan struct{})
	var calls sync.WaitGroup
	calls.Add(1)
	h.u.releases = func([]string) (map[string]*memory.ReleaseUpdate, error) {
		h.mu.Lock()
		h.fetches = append(h.fetches, nil)
		h.mu.Unlock()
		<-release
		return nil, nil
	}
	go func() {
		defer calls.Done()
		h.u.Handle(h.c, "updates/check")
	}()
	for deadline := time.Now().Add(5 * time.Second); h.fetchCount() == 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("first check never started")
		}
	}
	rows, _ := h.u.Items(h.c)
	if got := titles(rows); !strings.Contains(got, "updates/check=Checking for Updates…") {
		t.Fatal(got)
	}
	h.u.Handle(h.c, "updates/check")
	close(release)
	calls.Wait()
	if h.fetchCount() != 1 {
		t.Fatal("second check started another fetch", h.fetchCount())
	}
}

func TestMenuBarUpdatesRunningOlderEngineNeedsRestart(t *testing.T) {
	h := newUpdatesHarness(t, memory.UpdateStatus{Enabled: true, State: "unchecked"})
	h.c.Running = []menuRunningBinary{{Executable: filepath.Join(h.dir, "elephant-link"), Version: "1.1.0"}}
	rows, attention := h.items()
	if got := titles(rows); !attention || !strings.Contains(got, "Restart needed to use Elephant 1.2.0") || strings.Contains(got, "Hide Menu Bar") {
		t.Fatal(got)
	}
	h.c.Running = []menuRunningBinary{{Executable: h.target, Version: "1.2.0"}}
	if rows, _ = h.items(); strings.Contains(titles(rows), "Restart needed") {
		t.Fatal("matching engine reported as needing restart", titles(rows))
	}
	// The companion is one of the running binaries.
	h.u.self = func() (string, error) { return h.target, nil }
	h.c.Running = []menuRunningBinary{{Executable: h.target, Version: "1.1.0"}}
	if rows, _ = h.items(); !strings.Contains(titles(rows), "Restart needed to use Elephant 1.2.0 — includes this menu bar: choose Hide Menu Bar, then start a new session") {
		t.Fatal(titles(rows))
	}
}

func TestMenuBarUpdatesInstallsEachBinarysOwnRelease(t *testing.T) {
	h := newUpdatesHarness(t, memory.UpdateStatus{Enabled: true, State: "unchecked"})
	other := h.binary("opt/elephant", "2.0.0")
	h.c.Executables = append(h.c.Executables, other)
	h.stubReleases(map[string]string{"1.2.0": "1.3.0", "2.0.0": "2.1.0"})
	h.u.Handle(h.c, "updates/check")
	if last := h.informed[len(h.informed)-1]; !strings.Contains(last, "Elephant updates are available") || !strings.Contains(last, "update 2 binaries") {
		t.Fatal(last)
	}
	rows, attention := h.items()
	got := titles(rows)
	if !attention || !strings.Contains(got, "updates/install=Install Elephant updates…") || !strings.Contains(got, "Available: Elephant 1.3.0") || !strings.Contains(got, "Available: Elephant 2.1.0") {
		t.Fatal(got)
	}
	h.choice = "install"
	h.u.Handle(h.c, "updates/install")
	if len(h.dialogs) != 1 {
		t.Fatal("expected one confirmation", len(h.dialogs))
	}
	d := h.dialogs[0]
	if d.Default != "cancel" || d.Cancel != "cancel" || !strings.Contains(d.Message, h.target+" (Elephant 1.2.0 → 1.3.0)") || !strings.Contains(d.Message, other+" (Elephant 2.0.0 → 2.1.0)") {
		t.Fatal(d)
	}
	want := map[string]bool{h.target + "@1.3.0": true, other + "@2.1.0": true}
	if len(h.installs) != 2 || !want[h.installs[0]] || !want[h.installs[1]] {
		t.Fatal(h.installs)
	}
	if last := h.informed[len(h.informed)-1]; !strings.HasPrefix(last, "Restart Needed") || strings.Contains(last, "Hide Menu Bar") {
		t.Fatal(last)
	}
}

func TestMenuBarProductTitle(t *testing.T) {
	for _, tc := range []struct {
		versions []string
		n        int
		want     string
	}{
		{[]string{"1.3.0", "1.3.0"}, 1, "Elephant 1.3.0 · 1 connections"},
		{[]string{"1.3.0", "1.2.0", "1.3.0"}, 2, "Elephant 1.2.0, 1.3.0 · 2 connections"},
		{[]string{"", "1.3.0", ""}, 3, "Elephant 1.3.0 · 3 connections"},
	} {
		if got := menuBarProductTitle(tc.versions, tc.n); got != tc.want {
			t.Errorf("menuBarProductTitle(%q, %d) = %q, want %q", tc.versions, tc.n, got, tc.want)
		}
	}
}
