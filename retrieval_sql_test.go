package memory

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestScopedRecallParity(t *testing.T) {
	id, p, m := fixture()
	p.Conversation = "chat"
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	db, e := s.database(true)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	tx, e := db.Begin()
	if e != nil {
		t.Fatal(e)
	}
	all := []Memory{}
	for i := 0; i < 80; i++ {
		n := m
		n.ID = fmt.Sprint(i)
		n.Subject = "connections"
		n.Lesson = fmt.Sprintf("Bound query connections %d", i)
		switch i % 10 {
		case 0:
			n.Tenant = "elsewhere"
		case 1:
			n.Owner = "bob"
		case 2:
			n.Scope = "project"
			n.Project = p.Project
		case 3:
			n.Scope = "project"
			n.Project = "other"
		case 4:
			n.Scope = "team"
			n.Team = id.Team
			n.Owner = "bob"
			n.Approved = true
		case 5:
			n.Scope = "team"
			n.Team = id.Team
		case 6:
			n.Scope = "conversation"
			n.Conversation = p.Conversation
		case 7:
			n.Scope = "conversation"
			n.Conversation = "other"
		case 8:
			n.Retired = true
		case 9:
			n.Requires = map[string][]string{"unknown": {"fact"}}
		}
		if e = applyEvent(tx, Event{Kind: "put", Memory: &n}); e != nil {
			t.Fatal(e)
		}
		all = append(all, n)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	selected, e := readRecallMemories(db, id, p)
	if e != nil {
		t.Fatal(e)
	}
	if len(selected) != 32 {
		t.Fatalf("unexpected SQL candidates: %d", len(selected))
	}
	now := time.Now()
	for _, task := range []string{"query connections", "", "the and"} {
		for _, budget := range []int{1, 501, 4000, 64000} {
			q := Request{Profile: p, Task: task, Initialize: true, ByteBudget: budget}
			if a, b := Recall(all, id, q, now), Recall(selected, id, q, now); !reflect.DeepEqual(a, b) {
				t.Fatalf("ranking/budget parity failed %q %d", task, budget)
			}
		}
	}
	// A corrupted unrelated tenant must no longer break this user's recall.
	if _, e = db.Exec("UPDATE memories SET data='broken' WHERE tenant='elsewhere'"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Recall(id, Request{Profile: p, Task: "connections"}); e != nil {
		t.Fatal(e)
	}
}

// The token index is an experiment, not a replacement corpus for BM25. Keeping
// nonmatching eligible documents is necessary for identical IDF/length scores.
func BenchmarkSQLCandidateSelection(b *testing.B) {
	id, p, m := fixture()
	s := Store{Path: filepath.Join(b.TempDir(), "m.sqlite")}
	db, e := s.database(true)
	if e != nil {
		b.Fatal(e)
	}
	defer db.Close()
	tx, e := db.Begin()
	if e != nil {
		b.Fatal(e)
	}
	if _, e = tx.Exec("CREATE TABLE lexical(token TEXT, id TEXT, PRIMARY KEY(token,id)) WITHOUT ROWID"); e != nil {
		b.Fatal(e)
	}
	for i := 0; i < 10000; i++ {
		n := m
		n.ID = fmt.Sprint(i)
		n.Requires = nil
		if i >= 100 {
			n.Owner = "other"
		}
		n.Incident = "Normal maintenance"
		n.Lesson = "Bound concurrency capacity"
		if i%100 == 0 {
			n.Lesson = "Redis cache limits"
		}
		if e = applyEvent(tx, Event{Kind: "put", Memory: &n}); e != nil {
			b.Fatal(e)
		}
		for term := range set(contentWords(n.Incident + " " + n.Lesson)) {
			if _, e = tx.Exec("INSERT INTO lexical VALUES(?,?)", term, n.ID); e != nil {
				b.Fatal(e)
			}
		}
	}
	if e = tx.Commit(); e != nil {
		b.Fatal(e)
	}
	cases := map[string]func(*sql.DB) ([]Memory, error){
		"all_rows": func(d *sql.DB) ([]Memory, error) { return readMemories(d) },
		"scoped":   func(d *sql.DB) ([]Memory, error) { return readRecallMemories(d, id, p) },
		"indexed_lexical_experiment": func(d *sql.DB) ([]Memory, error) {
			return readMemoryQuery(d, "SELECT m.data FROM lexical l JOIN memories m ON m.id=l.id WHERE l.token=? AND m.tenant=? AND m.owner=? AND m.scope='personal' ORDER BY m.id", "redis", id.Tenant, id.User)
		},
	}
	for name, fn := range cases {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, e := fn(db); e != nil {
					b.Fatal(e)
				}
			}
		})
	}
}
