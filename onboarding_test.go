package memory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLegacyStoreAndSafeSetup(t *testing.T) {
	home := t.TempDir()
	modern := filepath.Join(home, ".elephant", "events.jsonl")
	legacy := filepath.Join(home, ".agent-memory", "events.jsonl")
	if DefaultStorePath(home) != modern {
		t.Fatal("new store")
	}
	os.MkdirAll(filepath.Dir(legacy), 0700)
	os.WriteFile(legacy, []byte{}, 0600)
	if DefaultStorePath(home) != legacy {
		t.Fatal("history must survive rebrand")
	}
	s := Service{Store: Store{Path: legacy}, Identity: Identity{Tenant: "local", User: "alice"}, Unattached: true, Conversation: "session-1"}
	config, err := SetupConfig(s, filepath.Join(home, "bin with spaces", "elephant"))
	if err != nil {
		t.Fatal(err)
	}
	server := config.MCP["mcpServers"]["elephant"]
	if server.Type != "stdio" || !filepath.IsAbs(server.Command) || !strings.Contains(config.CodexTOML, "[mcp_servers.elephant]") {
		t.Fatal(config)
	}
	if !strings.Contains(strings.Join(server.Args, " "), "--unattached") || strings.Contains(strings.Join(server.Args, " "), "--root") {
		t.Fatal(server.Args)
	}
	out := filepath.Join(home, "config")
	if err = WriteSetup(out, config); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(filepath.Join(out, "mcp.json"))
	if err = WriteSetup(out, config); err == nil {
		t.Fatal("must not overwrite")
	}
	now, _ := os.ReadFile(filepath.Join(out, "mcp.json"))
	if !bytes.Equal(original, now) {
		t.Fatal("setup overwrote existing configuration")
	}
	d, err := Doctor(s)
	if err != nil || !d.OK {
		t.Fatal(d, err)
	}
	contents, _ := os.ReadFile(legacy)
	if len(contents) != 0 {
		t.Fatal("doctor wrote memory")
	}
}

func TestConcurrentJournalUsers(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	_, _, m := fixture()
	var wg sync.WaitGroup
	failures := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				x := m
				x.ID = ""
				x.Lesson = string(rune('A'+i)) + " concurrent lesson"
				_, err := s.Record(x)
				failures <- err
			} else {
				_, err := s.All()
				failures <- err
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.All()
	if err != nil || len(all) != 12 {
		t.Fatal(len(all), err)
	}
	// A temporarily held lock must be retried rather than stolen or immediately failed.
	os.Mkdir(s.Path+".lock", 0700)
	go func() { time.Sleep(60 * time.Millisecond); os.Remove(s.Path + ".lock") }()
	if _, err = s.All(); err != nil {
		t.Fatal(err)
	}
}

func TestPromptsAndSchema(t *testing.T) {
	for _, tool := range MCPTools() {
		schema := tool["inputSchema"].(map[string]any)
		if required, ok := schema["required"]; ok && required == nil {
			t.Fatal("null required")
		}
	}
	s := Service{Store: Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}, Identity: Identity{Tenant: "local", User: "alice"}, Unattached: true}
	requests := []map[string]any{
		{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]string{"name": "strict-test", "version": "1"}, "capabilities": map[string]any{}}},
		{"jsonrpc": "2.0", "method": "notifications/initialized"},
		{"jsonrpc": "2.0", "id": 2, "method": "prompts/list"},
		{"jsonrpc": "2.0", "id": 3, "method": "prompts/get", "params": map[string]any{"name": "initmemory", "arguments": map[string]string{"task": "validate pooling"}}},
	}
	var in, out bytes.Buffer
	for _, r := range requests {
		json.NewEncoder(&in).Encode(r)
	}
	if err := ServeMCP(s, &in, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "validate pooling") || !strings.Contains(out.String(), "memory_review") || strings.Contains(out.String(), "\"error\"") {
		t.Fatal(out.String())
	}
}

func TestOnboardingSelfTest(t *testing.T) {
	d, err := SelfTest()
	if err != nil || !d.OK || len(d.Checks) != 7 {
		t.Fatal(d, err)
	}
}
