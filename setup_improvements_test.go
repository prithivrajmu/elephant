package memory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRetryCompletesPartialFilesAndPreservesEdits(t *testing.T) {
	root := t.TempDir()
	s := Service{Root: root, Store: Store{Path: filepath.Join(root, "memory.sqlite")}, Identity: Identity{Tenant: "local", User: "me"}}
	config, err := SetupConfig(s, "elephant")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "setup")
	if err = WriteSetup(out, config); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(out, "FIRST_TASK.txt")); err != nil {
		t.Fatal(err)
	}
	if err = WriteSetup(out, config); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(out, "FIRST_TASK.txt")); err != nil {
		t.Fatal(err)
	}
	edited := filepath.Join(out, "AGENT_INSTRUCTIONS.md")
	if err = os.WriteFile(edited, []byte("keep my instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(out, "mcp.json")); err != nil {
		t.Fatal(err)
	}
	if err = WriteSetup(out, config); err == nil {
		t.Fatal("edited file overwritten")
	}
	if _, err = os.Stat(filepath.Join(out, "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("failed preflight created partial files")
	}
	data, _ := os.ReadFile(edited)
	if string(data) != "keep my instructions" {
		t.Fatal("user instructions changed")
	}
}

func TestSetupConfigMakesLaunchPathsAbsolute(t *testing.T) {
	s := Service{Root: ".", Store: Store{Path: "memory.sqlite"}, Identity: Identity{Tenant: "local", User: "me"}}
	config, err := SetupConfig(s, "elephant")
	if err != nil {
		t.Fatal(err)
	}
	args := config.MCP["mcpServers"]["elephant"].Args
	for _, flag := range []string{"--store", "--root", "--project"} {
		found := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] == flag {
				found = filepath.IsAbs(args[i+1])
			}
		}
		if !found {
			t.Fatalf("%s must survive a different client working directory: %v", flag, args)
		}
	}
}

func TestWizardUsesNewRootAndPreservesPolicyOnFailedSetup(t *testing.T) {
	root, nextRoot := t.TempDir(), t.TempDir()
	s := Service{Root: root, Project: root, Store: Store{Path: filepath.Join(root, "memory.sqlite")}, Identity: Identity{Tenant: "local", User: "me"}}
	out := filepath.Join(root, "setup")
	var output bytes.Buffer
	if err := SetupWizard(s, "elephant", out, strings.NewReader(nextRoot+"\n\n\n\n"), &output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "setup.json"))
	if err != nil {
		t.Fatal(err)
	}
	var setup Setup
	if err = json.Unmarshal(data, &setup); err != nil {
		t.Fatal(err)
	}
	args := setup.MCP["mcpServers"]["elephant"].Args
	if !strings.Contains(strings.Join(args, " "), "--project "+nextRoot) {
		t.Fatalf("old root leaked into project: %v", args)
	}
	// Different policy would overwrite existing setup; failure must not change
	// global settings for every project that shares this store.
	if err = SetupWizard(s, "elephant", out, strings.NewReader(nextRoot+"\n\n100\n\n"), &output); err == nil {
		t.Fatal("expected conflicting setup")
	}
	policy, err := s.Store.LanguagePolicy()
	if err != nil || policy.Target != 80 {
		t.Fatalf("failed wizard changed policy: %+v %v", policy, err)
	}
}
