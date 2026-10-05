package memory

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestRecordDeduplicatesEquivalentLabelsWithoutChangingEvidence(t *testing.T) {
	_, _, m := fixture()
	s := Store{Path: filepath.Join(t.TempDir(), "memory.sqlite")}
	m.Features = map[string][]string{"language": {"Go", "typescript"}}
	m.Requires = map[string][]string{"database": {"Postgres", "sqlite"}}
	m.Excludes = nil
	first, err := s.Record(m)
	if err != nil {
		t.Fatal(err)
	}
	retry := m
	retry.Features = map[string][]string{"language": {"TYPESCRIPT", " go ", "go"}}
	retry.Requires = map[string][]string{"database": {"sqlite", "postgres"}}
	retry.Excludes = map[string][]string{}
	again, err := s.Record(retry)
	if err != nil || again.ID != first.ID {
		t.Fatalf("retry duplicated: %+v %v", again, err)
	}
	if !reflect.DeepEqual(again.Features, m.Features) || !reflect.DeepEqual(retry.Features["language"], []string{"TYPESCRIPT", " go ", "go"}) {
		t.Fatal("retry changed original evidence or caller labels")
	}
	// Different sources, conditions, and project origins remain independent.
	for _, change := range []func(*Memory){
		func(n *Memory) { n.Source += " new evidence" },
		func(n *Memory) { n.Project += "-other" },
		func(n *Memory) { n.Requires = map[string][]string{"database": {"mysql"}} },
	} {
		n := retry
		change(&n)
		got, err := s.Record(n)
		if err != nil || got.ID == first.ID {
			t.Fatalf("distinct evidence collapsed: %+v %v", got, err)
		}
	}
	if err := s.Retire(Identity{Tenant: m.Tenant, User: m.Owner}, first.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.Record(retry)
	if err != nil || got.ID == first.ID || got.Retired {
		t.Fatalf("retired memory reused: %+v %v", got, err)
	}
}

func TestRecordDoesNotDecodeOtherOwnersMemories(t *testing.T) {
	_, _, m := fixture()
	s := Store{Path: filepath.Join(t.TempDir(), "memory.sqlite")}
	other := m
	other.Owner = "another-owner"
	stored, err := s.Record(other)
	if err != nil {
		t.Fatal(err)
	}
	db, err := s.database(true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("UPDATE memories SET data='broken' WHERE id=?", stored.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Record(m); err != nil {
		t.Fatalf("unrelated owner blocked record: %v", err)
	}
}
