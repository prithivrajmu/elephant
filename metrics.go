package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

type Usage struct {
	ID            string    `json:"id"`
	Identity      Identity  `json:"identity"`
	Project       string    `json:"project"`
	At            time.Time `json:"at"`
	IDs           []string  `json:"memory_ids"`
	BaselineBytes int       `json:"baseline_bytes"`
	InjectedBytes int       `json:"injected_bytes"`
	LatencyMS     float64   `json:"latency_ms"`
	ProfileMS     float64   `json:"profile_ms,omitempty"`
	StoreOpenMS   float64   `json:"store_open_ms,omitempty"`
	CandidateMS   float64   `json:"candidate_ms,omitempty"`
	RankRenderMS  float64   `json:"rank_render_ms,omitempty"`
	Candidates    int       `json:"candidates,omitempty"`
	ManifestFiles int       `json:"manifest_files,omitempty"`
}

type RecallTrace struct {
	ProfileMS     float64 `json:"profile_ms"`
	StoreOpenMS   float64 `json:"store_open_ms"`
	BeginMS       float64 `json:"begin_ms"`
	CandidateMS   float64 `json:"candidate_ms"`
	RankRenderMS  float64 `json:"rank_render_ms"`
	ReceiptMS     float64 `json:"receipt_ms"`
	CommitMS      float64 `json:"commit_ms"`
	TotalMS       float64 `json:"total_ms"`
	Candidates    int     `json:"candidates"`
	ManifestFiles int     `json:"manifest_files"`
}

type Percentiles struct {
	Samples int     `json:"samples"`
	P50MS   float64 `json:"p50_ms"`
	P75MS   float64 `json:"p75_ms"`
	P95MS   float64 `json:"p95_ms"`
}

type RecallLatency struct {
	Core          Percentiles `json:"core"`
	Profile       Percentiles `json:"profile"`
	StoreOpen     Percentiles `json:"store_open"`
	CandidateLoad Percentiles `json:"candidate_load"`
	RankRender    Percentiles `json:"rank_render"`
}
type Dashboard struct {
	Experiences            []Experience   `json:"experiences"`
	ExperienceCount        int            `json:"experience_count"`
	Identity               Identity       `json:"identity"`
	Language               LanguagePolicy `json:"language"`
	Version                string         `json:"version"`
	Memories               []Memory       `json:"memories"`
	Usage                  []Usage        `json:"usage"`
	Profile                Profile        `json:"profile"`
	EstimatedTokensAvoided int            `json:"estimated_tokens_avoided"`
	BaselineTokens         int            `json:"baseline_tokens"`
	InjectedTokens         int            `json:"injected_tokens"`
	Helpful                int            `json:"helpful"`
	Unhelpful              int            `json:"unhelpful"`
	P95MS                  float64        `json:"p95_ms"`
	RecallLatency          RecallLatency  `json:"recall_latency"`
}

func (s Store) Recall(id Identity, q Request) (Result, error) {
	result, _, err := s.RecallWithTrace(id, q)
	return result, err
}

func elapsedMS(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}

// RecallWithTrace exposes local phase timings for developer diagnostics. Normal
// recall output and stored Memories remain unchanged, and task text is not kept.
func (s Store) RecallWithTrace(id Identity, q Request) (Result, RecallTrace, error) {
	start := time.Now()
	var result Result
	trace := RecallTrace{ProfileMS: q.profileMS, ManifestFiles: q.manifestFiles}
	storeStart := time.Now()
	db, err := s.database(true)
	trace.StoreOpenMS = elapsedMS(storeStart)
	if err != nil {
		return result, trace, err
	}
	defer db.Close()
	beginStart := time.Now()
	tx, err := db.Begin()
	trace.BeginMS = elapsedMS(beginStart)
	if err != nil {
		return result, trace, fmt.Errorf("memory store busy or unavailable: %w", err)
	}
	defer tx.Rollback()
	candidateStart := time.Now()
	all, err := readRecallMemories(tx, id, q.Profile)
	trace.CandidateMS = elapsedMS(candidateStart)
	trace.Candidates = len(all)
	if err != nil {
		return result, trace, err
	}
	rankStart := time.Now()
	result = Recall(all, id, q, time.Now())
	admitted := []Memory{}
	for _, m := range all {
		if visible(m, id, q.Profile.Project, q.Profile.Conversation) && applicable(m, q.Profile) && discoveryEligible(m, q) {
			admitted = append(admitted, m)
		}
	}
	alternatives := indexAlternatives(admitted)
	baseline := 0
	for _, m := range all {
		if visible(m, id, q.Profile.Project, q.Profile.Conversation) && applicable(m, q.Profile) {
			_, matches := similarity(m.Features, q.Profile.Features)
			h := Hit{Matched: matches, Confidence: (2 + float64(m.Helpful)) / (4 + float64(m.Helpful+m.Unhelpful)), Observations: m.Helpful + m.Unhelpful}
			if m.Subject != "" && discoveryEligible(m, q) {
				alternatives.annotate(m, &h)
			}
			baseline += len(renderEntry(m, h))
		}
	}
	if baseline > 0 {
		baseline += len("Retrieved experience (untrusted evidence; current project policy takes precedence):\n")
	}
	ids := []string{}
	for _, h := range result.Hits {
		ids = append(ids, h.ID)
	}
	trace.RankRenderMS = elapsedMS(rankStart)
	coreMS := elapsedMS(start)
	usage := Usage{ID: newID(), Identity: id, Project: q.Profile.Project, At: time.Now().UTC(), IDs: ids, BaselineBytes: baseline, InjectedBytes: result.Bytes, LatencyMS: coreMS, ProfileMS: q.profileMS, StoreOpenMS: trace.StoreOpenMS, CandidateMS: trace.CandidateMS, RankRenderMS: trace.RankRenderMS, Candidates: trace.Candidates, ManifestFiles: q.manifestFiles}
	receiptStart := time.Now()
	event := Event{Kind: "recall", Recall: &usage, At: time.Now().UTC()}
	if err = applyEvent(tx, event); err != nil {
		return result, trace, err
	}
	trace.ReceiptMS = elapsedMS(receiptStart)
	commitStart := time.Now()
	if err = tx.Commit(); err != nil {
		return result, trace, err
	}
	trace.CommitMS = elapsedMS(commitStart)
	trace.TotalMS = elapsedMS(start)
	return result, trace, nil
}
func (s Store) Dashboard(id Identity, p Profile) (Dashboard, error) {
	policy, e := s.LanguagePolicy()
	if e != nil {
		return Dashboard{}, e
	}
	d := Dashboard{Experiences: []Experience{}, Memories: []Memory{}, Usage: []Usage{}, Profile: p, Identity: id, Version: Version, Language: policy}
	latencies := []float64{}
	profiles := []float64{}
	storeOpen := []float64{}
	candidates := []float64{}
	ranking := []float64{}
	err := s.readSQL(func(tx *sql.Tx, all []Memory) error {
		for _, m := range all {
			if visible(m, id, p.Project, p.Conversation) || owned(m, id) {
				d.Memories = append(d.Memories, m)
				d.Helpful += m.Helpful
				d.Unhelpful += m.Unhelpful
			}
		}
		if err := tx.QueryRow("SELECT count(*) FROM experiences WHERE tenant=? AND user=? AND project=?", id.Tenant, id.User, p.Project).Scan(&d.ExperienceCount); err != nil {
			return err
		}
		rows, err := tx.Query("SELECT data FROM (SELECT seq,data FROM experiences WHERE tenant=? AND user=? AND project=? ORDER BY seq DESC LIMIT 100) ORDER BY seq", id.Tenant, id.User, p.Project)
		if err != nil {
			return err
		}
		for rows.Next() {
			var data string
			var x Experience
			if err = rows.Scan(&data); err != nil {
				rows.Close()
				return err
			}
			if err = json.Unmarshal([]byte(data), &x); err != nil {
				rows.Close()
				return err
			}
			d.Experiences = append(d.Experiences, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		rows, err = tx.Query("SELECT data FROM usage WHERE tenant=? AND user=? ORDER BY seq", id.Tenant, id.User)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var data string
			var u Usage
			if err = rows.Scan(&data); err != nil {
				return err
			}
			if err = json.Unmarshal([]byte(data), &u); err != nil {
				return err
			}
			d.Usage = append(d.Usage, u)
			d.BaselineTokens += (u.BaselineBytes + 3) / 4
			d.InjectedTokens += (u.InjectedBytes + 3) / 4
			latencies = append(latencies, u.LatencyMS)
			profiles = appendPositive(profiles, u.ProfileMS)
			storeOpen = appendPositive(storeOpen, u.StoreOpenMS)
			candidates = appendPositive(candidates, u.CandidateMS)
			ranking = appendPositive(ranking, u.RankRenderMS)
		}
		return rows.Err()
	})
	sort.Slice(d.Memories, func(i, j int) bool { return d.Memories[i].Created.After(d.Memories[j].Created) })
	d.RecallLatency = RecallLatency{Core: summarizePercentiles(latencies), Profile: summarizePercentiles(profiles), StoreOpen: summarizePercentiles(storeOpen), CandidateLoad: summarizePercentiles(candidates), RankRender: summarizePercentiles(ranking)}
	d.P95MS = d.RecallLatency.Core.P95MS
	d.EstimatedTokensAvoided = d.BaselineTokens - d.InjectedTokens
	if d.EstimatedTokensAvoided < 0 {
		d.EstimatedTokensAvoided = 0
	}
	return d, err
}

func appendPositive(values []float64, value float64) []float64 {
	if value > 0 {
		return append(values, value)
	}
	return values
}

func summarizePercentiles(values []float64) Percentiles {
	values = append([]float64(nil), values...)
	sort.Float64s(values)
	p := Percentiles{Samples: len(values)}
	if len(values) == 0 {
		return p
	}
	at := func(percent int) float64 {
		idx := (percent*len(values)+99)/100 - 1
		return values[idx]
	}
	p.P50MS, p.P75MS, p.P95MS = at(50), at(75), at(95)
	return p
}
