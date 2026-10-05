package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	memory "github.com/prithivrajmu/elephant"
)

func TestAutomaticSetupChecksInstalledHooks(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("automatic hooks require macOS or Linux")
	}
	root := t.TempDir()
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	previousArgs, previousOut := os.Args, os.Stdout
	t.Cleanup(func() { os.Args, os.Stdout = previousArgs, previousOut })
	os.Args = []string{"elephant", "setup", "--auto", "--agent", "codex", "--root", root, "--store", filepath.Join(root, "memory.sqlite")}
	os.Stdout = output
	if err = run(); err != nil {
		t.Fatal(err)
	}
	if _, err = output.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var result memory.InitResult
	if err = json.NewDecoder(output).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if !result.Configured || len(result.Agents) != 1 || result.Agents[0] != "codex" || len(result.Checks) == 0 {
		t.Fatalf("incomplete setup result: %+v", result)
	}
	for _, check := range result.Checks {
		if !check.OK {
			t.Fatalf("failed health check: %+v", check)
		}
	}
	os.Args = []string{"elephant", "setup", "--auto", "--root", root}
	if err = run(); err != nil {
		t.Fatal(err)
	}
	c, err := memory.ReadAutomation(filepath.Join(root, ".elephant", "automation.json"))
	if err != nil || len(c.Agents) != 1 || c.Agents[0] != "codex" {
		t.Fatalf("rerun changed adapter choice: %+v %v", c, err)
	}
	if _, err = os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatal("configured unrequested adapter")
	}
}

func TestAutomaticSetupRejectsConflictingModes(t *testing.T) {
	previous := os.Args
	t.Cleanup(func() { os.Args = previous })
	for _, args := range [][]string{
		{"elephant", "setup", "--auto", "--wizard"},
		{"elephant", "setup", "--auto", "--output", t.TempDir()},
		{"elephant", "recall", "--auto"},
	} {
		os.Args = args
		if err := run(); err == nil {
			t.Fatalf("accepted conflicting mode: %v", args)
		}
	}
}
