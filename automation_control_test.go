//go:build !windows

package memory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func automationControlFixture(t *testing.T) Service {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := Service{Root: root, Project: "control-test", Store: Store{Path: filepath.Join(root, "memory.sqlite")}, Identity: Identity{Tenant: "local", User: "test"}}
	binary := filepath.Join(t.TempDir(), "elephant")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := InitAutomation(svc, binary, "claude", 4000); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestAutomaticHookPauseKeepsExplicitMemoryCallsAvailable(t *testing.T) {
	svc := automationControlFixture(t)
	hostPath := filepath.Join(svc.Root, ".claude", "settings.local.json")
	before, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	config, err := svc.SetAutomaticHooks(false)
	if err != nil || config.Enabled {
		t.Fatalf("pause: %+v, %v", config, err)
	}
	value, err := svc.Call("record_memory", json.RawMessage(`{"scope":"project","outcome":"good","incident":"A synthetic queue exceeded its limit.","lesson":"Use bounded queues for event capture.","source":"test:automatic-hooks-pause"}`))
	if err != nil {
		t.Fatal(err)
	}
	id := value.(Memory).ID
	value, err = svc.Call("recall_memory", json.RawMessage(`{"task":"queue"}`))
	if err != nil {
		t.Fatal(err)
	}
	hits := value.(Result).Hits
	if len(hits) != 1 || hits[0].ID != id {
		t.Fatalf("explicit recall while automatic hooks are paused: %+v", hits)
	}
	config, err = svc.SetAutomaticHooks(true)
	if err != nil || !config.Enabled {
		t.Fatalf("resume: %+v, %v", config, err)
	}
	after, err := os.ReadFile(hostPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("pause/resume changed host settings: %v", err)
	}
}

func TestAutomaticHookControlRejectsAnotherMemoryContext(t *testing.T) {
	svc := automationControlFixture(t)
	path := filepath.Join(svc.Root, ".elephant", "automation.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		name  string
		apply func(*Service)
	}{
		{"store", func(s *Service) { s.Store.Path = filepath.Join(s.Root, "another.sqlite") }},
		{"identity", func(s *Service) { s.Identity.User = "other" }},
		{"project", func(s *Service) { s.Project = "other" }},
		{"conversation", func(s *Service) { s.Conversation = "another-chat" }},
		{"unattached", func(s *Service) { s.Unattached = true }},
	} {
		t.Run(change.name, func(t *testing.T) {
			other := svc
			change.apply(&other)
			if _, err := other.SetAutomaticHooks(false); err == nil {
				t.Fatal("changed another context's automatic hooks")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("rejected action modified configuration: %v", err)
			}
		})
	}
}
