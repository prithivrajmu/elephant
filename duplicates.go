package memory

import (
	"database/sql"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Near-duplicate lessons are found in two stages, as in agent memory systems
// such as Mem0 (arXiv 2504.19413): a cheap similarity search finds candidates,
// and the agent that wrote the lesson decides. A refused save lists up to three
// similar lessons; the agent then saves nothing, resends with supersedes (the
// old lessons are retired, not deleted), or resends with distinct_from.
//
// Similarity is TF-IDF cosine (sublinear tf, idf over the author's recallable
// lessons) on stemmed content words plus whole code identifiers, over incident
// and lesson. In a labelled replay of 159 real saves, 20% repeated or updated
// an earlier lesson. This measure ranked the earlier lesson in the top three
// for 97% of those pairs; word Jaccard, BM25, character n-grams and
// sentence-level matching did no better. The threshold was tuned on the first
// half of the saves. Over all saves it caught 72% of duplicate pairs, and 9% of
// saves were refused with no duplicate listed, mostly for related lessons.
// ponytail: lexical, so a paraphrase that shares few words still saves; add
// sentence embeddings if the replay shows that gap matters.
const duplicateThreshold = 0.22

var (
	identifierToken = regexp.MustCompile(`[A-Za-z0-9_~./:@-]+`)
	identifierShape = regexp.MustCompile(`[_/~@]|[a-z][A-Z]|[A-Za-z][0-9]|[0-9][A-Za-z]|\w\.\w`)
)

// lessonTerms keeps identifiers such as QUERY_CACHE_DISABLED or
// ~/.cache/pr-screenshots whole: they are rare, so they carry most of the
// signal that two lessons describe the same fix.
func lessonTerms(m Memory) []string {
	text := m.Incident + " " + m.Lesson
	terms := []string{}
	for _, w := range contentWords(text) {
		terms = append(terms, stem(w))
	}
	for _, t := range identifierToken.FindAllString(text, -1) {
		t = strings.Trim(t, ".:-/")
		if len(t) >= 4 && identifierShape.MatchString(t) {
			terms = append(terms, "#"+strings.ToLower(t))
		}
	}
	return terms
}

// stem folds common English inflections so retry, retried and retries meet.
func stem(w string) string {
	for _, suffix := range []string{"ing", "ed", "es", "s"} {
		if len(w) > len(suffix)+2 && strings.HasSuffix(w, suffix) {
			return w[:len(w)-len(suffix)]
		}
	}
	return w
}

// similarLessons returns up to three candidates at or above the threshold, most
// similar first. Candidates under other conditions, or named in checked, are
// not returned but still count toward idf.
func similarLessons(m Memory, candidates []Memory, checked map[string]bool) []Memory {
	tfs := make([]map[string]float64, 0, len(candidates)+1)
	df := map[string]int{}
	for _, c := range append(append([]Memory{}, candidates...), m) {
		tf := map[string]float64{}
		for _, t := range lessonTerms(c) {
			tf[t]++
		}
		for t := range tf {
			df[t]++
		}
		tfs = append(tfs, tf)
	}
	n := float64(len(tfs))
	weigh := func(tf map[string]float64) (map[string]float64, float64) {
		w, norm := map[string]float64{}, 0.0
		for t, f := range tf {
			w[t] = (1 + math.Log(f)) * math.Log((n+1)/float64(df[t]))
			norm += w[t] * w[t]
		}
		return w, math.Sqrt(norm)
	}
	query, queryNorm := weigh(tfs[len(candidates)])
	type scored struct {
		m Memory
		s float64
	}
	near := []scored{}
	for i, c := range candidates {
		if checked[c.ID] || !reflect.DeepEqual(canonicalLabels(c.Requires), canonicalLabels(m.Requires)) || !reflect.DeepEqual(canonicalLabels(c.Excludes), canonicalLabels(m.Excludes)) {
			continue
		}
		doc, docNorm := weigh(tfs[i])
		dot := 0.0
		for t, w := range query {
			dot += w * doc[t]
		}
		if queryNorm > 0 && docNorm > 0 && dot/(queryNorm*docNorm) >= duplicateThreshold {
			near = append(near, scored{c, dot / (queryNorm * docNorm)})
		}
	}
	sort.Slice(near, func(i, j int) bool {
		return near[i].s > near[j].s || near[i].s == near[j].s && near[i].m.ID < near[j].m.ID
	})
	out := []Memory{}
	for _, s := range near[:min(3, len(near))] {
		out = append(out, s.m)
	}
	return out
}

// supersede retires each named lesson inside the save transaction.
func supersede(tx *sql.Tx, ids []string, recallable []Memory) (map[string]bool, error) {
	replaced := set(ids)
	found := map[string]bool{}
	for _, m := range recallable {
		found[m.ID] = true
	}
	ordered := []string{}
	for id := range replaced {
		if !found[id] {
			return nil, fmt.Errorf("supersedes names %s, which is not an active lesson you can recall here: lesson not saved", id)
		}
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for _, id := range ordered {
		if err := applyEvent(tx, Event{Kind: "retire", ID: id, At: time.Now().UTC()}); err != nil {
			return nil, err
		}
	}
	return replaced, nil
}

func duplicateError(near []Memory) error {
	var b strings.Builder
	b.WriteString("a similar lesson is already saved: lesson not saved. Compare, then do one of these. If yours adds nothing, save nothing. If it corrects or extends a listed lesson, resend with \"supersedes\" set to the IDs it replaces; they are retired, not deleted. If it is a different lesson, resend with \"distinct_from\" set to the listed IDs.")
	for _, m := range near {
		fmt.Fprintf(&b, "\n[%s] scope=%s incident=%q lesson=%q", m.ID, m.Scope, m.Incident, m.Lesson)
	}
	return fmt.Errorf("%s", b.String())
}
