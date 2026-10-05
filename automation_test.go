//go:build !windows

package memory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func autoFixture(t *testing.T) (Service, string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module demo\ngo 1.22\n"), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{Root: root, Project: "demo", Store: Store{Path: filepath.Join(t.TempDir(), "memory.jsonl")}, Identity: Identity{Tenant: "local", User: "alice"}}
	r, err := InitAutomation(svc, binary, "both", 4000)
	if err != nil {
		t.Fatal(err)
	}
	return svc, r.Config, binary
}
func hookCall(t *testing.T, config, agent, event string, extra map[string]any) map[string]any {
	t.Helper()
	c, e := ReadAutomation(config)
	if e != nil {
		t.Fatal(e)
	}
	v := map[string]any{"hook_event_name": event, "session_id": "session-1", "cwd": c.Root}
	for k, x := range extra {
		v[k] = x
	}
	b, _ := json.Marshal(v)
	out, e := RunHook(config, agent, bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	return out
}

// editCall gives the current task review signal.
func editCall(t *testing.T, config, agent string) {
	t.Helper()
	hookCall(t, config, agent, "PostToolUse", map[string]any{"tool_name": "Edit", "tool_use_id": newID()})
}
func TestInitPreservesConfigAndIsIdempotent(t *testing.T) {
	svc, config, binary := autoFixture(t)
	p := filepath.Join(svc.Root, ".claude", "settings.local.json")
	original := []byte(`{"permissions":{"allow":["Read"]},"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"printf keep"}]}]}}`)
	os.WriteFile(p, original, 0600)
	agents := filepath.Join(svc.Root, "AGENTS.md")
	os.WriteFile(agents, []byte("# Existing rules\n\nKeep these exact words.\n"), 0600)
	r, e := InitAutomation(svc, binary, "both", 4000)
	if e != nil {
		t.Fatal(e)
	}
	if !r.Configured || len(r.Backups) < 2 {
		t.Fatalf("missing backups: %+v", r)
	}
	b, _ := os.ReadFile(p)
	if !bytes.Contains(b, []byte("printf keep")) || !bytes.Contains(b, []byte("permissions")) {
		t.Fatal("lost unrelated config")
	}
	b, _ = os.ReadFile(agents)
	if !bytes.HasPrefix(b, []byte("# Existing rules\n\nKeep these exact words.\n")) {
		t.Fatal("lost instructions")
	}
	r, e = InitAutomation(svc, binary, "both", 4000)
	if e != nil || len(r.Changed) != 0 {
		t.Fatalf("init not idempotent %+v %v", r, e)
	}
	c, e := ReadAutomation(config)
	if e != nil || c.Project != "demo" {
		t.Fatal(e)
	}
}
func TestInitRejectsBadConfigBeforeChangingFiles(t *testing.T) {
	for _, bad := range []string{`{"hooks":`, `{"hooks":{},"hooks":{}}`, `{"hooks":{"Stop":null}}`, `{"hooks":{"Stop":[{"hooks":[null]}]}}`, `{"hooks":{"Stop":[{"hooks":[],"hooks":[]}]}}`} {
		t.Run(bad, func(t *testing.T) {
			svc, config, binary := autoFixture(t)
			before, _ := os.ReadFile(config)
			path := filepath.Join(svc.Root, ".claude/settings.local.json")
			os.WriteFile(path, []byte(bad), 0600)
			_, e := InitAutomation(svc, binary, "both", 5000)
			if e == nil {
				t.Fatal("accepted invalid config")
			}
			after, _ := os.ReadFile(config)
			if !bytes.Equal(before, after) {
				t.Fatal("partial change")
			}
			after, _ = os.ReadFile(path)
			if string(after) != bad {
				t.Fatal("overwrote invalid config")
			}
		})
	}
}
func TestInitRejectsSymlinkParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, ".claude"))
	binary, _ := os.Executable()
	svc := Service{Root: root, Project: "test", Store: Store{Path: filepath.Join(t.TempDir(), "db")}, Identity: Identity{Tenant: "local", User: "me"}}
	if _, e := InitAutomation(svc, binary, "both", 4000); e == nil {
		t.Fatal("followed symlink")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) > 0 {
		t.Fatal("wrote outside root")
	}
	if _, e := os.Stat(automationPath(root)); !os.IsNotExist(e) {
		t.Fatal("partial config")
	}
}
func TestAutomaticLifecycle(t *testing.T) {
	svc, config, _ := autoFixture(t)
	for _, agent := range []string{"codex", "claude"} {
		hookCall(t, config, agent, "SessionStart", nil)
		hookCall(t, config, agent, "UserPromptSubmit", map[string]any{"prompt": "Fix query pool exhaustion"})
		args := map[string]any{"tool_name": "Bash", "tool_use_id": "tool-1", "tool_input": map[string]any{"command": "SECRET_COMMAND"}, "tool_response": map[string]any{"exit_code": 1, "stdout": "SECRET_OUTPUT"}, "transcript_path": "SECRET_TRANSCRIPT"}
		hookCall(t, config, agent, "PostToolUse", args)
		hookCall(t, config, agent, "PostToolUse", args)
		r := hookCall(t, config, agent, "Stop", nil)
		if r["decision"] != "block" || !strings.Contains(r["reason"].(string), "remember") {
			t.Fatal("no review", r)
		}
		if r = hookCall(t, config, agent, "Stop", nil); len(r) != 0 {
			t.Fatal("review loop", r)
		}
		hookCall(t, config, agent, "UserPromptSubmit", map[string]any{"prompt": reviewPrefix + " Continue"})
		if r = hookCall(t, config, agent, "Stop", nil); len(r) != 0 {
			t.Fatal("continuation resets review")
		}
		hookCall(t, config, agent, "UserPromptSubmit", map[string]any{"prompt": "Another task"})
		if r = hookCall(t, config, agent, "Stop", map[string]any{"stop_hook_active": true}); len(r) != 0 {
			t.Fatal("active stop loop")
		}
		editCall(t, config, agent)
		if r = hookCall(t, config, agent, "Stop", nil); r["decision"] != "block" {
			t.Fatal("next task lacks review")
		}
	}
	all, e := svc.Store.All()
	if e != nil || len(all) != 0 {
		t.Fatal("invented a lesson", all, e)
	}
	d, e := svc.Store.Dashboard(svc.Identity, Profile{Project: svc.Project})
	if e != nil {
		t.Fatal(e)
	}
	tools := 0
	for _, x := range d.Experiences {
		if x.Tool == "Bash" {
			tools++
			if x.Status != "failed" {
				t.Fatal("failed tool misclassified")
			}
		}
	}
	if tools != 2 {
		t.Fatalf("duplicate tool instances: %d", tools)
	}
	b, _ := os.ReadFile(svc.Store.Path)
	for _, secret := range []string{"SECRET_COMMAND", "SECRET_OUTPUT", "SECRET_TRANSCRIPT", "Fix query pool exhaustion"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("stored %s", secret)
		}
	}
	// Simulate the host agent's evidence-based extraction; no external model is
	// required for this protocol test. Real host/model behavior is tested separately.
	_, e = svc.Call("record_memory", json.RawMessage(`{"scope":"project","class":"warning","incident":"The query pool ran out of connections.","lesson":"Limit query concurrency to the pool size.","source":"Observed failing pool test, then passing bounded concurrency test.","features":{"language":["go"]}}`))
	if e != nil {
		t.Fatal(e)
	}
	r := hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Fix query pool concurrency"})
	b, _ = json.Marshal(r)
	if !bytes.Contains(b, []byte("Limit query concurrency")) {
		t.Fatal("stored lesson not recalled", string(b))
	}
	var output struct {
		Hook struct {
			Context string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	json.Unmarshal(b, &output)
	if len(output.Hook.Context) > 4000 {
		t.Fatal("budget exceeded")
	}
}
func TestHooksPauseAndIdentityIsolation(t *testing.T) {
	svc, config, _ := autoFixture(t)
	hookCall(t, config, "codex", "SessionStart", nil)
	paused := false
	if _, e := Automation(svc.Root, &paused); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(svc.Store.Path)
	for _, event := range []string{"UserPromptSubmit", "PostToolUse", "Stop"} {
		if r := hookCall(t, config, "codex", event, nil); len(r) != 0 {
			t.Fatal("paused hook output")
		}
	}
	after, _ := os.ReadFile(svc.Store.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("paused hook stored data")
	}
	d, e := svc.Store.Dashboard(Identity{Tenant: "other", User: "alice"}, Profile{Project: svc.Project})
	if e != nil || d.ExperienceCount != 0 {
		t.Fatal("cross-tenant experience leak")
	}
	resumed := true
	Automation(svc.Root, &resumed)
	editCall(t, config, "codex")
	if r := hookCall(t, config, "codex", "Stop", nil); r["decision"] != "block" {
		t.Fatal("resume failed")
	}
	b, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "s", "cwd": t.TempDir()})
	if _, e = RunHook(config, "codex", bytes.NewReader(b)); e == nil {
		t.Fatal("accepted wrong cwd")
	}
	if _, e = RunHook(config, "codex", strings.NewReader(strings.Repeat("x", (1<<20)+1))); e == nil {
		t.Fatal("accepted oversized input")
	}
}
func TestConcurrentToolCaptureDeduplicates(t *testing.T) {
	svc, config, _ := autoFixture(t)
	b, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "s", "cwd": svc.Root, "tool_use_id": "same-call", "tool_name": "Read"})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := RunHook(config, "codex", bytes.NewReader(b)); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	d, e := svc.Store.Dashboard(svc.Identity, Profile{Project: svc.Project})
	if e != nil || d.ExperienceCount != 1 {
		t.Fatalf("dedup failed %d %v", d.ExperienceCount, e)
	}
}
func TestUnanchoredAutomaticMemory(t *testing.T) {
	svc, _, binary := autoFixture(t)
	svc.Project = ""
	svc.Unattached = true
	svc.Conversation = "talk"
	r, e := InitAutomation(svc, binary, "claude", 512)
	if e != nil {
		t.Fatal(e)
	}
	hookCall(t, r.Config, "claude", "SessionStart", nil)
	editCall(t, r.Config, "claude")
	result := hookCall(t, r.Config, "claude", "Stop", nil)
	if !strings.Contains(result["reason"].(string), "personal by default") {
		t.Fatal("unanchored scope wrong")
	}
	d, e := svc.Store.Dashboard(svc.Identity, Profile{})
	if e != nil || d.ExperienceCount != 3 {
		t.Fatal("missing unanchored experiences", e)
	}
}

func TestStopReviewsOnlyFinishedTasksWithSignal(t *testing.T) {
	_, config, _ := autoFixture(t)
	hookCall(t, config, "claude", "UserPromptSubmit", map[string]any{"prompt": "Explain the hook flow"})
	hookCall(t, config, "claude", "PostToolUse", map[string]any{"tool_name": "Read", "tool_use_id": "r1", "tool_response": map[string]any{"exit_code": 0}})
	if r := hookCall(t, config, "claude", "Stop", nil); len(r) != 0 {
		t.Fatal("reviewed a read-only task", r)
	}
	hookCall(t, config, "claude", "PostToolUseFailure", map[string]any{"tool_name": "Bash", "tool_use_id": "b1"})
	if r := hookCall(t, config, "claude", "Stop", map[string]any{"background_tasks": []any{map[string]any{"id": "agent-1"}}}); len(r) != 0 {
		t.Fatal("reviewed while background work runs", r)
	}
	r := hookCall(t, config, "claude", "Stop", nil)
	if r["decision"] != "block" {
		t.Fatal("failed tool call lacks review", r)
	}
	reason := r["reason"].(string)
	if strings.Contains(reason, "--store") || !strings.Contains(reason, "remember --config") || strings.Count(reason, "\n") > 4 {
		t.Fatal("review prompt not compact", reason)
	}
	hookCall(t, config, "codex", "UserPromptSubmit", map[string]any{"prompt": "Fix a bug"})
	hookCall(t, config, "codex", "PostToolUse", map[string]any{"tool_name": "Bash", "tool_use_id": "c1", "tool_response": map[string]any{"exit_code": 2}})
	if r := hookCall(t, config, "codex", "Stop", nil); r["decision"] != "block" {
		t.Fatal("nonzero exit lacks review", r)
	}
}
