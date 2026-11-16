//go:build darwin

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

// fakeMenuBar stands in for the native item; no UI is launched in tests.
type fakeMenuBar struct {
	starts atomic.Int32
	ready  chan func(menubar.Action)
	models chan menubar.Model
	hide   chan struct{}
}

func (f *fakeMenuBar) run(ctx context.Context, states <-chan menubar.Model, onReady func(), onAction func(menubar.Action)) error {
	f.starts.Add(1)
	onReady()
	f.ready <- onAction
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.hide:
			return nil
		case model, ok := <-states:
			if !ok {
				states = nil
				continue
			}
			select {
			case f.models <- model:
			default:
				<-f.models
				f.models <- model
			}
		}
	}
}

// menuBarHarness runs hosts in-process where production starts a detached
// _menu-bar companion, sharing one private directory and socket.
type menuBarHarness struct {
	t      *testing.T
	dir    string
	ui     *fakeMenuBar
	opened chan string
	spawns atomic.Int32
	exits  chan error
}

func newMenuBarHarness(t *testing.T) *menuBarHarness {
	// macOS limits socket paths to 104 bytes; t.TempDir paths can be longer.
	dir, err := os.MkdirTemp("/tmp", "elephant-menu-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return &menuBarHarness{t: t, dir: dir, ui: &fakeMenuBar{ready: make(chan func(menubar.Action), 8), models: make(chan menubar.Model, 64), hide: make(chan struct{})}, opened: make(chan string, 8), exits: make(chan error, 64)}
}

func (h *menuBarHarness) spawn() (<-chan struct{}, error) {
	h.spawns.Add(1)
	host := &menuBarHost{dir: h.dir, runUI: h.ui.run, openURL: func(u string) error { h.opened <- u; return nil }, logf: func(string, ...any) {}, startupGrace: time.Second}
	host.controls = newMenuBarControls(context.Background(), host)
	// Metadata in this in-process harness names the Go test executable. Do not
	// execute that binary as a CLI version probe (it would start another suite).
	host.controls.updates.probe = func(context.Context, string) (string, error) { return memory.Version, nil }
	done := make(chan struct{})
	go func() {
		h.exits <- host.run(context.Background())
		close(done)
	}()
	return done, nil
}

func (h *menuBarHarness) client(warn func(string, ...any)) menuBarClient {
	return menuBarClient{dir: h.dir, timeout: 5 * time.Second, warn: warn, spawn: h.spawn}
}

func (h *menuBarHarness) service(root string) memory.Service {
	return memory.Service{Store: memory.Store{Path: filepath.Join(h.dir, "memories.sqlite")}, Identity: memory.Identity{Tenant: "local", User: "me"}, Root: root, Project: root}
}

func (h *menuBarHarness) attach(root string) net.Conn {
	h.t.Helper()
	conn, _, err := h.client(h.t.Errorf).attach(context.Background(), newMenuBarSession(h.service(root)))
	if err != nil {
		h.t.Fatal(err)
	}
	return conn
}

func (h *menuBarHarness) waitReady() func(menubar.Action) {
	h.t.Helper()
	select {
	case open := <-h.ui.ready:
		return open
	case <-time.After(5 * time.Second):
		h.t.Fatal("menu bar item did not start")
		return nil
	}
}

func (h *menuBarHarness) waitOpened() string {
	h.t.Helper()
	select {
	case u := <-h.opened:
		return u
	case <-time.After(5 * time.Second):
		h.t.Fatal("Open Memory Palace did not open a URL")
		return ""
	}
}

// waitHosts waits until every started host has exited. Hosts that lost the
// singleton lock exit cleanly; any other failure fails the test.
func (h *menuBarHarness) waitHosts() {
	h.t.Helper()
	for range h.spawns.Load() {
		select {
		case err := <-h.exits:
			if err != nil && !errors.Is(err, errMenuBarHostRunning) {
				h.t.Errorf("menu bar host failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			h.t.Fatal("menu bar host did not exit")
		}
	}
}

func palaceStatus(palace string, authorized bool) (int, error) {
	u, err := url.Parse(palace)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+u.Host+"/api/menu-bar-test", nil)
	if err != nil {
		return 0, err
	}
	if authorized {
		req.Header.Set("Authorization", "Bearer "+strings.TrimPrefix(u.Fragment, "token="))
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func waitPalaceClosed(t *testing.T, palace string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := palaceStatus(palace, true); err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("Memory Palace stayed open after its session ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMenuBarConcurrentSessionsShareOneHostUntilLastDisconnect(t *testing.T) {
	h := newMenuBarHarness(t)
	const clients = 8
	conns := make([]net.Conn, clients)
	errs := make([]error, clients)
	var wg sync.WaitGroup
	for i := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conns[i], _, errs[i] = h.client(t.Errorf).attach(context.Background(), newMenuBarSession(h.service("/tmp/project")))
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	h.waitReady()

	// Abrupt client exits leave the shared host running for the remaining session.
	for _, conn := range conns[1:] {
		conn.Close()
	}
	spawned := h.spawns.Load()
	late := h.attach("/tmp/other")
	if h.spawns.Load() != spawned {
		t.Fatal("a new session started another host while one was running")
	}
	conns[0].Close()
	late.Close()
	h.waitHosts()
	if n := h.ui.starts.Load(); n != 1 {
		t.Fatalf("menu bar items started = %d, want 1", n)
	}
	if conn, err := net.Dial("unix", filepath.Join(h.dir, menuBarSocketName)); err == nil {
		conn.Close()
		t.Fatal("host still accepts sessions after the last session ended")
	}
}

func TestMenuBarHideStopsOnlyTheCompanion(t *testing.T) {
	h := newMenuBarHarness(t)
	var warnings atomic.Int32
	c := h.client(func(format string, args ...any) { warnings.Add(1); t.Logf(format, args...) })
	errServe := errors.New("serve finished")
	release := make(chan struct{})
	served := make(chan error, 1)
	go func() {
		served <- c.serve(h.service("/tmp/project"), "mcp", func(memory.Service) error { <-release; return errServe })
	}()
	h.waitReady()
	close(h.ui.hide)
	h.waitHosts()
	select {
	case err := <-served:
		t.Fatalf("serve stopped with the menu bar: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-served; !errors.Is(err, errServe) {
		t.Fatalf("serve result = %v, want %v", err, errServe)
	}
	if n := h.spawns.Load(); n != 1 {
		t.Fatalf("hidden menu bar was started again: %d hosts", n)
	}
	if n := warnings.Load(); n != 0 {
		t.Fatalf("Hide Menu Bar produced %d warnings", n)
	}
}

func TestMenuBarOpenServesTokenProtectedPalaceForSelectedSession(t *testing.T) {
	h := newMenuBarHarness(t)
	first := h.attach("/tmp/first")
	act := h.waitReady()
	second := h.attach("/tmp/second")
	selected := h.waitAction("second", "Open Memory Palace")
	act(menubar.Action{ID: selected})
	palace := h.waitOpened()
	act(menubar.Action{ID: selected})
	if again := h.waitOpened(); again != palace {
		t.Fatal("Open started a second Memory Palace for the same session")
	}
	if code, err := palaceStatus(palace, false); err != nil || code != http.StatusUnauthorized {
		t.Fatalf("request without token = %d, %v; want 401", code, err)
	}
	if code, err := palaceStatus(palace, true); err != nil || code != http.StatusNotFound {
		t.Fatalf("request with token = %d, %v; want authenticated 404", code, err)
	}
	// The newer connection was explicitly selected, not the oldest default.
	second.Close()
	waitPalaceClosed(t, palace)
	act(menubar.Action{ID: h.waitAction("first", "Open Memory Palace")})
	next := h.waitOpened()
	if next == palace {
		t.Fatal("Open reused the disconnected session's Palace")
	}
	if code, err := palaceStatus(next, true); err != nil || code != http.StatusNotFound {
		t.Fatalf("remaining session Palace = %d, %v; want authenticated 404", code, err)
	}
	first.Close()
	h.waitHosts()
	if _, err := palaceStatus(next, true); err == nil {
		t.Fatal("Memory Palace stayed open after the host exited")
	}
}

func (h *menuBarHarness) waitAction(project, title string) string {
	h.t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case model := <-h.ui.models:
			for _, section := range model.Items {
				if section.Title != "Sessions" {
					continue
				}
				for _, group := range section.Items {
					if group.Title == project {
						if id := findNativeAction(group.Items, title); id != "" {
							return id
						}
					}
				}
			}
		case <-timer.C:
			h.t.Fatalf("menu action %q for project %q did not appear", title, project)
			return ""
		}
	}
}

func findNativeAction(items []menubar.Item, title string) string {
	for _, item := range items {
		if item.Title == title && item.ID != "" {
			return item.ID
		}
		if id := findNativeAction(item.Items, title); id != "" {
			return id
		}
	}
	return ""
}

func TestMenuBarIdleSessions(t *testing.T) {
	now := time.Now()
	old := now.Add(-menuBarIdleAfter - time.Minute)
	cases := []struct {
		name string
		view menuBarView
		want bool
	}{
		{"never called, connected long ago", menuBarView{Connected: old}, true},
		{"old connect, recent tool call", menuBarView{Connected: old, Observed: menuBarObservation{At: now}}, false},
		{"recent connect", menuBarView{Connected: now}, false},
		{"palace is never idle", menuBarView{Session: menuBarSession{Kind: "palace"}, Connected: old}, false},
	}
	for _, c := range cases {
		if got := c.view.idle(now); got != c.want {
			t.Errorf("%s: idle = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSessionRunsElsewhere(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	sub := filepath.Join(root, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// The client resolves its own directory (newMenuBarSession); the root stays
	// unresolved here so the host's resolution is exercised too.
	resolve := func(path string) string {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}
	cases := []struct {
		name    string
		session menuBarSession
		want    bool
	}{
		{"cwd is the root", menuBarSession{Kind: "mcp", Root: root, Cwd: resolve(root)}, false},
		{"cwd inside the root", menuBarSession{Kind: "mcp", Root: root, Cwd: resolve(sub)}, false},
		{"cwd in another repo", menuBarSession{Kind: "mcp", Root: root, Cwd: resolve(other)}, true},
		{"older client sent no cwd", menuBarSession{Kind: "mcp", Root: root}, false},
		{"unattached has no project", menuBarSession{Kind: "mcp", Unattached: true, Cwd: resolve(other)}, false},
		{"palace names its root", menuBarSession{Kind: "palace", Root: root, Cwd: resolve(other)}, false},
	}
	for _, c := range cases {
		if got := sessionRunsElsewhere(c.session); got != c.want {
			t.Errorf("%s: sessionRunsElsewhere = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestProductScopeIgnoresIdleProcessesForRestartChecks(t *testing.T) {
	now := time.Now()
	old := now.Add(-menuBarIdleAfter - time.Minute)
	host := &menuBarHost{sessions: map[*menuBarPeer]struct{}{}}
	c := newMenuBarControls(context.Background(), host)
	views := []menuBarView{
		{Session: menuBarSession{Kind: "mcp", Executable: "/bin/live", EngineVersion: "2"}, Connected: now},
		{Session: menuBarSession{Kind: "mcp", Executable: "/bin/stale", EngineVersion: "1"}, Connected: old},
	}
	scope := c.productScope(views)
	if len(scope.Executables) != 2 {
		t.Errorf("Executables = %v, want both processes kept as update targets", scope.Executables)
	}
	for _, r := range scope.Running {
		if r.Executable == "/bin/stale" {
			t.Errorf("idle process listed as running: %+v", r)
		}
	}
}
