package memory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLanguagePolicyAndEvidence(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	policy, err := s.LanguagePolicy()
	if err != nil || policy.Target != 80 || policy.FullValidation {
		t.Fatal(policy, err)
	}
	_, _, m := fixture()
	m.Requires = nil
	m.Lesson = "Utilize the cache in order to reduce requests."
	m.Incident = strings.Repeat("Original evidence. ", 30)
	saved, err := s.Record(m)
	if err != nil || saved.Writing == nil || len(saved.Writing.Issues) != 2 || saved.Incident != m.Incident {
		t.Fatal(saved, err)
	}
	if _, err = s.SetLanguagePolicy(100); err != nil {
		t.Fatal(err)
	}
	fresh := Store{Path: s.Path}
	policy, err = fresh.LanguagePolicy()
	if err != nil || policy.Target != 100 {
		t.Fatal(policy, err)
	}
	m.Lesson = "Utilize the pool."
	if _, err = fresh.Record(m); err == nil {
		t.Fatal("strict target accepted a known phrase")
	}
	m.Lesson = "Bound active queries to the pool limit. Keep `Promise.all()` unchanged."
	good, err := fresh.Record(m)
	if err != nil || good.Incident != m.Incident {
		t.Fatal(good, err)
	}
	all, err := fresh.All()
	if err != nil || len(all) != 2 || all[0].Incident != m.Incident {
		t.Fatal(all, err)
	}
	if _, err = fresh.SetLanguagePolicy(79); err == nil {
		t.Fatal("invalid target accepted")
	}
	os.WriteFile(s.Path+".settings.json", []byte("broken"), 0600)
	if _, err = fresh.Record(m); err == nil {
		t.Fatal("corrupt settings silently accepted")
	}
}
func TestLanguageChecksAndBrandClasses(t *testing.T) {
	p, _ := NewLanguagePolicy(100)
	text := strings.Repeat("word ", 26) + "."
	if r := CheckLanguage(p, text); r.Accepted || len(r.Issues) != 1 {
		t.Fatal(r)
	}
	if r := CheckLanguage(p, "Do not use the old key. Keep `don't` in code."); !r.Accepted {
		t.Fatal(r)
	}
	if r := CheckLanguage(p, "Don't use the old key."); r.Accepted {
		t.Fatal(r)
	}
	if r := CheckLanguage(p, "Keep `the code"); r.Accepted {
		t.Fatal("unclosed code bypass", r)
	}
	for _, class := range []string{"win", "lesson", "warning", "scar"} {
		m := Memory{Class: class}
		if MemoryClass(m) != class || MemoryClass(Memory{Outcome: OutcomeForClass(class)}) != class {
			t.Fatal(class)
		}
	}
}
func TestNewProfileConfigAndLegacyFallback(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".agent-memory.json"), []byte(`{"features":{"cloud":["aws"]}}`), 0600)
	p, err := ProfileProject(root, "test")
	if err != nil || p.Features["cloud"][0] != "aws" {
		t.Fatal(p, err)
	}
	os.WriteFile(filepath.Join(root, ".elephant.json"), []byte(`{"features":{"cloud":["azure"]}}`), 0600)
	p, err = ProfileProject(root, "test")
	if err != nil || len(p.Features["cloud"]) != 1 || p.Features["cloud"][0] != "azure" {
		t.Fatal(p, err)
	}
}
func TestSetupWizardAndFactMap(t *testing.T) {
	root := t.TempDir()
	s := Service{Store: Store{Path: filepath.Join(root, "events.jsonl")}, Identity: Identity{Tenant: "local", User: "alice"}, Root: root, Project: "test"}
	var out bytes.Buffer
	input := "\n\n100\n" + filepath.Join(root, "setup") + "\n"
	if err := SetupWizard(s, filepath.Join(root, "elephant"), "", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "setup", "setup.json"))
	var config Setup
	if err := json.Unmarshal(data, &config); err != nil || config.Language.Target != 100 || !strings.Contains(config.Instructions, "100%") {
		t.Fatal(config, err)
	}
	if err := SetupWizard(s, "elephant", "", strings.NewReader(""), &out); err == nil {
		t.Fatal("EOF accepted")
	}
	_, _, m := fixture()
	m.ID = "one"
	m.Features = map[string][]string{"database": {"snowflake"}}
	graph := BuildMemoryMap([]Memory{m})
	if len(graph.Nodes) != 4 || len(graph.Trails) != 3 {
		t.Fatal(graph)
	}
	m.Retired = true
	if len(BuildMemoryMap([]Memory{m}).Nodes) != 0 {
		t.Fatal("retired node")
	}
}
