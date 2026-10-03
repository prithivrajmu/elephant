package memory

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Never probe an established database with os.Open: closing an unrelated file
// descriptor can release POSIX advisory locks held by SQLite in this process.
var storeInitialization sync.Map

type storeInit struct {
	mu    sync.Mutex
	ready bool
}

const sqliteHeader = "SQLite format 3\x00"
const sqliteSchema = `
CREATE TABLE memories(id TEXT PRIMARY KEY, tenant TEXT NOT NULL, owner TEXT NOT NULL, project TEXT NOT NULL, scope TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX memories_context ON memories(tenant, project, scope);
CREATE INDEX memories_owner ON memories(tenant, owner);
CREATE TABLE seen(key TEXT PRIMARY KEY);
CREATE TABLE events(seq INTEGER PRIMARY KEY, kind TEXT NOT NULL, data TEXT NOT NULL);
CREATE TABLE experiences(seq INTEGER PRIMARY KEY REFERENCES events(seq), tenant TEXT NOT NULL, user TEXT NOT NULL, project TEXT NOT NULL, session TEXT NOT NULL, task TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX experiences_context ON experiences(tenant, user, project, seq);
CREATE INDEX experiences_task ON experiences(tenant, user, session, task, seq);
CREATE TABLE usage(seq INTEGER PRIMARY KEY REFERENCES events(seq), tenant TEXT NOT NULL, user TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX usage_identity ON usage(tenant, user, seq);
PRAGMA user_version=1;`

type sqlReader interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func openSQLite(path string, write bool) (*sql.DB, error) {
	return openSQLiteMode(path, write, false)
}
func openSQLiteMode(path string, write, readOnly bool) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	if readOnly {
		q.Set("mode", "ro")
	}
	q.Add("_pragma", "busy_timeout(3000)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Add("_pragma", "foreign_keys(ON)")
	if write {
		q.Set("_txlock", "immediate")
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: q.Encode()}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Only initialization and legacy conversion need the old directory lock.
// Established databases use SQLite's process locks and crash recovery.
func (s Store) database(write bool) (*sql.DB, error) {
	if s.Path == "" {
		return nil, fmt.Errorf("store path required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(s.Path)
	if err != nil {
		return nil, err
	}
	value, _ := storeInitialization.LoadOrStore(abs, &storeInit{})
	init := value.(*storeInit)
	init.mu.Lock()
	if !init.ready {
		ready, e := isSQLite(s.Path)
		if e == nil && !ready {
			var unlock func()
			unlock, e = migrationLock(s.Path)
			if e == nil {
				e = s.prepareSQLite()
				unlock()
			}
		}
		if e != nil {
			init.mu.Unlock()
			return nil, e
		}
		bootstrap, e := openSQLite(s.Path, true)
		if e == nil {
			e = configureSQLite(bootstrap)
		}
		if bootstrap != nil {
			bootstrap.Close()
		}
		if e != nil {
			init.mu.Unlock()
			return nil, e
		}
		init.ready = true
	}
	init.mu.Unlock()
	db, err := openSQLite(s.Path, write)
	if err != nil {
		return nil, err
	}
	if err = configureSQLite(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func configureSQLite(db *sql.DB) error {
	var version int
	var mode string
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("unsupported memory schema %d; expected 1", version)
	}
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return err
	}
	if mode != "wal" {
		if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			return err
		}
	}
	if mode != "wal" {
		return fmt.Errorf("WAL mode unavailable")
	}
	return nil
}

func isSQLite(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	b := make([]byte, len(sqliteHeader))
	n, err := io.ReadFull(f, b)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false, err
	}
	return n == len(b) && string(b) == sqliteHeader, nil
}

func migrationLock(path string) (func(), error) {
	lock := path + ".lock"
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := os.Mkdir(lock, 0700)
		if err == nil {
			return func() { os.Remove(lock) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("migration busy: verify all Elephant processes stopped before removing %s", lock)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func (s Store) prepareSQLite() error {
	ready, err := isSQLite(s.Path)
	if err != nil || ready {
		return err
	}
	// Rebuild a private candidate after an interrupted conversion. Never publish
	// it before a complete, validated import and a durable original backup.
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".elephant-migrate-*")
	if err != nil {
		return err
	}
	candidate := f.Name()
	if err = f.Close(); err != nil {
		return err
	}
	defer os.Remove(candidate)
	db, err := openSQLite(candidate, true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(sqliteSchema); err != nil {
		return err
	}
	source, err := os.Open(s.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		defer source.Close()
		scan := bufio.NewScanner(source)
		scan.Buffer(make([]byte, 4096), 2<<20)
		line := 0
		for scan.Scan() {
			line++
			var e Event
			if err = json.Unmarshal(scan.Bytes(), &e); err == nil {
				err = applyEvent(tx, e)
			}
			if err != nil {
				return fmt.Errorf("journal line %d: %w (original unchanged; repair or restore explicitly)", line, err)
			}
		}
		if err = scan.Err(); err != nil {
			return err
		}
		if _, err = source.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err = backupOriginal(source, s.Path+".jsonl-backup"); err != nil {
			return err
		}
		if err = source.Close(); err != nil {
			return err
		}
		if err = syncDirectory(filepath.Dir(s.Path)); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	if err = syncFile(candidate); err != nil {
		return err
	}
	if err = os.Rename(candidate, s.Path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(s.Path))
}

func backupOriginal(source *os.File, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		// A previous interrupted import may already have made its backup.
		old, e := os.Open(path)
		if e != nil {
			return e
		}
		defer old.Close()
		a, b := make([]byte, 65536), make([]byte, 65536)
		for {
			na, ea := io.ReadFull(source, a)
			nb, eb := io.ReadFull(old, b)
			if na != nb || !bytes.Equal(a[:na], b[:nb]) {
				return fmt.Errorf("existing migration backup differs: %s", path)
			}
			if ea == io.EOF || ea == io.ErrUnexpectedEOF {
				if eb != ea {
					return fmt.Errorf("migration backup length differs")
				}
				return syncFile(path)
			}
			if ea != nil {
				return ea
			}
			if eb != nil {
				return eb
			}
		}
	}
	if err != nil {
		return err
	}
	_, err = io.Copy(f, source)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		os.Remove(path)
		return err
	}
	return closeErr
}

func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func readMemories(q sqlReader) ([]Memory, error) {
	rows, err := q.Query("SELECT data FROM memories ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		var data string
		var m Memory
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(data), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func applyEvent(tx *sql.Tx, e Event) error {
	switch e.Kind {
	case "put":
		if e.Memory == nil || e.Memory.ID == "" {
			return fmt.Errorf("invalid put")
		}
		m := e.Memory
		if err := Validate(*m); err != nil {
			return err
		}
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO memories VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET tenant=excluded.tenant,owner=excluded.owner,project=excluded.project,scope=excluded.scope,data=excluded.data", m.ID, m.Tenant, m.Owner, m.Project, m.Scope, string(data)); err != nil {
			return err
		}
	case "feedback", "retire":
		var data string
		var m Memory
		if err := tx.QueryRow("SELECT data FROM memories WHERE id=?", e.ID).Scan(&data); err != nil {
			return fmt.Errorf("%s for unknown memory: %w", e.Kind, err)
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return err
		}
		if e.Kind == "feedback" {
			result, err := tx.Exec("INSERT OR IGNORE INTO seen VALUES(?)", e.ID+":"+e.FeedbackID)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 0 && e.Helpful {
				m.Helpful++
			} else if n != 0 {
				m.Unhelpful++
			}
		} else {
			m.Retired = true
		}
		b, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE memories SET data=? WHERE id=?", string(b), m.ID); err != nil {
			return err
		}
	case "experience":
		if e.Experience == nil {
			return fmt.Errorf("invalid experience")
		}
	case "recall":
		if e.Recall == nil {
			return fmt.Errorf("invalid recall")
		}
	default:
		return fmt.Errorf("unknown event kind %q", e.Kind)
	}
	if x := e.Experience; x != nil {
		if x.ID == "" || x.Identity.Tenant == "" || x.Identity.User == "" {
			return fmt.Errorf("invalid experience identity")
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO seen VALUES(?)", "experience:"+x.ID); err != nil {
			return err
		}
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	r, err := tx.Exec("INSERT INTO events(kind,data) VALUES(?,?)", e.Kind, string(b))
	if err != nil {
		return err
	}
	seq, err := r.LastInsertId()
	if err != nil {
		return err
	}
	if x := e.Experience; x != nil {
		data, err := json.Marshal(x)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO experiences VALUES(?,?,?,?,?,?,?)", seq, x.Identity.Tenant, x.Identity.User, x.Project, x.Session, x.Task, string(data)); err != nil {
			return err
		}
	}
	if u := e.Recall; u != nil {
		data, err := json.Marshal(u)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO usage VALUES(?,?,?,?)", seq, u.Identity.Tenant, u.Identity.User, string(data)); err != nil {
			return err
		}
	}
	return nil
}

// VACUUM INTO includes committed WAL pages and produces a standalone snapshot.
// The destination must be new. Publication never replaces an existing backup.
func (s Store) Backup(destination string) error {
	db, err := s.database(false)
	if err != nil {
		return err
	}
	defer db.Close()
	return backupDatabase(db, destination)
}
func backupDatabase(db *sql.DB, destination string) error {
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if abs == "" || destination == "" {
		return fmt.Errorf("backup destination required")
	}
	if _, err = os.Lstat(abs); err == nil {
		return fmt.Errorf("backup destination exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(abs), ".elephant-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "snapshot.sqlite")
	if _, err = db.ExecContext(context.Background(), "VACUUM INTO ?", path); err != nil {
		return err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return err
	}
	if err = verifyDatabase(path); err != nil {
		return err
	}
	if err = syncFile(path); err != nil {
		return err
	}
	if err = os.Link(path, abs); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(abs))
}

func verifyDatabase(path string) error {
	db, err := openSQLiteMode(path, false, true)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	var version int
	if err = db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("database integrity: %s", result)
	}
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("unsupported backup schema %d", version)
	}
	for _, table := range []string{"memories", "events", "seen", "experiences", "usage"} {
		var count int
		if err = db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
			return err
		}
	}
	return nil
}

// Restore creates a NEW store. Replacing a live database or its WAL is unsafe.
func RestoreStore(backup, destination string) error {
	if destination == "" {
		return fmt.Errorf("restore destination required")
	}
	if err := verifyDatabase(backup); err != nil {
		return err
	}
	db, err := openSQLiteMode(backup, false, true)
	if err != nil {
		return err
	}
	defer db.Close()
	return backupDatabase(db, destination)
}
