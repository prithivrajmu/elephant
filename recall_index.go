package memory

import "sort"

// Index only visible, applicable, task-admitted memories. Keep enough sorted
// IDs to report five alternatives without materializing every pair of lessons.
type alternativeIndex map[string]*subjectAlternatives

type alternativeRef struct{ id, lesson string }
type subjectAlternatives struct {
	count   int
	lessons map[string]int
	sample  []alternativeRef
}

func indexAlternatives(memories []Memory) alternativeIndex {
	index := alternativeIndex{}
	groups := map[string]map[string][]string{}
	for _, m := range memories {
		if m.Subject == "" {
			continue
		}
		if groups[m.Subject] == nil {
			groups[m.Subject] = map[string][]string{}
		}
		groups[m.Subject][m.Lesson] = append(groups[m.Subject][m.Lesson], m.ID)
	}
	for subject, lessons := range groups {
		entry := &subjectAlternatives{lessons: map[string]int{}}
		for lesson, ids := range lessons {
			entry.count += len(ids)
			entry.lessons[lesson] = len(ids)
			sort.Strings(ids)
			for _, id := range ids[:min(5, len(ids))] {
				entry.sample = append(entry.sample, alternativeRef{id, lesson})
			}
		}
		sort.Slice(entry.sample, func(i, j int) bool { return entry.sample[i].id < entry.sample[j].id })
		// At most five of these IDs can belong to any single lesson.
		entry.sample = entry.sample[:min(10, len(entry.sample))]
		index[subject] = entry
	}
	return index
}

func (index alternativeIndex) annotate(m Memory, h *Hit) {
	entry := index[m.Subject]
	if entry == nil || entry.lessons[m.Lesson] == 0 {
		return
	}
	h.AlternativeCount = entry.count - entry.lessons[m.Lesson]
	h.Conflict = h.AlternativeCount > 0
	for _, ref := range entry.sample {
		if ref.lesson != m.Lesson {
			h.Alternatives = append(h.Alternatives, ref.id)
			if len(h.Alternatives) == 5 {
				break
			}
		}
	}
}
