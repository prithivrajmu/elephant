package memory

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// DreamProbe is an explicitly authored retrieval contract, not a factual judge.
// Expected fragments should come from original evidence, independently of proposals.
type DreamProbe struct {
	Name     string              `json:"name"`
	Task     string              `json:"task"`
	Features map[string][]string `json:"features,omitempty"`
	Contains []string            `json:"contains,omitempty"`
	Absent   []string            `json:"absent,omitempty"`
}
type DreamEvalRequest struct {
	Replacements []IdleDreamProposal `json:"replacements"`
	Probes       []DreamProbe        `json:"probes"`
	ByteBudget   int                 `json:"byte_budget,omitempty"`
	Limit        int                 `json:"limit,omitempty"`
}
type DreamProbeResult struct {
	Name        string `json:"name"`
	Before      bool   `json:"before"`
	After       bool   `json:"after"`
	BeforeBytes int    `json:"before_bytes"`
	AfterBytes  int    `json:"after_bytes"`
}
type DreamEvalResult struct {
	Passed       bool               `json:"passed"`
	BeforePassed int                `json:"before_passed"`
	AfterPassed  int                `json:"after_passed"`
	Regressions  int                `json:"regressions"`
	Probes       []DreamProbeResult `json:"probes"`
}

// ReadDreamProbes reads trusted local configuration. It never follows source
// pointers from memories, invokes a model, or accepts executable probe content.
func ReadDreamProbes(path string) ([]DreamProbe, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open dream probes")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(b) > 65536 {
		return nil, fmt.Errorf("dream probe file exceeds 64 KiB or cannot be read")
	}
	var probes []DreamProbe
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(&probes); err != nil {
		return nil, fmt.Errorf("invalid dream probe file")
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing dream probe data")
	}
	if err = validateDreamProbes(probes); err != nil {
		return nil, err
	}
	return probes, nil
}
func validateDreamProbes(probes []DreamProbe) error {
	if len(probes) == 0 || len(probes) > 32 {
		return fmt.Errorf("dream eval requires 1..32 probes")
	}
	seen := map[string]bool{}
	for _, p := range probes {
		if strings.TrimSpace(p.Name) == "" || len(p.Name) > 128 || seen[p.Name] || strings.TrimSpace(p.Task) == "" || len(p.Task) > 2048 || len(p.Contains)+len(p.Absent) == 0 || len(p.Contains)+len(p.Absent) > 16 {
			return fmt.Errorf("invalid dream probe")
		}
		seen[p.Name] = true
		if err := ValidateLabels(p.Features); err != nil {
			return err
		}
		for _, s := range append(append([]string{}, p.Contains...), p.Absent...) {
			if strings.TrimSpace(s) == "" || len(s) > 512 {
				return fmt.Errorf("invalid dream probe fragment")
			}
		}
	}
	return nil
}

// EvaluateDream validates proposals and evaluates a candidate view through the
// production Recall function. It never writes memories, usage, or feedback.
func (s Service) EvaluateDream(q DreamEvalRequest) (DreamEvalResult, error) {
	var out DreamEvalResult
	if s.Backend != nil {
		return out, fmt.Errorf("dream eval requires the local store")
	}
	if err := validateDreamProbes(q.Probes); err != nil {
		return out, err
	}
	if q.ByteBudget == 0 {
		q.ByteBudget = 4000
	}
	if q.Limit == 0 {
		q.Limit = 6
	}
	if q.ByteBudget < 512 || q.ByteBudget > 64000 || q.Limit < 1 || q.Limit > 20 {
		return out, fmt.Errorf("invalid dream eval recall limits")
	}
	if s.Identity.Tenant == "" || s.Identity.User == "" {
		return out, fmt.Errorf("identity required")
	}
	db, err := s.Store.database(false)
	if err != nil {
		return out, err
	}
	defer db.Close()
	all, err := readMemoryQuery(db, `SELECT data FROM memories WHERE tenant=? AND owner=? AND json_extract(data,'$.retired')=0 AND
 (scope='personal' OR (scope='project' AND project=? AND project<>'') OR
 (scope='conversation' AND json_extract(data,'$.conversation')=? AND ?<>'')) ORDER BY id LIMIT 1001`, s.Identity.Tenant, s.Identity.User, s.Project, s.Conversation, s.Conversation)
	if err != nil {
		return out, err
	}
	if len(all) > 1000 {
		return out, fmt.Errorf("dream eval snapshot exceeds 1000 memories")
	}
	b, _ := json.Marshal(all)
	if len(b) > 8*1024*1024 {
		return out, fmt.Errorf("dream eval snapshot exceeds 8 MiB")
	}
	batch := DreamBatch{Candidates: []DreamCandidate{}}
	for _, m := range all {
		batch.Candidates = append(batch.Candidates, DreamCandidate{Memory: m, Revision: dreamRevision(m)})
	}
	replacements, err := s.prepareIdleReplacements(IdleDreamOutput{Replacements: q.Replacements}, batch, 3)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC()
	retired := map[string]bool{}
	after := append([]Memory{}, all...)
	for i, m := range replacements {
		for _, id := range m.Supersedes {
			retired[id] = true
		}
		m.ID = fmt.Sprintf("dream-eval-%d", i)
		m.Created = now
		m.Updated = now
		m.Helpful = 0
		m.Unhelpful = 0
		m.Retired = false
		after = append(after, m)
	}
	for i := range after {
		if retired[after[i].ID] {
			after[i].Retired = true
		}
	}
	profile, err := s.Profile()
	if err != nil {
		return out, err
	}
	out.Passed = true
	out.Probes = []DreamProbeResult{}
	for _, p := range q.Probes {
		features := map[string][]string{}
		for k, v := range profile.Features {
			features[k] = append([]string{}, v...)
		}
		for k, v := range p.Features {
			features[k] = append([]string{}, v...)
		}
		request := Request{Profile: Profile{Project: s.Project, Conversation: s.Conversation, Features: features}, Task: p.Task, ByteBudget: q.ByteBudget, Limit: q.Limit}
		beforeResult := Recall(all, s.Identity, request, now)
		afterResult := Recall(after, s.Identity, request, now)
		beforeOK, afterOK := probeMatches(p, beforeResult.Context), probeMatches(p, afterResult.Context)
		if beforeOK {
			out.BeforePassed++
		}
		if afterOK {
			out.AfterPassed++
		} else {
			out.Passed = false
		}
		if beforeOK && !afterOK {
			out.Regressions++
		}
		out.Probes = append(out.Probes, DreamProbeResult{Name: p.Name, Before: beforeOK, After: afterOK, BeforeBytes: beforeResult.Bytes, AfterBytes: afterResult.Bytes})
	}
	return out, nil
}
func probeMatches(p DreamProbe, context string) bool {
	for _, s := range p.Contains {
		if !strings.Contains(context, s) {
			return false
		}
	}
	for _, s := range p.Absent {
		if strings.Contains(context, s) {
			return false
		}
	}
	return true
}
