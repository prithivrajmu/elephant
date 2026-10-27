//go:build !windows

package memory

import (
	"encoding/json"
	"errors"
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
	editCall(t, config, "codex")
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
	if _, _, e = CaptureForTask(config, session, capture.Task); e == nil || !strings.Contains(e.Error(), "already reported") {
		t.Fatal("stale review captured into the next task", e)
	}
	// A stale status command is read-only and names its own task's result.
	d, e = TaskSummary(config, session, true, capture.Task)
	if e != nil || d.State != "already_reported" || d.Line != "Elephant: saved 1 lesson · this review was already reported." {
		t.Fatal(d, e)
	}
	d, e = TaskSummary(config, session, false)
	if e != nil || d.State != "review_incomplete" || d.Saved != 0 || d.Recalled != 1 {
		t.Fatal("stale review changed the next task", d, e)
	}
	editCall(t, config, "codex")
	hookCall(t, config, "codex", "Stop", nil)
	d, e = TaskSummary(config, session, true)
	if e != nil || d.State != "no_lesson" || !strings.Contains(d.Line, "no reusable lesson found") {
		t.Fatal(d, e)
	}
}
func TestTaskStatusInterruptedReviewKeepsItsTask(t *testing.T) {
	svc, config, session := taskFixture(t)
	editCall(t, config, "codex")
	hookCall(t, config, "codex", "Stop", nil)
	_, old, e := CaptureForTask(config, session)
	if e != nil {
		t.Fatal(e)
	}
	// The user sends a prompt while the agent runs the requested review.
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Also update the docs"})
	if _, _, e = CaptureForTask(config, session, newID()); e == nil || !strings.Contains(e.Error(), "unknown task ID") {
		t.Fatal("unknown task accepted", e)
	}
	_, capture, e := CaptureForTask(config, session, old.Task)
	if e != nil || capture.Task != old.Task {
		t.Fatal(capture, e)
	}
	svc.Task = capture
	lesson := json.RawMessage(`{"scope":"project","class":"warning","incident":"Pool test failed.","lesson":"Limit query concurrency to the pool size.","source":"Synthetic interrupted review test"}`)
	if _, e = svc.Call("record_memory", lesson); e != nil {
		t.Fatal(e)
	}
	if _, e = TaskSummary(config, session, true); e == nil {
		t.Fatal("interrupted review completed the next task")
	}
	d, e := TaskSummary(config, session, true, old.Task)
	if e != nil || d.State != "saved" || d.Saved != 1 || d.Line != "Elephant: recalled 0 memories · saved 1 lesson." {
		t.Fatal(d, e)
	}
	if d, e = TaskSummary(config, session, true, old.Task); e != nil || d.Saved != 1 {
		t.Fatal("repeated acknowledgement", d, e)
	}
	// The next task owns no receipt and keeps its own review cycle.
	if d, e = TaskSummary(config, session, false); e != nil || d.State != "review_incomplete" || d.Saved != 0 {
		t.Fatal(d, e)
	}
	editCall(t, config, "codex")
	hookCall(t, config, "codex", "Stop", nil)
	if d, e = TaskSummary(config, session, true); e != nil || d.State != "no_lesson" {
		t.Fatal(d, e)
	}
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Next"})
	if _, _, e = CaptureForTask(config, session, old.Task); e == nil {
		t.Fatal("interrupted review outlived the next task")
	}
}
func TestTaskStatusSupersededReview(t *testing.T) {
	_, config, session := taskFixture(t)
	editCall(t, config, "codex")
	hookCall(t, config, "codex", "Stop", nil)
	_, x, _ := CaptureForTask(config, session)
	// Two read-only prompts during the review keep it pending.
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "One"})
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Two"})
	if _, _, e := CaptureForTask(config, session, x.Task); e != nil {
		t.Fatal("read-only prompts dropped the pending review", e)
	}
	// A newer interrupted review takes the single pending slot.
	editCall(t, config, "codex")
	hookCall(t, config, "codex", "Stop", nil)
	_, y, _ := CaptureForTask(config, session)
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Three"})
	if _, _, e := CaptureForTask(config, session, y.Task); e != nil {
		t.Fatal(e)
	}
	if _, _, e := CaptureForTask(config, session, x.Task); e == nil || !strings.Contains(e.Error(), "newer task replaced") {
		t.Fatal("superseded review captured", e)
	}
	d, e := TaskSummary(config, session, true, x.Task)
	if e != nil || d.State != "superseded" || d.Line != "Elephant: saved 0 lessons · a newer task replaced this review." {
		t.Fatal(d, e)
	}
	if d, e = TaskSummary(config, session, false); e != nil || d.State != "review_incomplete" {
		t.Fatal("superseded review changed the current task", d, e)
	}
}
func TestUnreviewedWorkCarriesToNextTask(t *testing.T) {
	_, config, session := taskFixture(t)
	editCall(t, config, "codex")
	// The user interrupts, so the host sends no Stop for this task.
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Explain the change"})
	if r := hookCall(t, config, "codex", "Stop", nil); r["decision"] != "block" {
		t.Fatal("unreviewed edit lost at the next prompt", r)
	}
	if d, e := TaskSummary(config, session, true); e != nil || d.State != "no_lesson" {
		t.Fatal(d, e)
	}
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Explain it again"})
	if r := hookCall(t, config, "codex", "Stop", nil); len(r) != 0 {
		t.Fatal("reviewed work carried past its review", r)
	}
}
func TestTaskStatusFailurePauseAndIsolation(t *testing.T) {
	svc, config, session := taskFixture(t)
	editCall(t, config, "codex")
	hookCall(t, config, "codex", "Stop", nil)
	_, capture, e := CaptureForTask(config, session)
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.Store.ObserveCaptureFailure(svc.Identity, svc.Project, capture, errors.New("disk full")); e != nil {
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
	if _, _, e = CaptureForTask(config, session); e == nil || !strings.Contains(e.Error(), "lesson not saved") {
		t.Fatal("paused capture lacks guidance", e)
	}
	d, e = TaskSummary(config, session, false)
	if e != nil || d.Line != "Elephant: automation paused." {
		t.Fatal(d, e)
	}
}

// A refused lesson is an answer to the author, not a lost capture.
func TestCaptureRejectionIsNotFailure(t *testing.T) {
	svc, config, session := taskFixture(t)
	editCall(t, config, "codex")
	hookCall(t, config, "codex", "Stop", nil)
	_, capture, e := CaptureForTask(config, session)
	if e != nil {
		t.Fatal(e)
	}
	svc.Task = capture
	record := func(lesson string, kind string) {
		t.Helper()
		_, e := svc.Call("record_memory", json.RawMessage(lesson))
		var r *Rejection
		if kind == "" && e != nil || kind != "" && (!errors.As(e, &r) || r.Kind != kind) {
			t.Fatalf("want %q, got %v", kind, e)
		}
		if e != nil {
			svc.Store.ObserveCaptureFailure(svc.Identity, svc.Project, capture, e)
		}
	}
	record(`{}`, "invalid")
	d, e := TaskSummary(config, session, true)
	if e != nil || d.State != "rejected" || d.Line != "Elephant: recalled 0 memories · saved 0 lessons · 1 attempt rejected." {
		t.Fatal(d, e)
	}
	record(`{"scope":"project","class":"warning","incident":"A Bash command used echo ======= as a separator between git show outputs; zsh treated the leading = as command-path expansion and failed.","lesson":"In this zsh-based Bash tool, never start an unquoted word with =. Zsh EQUALS expansion looks it up as a command. Use quoted separators.","source":"first task"}`, "")
	record(`{"scope":"project","class":"warning","incident":"A Bash command used echo ====== as a section separator between git show outputs and failed in zsh.","lesson":"In zsh, an unquoted word starting with = triggers =command expansion and fails with not found. Quote separators.","source":"another task"}`, "duplicate")
	d, e = TaskSummary(config, session, true)
	if e != nil || d.State != "saved" || d.Rejected != 2 || d.Line != "Elephant: recalled 0 memories · saved 1 lesson." {
		t.Fatal(d, e)
	}
	// A store error still fails capture, even after a save.
	svc.Store.ObserveCaptureFailure(svc.Identity, svc.Project, capture, errors.New("disk I/O error"))
	if d, e = TaskSummary(config, session, true); e != nil || d.State != "capture_failed" || d.Line != "Elephant: recalled 0 memories · saved 1 lesson · capture failed." {
		t.Fatal(d, e)
	}
}
