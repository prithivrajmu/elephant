//go:build darwin

package menubar

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteStateIsPrivateAndSkipsStaleRevisions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	states := make(chan Model, 3)
	states <- Model{Revision: 2, Tooltip: "new state"}
	states <- Model{Revision: 1, Tooltip: "stale"}
	close(states)
	if err := writeStates(context.Background(), dir, path, states); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("state mode = %o, want 600", perm)
	}
	data, _ := os.ReadFile(path)
	var got Model
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 {
		t.Fatalf("an older update replaced the current menu state: %+v", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover temp files: %v", entries)
	}
}

func TestWriteStatesStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writeStates(ctx, t.TempDir(), "unused", make(chan Model)) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not stop after cancellation")
	}
}

func TestAppExecutablePreservesSignatureWithDialogArtwork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	executable, err := appExecutable(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	if err := nativeTool(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", bundle); err != nil {
		t.Fatalf("native app cannot validate after adding dialog artwork: %v", err)
	}
}
