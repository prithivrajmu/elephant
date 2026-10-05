package memory

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAlternativeIndexMatchesFullScan(t *testing.T) {
	_, _, original := fixture()
	all := make([]Memory, 300)
	for i := range all {
		all[i] = original
		all[i].ID = fmt.Sprintf("%04d", len(all)-i)
		all[i].Subject = fmt.Sprint(i % 3)
		// Include many copies of the same lesson before alternative IDs.
		all[i].Lesson = fmt.Sprint(i / 40)
	}
	index := indexAlternatives(all)
	for _, m := range all {
		want := []string{}
		for _, n := range all {
			if n.Subject == m.Subject && n.Lesson != m.Lesson {
				want = append(want, n.ID)
			}
		}
		sort.Strings(want)
		var got Hit
		index.annotate(m, &got)
		if got.AlternativeCount != len(want) || !got.Conflict || !reflect.DeepEqual(got.Alternatives, want[:5]) {
			t.Fatalf("%s: %+v; want %v", m.ID, got, want)
		}
	}
}

func TestRecallOversizedMemoriesKeepAlternativesAndUsefulResults(t *testing.T) {
	id, p, m := fixture()
	m.Subject = "query-pool"
	all := []Memory{m}
	for i := 0; i < 100; i++ {
		n := m
		n.ID = fmt.Sprintf("large-%03d", i)
		n.Lesson = strings.Repeat("query concurrency ", 150)
		all = append(all, n)
	}
	r := Recall(all, id, Request{Profile: p, Task: "query concurrency", ByteBudget: 1200}, m.Created)
	if len(r.Hits) != 1 || r.Hits[0].ID != m.ID || r.Hits[0].AlternativeCount != 100 || r.Bytes > 1200 {
		t.Fatalf("large entries suppressed useful evidence or alternative metadata: %+v", r)
	}
}

// Includes the SQLite transaction, usage receipt, and baseline calculation.
func BenchmarkRecallWithAlternatives(b *testing.B) {
	for _, size := range []int{100, 1000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			id, p, m := fixture()
			s := Store{Path: filepath.Join(b.TempDir(), "memories.sqlite")}
			db, err := s.database(true)
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			tx, err := db.Begin()
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < size; i++ {
				n := m
				n.ID = fmt.Sprint(i)
				n.Subject = "query-pool"
				n.Lesson = fmt.Sprintf("Bound query concurrency with limit %d", i)
				if err := applyEvent(tx, Event{Kind: "put", Memory: &n, At: time.Now()}); err != nil {
					b.Fatal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.Recall(id, Request{Profile: p, Task: "query concurrency"}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
