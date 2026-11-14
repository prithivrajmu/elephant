package memory

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPObserverIdentifiesClientWithoutCapturingToolPayloads(t *testing.T) {
	root := t.TempDir()
	var events []MCPEvent
	svc := Service{Store: Store{Path: filepath.Join(root, "memory.sqlite")}, Root: root, Project: "observer-test", Identity: Identity{Tenant: "local", User: "test"}, ObserveMCP: func(event MCPEvent) { events = append(events, event) }}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"omp","version":"1.2.3","title":"OMP"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"profile_memory","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"record_memory","arguments":{"lesson":"raw-lesson-text-must-not-leak"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"unknown","arguments":{"payload":"raw-lesson-text-must-not-leak"}}}`,
	}, "\n")
	var output bytes.Buffer
	if err := ServeMCP(svc, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Client.Name != "omp" || events[0].Client.Version != "1.2.3" || events[0].Client.Title != "OMP" {
		t.Fatalf("initialization metadata: %+v", events)
	}
	if events[1].Tool != "profile_memory" || events[1].Failed || events[2].Tool != "record_memory" || !events[2].Failed {
		t.Fatalf("completed tool status: %+v", events)
	}
	for _, event := range events {
		if event.At.IsZero() {
			t.Fatal("metadata lacks an observation time")
		}
	}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("raw-lesson-text-must-not-leak")) {
		t.Fatal("the observer retained tool arguments or error text")
	}
	decoder := json.NewDecoder(&output)
	for id := 1; id <= 4; id++ {
		var response struct {
			ID int `json:"id"`
		}
		if err := decoder.Decode(&response); err != nil || response.ID != id {
			t.Fatalf("observer changed MCP output for request %d: %+v, %v", id, response, err)
		}
	}
}
