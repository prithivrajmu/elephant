package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Event struct {
	Experience *Experience `json:"experience,omitempty"`
	Kind       string      `json:"kind"`
	Memory     *Memory     `json:"memory,omitempty"`
	Recall     *Usage      `json:"recall,omitempty"`
	ID         string      `json:"id,omitempty"`
	FeedbackID string      `json:"feedback_id,omitempty"`
	Helpful    bool        `json:"helpful,omitempty"`
	At         time.Time   `json:"at"`
}
type Store struct{ Path string }

func (s Store) All() ([]Memory, error) {
	db, err := s.database(false)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return readMemories(db)
}

// The callback runs once under SQLite's write transaction. It is never replayed
// on a busy error, because hook callbacks can also update local task state.
func (s Store) transact(fn func([]Memory, map[string]bool) (*Event, error)) error {
	return s.transactSQL(true, nil, func(_ *sql.Tx, all []Memory, seen map[string]bool) (*Event, error) { return fn(all, seen) })
}
func (s Store) transactState(fn func([]Memory, map[string]bool) (*Event, error)) error {
	return s.transactSQL(false, nil, func(_ *sql.Tx, all []Memory, seen map[string]bool) (*Event, error) { return fn(all, seen) })
}
func (s Store) transactKeys(keys []string, fn func([]Memory, map[string]bool) (*Event, error)) error {
	return s.transactSQL(true, keys, func(_ *sql.Tx, all []Memory, seen map[string]bool) (*Event, error) { return fn(all, seen) })
}
func (s Store) transactSQL(memories bool, keys []string, fn func(*sql.Tx, []Memory, map[string]bool) (*Event, error)) error {
	db, err := s.database(true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("memory store busy or unavailable: %w", err)
	}
	defer tx.Rollback()
	all := []Memory{}
	if memories {
		all, err = readMemories(tx)
		if err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, key := range keys {
		var exists int
		if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM seen WHERE key=?)", key).Scan(&exists); err != nil {
			return err
		}
		seen[key] = exists != 0
	}
	event, err := fn(tx, all, seen)
	if err != nil {
		return err
	}
	if event != nil {
		event.At = time.Now().UTC()
		if err = applyEvent(tx, *event); err != nil {
			return err
		}
	}
	return tx.Commit()
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
	a.Features, b.Features = canonicalLabels(a.Features), canonicalLabels(b.Features)
	a.Requires, b.Requires = canonicalLabels(a.Requires), canonicalLabels(b.Requires)
	a.Excludes, b.Excludes = canonicalLabels(a.Excludes), canonicalLabels(b.Excludes)
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// Label values already use case-insensitive set semantics during retrieval.
// Apply the same semantics for retry detection without rewriting evidence.
func canonicalLabels(labels map[string][]string) map[string][]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string][]string, len(labels))
	for key, values := range labels {
		out[key] = []string{}
		for value := range set(values) {
			out[key] = append(out[key], value)
		}
		sort.Strings(out[key])
	}
	return out
}
func (s Store) Record(m Memory, task ...*TaskCapture) (Memory, error) {
	if m.Outcome == "" {
		m.Outcome = OutcomeForClass(m.Class)
	}
	m.Class = MemoryClass(m)
	// Preserve the authored subject for case-sensitive credential signatures.
	screening := m
	m.Subject = strings.ToLower(strings.Join(strings.Fields(m.Subject), " "))
	if err := Validate(m); err != nil {
		return m, err
	}
	if err := ScreenSensitive(screening); err != nil {
		return m, err
	}
	m.ID = newID()
	m.Approved = false
	m.Retired = false
	m.Helpful = 0
	m.Unhelpful = 0
	m.Created = time.Now().UTC()
	m.Updated = m.Created
	err := s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
		policy, e := s.LanguagePolicy()
		if e != nil {
			return nil, e
		}
		writing := CheckLanguage(policy, m.Lesson)
		if !writing.Accepted {
			return nil, fmt.Errorf("STE target 100: %s Revise the summary. Source evidence stays unchanged.", writing.Issues[0].Message)
		}
		m.Writing = &writing
		// Only exact evidence in this owner's scope can be a retry. Keep this
		// lookup and the write in one transaction so concurrent retries agree.
		all, e := readMemoryQuery(tx, `SELECT data FROM memories
			WHERE tenant=? AND owner=? AND scope=? AND project=?
			AND json_extract(data,'$.retired')=0
			AND json_extract(data,'$.incident')=?
			AND json_extract(data,'$.lesson')=?
			AND json_extract(data,'$.source')=? ORDER BY id`,
			m.Tenant, m.Owner, m.Scope, m.Project, m.Incident, m.Lesson, m.Source)
		if e != nil {
			return nil, e
		}
		var receipt *Experience
		if len(task) > 0 && task[0] != nil {
			t := task[0]
			receipt = &Experience{ID: newID(), Identity: Identity{Tenant: m.Tenant, User: m.Owner}, Project: m.Project, Agent: t.Agent, Session: t.Session, Task: t.Task, Event: "LessonReview", Status: "memory_saved", At: time.Now().UTC()}
		}
		for _, old := range all {
			if !old.Retired && sameMemory(old, m) {
				m = old
				if receipt != nil {
					receipt.MemoryID = m.ID
					return &Event{Kind: "experience", Experience: receipt}, nil
				}
				return nil, nil
			}
		}
		if receipt != nil {
			receipt.MemoryID = m.ID
		}
		return &Event{Kind: "put", Memory: &m, Experience: receipt}, nil
	})
	return m, err
}
func owned(m Memory, id Identity) bool { return m.Tenant == id.Tenant && m.Owner == id.User }
func (s Store) Feedback(id Identity, project, memoryID, eventID string, helpful bool, conversation ...string) error {
	if strings.TrimSpace(eventID) == "" || len(eventID) > 256 {
		return fmt.Errorf("feedback_id required, max 256 bytes; use a unique observed task/run ID")
	}
	parts, _ := json.Marshal([]string{id.User, eventID})
	feedbackKey := string(parts)
	return s.transactKeys([]string{memoryID + ":" + feedbackKey, memoryID + ":" + id.User + ":" + eventID}, func(all []Memory, seen map[string]bool) (*Event, error) {
		for _, m := range all {
			if m.ID == memoryID && visible(m, id, project, conversation...) {
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

// Read transactions retain a consistent snapshot and do not acquire a writer lock.
func (s Store) readSQL(fn func(*sql.Tx, []Memory) error) error {
	db, err := s.database(false)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	all, err := readMemories(tx)
	if err != nil {
		return err
	}
	if err = fn(tx, all); err != nil {
		return err
	}
	return tx.Commit()
}
