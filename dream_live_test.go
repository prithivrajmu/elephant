package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This opt-in test spends the configured Codex allowance on synthetic evidence.
func TestDreamLiveReviewer(t *testing.T) {
	if os.Getenv("ELEPHANT_LIVE_DREAM_EVAL") != "1" {
		t.Skip("set ELEPHANT_LIVE_DREAM_EVAL=1 to exercise the authenticated reviewer")
	}
	runner, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	c, path, old, lease := idleFixture(t)
	c.IdleDream.Runner = []string{runner}
	source := filepath.Join(c.Root, "pool.md")
	if err = os.WriteFile(source, []byte("# Current source of truth\nThe active query pool capacity is 8. Never use 64.\nThis rule applies to the postgres pool only.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old.Source = source
	old.Incident = "A synthetic note recorded an outdated query pool capacity."
	old.Lesson = "Set the active query pool capacity to 64."
	if err = c.Service().Store.transact(func([]Memory, map[string]bool) (*Event, error) { return &Event{Kind: "put", Memory: &old}, nil }); err != nil {
		t.Fatal(err)
	}
	writeIdleConfig(t, path, c)
	if err = runIdleDream(context.Background(), path, c, lease, executeDreamRunner); err != nil {
		t.Fatal(err)
	}
	status, err := DreamScheduleStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("real adapter status=%s saved=%d attempts=%d", status.State.LastResult, len(status.State.LastSaved), status.State.Attempts)
	if status.State.LastResult != "saved" {
		t.Fatalf("review did not save a supported correction: %s", status.State.LastResult)
	}
	all, err := c.Service().Store.All()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range all {
		if m.ID == old.ID {
			if !m.Retired {
				t.Fatal("input not retired")
			}
			continue
		}
		if !m.Retired && m.Dream != nil && strings.Contains(m.Lesson, "8") && m.Helpful == 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("no source-backed capacity correction found")
	}
}
