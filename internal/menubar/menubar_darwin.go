//go:build darwin

// Package menubar shows a native macOS status-bar item using the system JXA
// applet runtime, without cgo, Swift, or an additional package.
package menubar

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed menu_bar.js
var script string

// stateEnv names the private environment variable carrying the state path.
const stateEnv = "ELEPHANT_MENU_STATE"

// maxEventLine bounds one native stdout event (dialog text included).
const maxEventLine = 1 << 20

// Run shows the menu-bar item and blocks until it exits. The native side
// renders the latest Model received from states; it starts with a placeholder
// menu and never blocks waiting for the first state. onReady is called once
// after the status item exists; onAction for every menu click or dialog answer,
// sequentially on the reader goroutine (handlers must not block for long).
// "Hide Menu Bar" reports Action{ID: HideAction} and Run returns nil. Context
// cancellation terminates and reaps the native child and returns ctx.Err().
// Nothing is written to the caller's stdout.
func Run(ctx context.Context, states <-chan Model, onReady func(), onAction func(Action)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	executable, err := appExecutable(ctx)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "elephant-menu-") // 0700
	if err != nil {
		return fmt.Errorf("menubar: create state directory: %w", err)
	}
	defer os.RemoveAll(dir)
	statePath := filepath.Join(dir, "state.json")

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(runCtx, executable)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second
	cmd.Env = append(os.Environ(), stateEnv+"="+statePath)
	var stderr limitedBuffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("menubar: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("menubar: start native app: %w", err)
	}

	var writeErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := writeStates(runCtx, dir, statePath, states); err != nil {
			writeErr = err
			cancel()
		}
	}()

	var readyOnce sync.Once
	hidden := false
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 4096), maxEventLine)
	for sc.Scan() {
		ev, ok := parseEvent(sc.Bytes())
		if !ok {
			continue
		}
		switch ev.Event {
		case "ready":
			if onReady != nil {
				readyOnce.Do(onReady)
			}
		case "action":
			if ev.ID == HideAction {
				hidden = true
			}
			if onAction != nil {
				onAction(Action{ID: ev.ID, Choice: ev.Choice, Text: ev.Text})
			}
		}
	}
	_, _ = io.Copy(io.Discard, stdout) // oversized line: keep the child unblocked
	waitErr := cmd.Wait()
	cancel()
	wg.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if writeErr != nil {
		return writeErr
	}
	if hidden && (waitErr == nil || isTerminated(waitErr)) {
		return nil
	}
	detail := strings.TrimSpace(stderr.String())
	if detail == "" {
		detail = "no stderr output"
	}
	if waitErr != nil {
		return fmt.Errorf("menubar: native app failed (requires a logged-in macOS GUI session): %w: %s", waitErr, detail)
	}
	return fmt.Errorf("menubar: native app exited unexpectedly before Hide Menu Bar: %s", detail)
}

type nativeEvent struct {
	Event  string `json:"event"`
	ID     string `json:"id"`
	Choice string `json:"choice"`
	Text   string `json:"text"`
}

// parseEvent decodes one private stdout line. Lines that are not well-formed
// events (for example runtime noise) are ignored.
func parseEvent(line []byte) (nativeEvent, bool) {
	var ev nativeEvent
	if err := json.Unmarshal(bytes.TrimSpace(line), &ev); err != nil {
		return nativeEvent{}, false
	}
	switch ev.Event {
	case "ready":
		return ev, true
	case "action":
		return ev, ev.ID != ""
	}
	return nativeEvent{}, false
}

// writeStates publishes each newer model until ctx ends or states closes.
func writeStates(ctx context.Context, dir, path string, states <-chan Model) error {
	var last uint64
	written := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case m, ok := <-states:
			if !ok {
				return nil
			}
			if written && m.Revision <= last {
				continue
			}
			if err := writeState(dir, path, m); err != nil {
				return err
			}
			last, written = m.Revision, true
		}
	}
}

// writeState atomically replaces path with m as 0600 JSON.
func writeState(dir, path string, m Model) error {
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("menubar: encode state: %w", err)
	}
	f, err := os.CreateTemp(dir, ".state-") // 0600
	if err != nil {
		return fmt.Errorf("menubar: write state: %w", err)
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("menubar: write state: %w", err)
	}
	return nil
}

func isTerminated(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGTERM
}

// limitedBuffer keeps the first 8 KiB of stderr for error detail.
type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := 8192 - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
