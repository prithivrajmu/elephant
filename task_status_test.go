//go:build !windows

package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func taskFixture(t *testing.T) (Service, string, string) {
	svc, config, _ := autoFixture(t)
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Fix query pool concurrency"})
	files, e := os.ReadDir(svc.Store.Path + ".sessions")
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
	return svc, config, strings.TrimSuffix(files[0].Name(), ".json")
}
func TestTaskStatusAcknowledgedSavesAndRestart(t *testing.T) {
	svc, config, session := taskFixture(t)
	if d, e := TaskSummary(config, session, false); e != nil || d.State != "review_incomplete" || d.Saved != 0 {
		t.Fatal(d, e)
	}
	if _, e := TaskSummary(config, session, true); e == nil {
		t.Fatal("completed review before request")
	}
	hookCall(t, config, "codex", "Stop", nil)
	if d, e := TaskSummary(config, session, false); e != nil || d.State != "review_incomplete" {
		t.Fatal(d, e)
	}
	_, capture, e := CaptureForTask(config, session)
	if e != nil {
		t.Fatal(e)
	}
	svc.Task = capture
	lesson := json.RawMessage(`{"scope":"project","class":"warning","incident":"Pool test failed.","lesson":"Limit query concurrency to the pool size.","source":"Synthetic task receipt test"}`)
	for i := 0; i < 2; i++ {
		if _, e = svc.Call("record_memory", lesson); e != nil {
			t.Fatal(e)
		}
	}
	d, e := TaskSummary(config, session, true)
	if e != nil || d.State != "saved" || d.Saved != 1 || len(d.MemoryIDs) != 1 || d.Line != "Elephant: recalled 0 memories · saved 1 lesson." {
		t.Fatal(d, e)
	}
	// A new process reading the same config sees receipts, not transient counters.
	d, e = TaskSummary(config, session, false)
	if e != nil || d.Saved != 1 {
		t.Fatal(d, e)
	}
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Change query pool concurrency"})
	if _, _, e = CaptureForTask(config, session, capture.Task); e == nil {
		t.Fatal("stale review captured into the next task")
	}
	if _, e = TaskSummary(config, session, true, capture.Task); e == nil {
		t.Fatal("stale review completed the next task")
	}
	d, e = TaskSummary(config, session, false)
	if e != nil || d.Saved != 0 || d.Recalled != 1 {
		t.Fatal(d, e)
	}
	hookCall(t, config, "codex", "Stop", nil)
	d, e = TaskSummary(config, session, true)
	if e != nil || d.State != "no_lesson" || !strings.Contains(d.Line, "no reusable lesson found") {
		t.Fatal(d, e)
	}
}
func TestTaskStatusFailurePauseAndIsolation(t *testing.T) {
	svc, config, session := taskFixture(t)
	hookCall(t, config, "codex", "Stop", nil)
	_, capture, e := CaptureForTask(config, session)
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.Store.ObserveCaptureFailure(svc.Identity, svc.Project, capture); e != nil {
		t.Fatal(e)
	}
	d, e := TaskSummary(config, session, true)
	if e != nil || d.State != "capture_failed" || d.Saved != 0 {
		t.Fatal(d, e)
	}
	if _, e = TaskSummary(config, "../../escape", true); e == nil {
		t.Fatal("invalid session accepted")
	}
	c, _ := ReadAutomation(config)
	c.Identity.User = "other"
	b, _ := json.Marshal(c)
	other := filepath.Join(t.TempDir(), "automation.json")
	os.WriteFile(other, b, 0600)
	if _, e = TaskSummary(other, session, false); e == nil {
		t.Fatal("cross-identity receipt leak")
	}
	paused := false
	Automation(svc.Root, &paused)
	d, e = TaskSummary(config, session, false)
	if e != nil || d.Line != "Elephant: automation paused." {
		t.Fatal(d, e)
	}
}
