package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDreamEvalCandidateViewAndNoWrites(t *testing.T) {
	s, old := dreamFixture(t)
	p := idleProposal(old)
	probes := []DreamProbe{{Name: "corrected pool rule", Task: "pool capacity active queries", Features: old.Features, Contains: []string{p.Lesson}}}
	before, _ := s.Store.All()
	result, err := s.EvaluateDream(DreamEvalRequest{Replacements: []IdleDreamProposal{p}, Probes: probes})
	if err != nil || !result.Passed || result.BeforePassed != 0 || result.AfterPassed != 1 {
		t.Fatalf("eval: %+v %v", result, err)
	}
	after, _ := s.Store.All()
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("read-only eval changed memories")
	}
	db, err := s.Store.database(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err = db.QueryRow("SELECT count(*) FROM events WHERE kind='recall'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("eval manufactured usage")
	}
	p.Checks[0].Revision = "stale"
	if _, err = s.EvaluateDream(DreamEvalRequest{Replacements: []IdleDreamProposal{p}, Probes: probes}); err == nil {
		t.Fatal("accepted stale proposal")
	}
}
func TestDreamEvalDetectsLostExceptionAndApplicability(t *testing.T) {
	s, old := dreamFixture(t)
	p := idleProposal(old)
	probes := []DreamProbe{{Name: "lost original detail", Task: old.Lesson, Features: old.Features, Contains: []string{old.Lesson}}}
	r, err := s.EvaluateDream(DreamEvalRequest{Replacements: []IdleDreamProposal{p}, Probes: probes})
	if err != nil || r.Passed || r.Regressions != 1 {
		t.Fatalf("lost detail: %+v %v", r, err)
	}
	probes = []DreamProbe{{Name: "wrong dependency", Task: "pool capacity active queries", Features: map[string][]string{"database": {"sqlite"}}, Absent: []string{p.Lesson}}}
	r, err = s.EvaluateDream(DreamEvalRequest{Replacements: []IdleDreamProposal{p}, Probes: probes})
	if err != nil || !r.Passed {
		t.Fatalf("applicability: %+v %v", r, err)
	}
	for _, q := range []DreamEvalRequest{{Replacements: []IdleDreamProposal{}, Probes: nil}, {Replacements: []IdleDreamProposal{}, Probes: probes, ByteBudget: 1}, {Replacements: nil, Probes: probes}} {
		if _, err = s.EvaluateDream(q); err == nil {
			t.Fatal("accepted invalid eval")
		}
	}
}
func TestIdleDreamProbeGate(t *testing.T) {
	for _, kind := range []string{"pass", "fail", "missing", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			c, path, old, lease := idleFixture(t)
			proposal := idleProposal(old)
			probe := DreamProbe{Name: "pool", Task: "pool capacity active queries", Features: old.Features, Contains: []string{proposal.Lesson}}
			if kind == "fail" {
				probe.Contains = []string{"UNSUPPORTED EXPECTATION"}
			}
			c.IdleDream.ProbeFile = filepath.Join(c.Root, "probes.json")
			b, _ := json.Marshal([]DreamProbe{probe})
			if kind == "invalid" {
				b = []byte("null")
			}
			if kind != "missing" {
				if err := os.WriteFile(c.IdleDream.ProbeFile, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeIdleConfig(t, path, c)
			err := runIdleDream(context.Background(), path, c, lease, func(context.Context, AutomationConfig, DreamBatch) (IdleDreamOutput, error) {
				return IdleDreamOutput{Replacements: []IdleDreamProposal{proposal}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			status, err := DreamScheduleStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "eval_failed"
			if kind == "pass" {
				want = "saved"
			}
			if status.State.LastResult != want {
				t.Fatalf("got %s want %s", status.State.LastResult, want)
			}
			all, _ := c.Service().Store.All()
			if kind != "pass" && (len(all) != 1 || all[0].Retired) {
				t.Fatal("failed eval changed memories")
			}
		})
	}
}
func TestDreamRiskOrderPreservesCoverage(t *testing.T) {
	s, first := dreamFixture(t)
	second := first
	second.ID = "zz-risk"
	second.Unhelpful = 2
	if err := s.Store.transact(func([]Memory, map[string]bool) (*Event, error) { return &Event{Kind: "put", Memory: &second}, nil }); err != nil {
		t.Fatal(err)
	}
	normal, err := s.dream(DreamRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	risk, err := s.dream(DreamRequest{Limit: 2, ReviewOrder: "risk"})
	if err != nil {
		t.Fatal(err)
	}
	if normal.Candidates[0].Memory.ID != first.ID || risk.Candidates[0].Memory.ID != second.ID || normal.NextAfter != risk.NextAfter {
		t.Fatal("risk order or cursor failure")
	}
	if _, err = s.dream(DreamRequest{ReviewOrder: "unknown"}); err == nil {
		t.Fatal("invalid order accepted")
	}
}

func TestDreamEvalIsolationAndInputValidation(t *testing.T) {
	s, old := dreamFixture(t)
	for i, mutate := range []func(*Memory){
		func(m *Memory) { m.Owner = "other" },
		func(m *Memory) { m.Tenant = "other" },
		func(m *Memory) { m.Project = "other" },
		func(m *Memory) { m.Scope = "team"; m.Team = "eval-team"; m.Approved = true },
		func(m *Memory) { m.Scope = "conversation"; m.Conversation = "other" },
	} {
		m := old
		m.ID = fmt.Sprintf("foreign-%d", i)
		m.Lesson = "FOREIGN secret pool capacity"
		mutate(&m)
		if err := s.Store.transact(func([]Memory, map[string]bool) (*Event, error) { return &Event{Kind: "put", Memory: &m}, nil }); err != nil {
			t.Fatal(err)
		}
	}
	probes := []DreamProbe{{Name: "private boundary", Task: "pool capacity secret", Features: old.Features, Absent: []string{"FOREIGN"}}}
	result, err := s.EvaluateDream(DreamEvalRequest{Replacements: []IdleDreamProposal{}, Probes: probes})
	if err != nil || !result.Passed {
		t.Fatalf("isolation: %+v %v", result, err)
	}
	for _, data := range []string{`null`, `[]`, `[{"name":"x","task":"pool","contains":[""]}]`, `[{"name":"x","task":"pool","unknown":true,"absent":["x"]}]`, `[] {}`} {
		path := filepath.Join(t.TempDir(), "probes.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadDreamProbes(path); err == nil {
			t.Fatalf("accepted invalid probes %s", data)
		}
	}
}
