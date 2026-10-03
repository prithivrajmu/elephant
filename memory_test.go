package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture() (Identity, Profile, Memory) {
	id := Identity{Tenant: "acme", User: "alice", Team: "platform"}
	p := Profile{Project: "new-app", Features: map[string][]string{"language": {"typescript"}, "framework": {"nextjs"}, "database": {"postgres"}}}
	m := Memory{ID: "a", Tenant: "acme", Owner: "alice", Project: "old-app", Scope: "personal", Outcome: "worst", Incident: "Connections exhausted", Lesson: "Bound query concurrency to available pool capacity", Source: "incident-42", Features: p.Features, Requires: map[string][]string{"database": {"postgres"}}, Created: time.Now().UTC(), Updated: time.Now().UTC()}
	return id, p, m
}
func TestIsolationAndApplicability(t *testing.T) {
	id, p, m := fixture()
	all := []Memory{m}
	for i, change := range []func(*Memory){func(x *Memory) { x.Tenant = "other" }, func(x *Memory) { x.Owner = "bob" }, func(x *Memory) { x.Scope = "project" }, func(x *Memory) { x.Scope = "team"; x.Team = "other"; x.Approved = true }, func(x *Memory) { x.Scope = "team"; x.Team = "platform" }, func(x *Memory) { x.Retired = true }, func(x *Memory) { x.Requires = map[string][]string{"cloud": {"azure"}} }, func(x *Memory) { x.Excludes = map[string][]string{"database": {"postgres"}} }} {
		x := m
		x.ID = fmt.Sprint(i)
		change(&x)
		all = append(all, x)
	}
	r := Recall(all, id, Request{Profile: p, Task: "query connections"}, time.Now())
	if len(r.Hits) != 1 || r.Hits[0].ID != "a" {
		t.Fatalf("leaked inapplicable memory: %+v", r.Hits)
	}
}
func TestTeamAndBudget(t *testing.T) {
	id, p, m := fixture()
	m.Scope = "team"
	m.Team = "platform"
	m.Owner = "bob"
	m.Approved = true
	for b := 1; b < 2000; b += 17 {
		r := Recall([]Memory{m}, id, Request{Profile: p, ByteBudget: b, Initialize: true}, time.Now())
		if len(r.Context) > b || r.Bytes != len(r.Context) {
			t.Fatal("budget overflow")
		}
	}
	if len(Recall([]Memory{m}, id, Request{Profile: p, Initialize: true}, time.Now()).Hits) != 1 {
		t.Fatal("approved peer should be visible")
	}
}
func TestRankingFeedbackAndConflict(t *testing.T) {
	id, p, m := fixture()
	m.Requires = nil
	m.Subject = "pool"
	n := m
	n.ID = "b"
	n.Lesson = "Use an unbounded connection pool"
	n.Features = map[string][]string{"language": {"typescript"}}
	r := Recall([]Memory{n, m}, id, Request{Profile: p, Task: "query concurrency pool"}, time.Now())
	if r.Hits[0].ID != "a" || !r.Hits[0].Conflict {
		t.Fatalf("ranking/conflict: %+v", r)
	}
	m.Helpful = 10
	a := Recall([]Memory{m}, id, Request{Profile: p, Initialize: true}, time.Now()).Hits[0]
	m.Helpful = 0
	m.Unhelpful = 10
	b := Recall([]Memory{m}, id, Request{Profile: p, Initialize: true}, time.Now()).Hits[0]
	if a.Score <= b.Score {
		t.Fatal("usefulness should affect ranking")
	}
}
func TestJournalLifecycle(t *testing.T) {
	id, p, m := fixture()
	s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	a, e := s.Record(m)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.Record(m)
	if e != nil || a.ID != b.ID {
		t.Fatal("duplicate learning")
	}
	for i := 0; i < 2; i++ {
		if e = s.Feedback(id, p.Project, a.ID, "run-1", true); e != nil {
			t.Fatal(e)
		}
	}
	all, e := s.All()
	if e != nil || len(all) != 1 || all[0].Helpful != 1 {
		t.Fatalf("feedback dedup: %+v %v", all, e)
	}
	if _, e = s.Recall(id, Request{Profile: p, Initialize: true}); e != nil {
		t.Fatal(e)
	}
	d, e := s.Dashboard(id, p)
	if e != nil || len(d.Usage) != 1 {
		t.Fatalf("usage: %+v %v", d, e)
	}
	other := id
	other.User = "bob"
	if e = s.Retire(other, a.ID); e == nil {
		t.Fatal("peer retired owner's memory")
	}
	if e = s.Retire(id, a.ID); e != nil {
		t.Fatal(e)
	}
	all, _ = s.All()
	if len(Recall(all, id, Request{Profile: p, Initialize: true}, time.Now()).Hits) != 0 {
		t.Fatal("retired memory retrieved")
	}
}
func TestCorruptAndLockedJournal(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	if e := os.WriteFile(s.Path, []byte("{partial"), 0600); e != nil {
		t.Fatal(e)
	}
	_, _, m := fixture()
	if _, e := s.Record(m); e == nil {
		t.Fatal("must not append to corrupt journal")
	}
	if e := os.Mkdir(s.Path+".lock", 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := s.All(); e == nil {
		t.Fatal("must respect writer lock")
	}
}
func TestManifestProfile(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"dependencies":{"next":"1","pg":"1","@aws-sdk/client-s3":"1"},"devDependencies":{"typescript":"1"}}`), 0600)
	os.WriteFile(filepath.Join(root, ".agent-memory.json"), []byte(`{"features":{"app":["analytics-dashboard"],"workflow":["stacked-prs"]}}`), 0600)
	p, e := ProfileProject(root, "app")
	if e != nil || !overlap(p.Features["cloud"], []string{"aws"}) || !overlap(p.Features["framework"], []string{"nextjs"}) || !overlap(p.Features["workflow"], []string{"stacked-prs"}) {
		t.Fatalf("profile: %+v %v", p, e)
	}
	os.Remove(filepath.Join(root, "package.json"))
	os.Symlink("/etc/passwd", filepath.Join(root, "package.json"))
	if _, e = ProfileProject(root, "app"); e == nil {
		t.Fatal("symlink must fail closed")
	}
}
func TestMCP(t *testing.T) {
	root := t.TempDir()
	id, _, _ := fixture()
	svc := Service{Store: Store{Path: filepath.Join(root, "journal")}, Identity: id, Root: root, Project: "app"}
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"record_memory","arguments":{"tenant":"other","owner":"bob","approved":true,"scope":"team","outcome":"good","incident":"Observed win","lesson":"Keep evidence","source":"test"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"init_memory","arguments":{"task":"evidence"}}}
{bad
`
	var out bytes.Buffer
	if e := ServeMCP(svc, strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("notification reply or missing response: %s", out.String())
	}
	for _, line := range lines {
		var v map[string]any
		if e := json.Unmarshal([]byte(line), &v); e != nil {
			t.Fatal(e)
		}
	}
	all, e := svc.Store.All()
	if e != nil || len(all) != 1 || all[0].Tenant != "acme" || all[0].Owner != "alice" || all[0].Approved {
		t.Fatalf("agent escalated identity/approval: %+v %v", all, e)
	}
}
func TestDashboardBoundaries(t *testing.T) {
	root := t.TempDir()
	id, _, _ := fixture()
	h := DashboardHandler(Service{Store: Store{Path: filepath.Join(root, "events")}, Identity: id, Root: root, Project: "app"}, "127.0.0.1:7331", "test-token")
	for _, test := range []struct {
		host, origin, token string
		want                int
	}{{"evil.example", "", "test-token", 403}, {"127.0.0.1:7331", "https://evil.example", "test-token", 403}, {"127.0.0.1:7331", "", "", 401}, {"127.0.0.1:7331", "", "test-token", 200}} {
		r := httptest.NewRequest("GET", "http://"+test.host+"/api/dashboard", nil)
		r.Header.Set("Origin", test.origin)
		if test.token != "" {
			r.Header.Set("Authorization", "Bearer "+test.token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("got %d want %d", w.Code, test.want)
		}
	}
}
func TestTeamImportReview(t *testing.T) {
	root := t.TempDir()
	id, _, m := fixture()
	s := Service{Store: Store{Path: filepath.Join(root, "events")}, Identity: id, Root: root, Project: "app"}
	m.Scope = "team"
	m.Team = "platform"
	m.Owner = "bob"
	m.Approved = true
	ids, e := ImportTeam(s, []Memory{m})
	if e != nil {
		t.Fatal(e)
	}
	all, _ := s.Store.All()
	if all[0].Approved {
		t.Fatal("import bypassed review")
	}
	if e = s.Store.Approve(id, ids[0]); e != nil {
		t.Fatal(e)
	}
	ids2, e := ImportTeam(s, []Memory{m})
	if e != nil || ids[0] != ids2[0] {
		t.Fatal("import dedup failed")
	}
	export, e := ExportTeam(s)
	if e != nil || len(export) != 1 {
		t.Fatalf("team export: %+v %v", export, e)
	}
}
func BenchmarkRecall1000(b *testing.B) {
	id, p, m := fixture()
	all := make([]Memory, 1000)
	for i := range all {
		all[i] = m
		all[i].ID = fmt.Sprint(i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Recall(all, id, Request{Profile: p, Task: "connection concurrency"}, time.Now())
	}
}
