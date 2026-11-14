//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"

	memory "github.com/prithivrajmu/elephant"
)

func TestMenuBarHealthDetectsMissingConfiguredReviewer(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc := memory.Service{Root: root, Project: "reviewer-health", Store: memory.Store{Path: filepath.Join(root, "memory.sqlite")}, Identity: memory.Identity{Tenant: "local", User: "test"}}
	binary := filepath.Join(root, "elephant-test")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.InitAutomation(svc, binary, "codex", 4000); err != nil {
		t.Fatal(err)
	}
	settings := memory.DefaultIdleDreamConfig()
	settings.Enabled = false
	settings.Runner = []string{filepath.Join(root, "reviewer-does-not-exist")}
	if _, err := memory.ConfigureIdleDream(filepath.Join(root, ".elephant", "automation.json"), settings, false); err != nil {
		t.Fatal(err)
	}
	report := runMenuBarHealth(svc)
	for _, check := range report.Checks {
		if check.Name == "dream_reviewer" {
			if check.OK || report.OK {
				t.Fatal("an unavailable configured reviewer was reported healthy")
			}
			return
		}
	}
	t.Fatal("health report omitted configured reviewer availability")
}
