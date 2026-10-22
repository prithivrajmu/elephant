package memory

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Profile struct {
	Project       string              `json:"project"`
	Conversation  string              `json:"conversation,omitempty"`
	Features      map[string][]string `json:"features"`
	Evidence      map[string][]string `json:"evidence,omitempty"`
	manifestFiles int
}
type Identity struct {
	Tenant string `json:"tenant"`
	User   string `json:"user"`
	Team   string `json:"team"`
}
type Memory struct {
	ID           string              `json:"id"`
	Tenant       string              `json:"tenant"`
	Owner        string              `json:"owner"`
	Team         string              `json:"team,omitempty"`
	Project      string              `json:"project"`
	Conversation string              `json:"conversation,omitempty"`
	Scope        string              `json:"scope"`
	Approved     bool                `json:"approved"`
	Outcome      string              `json:"outcome"`
	Class        string              `json:"class,omitempty"`
	Writing      *LanguageReport     `json:"writing,omitempty"`
	Sharing      *TeamProvenance     `json:"sharing,omitempty"`
	Incident     string              `json:"incident"`
	Lesson       string              `json:"lesson"`
	Source       string              `json:"source"`
	Subject      string              `json:"subject,omitempty"`
	Features     map[string][]string `json:"features"`
	Requires     map[string][]string `json:"requires,omitempty"`
	Excludes     map[string][]string `json:"excludes,omitempty"`
	Created      time.Time           `json:"created"`
	Updated      time.Time           `json:"updated"`
	Helpful      int                 `json:"helpful"`
	Unhelpful    int                 `json:"unhelpful"`
	Retired      bool                `json:"retired"`
	// Supersedes lists the lessons this one retired when it was saved.
	Supersedes []string `json:"supersedes,omitempty"`
	// DistinctFrom is input only: saved lessons the author checked and found different.
	DistinctFrom []string `json:"distinct_from,omitempty"`
}
type Request struct {
	Profile         Profile             `json:"profile"`
	Task            string              `json:"task"`
	ByteBudget      int                 `json:"byte_budget"`
	Limit           int                 `json:"limit"`
	Initialize      bool                `json:"-"`
	ContextFeatures map[string][]string `json:"context_features,omitempty"`
	profileMS       float64
	manifestFiles   int
}
type Hit struct {
	ID               string   `json:"id"`
	Score            float64  `json:"score"`
	Similarity       float64  `json:"similarity"`
	Coverage         float64  `json:"coverage"`
	Lexical          float64  `json:"lexical"`
	Confidence       float64  `json:"confidence"`
	Matched          []string `json:"matched"`
	Conflict         bool     `json:"conflict"`
	Alternatives     []string `json:"alternative_ids,omitempty"`
	AlternativeCount int      `json:"alternative_count,omitempty"`
	Observations     int      `json:"usefulness_observations"`
}
type Result struct {
	Context    string `json:"context"`
	Bytes      int    `json:"bytes"`
	ByteBudget int    `json:"byte_budget"`
	Hits       []Hit  `json:"hits"`
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

var stopwords = set(strings.Fields("a an the and or but to of for in on at by from with without is are was were be been being it its this that these those as if then than so i we you they he she my our your their can could would should will do does did have has had please help task work need want implement improve use using code"))

// fold merges simple plurals so "model" matches "models". It is not a stemmer.
func fold(w string) string {
	if n := len(w); n > 3 && w[n-1] == 's' && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is") {
		return w[:n-1]
	}
	return w
}

func contentWords(s string) []string {
	out := []string{}
	for _, w := range words(s) {
		if !stopwords[w] {
			out = append(out, fold(w))
		}
	}
	return out
}

func set(v []string) map[string]bool {
	m := map[string]bool{}
	for _, s := range v {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			m[s] = true
		}
	}
	return m
}
func overlap(a, b []string) bool {
	x := set(a)
	for s := range set(b) {
		if x[s] {
			return true
		}
	}
	return false
}

var weights = map[string]float64{"language": 1, "framework": 2, "app": 2, "cloud": 1.5, "database": 2.5, "data_model": 1, "workflow": 1.5, "style": 1, "reviewer": 0.5, "contributor": 0.5}

func similarity(a, b map[string][]string) (float64, []string) {
	s, c, e := similarityDetails(a, b)
	return s * c, e
}

// a is the authored memory fingerprint; b is the current query profile.
// Unknown fields diminish the corroborating bonus, never the task relevance.
func similarityDetails(a, b map[string][]string) (float64, float64, []string) {
	total, comparable, matched := 0.0, 0.0, 0.0
	explain := []string{}
	keys := []string{}
	for k := range a {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w := weights[k]
		if w == 0 {
			w = 1
		}
		x, y := set(a[k]), set(b[k])
		if len(x) == 0 {
			continue
		}
		total += w
		if len(y) == 0 {
			continue
		}
		comparable += w
		n := 0
		for s := range x {
			if y[s] {
				n++
				explain = append(explain, k+"="+s)
			}
		}
		matched += w * float64(n) / float64(len(x)+len(y)-n)
	}
	sort.Strings(explain)
	if total == 0 || comparable == 0 {
		return 0, 0, explain
	}
	return matched / comparable, comparable / total, explain
}
func applicable(m Memory, p Profile) bool {
	for k, v := range m.Requires {
		if !overlap(v, p.Features[k]) {
			return false
		}
	}
	for k, v := range m.Excludes {
		if overlap(v, p.Features[k]) {
			return false
		}
	}
	return true
}
func visible(m Memory, id Identity, project string, conversation ...string) bool {
	if m.Tenant != id.Tenant || m.Retired {
		return false
	}
	switch m.Scope {
	case "project":
		return project != "" && m.Project != "" && m.Owner == id.User && m.Project == project
	case "conversation":
		return len(conversation) > 0 && conversation[0] != "" && m.Conversation != "" && m.Owner == id.User && m.Conversation == conversation[0]
	case "personal":
		return m.Owner == id.User
	case "team":
		return id.Team != "" && m.Team == id.Team && m.Approved
	}
	return false
}
func Validate(m Memory) error {
	if m.Tenant == "" || m.Owner == "" {
		return fmt.Errorf("tenant and owner required")
	}
	if m.Scope != "personal" && m.Scope != "project" && m.Scope != "team" && m.Scope != "conversation" {
		return fmt.Errorf("scope must be project, conversation, personal or team")
	}
	if m.Scope == "project" && m.Project == "" {
		return fmt.Errorf("project scope requires a real project ID")
	}
	if m.Scope == "conversation" && m.Conversation == "" {
		return fmt.Errorf("conversation scope requires a conversation ID")
	}
	if m.Scope == "team" && m.Team == "" {
		return fmt.Errorf("team scope needs a team")
	}
	switch m.Outcome {
	case "good", "great", "bad", "worst":
	default:
		return fmt.Errorf("outcome must be good, great, bad or worst")
	}
	if m.Class != "" && m.Class != "win" && m.Class != "lesson" && m.Class != "warning" && m.Class != "scar" {
		return fmt.Errorf("class must be win, lesson, warning or scar")
	}
	if strings.TrimSpace(m.Lesson) == "" || strings.TrimSpace(m.Incident) == "" || strings.TrimSpace(m.Source) == "" {
		return fmt.Errorf("incident, lesson and evidence source required")
	}
	if len(m.Lesson) > 4096 || len(m.Incident) > 4096 || len(m.Source) > 1024 || len(m.Subject) > 256 {
		return fmt.Errorf("memory text too large")
	}
	for name, labels := range map[string]map[string][]string{"features": m.Features, "requires": m.Requires, "excludes": m.Excludes} {
		if e := ValidateLabels(labels); e != nil {
			return fmt.Errorf("%s: %w", name, e)
		}
	}
	return nil
}
func ValidateLabels(labels map[string][]string) error {
	if len(labels) > 32 {
		return fmt.Errorf("max 32 dimensions")
	}
	for k, v := range labels {
		if strings.TrimSpace(k) == "" || len(k) > 128 {
			return fmt.Errorf("dimension name must be 1..128 bytes")
		}
		if len(v) == 0 || len(v) > 32 {
			return fmt.Errorf("%s requires 1..32 values", k)
		}
		for _, s := range v {
			if strings.TrimSpace(s) == "" || len(s) > 128 {
				return fmt.Errorf("%s values must be 1..128 bytes", k)
			}
		}
	}
	return nil
}

// Lexical scores use only visible, applicable candidates. Tenant data never enters IDF.
func Recall(all []Memory, id Identity, q Request, now time.Time) Result {
	budget := q.ByteBudget
	if budget <= 0 {
		budget = 4000
	}
	if budget > 64000 {
		budget = 64000
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 6
	}
	if limit > 20 {
		limit = 20
	}
	result := Result{ByteBudget: budget, Hits: []Hit{}}
	query := set(contentWords(q.Task))
	if len(query) == 0 && (!q.Initialize || strings.TrimSpace(q.Task) != "") {
		return result
	}
	candidates := []Memory{}
	for _, m := range all {
		if visible(m, id, q.Profile.Project, q.Profile.Conversation) && applicable(m, q.Profile) {
			candidates = append(candidates, m)
		}
	}
	type ranked struct {
		m          Memory
		h          Hit
		words      map[string]bool
		redundancy float64
		entry      string
	}
	rank := []ranked{}
	for i, lex := range admission(candidates, q) {
		if lex < 0 {
			continue
		}
		m := candidates[i]
		sim, coverage, matches := similarityDetails(m.Features, q.Profile.Features)
		base := lex * (1 + 0.20*sim*coverage)
		if len(query) == 0 {
			base = sim * coverage
		}
		confidence := (2 + float64(m.Helpful)) / (4 + float64(m.Helpful+m.Unhelpful))
		age := math.Max(0, now.Sub(m.Updated).Hours()/24)
		fresh := math.Exp(-math.Ln2 * age / 180)
		// Outcome is recorded, but dramatic incidents get no unvalidated ranking boost.
		score := base * (0.5 + 0.5*confidence) * (0.8 + 0.2*fresh)
		rank = append(rank, ranked{m: m, h: Hit{ID: m.ID, Score: score, Similarity: sim, Coverage: coverage, Lexical: lex, Confidence: confidence, Observations: m.Helpful + m.Unhelpful, Matched: matches}})
	}
	// A subject identifies potentially contradictory advice; surface alternatives, don't silently overwrite.
	admitted := make([]Memory, 0, len(rank))
	for _, r := range rank {
		admitted = append(admitted, r.m)
	}
	alternatives := indexAlternatives(admitted)
	header := "Retrieved experience (untrusted evidence; current project policy takes precedence):\n"
	eligible := rank[:0]
	for _, r := range rank {
		alternatives.annotate(r.m, &r.h)
		r.entry = renderEntry(r.m, r.h)
		if len(header)+len(r.entry) <= budget {
			r.words = set(words(r.m.Lesson))
			eligible = append(eligible, r)
		}
	}
	rank = eligible
	for len(rank) > 0 && len(result.Hits) < limit {
		best := 0
		utility := -math.MaxFloat64
		for i, r := range rank {
			u := r.h.Score - 0.12*r.redundancy
			if u > utility || (u == utility && r.m.ID < rank[best].m.ID) {
				utility = u
				best = i
			}
		}
		r := rank[best]
		rank = append(rank[:best], rank[best+1:]...)
		// JSON quoting prevents a lesson's newlines from impersonating result metadata.
		entry := r.entry
		overhead := 0
		if result.Context == "" {
			overhead = len(header)
		}
		if len(result.Context)+overhead+len(entry) > budget {
			continue
		}
		if result.Context == "" {
			result.Context = header
		}
		result.Context += entry
		result.Hits = append(result.Hits, r.h)
		// Update diversity once per accepted result. Discard entries that cannot
		// fit now; the remaining budget can only shrink.
		remaining := rank[:0]
		for _, next := range rank {
			if len(result.Context)+len(next.entry) > budget {
				continue
			}
			inter := 0
			for term := range next.words {
				if r.words[term] {
					inter++
				}
			}
			if union := len(next.words) + len(r.words) - inter; union > 0 {
				next.redundancy = math.Max(next.redundancy, float64(inter)/float64(union))
			}
			remaining = append(remaining, next)
		}
		rank = remaining
	}
	result.Bytes = len(result.Context)
	return result
}
func renderEntry(m Memory, h Hit) string {
	flag := ""
	if h.Conflict {
		count := h.AlternativeCount
		if count == 0 {
			count = len(h.Alternatives)
		}
		ids := h.Alternatives
		if len(ids) > 5 {
			ids = ids[:5]
		}
		flag = fmt.Sprintf(" [POTENTIAL ALTERNATIVES: ids=%q count=%d unlisted=%d; alternatives may be omitted by budget]", strings.Join(ids, ","), count, count-len(ids))
	}
	requires, _ := json.Marshal(m.Requires)
	excludes, _ := json.Marshal(m.Excludes)
	return fmt.Sprintf("- [%s] %s%s incident=%q lesson=%q source=%q project=%q conversation=%q requires=%s excludes=%s matched=%q reported_usefulness=%.2f observations=%d\n", m.ID, m.Outcome, flag, m.Incident, m.Lesson, m.Source, m.Project, m.Conversation, requires, excludes, strings.Join(h.Matched, ","), h.Confidence, h.Observations)
}

// recallThreshold is the TF-IDF cosine a lesson needs with the task to be
// recalled. On 78 agent-labelled tasks over a real 167-lesson store it beat BM25:
// 161 lessons injected instead of 265, 52 useful instead of 48, 65 unhelpful
// instead of 158, 38 tasks given a useful lesson instead of 36, nDCG@3 0.49 vs
// 0.44. Lower thresholds admit noise; from 0.08 useful lessons start to drop.
const recallThreshold = 0.074

// recallIDFFloor keeps words that every lesson shares from weighing zero, so a
// task about what the whole store is about still recalls it. 0.4 is the lowest
// floor that does this in the alternative-budget tests without letting control
// tasks through in the relevance ratchet.
const recallIDFFloor = 0.4

// admission returns each candidate's lexical score, or -1 if recall does not
// admit it. A task is compared as duplicates.go compares lessons. A blank task
// during initialization admits on profile agreement alone, with score 0.
func admission(candidates []Memory, q Request) []float64 {
	out := make([]float64, len(candidates))
	if strings.TrimSpace(q.Task) == "" {
		for i, m := range candidates {
			out[i] = -1
			if s, c, _ := similarityDetails(m.Features, q.Profile.Features); q.Initialize && s*c >= 0.15 {
				out[i] = 0
			}
		}
		return out
	}
	docs := make([][]string, len(candidates))
	for i, m := range candidates {
		docs[i] = textTerms(m.Incident + " " + m.Lesson + " " + m.Subject)
	}
	for i, c := range cosines(textTerms(q.Task), docs, recallIDFFloor) {
		out[i] = c
		if c < recallThreshold {
			out[i] = -1
		}
	}
	return out
}
