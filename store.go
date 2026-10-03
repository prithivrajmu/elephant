package memory

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Event struct {
	Kind       string    `json:"kind"`
	Memory     *Memory   `json:"memory,omitempty"`
	Recall     *Usage    `json:"recall,omitempty"`
	ID         string    `json:"id,omitempty"`
	FeedbackID string    `json:"feedback_id,omitempty"`
	Helpful    bool      `json:"helpful,omitempty"`
	At         time.Time `json:"at"`
}
type Store struct{ Path string }

func (s Store) load() ([]Memory, map[string]bool, error) {
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Memory{}, map[string]bool{}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	memories := map[string]Memory{}
	feedback := map[string]bool{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	line := 0
	for scanner.Scan() {
		line++
		var e Event
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, nil, fmt.Errorf("journal line %d: %w (restore or repair explicitly)", line, err)
		}
		switch e.Kind {
		case "put":
			if e.Memory == nil {
				return nil, nil, fmt.Errorf("invalid put at line %d", line)
			}
			memories[e.Memory.ID] = *e.Memory
		case "feedback":
			m, ok := memories[e.ID]
			if !ok {
				return nil, nil, fmt.Errorf("feedback for unknown memory")
			}
			key := e.ID + ":" + e.FeedbackID
			if feedback[key] {
				continue
			}
			feedback[key] = true
			if e.Helpful {
				m.Helpful++
			} else {
				m.Unhelpful++
			}
			memories[e.ID] = m
		case "retire":
			m, ok := memories[e.ID]
			if !ok {
				return nil, nil, fmt.Errorf("retire for unknown memory")
			}
			m.Retired = true
			memories[e.ID] = m
		case "recall":
			if e.Recall == nil {
				return nil, nil, fmt.Errorf("invalid recall event")
			}
		default:
			return nil, nil, fmt.Errorf("unknown event kind %q", e.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	out := []Memory{}
	for _, m := range memories {
		out = append(out, m)
	}
	return out, feedback, nil
}
func (s Store) All() ([]Memory, error) {
	var out []Memory
	e := s.transact(func(m []Memory, _ map[string]bool) (*Event, error) { out = m; return nil, nil })
	return out, e
}

// Lock directories serialize independent CLI and MCP processes. Stale locks fail closed.
func (s Store) transact(fn func([]Memory, map[string]bool) (*Event, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	lock := s.Path + ".lock"
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := os.Mkdir(lock, 0700)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("acquire memory lock: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("memory store busy after 3s; after a crashed writer verify it stopped before removing %s: %w", lock, err)
		}
		time.Sleep(15 * time.Millisecond)
	}
	defer os.Remove(lock)
	all, seen, err := s.load()
	if err != nil {
		return err
	}
	e, err := fn(all, seen)
	if err != nil || e == nil {
		return err
	}
	e.At = time.Now().UTC()
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}
func sameMemory(a, b Memory) bool {
	// Compare complete immutable learning payload; evidence from separate projects stays independent.
	a.ID = ""
	b.ID = ""
	a.Created = time.Time{}
	b.Created = time.Time{}
	a.Updated = time.Time{}
	b.Updated = time.Time{}
	a.Helpful = 0
	b.Helpful = 0
	a.Unhelpful = 0
	b.Unhelpful = 0
	a.Approved = false
	b.Approved = false
	a.Writing = nil
	b.Writing = nil
	a.Class = MemoryClass(a)
	b.Class = MemoryClass(b)
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func (s Store) Record(m Memory) (Memory, error) {
	if m.Outcome == "" {
		m.Outcome = OutcomeForClass(m.Class)
	}
	m.Class = MemoryClass(m)
	m.Subject = strings.ToLower(strings.Join(strings.Fields(m.Subject), " "))
	if err := Validate(m); err != nil {
		return m, err
	}
	m.ID = newID()
	m.Approved = false
	m.Retired = false
	m.Helpful = 0
	m.Unhelpful = 0
	m.Created = time.Now().UTC()
	m.Updated = m.Created
	err := s.transact(func(all []Memory, _ map[string]bool) (*Event, error) {
		policy, e := s.LanguagePolicy()
		if e != nil {
			return nil, e
		}
		writing := CheckLanguage(policy, m.Lesson)
		if !writing.Accepted {
			return nil, fmt.Errorf("STE target 100: %s Revise the summary. Source evidence stays unchanged.", writing.Issues[0].Message)
		}
		m.Writing = &writing
		for _, old := range all {
			if !old.Retired && sameMemory(old, m) {
				m = old
				return nil, nil
			}
		}
		return &Event{Kind: "put", Memory: &m}, nil
	})
	return m, err
}
func owned(m Memory, id Identity) bool { return m.Tenant == id.Tenant && m.Owner == id.User }
func (s Store) Feedback(id Identity, project, memoryID, eventID string, helpful bool, conversation ...string) error {
	if strings.TrimSpace(eventID) == "" || len(eventID) > 256 {
		return fmt.Errorf("feedback_id required, max 256 bytes; use a unique observed task/run ID")
	}
	return s.transact(func(all []Memory, seen map[string]bool) (*Event, error) {
		for _, m := range all {
			if m.ID == memoryID && visible(m, id, project, conversation...) {
				parts, _ := json.Marshal([]string{id.User, eventID})
				feedbackKey := string(parts)
				key := memoryID + ":" + feedbackKey
				if seen[key] || seen[memoryID+":"+id.User+":"+eventID] {
					return nil, nil
				}
				return &Event{Kind: "feedback", ID: memoryID, FeedbackID: feedbackKey, Helpful: helpful}, nil
			}
		}
		return nil, fmt.Errorf("memory not found or not accessible")
	})
}
func (s Store) Retire(id Identity, memoryID string) error {
	return s.transact(func(all []Memory, _ map[string]bool) (*Event, error) {
		for _, m := range all {
			if m.ID == memoryID && owned(m, id) {
				return &Event{Kind: "retire", ID: memoryID}, nil
			}
		}
		return nil, fmt.Errorf("memory not found or not owned")
	})
}

// Approve is a trusted local CLI operation; never exposed as an agent MCP tool.
func (s Store) Approve(id Identity, memoryID string) error {
	return s.transact(func(all []Memory, _ map[string]bool) (*Event, error) {
		for _, m := range all {
			if m.ID == memoryID && owned(m, id) && m.Scope == "team" && !m.Retired {
				m.Approved = true
				m.Updated = time.Now().UTC()
				return &Event{Kind: "put", Memory: &m}, nil
			}
		}
		return nil, fmt.Errorf("team draft not found or not owned")
	})
}
