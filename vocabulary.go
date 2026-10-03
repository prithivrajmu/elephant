package memory

import "sort"

// Memory classes are product labels. They do not assert a probability or change ranking.
func MemoryClass(m Memory) string {
	if m.Class != "" {
		return m.Class
	}
	switch m.Outcome {
	case "great":
		return "win"
	case "good":
		return "lesson"
	case "bad":
		return "warning"
	case "worst":
		return "scar"
	}
	return "lesson"
}
func OutcomeForClass(class string) string {
	switch class {
	case "win":
		return "great"
	case "lesson":
		return "good"
	case "warning":
		return "bad"
	case "scar":
		return "worst"
	}
	return ""
}

type Fingerprint = Profile
type MemoryScope = string
type RecallResult = Result
type RecallCandidate = Hit

type MapNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	MemoryID string `json:"memory_id,omitempty"`
}
type Trail struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}
type MemoryMap struct {
	Nodes  []MapNode `json:"nodes"`
	Trails []Trail   `json:"trails"`
	Note   string    `json:"note"`
}

func BuildMemoryMap(memories []Memory) MemoryMap {
	out := MemoryMap{Nodes: []MapNode{}, Trails: []Trail{}, Note: "Trails come from stored facts. Signal overlap is not proof of an independent experience or a project link."}
	seen := map[string]bool{}
	add := func(id, kind, label, memory string) {
		if !seen[id] {
			seen[id] = true
			out.Nodes = append(out.Nodes, MapNode{ID: id, Kind: kind, Label: label, MemoryID: memory})
		}
	}
	for _, m := range memories {
		if m.Retired {
			continue
		}
		mid := "memory:" + m.ID
		add(mid, "memory", m.Lesson, m.ID)
		if m.Project != "" {
			pid := "project:" + m.Project
			add(pid, "project", m.Project, "")
			out.Trails = append(out.Trails, Trail{From: pid, To: mid, Kind: "origin"})
		} else if m.Conversation != "" {
			cid := "conversation:" + m.Conversation
			add(cid, "conversation", m.Conversation, "")
			out.Trails = append(out.Trails, Trail{From: cid, To: mid, Kind: "origin"})
		}
		source := "source:" + m.ID
		add(source, "evidence", m.Source, "")
		out.Trails = append(out.Trails, Trail{From: source, To: mid, Kind: "evidence"})
		for key, values := range m.Features {
			for _, value := range values {
				sid := "signal:" + key + "=" + value
				add(sid, "signal", key+": "+value, "")
				out.Trails = append(out.Trails, Trail{From: sid, To: mid, Kind: "signal"})
			}
		}
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].ID < out.Nodes[j].ID })
	sort.Slice(out.Trails, func(i, j int) bool {
		a, b := out.Trails[i], out.Trails[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Kind < b.Kind
	})
	return out
}
