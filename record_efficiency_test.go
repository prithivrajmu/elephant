package memory

import (
	"path/filepath"
	"reflect"
	"strings"
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
	// The same lesson with new evidence is a near-duplicate, not a new memory.
	resourced := retry
	resourced.Source += " new evidence"
	if got, err := s.Record(resourced); err == nil || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("same lesson with new source saved again: %+v %v", got, err)
	}
	// A personal lesson is recalled in every project, so a copy saved from
	// another project is refused with the original listed, not merged into it.
	elsewhere := retry
	elsewhere.Project += "-other"
	if got, err := s.Record(elsewhere); err == nil || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("personal copy from another project saved again: %+v %v", got, err)
	}
	// Different conditions remain independent.
	conditioned := retry
	conditioned.Requires = map[string][]string{"database": {"mysql"}}
	if got, err := s.Record(conditioned); err != nil || got.ID == first.ID {
		t.Fatalf("distinct conditions collapsed: %+v %v", got, err)
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
