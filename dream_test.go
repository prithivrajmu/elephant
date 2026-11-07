package memory

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func dreamFixture(t *testing.T) (Service, Memory) {
	t.Helper()
	id, _, m := fixture()
	s := Service{Store: Store{Path: filepath.Join(t.TempDir(), "dream.sqlite")}, Identity: id, Project: "p", Root: t.TempDir()}
	m.Scope, m.Project = "project", s.Project
	m, err := s.Store.Record(m)
	if err != nil {
		t.Fatal(err)
	}
	return s, m
}

func dreamReplacement(inputs ...Memory) Memory {
	m := inputs[0]
	m.Incident = "A source check confirmed the pool limit."
	m.Lesson = "Limit active queries to the pool size."
	m.Source = "test:pool-capacity"
	m.Dream = &DreamProvenance{Kind: "correct", Checks: []DreamCheck{}}
	m.Supersedes = nil
	if len(inputs) > 1 {
		m.Dream.Kind = "consolidate"
	}
	for _, old := range inputs {
		m.Dream.Checks = append(m.Dream.Checks, DreamCheck{ID: old.ID, Revision: dreamRevision(old), Source: m.Source, Observation: "The pool capacity test passed with bounded queries."})
		m.Supersedes = append(m.Supersedes, old.ID)
	}
	return m
}

func TestDreamIsolationPaginationAndBudget(t *testing.T) {
	s, first := dreamFixture(t)
	// Bypass admission only to create isolated retrieval fixtures.
	for i, mutate := range []func(*Memory){
		func(m *Memory) { m.Owner = "bob" },
		func(m *Memory) { m.Tenant = "other" },
		func(m *Memory) { m.Project = "other" },
		func(m *Memory) { m.Scope = "team"; m.Team = s.Identity.Team; m.Approved = true },
		func(m *Memory) { m.Retired = true },
		func(m *Memory) { m.Scope = "conversation"; m.Conversation = "other" },
	} {
		m := first
		m.ID = string(rune('a' + i))
		mutate(&m)
		if err := s.Store.transact(func(_ []Memory, _ map[string]bool) (*Event, error) { return &Event{Kind: "put", Memory: &m}, nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, budget := range []int{2048, 4000, 16000, 64000} {
		batch, err := s.dream(DreamRequest{ByteBudget: budget})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(batch)
		if len(b) > budget || len(batch.Candidates) != 1 || batch.Candidates[0].Memory.ID != first.ID || batch.Candidates[0].Revision != dreamRevision(first) {
			t.Fatalf("budget or isolation failure: %s", b)
		}
	}
	second := first
	second.ID, second.Scope, second.Project = "zz-last", "personal", "other"
	if err := s.Store.transact(func(_ []Memory, _ map[string]bool) (*Event, error) { return &Event{Kind: "put", Memory: &second}, nil }); err != nil {
		t.Fatal(err)
	}
	page, err := s.dream(DreamRequest{Limit: 1})
	if err != nil || len(page.Candidates) != 1 || page.NextAfter != first.ID {
		t.Fatalf("first page: %+v %v", page, err)
	}
	page, err = s.dream(DreamRequest{Limit: 1, After: page.NextAfter})
	if err != nil || len(page.Candidates) != 1 || page.Candidates[0].Memory.ID != second.ID || page.NextAfter != "" {
		t.Fatalf("last page: %+v %v", page, err)
	}
	for _, q := range []DreamRequest{{ByteBudget: 1}, {ByteBudget: 64001}, {Limit: -1}, {Limit: 21}} {
		if _, err := s.dream(q); err == nil {
			t.Fatalf("accepted invalid request: %+v", q)
		}
	}
}

func TestDreamOversizedPaginationPreservesBudgetAndCoverage(t *testing.T) {
	s, original := dreamFixture(t)
	for i := 0; i < 24; i++ {
		m := original
		m.ID = fmt.Sprintf("%03d-%s", i, strings.Repeat("x", 220))
		if i%3 != 0 {
			// JSON escaping must count toward the byte budget too.
			m.Lesson = strings.Repeat("\"<large>\" ", 400)
		}
		if err := s.Store.transact(func([]Memory, map[string]bool) (*Event, error) {
			return &Event{Kind: "put", Memory: &m}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, budget := range []int{2048, 3000, 16000, 64000} {
		for _, limit := range []int{1, 3, 20} {
			t.Run(fmt.Sprintf("budget=%d/limit=%d", budget, limit), func(t *testing.T) {
				seen := map[string]bool{}
				after := ""
				for page := 0; ; page++ {
					if page >= 25 {
						t.Fatal("pagination did not finish")
					}
					batch, err := s.dream(DreamRequest{ByteBudget: budget, Limit: limit, After: after})
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(batch)
					if err != nil || len(encoded) > budget || len(batch.Candidates)+len(batch.Skipped) > limit {
						t.Fatalf("page exceeded limits: bytes=%d candidates=%d skipped=%d err=%v", len(encoded), len(batch.Candidates), len(batch.Skipped), err)
					}
					ids := []string{}
					for _, candidate := range batch.Candidates {
						ids = append(ids, candidate.Memory.ID)
						if candidate.Revision != dreamRevision(candidate.Memory) {
							t.Fatal("candidate was truncated")
						}
					}
					for _, skipped := range batch.Skipped {
						ids = append(ids, skipped.ID)
						if skipped.RequiredBytes <= budget {
							t.Fatalf("skipped a memory that fits: %+v", skipped)
						}
					}
					for _, id := range ids {
						if seen[id] || id <= after {
							t.Fatalf("repeated memory %s", id)
						}
						seen[id] = true
					}
					if batch.NextAfter == "" {
						break
					}
					if batch.NextAfter <= after || !seen[batch.NextAfter] {
						t.Fatal("cursor did not advance past a reported memory")
					}
					after = batch.NextAfter
				}
				if len(seen) != 25 {
					t.Fatalf("lost memories: saw %d of 25", len(seen))
				}
			})
		}
	}
	all, err := s.Store.All()
	if err != nil || len(all) != 25 {
		t.Fatalf("preparation changed the store: count=%d err=%v", len(all), err)
	}
	for _, m := range all {
		if m.Retired {
			t.Fatal("preparation retired a memory")
		}
	}
}

func TestDreamCorrectionIsAtomicAndRetrySafe(t *testing.T) {
	s, old := dreamFixture(t)
	m := dreamReplacement(old)
	b, _ := json.Marshal(m)
	v, err := s.Call("commit_dream", b)
	if err != nil {
		t.Fatal(err)
	}
	saved := v.(Memory)
	if saved.Helpful != 0 || saved.Unhelpful != 0 || saved.Dream == nil || saved.ID == old.ID {
		t.Fatalf("invalid derived memory: %+v", saved)
	}
	v, err = s.Call("commit_dream", b)
	if err != nil || v.(Memory).ID != saved.ID {
		t.Fatalf("retry duplicated the dream: %v %v", v, err)
	}
	all, err := s.Store.All()
	if err != nil || len(all) != 2 {
		t.Fatalf("lost history: %v %v", all, err)
	}
	for _, m := range all {
		if m.ID == old.ID && (!m.Retired || m.Lesson != old.Lesson || m.Source != old.Source) {
			t.Fatal("original evidence was lost")
		}
	}
	profile := Profile{Project: s.Project, Features: old.Features}
	r := Recall(all, s.Identity, Request{Profile: profile, Task: "pool capacity active queries"}, saved.Created)
	if len(r.Hits) != 1 || r.Hits[0].ID != saved.ID {
		t.Fatalf("recall did not use the correction: %+v", r)
	}
}

func TestDreamConsolidatesCompatibleInputs(t *testing.T) {
	s, old := dreamFixture(t)
	second := old
	second.Lesson, second.Incident, second.Source = "Check the pool limit before each batch.", "A batch ran too many queries.", "test:second-batch"
	second.DistinctFrom = []string{old.ID}
	second, err := s.Store.Record(second)
	if err != nil {
		t.Fatal(err)
	}
	m := dreamReplacement(old, second)
	saved, err := s.Store.Record(m)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := s.Store.All()
	active := 0
	for _, m := range all {
		if !m.Retired {
			active++
			if m.ID != saved.ID {
				t.Fatal("input was not retired")
			}
		}
	}
	if active != 1 || len(all) != 3 || len(saved.Dream.Checks) != 2 {
		t.Fatalf("bad consolidation: %+v", all)
	}
}

func TestDreamRejectsUnsafeChangesWithoutRetiringInputs(t *testing.T) {
	for name, mutate := range map[string]func(*Memory){
		"stale":         func(m *Memory) { m.Dream.Checks[0].Revision = strings.Repeat("0", 64) },
		"scope":         func(m *Memory) { m.Scope = "personal" },
		"project":       func(m *Memory) { m.Project = "other" },
		"conditions":    func(m *Memory) { m.Requires = nil },
		"features":      func(m *Memory) { m.Features = nil },
		"source":        func(m *Memory) { m.Source = "unchecked-source" },
		"missing-check": func(m *Memory) { m.Dream.Checks = nil },
		"duplicate-check": func(m *Memory) {
			m.Dream.Checks = append(m.Dream.Checks, m.Dream.Checks[0])
			m.Dream.Kind = "consolidate"
		},
		"missing-retirement": func(m *Memory) { m.Supersedes = nil },
		"foreign-input":      func(m *Memory) { m.Dream.Checks[0].ID = "missing"; m.Supersedes = []string{"missing"} },
		"secret":             func(m *Memory) { m.Dream.Checks[0].Observation = "ghp_" + strings.Repeat("A", 40) },
		"invalid-lesson":     func(m *Memory) { m.Lesson = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s, old := dreamFixture(t)
			m := dreamReplacement(old)
			mutate(&m)
			if _, err := s.Store.Record(m); err == nil {
				t.Fatal("unsafe dream accepted")
			}
			all, err := s.Store.All()
			if err != nil || len(all) != 1 || all[0].Retired {
				t.Fatalf("failed dream changed inputs: %+v %v", all, err)
			}
		})
	}
}

func TestDreamDetectsFeedbackAndRetirementDuringReview(t *testing.T) {
	for _, retire := range []bool{false, true} {
		s, old := dreamFixture(t)
		m := dreamReplacement(old)
		var err error
		if retire {
			err = s.Store.Retire(s.Identity, old.ID)
		} else {
			err = s.Store.Feedback(s.Identity, s.Project, old.ID, "observed-run", false)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Store.Record(m); err == nil {
			t.Fatal("stale review accepted")
		}
	}
}

// The backend deliberately implements only the original lifecycle interface.
type noDreamBackend struct{ MemoryBackend }

func TestDreamUsesConfiguredBackendAndRequiresProvenance(t *testing.T) {
	s, old := dreamFixture(t)
	s.Backend = noDreamBackend{s.Store}
	if _, err := s.Call("dream_memory", nil); err == nil {
		t.Fatal("fell back to local store")
	}
	m := dreamReplacement(old)
	b, _ := json.Marshal(m)
	if _, err := s.Call("commit_dream", b); err == nil {
		t.Fatal("unsupported backend accepted commit")
	}
	s.Backend = nil
	if _, err := s.Call("record_memory", b); err == nil {
		t.Fatal("ordinary recording accepted a dream")
	}
	m.Dream = nil
	b, _ = json.Marshal(m)
	if _, err := s.Call("commit_dream", b); err == nil {
		t.Fatal("commit accepted without provenance")
	}
}

func TestDreamCloudSyncCannotDropProvenance(t *testing.T) {
	s, old := dreamFixture(t)
	if _, err := s.Store.Record(dreamReplacement(old)); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.checkDreamSync(s.Identity, map[string]bool{"project": true}); err == nil {
		t.Fatal("cloud sync would flatten dream provenance")
	}
	if err := s.Store.checkDreamSync(s.Identity, map[string]bool{"personal": true}); err != nil {
		t.Fatal("unrelated scope blocked", err)
	}
}
