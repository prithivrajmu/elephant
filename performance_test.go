package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type performanceRatchets struct {
	Version  int              `json:"version"`
	Ceilings map[string]int64 `json:"ceilings"`
}

func loadPerformanceRatchets(t *testing.T) performanceRatchets {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "performance-ratchets.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r performanceRatchets
	if err = json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if r.Version != 1 || len(r.Ceilings) == 0 {
		t.Fatal("invalid performance ratchets")
	}
	return r
}

func collectPerformanceMetrics(t *testing.T) map[string]int64 {
	t.Helper()
	id, p, m := fixture()
	all := make([]Memory, 1000)
	for i := range all {
		all[i] = m
		all[i].ID = fmt.Sprintf("memory-%04d", i)
	}
	allocs := testing.AllocsPerRun(3, func() {
		Recall(all, id, Request{Profile: p, Task: "connection concurrency", ByteBudget: 4000}, time.Now())
	})
	result := Recall(all, id, Request{Profile: p, Task: "connection concurrency", ByteBudget: 4000}, time.Now())

	root := t.TempDir()
	files := map[string]string{
		"package.json":   `{"dependencies":{"typescript":"1","react":"1"}}`,
		"go.mod":         "module example.test/perf\n",
		"Cargo.toml":     "[package]\nname='perf'\n",
		"pyproject.toml": "[project]\nname='perf'\n",
		"tsconfig.json":  `{}`,
		".elephant.json": `{"features":{"workflow":["perf"]}}`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := ProfileProject(root, "perf")
	if err != nil {
		t.Fatal(err)
	}

	store := Store{Path: filepath.Join(t.TempDir(), "scoped.sqlite")}
	db, err := store.database(true)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		n := m
		n.ID = fmt.Sprintf("scoped-%04d", i)
		n.Scope = "personal"
		if i >= 100 {
			n.Owner = "other"
		}
		if err = applyEvent(tx, Event{Kind: "put", Memory: &n, At: time.Now().UTC()}); err != nil {
			tx.Rollback()
			db.Close()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		db.Close()
		t.Fatal(err)
	}
	selected, err := readRecallMemories(db, id, p)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	toolJSON, err := json.Marshal(MCPTools())
	if err != nil {
		t.Fatal(err)
	}
	mapMemories := make([]Memory, 120)
	for i := range mapMemories {
		mapMemories[i] = m
		mapMemories[i].ID = fmt.Sprintf("map-%03d", i)
		mapMemories[i].Created = time.Unix(int64(i+1), 0)
	}
	memoryMap := BuildMemoryMap(mapMemories)
	palaceMemoryNodes := 0
	for _, node := range memoryMap.Nodes {
		if node.Kind == "memory" {
			palaceMemoryNodes++
		}
	}

	return map[string]int64{
		"mcp_tools_payload_bytes":      int64(len(toolJSON)),
		"palace_memory_nodes_from_120": int64(palaceMemoryNodes),
		"profile_manifest_files":       int64(profileManifestCount(profile)),
		"recall_1000_allocations":      int64(allocs + 0.5),
		"recall_context_bytes":         int64(result.Bytes),
		"scoped_candidates_1000":       int64(len(selected)),
	}
}

func TestPerformanceRatchets(t *testing.T) {
	metrics := collectPerformanceMetrics(t)
	if path := os.Getenv("ELEPHANT_PERF_METRICS"); path != "" {
		b, err := json.MarshalIndent(map[string]any{"version": 1, "metrics": metrics}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, append(b, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	ratchets := loadPerformanceRatchets(t)
	for name, ceiling := range ratchets.Ceilings {
		value, ok := metrics[name]
		if !ok {
			t.Errorf("ratchet %s has no measured metric", name)
			continue
		}
		if value > ceiling {
			t.Errorf("performance ratchet %s regressed: measured %d > ceiling %d", name, value, ceiling)
		}
	}
	for name := range metrics {
		if _, ok := ratchets.Ceilings[name]; !ok {
			t.Errorf("measured metric %s has no ratchet", name)
		}
	}
}

func TestRecallTraceAndDashboardPercentiles(t *testing.T) {
	id, p, m := fixture()
	store := Store{Path: filepath.Join(t.TempDir(), "trace.sqlite")}
	if _, err := store.Record(m); err != nil {
		t.Fatal(err)
	}
	request := Request{Profile: p, Task: "query connections", ByteBudget: 4000, profileMS: 1.25, manifestFiles: 3}
	result, trace, err := store.RecallWithTrace(id, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || trace.Candidates != 1 || trace.ManifestFiles != 3 || trace.ProfileMS != 1.25 || trace.TotalMS < trace.CommitMS {
		t.Fatalf("unexpected trace: result=%+v trace=%+v", result, trace)
	}
	if _, _, err = store.RecallWithTrace(id, request); err != nil {
		t.Fatal(err)
	}
	dashboard, err := store.Dashboard(id, p)
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.RecallLatency.Core.Samples != 2 || dashboard.RecallLatency.Profile.Samples != 2 || dashboard.P95MS != dashboard.RecallLatency.Core.P95MS {
		t.Fatalf("unexpected percentiles: %+v", dashboard.RecallLatency)
	}
	for _, usage := range dashboard.Usage {
		if usage.Candidates != 1 || usage.ManifestFiles != 3 || usage.ProfileMS != 1.25 {
			t.Fatalf("missing local phase receipt: %+v", usage)
		}
		data, err := json.Marshal(usage)
		if err != nil || string(data) == "" || strings.Contains(string(data), request.Task) {
			t.Fatalf("usage receipt retained task text: %s", data)
		}
	}
}

// This repository-only helper creates synthetic stores for the external binary
// runner. It is inert in normal tests and never writes to a user's Elephant store.
func TestGeneratePerformanceFixture(t *testing.T) {
	path := os.Getenv("ELEPHANT_PERF_FIXTURE")
	if path == "" {
		t.Skip("performance fixture helper")
	}
	size, err := strconv.Atoi(os.Getenv("ELEPHANT_PERF_SIZE"))
	if err != nil || size < 1 || size > 100000 {
		t.Fatal("ELEPHANT_PERF_SIZE must be 1..100000")
	}
	_, _, m := fixture()
	m.Tenant, m.Owner, m.Project, m.Scope = "local", "me", "perf", "project"
	m.Features = map[string][]string{"language": {"go"}}
	m.Requires = nil
	store := Store{Path: path}
	db, err := store.database(true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < size; i++ {
		n := m
		n.ID = fmt.Sprintf("perf-%06d", i)
		n.Subject = "query-pool"
		n.Incident = "Synthetic performance fixture"
		n.Lesson = fmt.Sprintf("Bound query concurrency with measured limit %d", i)
		n.Source = "SYNTHETIC PERFORMANCE FIXTURE"
		if err = applyEvent(tx, Event{Kind: "put", Memory: &n, At: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
