package memory

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnattachedWithoutReadableRoot(t *testing.T) {
	id, _, m := fixture()
	svc := Service{Store: Store{Path: filepath.Join(t.TempDir(), "events")}, Identity: id, Unattached: true, Conversation: "conversation-42", Root: "/does-not-exist", Project: "should-not-leak"}
	m.Requires = nil
	m.Features = nil
	m.Incident = "Review feedback requested proof"
	m.Lesson = "Attach validation evidence to reviews"
	b, _ := json.Marshal(m)
	v, e := svc.Call("record_memory", b)
	if e != nil {
		t.Fatal(e)
	}
	stored := v.(Memory)
	if stored.Project != "" || stored.Conversation != "conversation-42" || len(stored.Features) != 0 {
		t.Fatalf("ambient project leaked: %+v", stored)
	}
	v, e = svc.Call("recall_memory", json.RawMessage(`{"task":"validation evidence"}`))
	if e != nil || len(v.(Result).Hits) != 1 {
		t.Fatalf("unattached recall: %+v %v", v, e)
	}
}
func TestManyAlternativesStillFit(t *testing.T) {
	id, p, m := fixture()
	m.Requires = nil
	m.Subject = "pool"
	all := make([]Memory, 150)
	for i := range all {
		all[i] = m
		all[i].ID = fmt.Sprintf("%032d", i)
		all[i].Lesson = fmt.Sprintf("Query concurrency lesson %d", i)
	}
	r := Recall(all, id, Request{Profile: p, Task: "query concurrency", ByteBudget: 4000}, time.Now())
	if len(r.Hits) == 0 || r.Bytes > 4000 || r.Hits[0].AlternativeCount != 149 || len(r.Hits[0].Alternatives) > 5 {
		t.Fatalf("alternatives consumed budget: %+v", r)
	}
}
func TestNoEmptyScopeCollisions(t *testing.T) {
	id, _, m := fixture()
	m.Project = ""
	m.Scope = "project"
	if Validate(m) == nil || visible(m, id, "") {
		t.Fatal("empty project scope must fail closed")
	}
	m.Scope = "conversation"
	m.Conversation = ""
	if Validate(m) == nil || visible(m, id, "", "") {
		t.Fatal("empty conversation scope must fail closed")
	}
	m.Conversation = "c1"
	if visible(m, id, "", "c2") || !visible(m, id, "", "c1") {
		t.Fatal("conversation scope mismatch")
	}
	other := id
	other.User = "peer"
	if visible(m, other, "", "c1") {
		t.Fatal("conversation owner leaked")
	}
}
func TestTaskAdmissionIndependentOfCorpusAndStack(t *testing.T) {
	id, p, m := fixture()
	if len(Recall([]Memory{m}, id, Request{Profile: p, Task: "rewrite marketing headline"}, time.Now()).Hits) > 0 {
		t.Fatal("same stack rescued irrelevant lesson")
	}
	m.Project = ""
	m.Requires = nil
	m.Features = nil
	m.Incident = "evidence"
	m.Lesson = "proof"
	m.Subject = ""
	q := Request{Profile: Profile{Features: map[string][]string{}}, Task: "evidence"}
	a := Recall([]Memory{m}, id, q, time.Now())
	n := m
	n.ID = "b"
	n.Incident = "flowers"
	n.Lesson = "gardening"
	b := Recall([]Memory{n, m}, id, q, time.Now())
	if len(a.Hits) != 1 || len(b.Hits) != 1 {
		t.Fatalf("corpus controls admission: %+v %+v", a, b)
	}
	q.Task = "please help with this"
	if len(Recall([]Memory{m}, id, q, time.Now()).Hits) != 0 {
		t.Fatal("stopword-only request admitted")
	}
	q.Task = ""
	q.Initialize = true
	if len(Recall([]Memory{m}, id, q, time.Now()).Hits) != 0 {
		t.Fatal("empty initialization must not dump general memories")
	}
}
func TestKnownAgreementAndCoverage(t *testing.T) {
	a := map[string][]string{"database": {"postgres"}, "cloud": {"aws"}}
	b := map[string][]string{"database": {"postgres"}}
	s, c, _ := similarityDetails(a, b)
	if s != 1 || c != 2.5/4 {
		t.Fatalf("unknown vs known: %f %f", s, c)
	}
	b["framework"] = []string{"nextjs"}
	s2, c2, _ := similarityDetails(a, b)
	if s2 != s || c2 != c {
		t.Fatal("query-only fields diluted agreement")
	}
}
func TestRenderedConditionsAndDeterminism(t *testing.T) {
	id, p, m := fixture()
	m.Lesson = "Bound query concurrency safely — அளவு"
	m.Excludes = map[string][]string{"cloud": {"azure"}}
	n := m
	n.ID = "b"
	n.Source = "other-evidence"
	q := Request{Profile: p, Task: "query concurrency", ByteBudget: 1000}
	a := Recall([]Memory{m, n}, id, q, time.Now())
	b := Recall([]Memory{n, m}, id, q, time.Now())
	if a.Context != b.Context {
		t.Fatal("input order changed retrieval")
	}
	if !strings.Contains(a.Context, `requires={"database":["postgres"]}`) || !strings.Contains(a.Context, `excludes={"cloud":["azure"]}`) || a.Bytes > q.ByteBudget {
		t.Fatalf("conditions lost or budget exceeded: %s", a.Context)
	}
}
func TestBudgetOmittedAlternativesIdentifiable(t *testing.T) {
	id, p, m := fixture()
	m.Requires = nil
	m.Subject = "pool"
	n := m
	n.ID = "b"
	n.Lesson = "Query concurrency may be unbounded"
	q := Request{Profile: p, Task: "query concurrency", Limit: 1}
	r := Recall([]Memory{m, n}, id, q, time.Now())
	if len(r.Hits) != 1 || len(r.Hits[0].Alternatives) != 1 || !strings.Contains(r.Context, "alternatives may be omitted") {
		t.Fatalf("missing alternative metadata: %+v", r)
	}
}
func TestUnifiedBaselineRendering(t *testing.T) {
	id, p, m := fixture()
	m.Subject = "pool"
	n := m
	n.ID = "b"
	n.Lesson = "Different query concurrency policy"
	s := Store{Path: filepath.Join(t.TempDir(), "events")}
	s.Record(m)
	s.Record(n)
	q := Request{Profile: p, Task: "query concurrency", ByteBudget: 60000, Limit: 20}
	r, e := s.Recall(id, q)
	if e != nil || len(r.Hits) != 2 {
		t.Fatalf("recall: %+v %v", r, e)
	}
	d, e := s.Dashboard(id, p)
	if e != nil || d.Usage[0].BaselineBytes != d.Usage[0].InjectedBytes {
		t.Fatalf("baseline serializer mismatch: %+v %v", d.Usage, e)
	}
}
func TestOutcomeExtremityNotRankingBoost(t *testing.T) {
	id, p, m := fixture()
	q := Request{Profile: p, Task: "query concurrency"}
	a := Recall([]Memory{m}, id, q, time.Now()).Hits[0].Score
	m.Outcome = "good"
	b := Recall([]Memory{m}, id, q, time.Now()).Hits[0].Score
	if a-b > 0.000001 || b-a > 0.000001 {
		t.Fatal("dramatic outcome got a boost")
	}
}
func TestExplicitContextInUnattachedMode(t *testing.T) {
	id, _, m := fixture()
	svc := Service{Store: Store{Path: filepath.Join(t.TempDir(), "events")}, Identity: id, Unattached: true, Conversation: "c1", Root: "/missing"}
	m.Project = ""
	m.Features = nil
	m.Requires = map[string][]string{"database": {"postgres"}}
	svc.Store.Record(m)
	v, e := svc.Call("recall_memory", json.RawMessage(`{"task":"query concurrency"}`))
	if e != nil || len(v.(Result).Hits) > 0 {
		t.Fatal("unknown required context admitted")
	}
	v, e = svc.Call("recall_memory", json.RawMessage(`{"task":"query concurrency","context_features":{"database":["postgres"]}}`))
	if e != nil || len(v.(Result).Hits) != 1 {
		t.Fatalf("explicit context rejected: %+v %v", v, e)
	}
}
