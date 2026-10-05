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
	Project      string              `json:"project"`
	Conversation string              `json:"conversation,omitempty"`
	Features     map[string][]string `json:"features"`
	Evidence     map[string][]string `json:"evidence,omitempty"`
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
}
type Request struct {
	Profile         Profile             `json:"profile"`
	Task            string              `json:"task"`
	ByteBudget      int                 `json:"byte_budget"`
	Limit           int                 `json:"limit"`
	Initialize      bool                `json:"-"`
	ContextFeatures map[string][]string `json:"context_features,omitempty"`
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

func contentWords(s string) []string {
	out := []string{}
	for _, w := range words(s) {
		if !stopwords[w] {
			out = append(out, w)
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
		union := map[string]bool{}
		n := 0
		for s := range x {
			union[s] = true
			if y[s] {
				n++
				explain = append(explain, k+"="+s)
			}
		}
		for s := range y {
			union[s] = true
		}
		if len(union) > 0 {
			matched += w * float64(n) / float64(len(union))
		}
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

// BM25 is computed only over visible, applicable candidates. Tenant data never enters IDF.
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
	queryTerms := []string{}
	for term := range query {
		queryTerms = append(queryTerms, term)
	}
	sort.Strings(queryTerms)
	type document struct {
		length int
		tf     map[string]int
	}
	docs := make([]document, len(candidates))
	df := map[string]int{}
	avg := 0.0
	for i, m := range candidates {
		terms := contentWords(m.Incident + " " + m.Lesson + " " + m.Subject)
		docs[i] = document{length: len(terms), tf: map[string]int{}}
		avg += float64(len(terms))
		for _, term := range terms {
			if query[term] {
				docs[i].tf[term]++
			}
		}
		for term := range docs[i].tf {
			df[term]++
		}
	}
	if len(docs) > 0 {
		avg /= float64(len(docs))
	}
	if avg == 0 {
		avg = 1
	}
	type ranked struct {
		m          Memory
		h          Hit
		words      map[string]bool
		redundancy float64
		entry      string
	}
	rank := []ranked{}
	for i, m := range candidates {
		if len(query) > 0 && len(docs[i].tf) == 0 {
			continue
		}
		sim, coverage, matches := similarityDetails(m.Features, q.Profile.Features)
		bm := 0.0
		termMatch := false
		for _, term := range queryTerms {
			f := float64(docs[i].tf[term])
			if f == 0 {
				continue
			}
			termMatch = true
			idf := math.Log(1 + (float64(len(docs)-df[term])+0.5)/(float64(df[term])+0.5))
			bm += idf * (f * 2.2) / (f + 1.2*(0.25+0.75*float64(docs[i].length)/avg))
		}
		lex := bm / (bm + 2)
		base := lex * (1 + 0.20*sim*coverage)
		if len(query) > 0 {
			if !termMatch {
				continue
			}
		} else if q.Initialize && strings.TrimSpace(q.Task) == "" {
			if sim*coverage < 0.15 {
				continue
			}
			base = sim * coverage
		} else {
			continue
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
func discoveryEligible(m Memory, q Request) bool {
	query := set(contentWords(q.Task))
	if len(query) > 0 {
		for _, w := range contentWords(m.Incident + " " + m.Lesson + " " + m.Subject) {
			if query[w] {
				return true
			}
		}
		return false
	}
	if !q.Initialize || strings.TrimSpace(q.Task) != "" {
		return false
	}
	s, c, _ := similarityDetails(m.Features, q.Profile.Features)
	return s*c >= 0.15
}
