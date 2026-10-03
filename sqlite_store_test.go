package memory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func legacyFixture(t *testing.T) (Store, []byte, Memory, Identity, Profile) {
	t.Helper()
	id, profile, m := fixture()
	m.Scope = "team"
	m.Team = id.Team
	m.Approved = true
	x := Experience{ID: "receipt", Identity: id, Project: m.Project, Session: "session", Task: "task", Status: "memory_saved", MemoryID: m.ID}
	u := Usage{ID: "usage", Identity: id, Project: m.Project, IDs: []string{m.ID}, BaselineBytes: 100, InjectedBytes: 20}
	events := []Event{{Kind: "put", Memory: &m, Experience: &x}, {Kind: "feedback", ID: m.ID, FeedbackID: "run", Helpful: true}, {Kind: "feedback", ID: m.ID, FeedbackID: "run", Helpful: true}, {Kind: "recall", Recall: &u}, {Kind: "retire", ID: m.ID}}
	var b bytes.Buffer
	for _, e := range events {
		if err := json.NewEncoder(&b).Encode(e); err != nil {
			t.Fatal(err)
		}
	}
	s := Store{Path: filepath.Join(t.TempDir(), "history with spaces.jsonl")}
	if err := os.WriteFile(s.Path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return s, b.Bytes(), m, id, profile
}

func TestSQLiteMigrationPreservesStateAndReceipts(t *testing.T) {
	s, original, m, id, p := legacyFixture(t)
	all, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	m.Helpful = 1
	m.Retired = true
	if len(all) != 1 || !reflect.DeepEqual(all[0], m) {
		t.Fatalf("migration changed memory: %+v", all)
	}
	b, err := os.ReadFile(s.Path + ".jsonl-backup")
	if err != nil || !bytes.Equal(b, original) {
		t.Fatal("original backup", err)
	}
	if ready, err := isSQLite(s.Path); err != nil || !ready {
		t.Fatal(ready, err)
	}
	d, err := s.Dashboard(id, Profile{Project: m.Project})
	if err != nil || d.ExperienceCount != 1 || len(d.Usage) != 1 || d.Experiences[0].MemoryID != m.ID {
		t.Fatal(d, err)
	}
	if len(Recall(all, id, Request{Profile: p, Initialize: true}, time.Now()).Hits) != 0 {
		t.Fatal("retirement lost")
	}
	// Restart and a duplicate receipt must not inflate the imported task count.
	x := d.Experiences[0]
	if err = (Store{Path: s.Path}).observe(x); err != nil {
		t.Fatal(err)
	}
	d, err = s.Dashboard(id, Profile{Project: m.Project})
	if err != nil || d.ExperienceCount != 1 {
		t.Fatal(d, err)
	}
	db, err := s.database(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode, version string
	var sync int
	db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	db.QueryRow("PRAGMA synchronous").Scan(&sync)
	db.QueryRow("SELECT sqlite_version()").Scan(&version)
	var events int
	if err = db.QueryRow("SELECT count(*) FROM events").Scan(&events); err != nil || events != 5 {
		t.Fatal("audit history changed", events, err)
	}
	if mode != "wal" || sync != 2 {
		t.Fatal(mode, sync)
	}
	// Driver is pinned to SQLite 3.51.3, including the WAL-reset fix.
	if version != "3.51.3" {
		t.Fatalf("review bundled SQLite change: %s", version)
	}
}

func TestSQLiteMigrationFailureAndRetry(t *testing.T) {
	s, original, _, _, _ := legacyFixture(t)
	if err := os.WriteFile(s.Path, append(original, []byte("{partial")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.All(); err == nil {
		t.Fatal("corrupt import accepted")
	}
	b, _ := os.ReadFile(s.Path)
	if !bytes.Equal(b, append(original, []byte("{partial")...)) {
		t.Fatal("failed import changed source")
	}
	if err := os.WriteFile(s.Path, original, 0600); err != nil {
		t.Fatal(err)
	}
	// Model interruption after the original backup, before candidate publication.
	os.WriteFile(s.Path+".jsonl-backup", original, 0600)
	os.WriteFile(filepath.Join(filepath.Dir(s.Path), ".elephant-migrate-interrupted"), []byte("partial candidate"), 0600)
	if _, err := s.All(); err != nil {
		t.Fatal("retry", err)
	}
	if err := verifyDatabase(s.Path); err != nil {
		t.Fatal(err)
	}
	bad := Store{Path: filepath.Join(t.TempDir(), "events")}
	os.WriteFile(bad.Path, original, 0600)
	os.WriteFile(bad.Path+".jsonl-backup", []byte("different history"), 0600)
	if _, err := bad.All(); err == nil {
		t.Fatal("mismatching backup overwritten")
	}
}

func TestSQLiteReaderAndBackupDuringWriter(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "memories.sqlite")}
	id, _, m := fixture()
	db, err := s.database(true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Keep a connection alive so the acknowledged first memory remains in WAL.
	first, err := s.Record(m)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(s.Path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatal("expected committed WAL data", err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	m.ID = "uncommitted"
	m.Lesson = "Check the rollback result"
	if err = applyEvent(tx, Event{Kind: "put", Memory: &m}); err != nil {
		t.Fatal(err)
	}
	// Readers and backups see the last committed snapshot while a writer is active.
	all, err := s.All()
	if err != nil || len(all) != 1 || all[0].ID != first.ID {
		t.Fatal(all, err)
	}
	if _, err = s.Dashboard(id, Profile{}); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err = s.Backup(backup); err != nil {
		t.Fatal(err)
	}
	if err = s.Backup(backup); err == nil {
		t.Fatal("overwrote backup")
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "restored.sqlite")
	originalBackup, readErr := os.ReadFile(backup)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err = RestoreStore(backup, restored); err != nil {
		t.Fatal(err)
	}
	afterRestore, _ := os.ReadFile(backup)
	if !bytes.Equal(originalBackup, afterRestore) {
		t.Fatal("restore changed backup")
	}
	all, err = (Store{Path: restored}).All()
	if err != nil || len(all) != 1 || all[0].ID != first.ID {
		t.Fatal(all, err)
	}
	if err = RestoreStore(backup, restored); err == nil {
		t.Fatal("overwrote restored store")
	}
	all, err = s.All()
	if err != nil || len(all) != 2 {
		t.Fatal(all, err)
	}
}

func TestSQLiteTransactionRollback(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "state.sqlite")}
	_, _, m := fixture()
	// Invalid receipt must roll back both the memory and audit event.
	if err := s.transact(func([]Memory, map[string]bool) (*Event, error) {
		return &Event{Kind: "put", Memory: &m, Experience: &Experience{}}, nil
	}); err == nil {
		t.Fatal("invalid receipt committed")
	}
	all, err := s.All()
	if err != nil || len(all) != 0 {
		t.Fatal(all, err)
	}
	db, err := s.database(false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	db.QueryRow("SELECT count(*) FROM events").Scan(&n)
	if n != 0 {
		t.Fatal("partial audit event")
	}
}

func TestSQLiteBusyTimeoutDoesNotReplayCallback(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "busy.sqlite")}
	db, err := s.database(true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	called := 0
	start := time.Now()
	err = s.transact(func([]Memory, map[string]bool) (*Event, error) { called++; return nil, nil })
	if err == nil || called != 0 || time.Since(start) < 2500*time.Millisecond || time.Since(start) > 6*time.Second {
		t.Fatal("busy writer timeout/callback", called, time.Since(start), err)
	}
}

func TestSQLiteUnsupportedSchemaIsPreserved(t *testing.T) {
	s := Store{Path: filepath.Join(t.TempDir(), "future.sqlite")}
	db, err := s.database(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("PRAGMA user_version=99"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	original, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.All(); err == nil {
		t.Fatal("future schema accepted")
	}
	if err = RestoreStore(s.Path, filepath.Join(t.TempDir(), "restore.sqlite")); err == nil {
		t.Fatal("future backup accepted")
	}
	after, err := os.ReadFile(s.Path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("unsupported schema changed", err)
	}
}

func TestSQLiteCrashHelper(t *testing.T) {
	path := os.Getenv("ELEPHANT_CRASH_STORE")
	if path == "" {
		return
	}
	db, err := (Store{Path: path}).database(true)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, _, m := fixture()
	m.ID = "crash"
	if err = applyEvent(tx, Event{Kind: "put", Memory: &m}); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("ELEPHANT_CRASH_COMMIT") == "1" {
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(path+".ready", []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestSQLiteKilledWriterRecovery(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			s := Store{Path: filepath.Join(t.TempDir(), "crash.sqlite")}
			if _, err := s.All(); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteCrashHelper$")
			cmd.Env = append(os.Environ(), "ELEPHANT_CRASH_STORE="+s.Path, fmt.Sprintf("ELEPHANT_CRASH_COMMIT=%d", map[bool]int{false: 0, true: 1}[committed]))
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { cmd.Process.Kill(); cmd.Wait() }()
			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, err := os.Stat(s.Path + ".ready"); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("child not ready", out.String())
				}
				time.Sleep(10 * time.Millisecond)
			}
			cmd.Process.Kill()
			cmd.Wait()
			all, err := s.All()
			want := 0
			if committed {
				want = 1
			}
			if err != nil || len(all) != want {
				t.Fatal("crash lost commit or retained rollback", all, err)
			}
			if err = verifyDatabase(s.Path); err != nil {
				t.Fatal(err)
			}
			_, _, m := fixture()
			m.Lesson = "Check recovery before the next write"
			if _, err = s.Record(m); err != nil {
				t.Fatal("writer remained locked", err)
			}
		})
	}
}

// Fixed memory inventory with growing recall history isolates replay overhead.
func BenchmarkSQLiteHistory(b *testing.B) {
	for _, n := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "history.jsonl")
			f, err := os.Create(path)
			if err != nil {
				b.Fatal(err)
			}
			_, _, m := fixture()
			enc := json.NewEncoder(f)
			enc.Encode(Event{Kind: "put", Memory: &m})
			for i := 0; i < n; i++ {
				enc.Encode(Event{Kind: "recall", Recall: &Usage{ID: fmt.Sprint(i)}})
			}
			f.Close()
			s := Store{Path: path}
			if _, err = s.All(); err != nil {
				b.Fatal(err)
			}
			b.Run("SQLite", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := s.All(); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("JSONL_replay", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					f, err := os.Open(path + ".jsonl-backup")
					if err != nil {
						b.Fatal(err)
					}
					scan := bufio.NewScanner(f)
					scan.Buffer(make([]byte, 4096), 2<<20)
					for scan.Scan() {
						var e Event
						if err = json.Unmarshal(scan.Bytes(), &e); err != nil {
							b.Fatal(err)
						}
					}
					err = scan.Err()
					f.Close()
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
