//go:build darwin

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

// menuBarCommand is the internal command that runs the shared per-user host.
const menuBarCommand = "_menu-bar"

const (
	menuBarProtocol         = 2
	menuBarSocketName       = "host.sock"
	menuBarLockName         = "host.lock"
	menuBarLogName          = "host.log"
	menuBarMaxMessage       = 16 << 10
	menuBarMaxError         = 2 << 10
	menuBarLogLimit         = 1 << 20
	menuBarMaxSocketPath    = 103 // sun_path holds 104 bytes including the NUL terminator.
	menuBarHandshakeTimeout = 2 * time.Second
	menuBarAttachTimeout    = 10 * time.Second
	menuBarStartupGrace     = 20 * time.Second
	menuBarMaxSpawns        = 20
	menuBarOpenTimeout      = 15 * time.Second
)

var errMenuBarHostRunning = errors.New("menu bar host already running")

// menuBarSession carries only what the host needs to rebuild the Service for
// the Memory Palace. It never carries memories or task context.
type menuBarSession struct {
	Version       int             `json:"v"`
	Store         string          `json:"store"`
	Identity      memory.Identity `json:"identity"`
	Root          string          `json:"root"`
	Project       string          `json:"project,omitempty"`
	Conversation  string          `json:"conversation,omitempty"`
	Unattached    bool            `json:"unattached,omitempty"`
	PID           int             `json:"pid"`
	Executable    string          `json:"executable"`
	EngineVersion string          `json:"engine_version"`
	Kind          string          `json:"kind"`
	// Cwd is where the agent started this process. A client config may pin
	// Root to another project, so the menu compares the two.
	Cwd string `json:"cwd,omitempty"`
	// ElephantPath is the elephant this process finds on its own PATH; the
	// companion's PATH may differ. Empty when the lookup failed.
	ElephantPath string `json:"elephant_path,omitempty"`
	// UpdateChecksDisabled reports ELEPHANT_UPDATE_CHECKS=0 in this process.
	UpdateChecksDisabled bool `json:"update_checks_disabled,omitempty"`
}

func newMenuBarSession(svc memory.Service) menuBarSession {
	executable, _ := os.Executable()
	elephantPath, err := exec.LookPath("elephant")
	if err == nil {
		elephantPath, err = filepath.Abs(elephantPath)
	}
	if err != nil {
		elephantPath = ""
	}
	cwd, _ := os.Getwd()
	return menuBarSession{Cwd: cwd, Version: menuBarProtocol, Store: svc.Store.Path, Identity: svc.Identity, Root: svc.Root, Project: svc.Project, Conversation: svc.Conversation, Unattached: svc.Unattached, PID: os.Getpid(), Executable: executable, EngineVersion: memory.Version, Kind: "mcp", ElephantPath: elephantPath, UpdateChecksDisabled: os.Getenv("ELEPHANT_UPDATE_CHECKS") == "0"}
}

func (s menuBarSession) validate() error {
	switch {
	case s.Version != menuBarProtocol:
		return fmt.Errorf("unsupported menu bar protocol %d; the running menu bar belongs to another Elephant version, use Hide Menu Bar and start a new session", s.Version)
	case s.PID <= 0 || !filepath.IsAbs(s.Executable):
		return errors.New("process ID and absolute executable path required")
	case s.Kind != "mcp" && s.Kind != "palace":
		return errors.New("unknown connection kind")
	case !filepath.IsAbs(s.Store):
		return errors.New("store path must be absolute")
	case s.Identity.Tenant == "" || s.Identity.User == "":
		return errors.New("tenant and user required")
	case !filepath.IsAbs(s.Root):
		return errors.New("root must be absolute")
	case s.Unattached && s.Project != "":
		return errors.New("unattached sessions cannot name a project")
	}
	return nil
}

func (s menuBarSession) service() memory.Service {
	return memory.Service{Store: memory.Store{Path: s.Store}, Identity: s.Identity, Root: s.Root, Project: s.Project, Conversation: s.Conversation, Unattached: s.Unattached}
}

// menuBarReply is one host-to-client line: the registration ack, a refusal,
// or the failure that stopped the host.
type menuBarReply struct {
	OK    bool   `json:"ok,omitempty"`
	Error string `json:"error,omitempty"`
}

func encodeMenuBarReply(r menuBarReply) []byte {
	if len(r.Error) > menuBarMaxError {
		r.Error = r.Error[:menuBarMaxError]
	}
	data, _ := json.Marshal(r)
	return append(data, '\n')
}

func readMenuBarLine(r *bufio.Reader, v any) error {
	line, err := r.ReadSlice('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(line, v)
}

type menuBarRejected string

func (e menuBarRejected) Error() string {
	if e == "" {
		return "menu bar host rejected the session"
	}
	return "menu bar host rejected the session: " + string(e)
}

// serveWithMenuBar runs serve while this process holds a session with the
// shared per-user menu-bar host. Menu-bar problems only warn on stderr; serve
// keeps stdout and its result is returned unchanged.
func serveWithMenuBar(svc memory.Service, kind string, serve func(memory.Service) error) error {
	if os.Getenv("ELEPHANT_MENU_BAR") == "0" {
		return serve(svc)
	}
	dir, err := menuBarDir()
	if err != nil {
		warnMenuBar("menu bar unavailable: %v", err)
		return serve(svc)
	}
	c := menuBarClient{dir: dir, timeout: menuBarAttachTimeout, warn: warnMenuBar, spawn: func() (<-chan struct{}, error) { return spawnMenuBarHost(dir) }}
	return c.serve(svc, kind, serve)
}

func warnMenuBar(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "elephant: "+format+"\n", args...)
}

type menuBarClient struct {
	dir     string
	timeout time.Duration
	warn    func(format string, args ...any)
	// spawn starts a host and returns a channel closed when that host exits.
	spawn func() (<-chan struct{}, error)
}

// attach registers with the running host or starts one. Startup is bounded by
// c.timeout; a host that lost the singleton lock exits, so another start is
// attempted only after the previous one has exited.
func (c menuBarClient) attach(ctx context.Context, session menuBarSession) (net.Conn, *bufio.Reader, error) {
	payload, err := json.Marshal(session)
	if err != nil {
		return nil, nil, err
	}
	if len(payload) >= menuBarMaxMessage {
		return nil, nil, errors.New("session registration is too large")
	}
	payload = append(payload, '\n')
	sock, err := menuBarSocketPath(c.dir)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var host <-chan struct{}
	spawns := 0
	delay := 10 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		conn, r, err := dialMenuBar(ctx, sock, payload)
		if err == nil {
			return conn, r, nil
		}
		var rejected menuBarRejected
		if errors.As(err, &rejected) {
			return nil, nil, err
		}
		if spawns < menuBarMaxSpawns && (host == nil || isClosed(host)) {
			started, spawnErr := c.spawn()
			if spawnErr != nil {
				return nil, nil, fmt.Errorf("start menu bar host: %w", spawnErr)
			}
			host = started
			spawns++
		}
		wait := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			wait.Stop()
			return nil, nil, fmt.Errorf("menu bar host did not accept the session within %s: %w", c.timeout, err)
		case <-wait.C:
		}
		delay = min(delay*2, 250*time.Millisecond)
	}
}

func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

func dialMenuBar(ctx context.Context, sock string, payload []byte) (net.Conn, *bufio.Reader, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	deadline := time.Now().Add(menuBarHandshakeTimeout)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	r := bufio.NewReaderSize(conn, menuBarMaxMessage)
	var reply menuBarReply
	err = conn.SetDeadline(deadline)
	if err == nil {
		_, err = conn.Write(payload)
	}
	if err == nil {
		err = readMenuBarLine(r, &reply)
	}
	if err == nil && !reply.OK {
		err = menuBarRejected(reply.Error)
	}
	if err == nil {
		err = conn.SetDeadline(time.Time{})
	}
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, r, nil
}

// spawnMenuBarHost starts the same executable as a detached session leader.
// Its stdin and stdout are /dev/null and its stderr is the private log file,
// so it holds no MCP stream or pipe that could keep a client waiting.
func spawnMenuBarHost(dir string) (<-chan struct{}, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logFile, err := openMenuBarLog(filepath.Join(dir, menuBarLogName))
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	cmd := exec.Command(exe, menuBarCommand)
	cmd.Dir = "/"
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return done, nil
}

func openMenuBarLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	if info, err := f.Stat(); err == nil && info.Size() > menuBarLogLimit {
		_ = f.Truncate(0)
	}
	return f, nil
}

func menuBarDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".elephant", "menu-bar")
	return dir, ensurePrivateDir(dir)
}

// ensurePrivateDir makes dir a real directory owned by the current user with
// no group or other access. Same-user access is the socket trust boundary.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case !info.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	case !ok || int(st.Uid) != os.Getuid():
		return fmt.Errorf("%s is not owned by the current user", dir)
	case info.Mode().Perm()&0o077 != 0:
		return os.Chmod(dir, 0o700)
	}
	return nil
}

func menuBarSocketPath(dir string) (string, error) {
	path := filepath.Join(dir, menuBarSocketName)
	if len(path) > menuBarMaxSocketPath {
		return "", fmt.Errorf("socket path %s exceeds the macOS limit of %d bytes", path, menuBarMaxSocketPath)
	}
	return path, nil
}

func lockMenuBar(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errMenuBarHostRunning
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return f, nil
}

// runMenuBarHost is the internal _menu-bar command. A second host exits
// cleanly; the first one serves until the last session disconnects, Hide Menu
// Bar is chosen, or the native item fails.
func runMenuBarHost() error {
	dir, err := menuBarDir()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := log.New(os.Stderr, "elephant menu bar: ", log.LstdFlags)
	h := &menuBarHost{dir: dir, runUI: menubar.Run, openURL: openMenuBarURL, logf: logger.Printf, startupGrace: menuBarStartupGrace}
	err = h.run(ctx)
	if errors.Is(err, errMenuBarHostRunning) {
		return nil
	}
	return err
}

func openMenuBarURL(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), menuBarOpenTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/open", url).CombinedOutput()
	if err != nil {
		return fmt.Errorf("/usr/bin/open: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

type menuBarHost struct {
	dir          string
	runUI        func(context.Context, <-chan menubar.Model, func(), func(menubar.Action)) error
	openURL      func(url string) error
	logf         func(format string, args ...any)
	startupGrace time.Duration

	mu       sync.Mutex
	wg       sync.WaitGroup
	ln       *net.UnixListener
	closing  bool
	seq      uint64
	sessions map[*menuBarPeer]struct{}
	active   chan struct{}
	empty    chan struct{}
	fatal    chan error
	ctx      context.Context
	controls *menuBarControls
}

type menuBarPeer struct {
	seq       uint64
	id        string
	conn      net.Conn
	svc       memory.Service
	session   menuBarSession
	observed  menuBarObservation
	connected time.Time
	nickname  string
	ctx       context.Context
	cancel    context.CancelFunc
	dashboard *menuBarDashboard
}

type menuBarDashboard struct {
	url    string
	server *http.Server
}

func (p *menuBarPeer) closeDashboard() {
	if p.dashboard != nil {
		_ = p.dashboard.server.Close()
		p.dashboard = nil
	}
}

func (h *menuBarHost) run(ctx context.Context) error {
	if err := ensurePrivateDir(h.dir); err != nil {
		return err
	}
	sock, err := menuBarSocketPath(h.dir)
	if err != nil {
		return err
	}
	lock, err := lockMenuBar(filepath.Join(h.dir, menuBarLockName))
	if err != nil {
		return err
	}
	// The lock is released last, after the socket, sessions, dashboards and
	// native item are gone, so a new host never overlaps this one.
	defer lock.Close()
	if err := os.Remove(sock); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if err != nil {
		return err
	}
	if err := os.Chmod(sock, 0o600); err != nil {
		ln.Close()
		return err
	}
	h.ln = ln
	h.sessions = map[*menuBarPeer]struct{}{}
	h.active = make(chan struct{}, 1)
	h.empty = make(chan struct{}, 1)
	h.fatal = make(chan error, 1)
	uiCtx, cancelUI := context.WithCancel(ctx)
	defer cancelUI()
	h.ctx = uiCtx
	if h.controls == nil {
		h.controls = newMenuBarControls(uiCtx, h)
	} else {
		h.controls.ctx = uiCtx
	}
	h.wg.Add(1)
	go h.accept()
	refresh := time.NewTicker(15 * time.Second)
	defer refresh.Stop()
	var uiDone chan error
	uiStarted := false
	grace := time.NewTimer(h.startupGrace)
	defer grace.Stop()
	var result error
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case result = <-h.fatal:
			break loop
		case <-grace.C:
			if h.idle() {
				break loop
			}
		case <-h.empty:
			if h.idle() {
				break loop
			}
		case <-h.active:
			h.controls.refresh()
			if !uiStarted {
				uiStarted = true
				done := make(chan error, 1)
				uiDone = done
				go func() { done <- h.runUI(uiCtx, h.controls.models, h.ready, h.controls.action) }()
			}
		case <-h.controls.changed:
			h.controls.refresh()
		case <-refresh.C:
			if uiStarted {
				h.controls.refresh()
			}
		case err := <-uiDone:
			uiDone = nil
			if err != nil && ctx.Err() == nil {
				result = fmt.Errorf("native menu bar: %w", err)
			}
			break loop
		}
	}
	h.shutdown(result)
	cancelUI()
	if uiDone != nil {
		if err := <-uiDone; err != nil && !errors.Is(err, context.Canceled) {
			h.logf("native menu bar stopped: %v", err)
		}
	}
	h.wg.Wait()
	return result
}

func (h *menuBarHost) accept() {
	defer h.wg.Done()
	for {
		conn, err := h.ln.Accept()
		if err != nil {
			h.mu.Lock()
			closing := h.closing
			h.mu.Unlock()
			if !closing {
				h.fatal <- fmt.Errorf("accept menu bar session: %w", err)
			}
			return
		}
		h.wg.Add(1)
		go h.handle(conn)
	}
}

// handle owns one client connection: its lifetime is the session lifetime,
// so an abrupt client exit unregisters it through EOF.
func (h *menuBarHost) handle(conn net.Conn) {
	defer h.wg.Done()
	defer conn.Close()
	r := bufio.NewReaderSize(conn, menuBarMaxMessage)
	var session menuBarSession
	err := conn.SetDeadline(time.Now().Add(menuBarHandshakeTimeout))
	if err == nil {
		err = readMenuBarLine(r, &session)
	}
	if err == nil {
		err = session.validate()
	}
	if err != nil {
		_, _ = conn.Write(encodeMenuBarReply(menuBarReply{Error: err.Error()}))
		return
	}
	p := &menuBarPeer{conn: conn, svc: session.service(), session: session}
	if !h.register(p) {
		return
	}
	defer h.unregister(p)
	if _, err := conn.Write(encodeMenuBarReply(menuBarReply{OK: true})); err != nil {
		return
	}
	select {
	case h.active <- struct{}{}:
	default:
	}
	if conn.SetDeadline(time.Time{}) != nil {
		return
	}
	for {
		var observed menuBarObservation
		if readMenuBarLine(r, &observed) != nil {
			return
		}
		h.mu.Lock()
		p.observed = observed.clean()
		h.mu.Unlock()
		h.controls.requestRefresh()
	}
}

func (h *menuBarHost) register(p *menuBarPeer) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.seq++
	p.seq = h.seq
	p.id, p.connected = rand.Text(), time.Now().UTC()
	p.ctx, p.cancel = context.WithCancel(h.ctx)
	h.sessions[p] = struct{}{}
	return true
}

func (h *menuBarHost) unregister(p *menuBarPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.sessions, p)
	p.closeDashboard()
	p.cancel()
	if len(h.sessions) == 0 && !h.closing {
		select {
		case h.empty <- struct{}{}:
		default:
		}
	}
	if len(h.sessions) > 0 {
		h.controls.requestRefresh()
	}
}

// idle begins shutdown when no session is registered. A registration racing
// with it is refused under the same lock, so that client starts a new host.
func (h *menuBarHost) idle() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.sessions) > 0 {
		return false
	}
	h.closing = true
	return true
}

func (h *menuBarHost) shutdown(reason error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closing = true
	h.ln.Close()
	var notice []byte
	if reason != nil {
		notice = encodeMenuBarReply(menuBarReply{Error: reason.Error()})
	}
	for p := range h.sessions {
		p.cancel()
		if notice != nil {
			_ = p.conn.SetWriteDeadline(time.Now().Add(time.Second))
			_, _ = p.conn.Write(notice)
		}
		p.conn.Close()
		p.closeDashboard()
	}
}

func (h *menuBarHost) ready() {
	h.logf("ready")
}

// dashboardURL opens only the explicitly selected connection's Memory Palace.
// Its token and listener remain private to that connection's lifetime.
func (h *menuBarHost) dashboardURL(instanceID string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return "", errors.New("menu bar host is closing")
	}
	var selected *menuBarPeer
	for p := range h.sessions {
		if p.id == instanceID {
			selected = p
			break
		}
	}
	if selected == nil {
		return "", errors.New("the selected session is no longer connected")
	}
	if selected.dashboard == nil {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", err
		}
		host, token := ln.Addr().String(), rand.Text()
		server := &http.Server{Handler: memory.DashboardHandler(selected.svc, host, token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
		selected.dashboard = &menuBarDashboard{url: "http://" + host + "/#token=" + token, server: server}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			if err := server.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				h.logf("Memory Palace server stopped: %v", err)
			}
		}()
	}
	return selected.dashboard.url, nil
}
