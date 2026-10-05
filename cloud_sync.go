package memory

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CloudSyncOptions struct {
	Endpoint, Token string
	Remote          Identity
	Scopes          []string
	Resolve, ID     string
	Client          *http.Client
}
type CloudSyncResult struct {
	Uploaded, Downloaded int
	Pending              int
	// KeptLocalRetirements lists memory IDs whose local retirement overrode an
	// active remote snapshot during --resolve remote.
	KeptLocalRetirements []string `json:",omitempty"`
}
type cloudItem struct {
	Revision int    `json:"revision"`
	Memory   Memory `json:"memory"`
}
type cloudResponse struct {
	Version  int         `json:"version"`
	Revision int         `json:"revision"`
	Memory   Memory      `json:"memory"`
	Items    []cloudItem `json:"items"`
	Cursor   int         `json:"cursor"`
	More     bool        `json:"more"`
}

func syncPayload(m Memory) map[string]any {
	v := map[string]any{"id": m.ID, "scope": m.Scope, "retired": m.Retired, "incident": m.Incident, "lesson": m.Lesson, "source": m.Source, "outcome": m.Outcome, "class": MemoryClass(m), "created": m.Created.UTC().Format(time.RFC3339Nano), "updated": m.Updated.UTC().Format(time.RFC3339Nano)}
	if m.Project != "" {
		v["project_id"] = m.Project
	}
	if m.Conversation != "" {
		v["conversation_id"] = m.Conversation
	}
	if m.Subject != "" {
		v["subject"] = m.Subject
	}
	for k, x := range map[string]map[string][]string{"features": m.Features, "requires": m.Requires, "excludes": m.Excludes} {
		if len(x) > 0 {
			v[k] = x
		}
	}
	return v
}

// isPathProject reports whether a project ID is a local filesystem path: POSIX
// absolute, Windows drive or UNC. Default projects are the absolute project root.
func isPathProject(project string) bool {
	if filepath.IsAbs(project) || strings.HasPrefix(project, "/") || strings.HasPrefix(project, `\\`) {
		return true
	}
	if len(project) < 3 || project[1] != ':' || (project[2] != '\\' && project[2] != '/') {
		return false
	}
	letter := project[0] | 0x20
	return letter >= 'a' && letter <= 'z'
}

// cloudProjectID is the opaque project_id uploaded for a path-derived project.
// Explicit non-path project IDs pass through so users can choose cloud-safe IDs.
func cloudProjectID(tenant, project string) string {
	if !isPathProject(project) {
		return project
	}
	sum := sha256.Sum256([]byte(tenant + "\x00" + project))
	return "p-" + hex.EncodeToString(sum[:])[:32]
}

// cloudWireMemory returns m with the project_id the hosted copy uses. The hosted
// service fixes project_id per memory ID, so a hosted copy that already carries
// the raw local project (uploaded before pseudonymization) keeps it; otherwise
// path-derived projects are pseudonymized. syncHash is computed on this form.
func cloudWireMemory(tx *sql.Tx, connection, tenant string, m Memory) (Memory, error) {
	var hosted string
	e := tx.QueryRow("SELECT project FROM cloud_projects WHERE connection=? AND id=?", connection, m.ID).Scan(&hosted)
	switch {
	case e == sql.ErrNoRows:
		var linked int
		if e = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM cloud_links WHERE connection=? AND id=?)", connection, m.ID).Scan(&linked); e != nil {
			return m, e
		}
		if linked == 0 {
			m.Project = cloudProjectID(tenant, m.Project)
		}
	case e != nil:
		return m, e
	case hosted != m.Project:
		m.Project = cloudProjectID(tenant, m.Project)
	}
	return m, nil
}

// linkCloud records the acknowledged hosted revision, its hash and project_id.
// Revisions never move backwards when concurrent sync processes acknowledge
// replayed receipts out of order.
func linkCloud(tx *sql.Tx, connection string, revision int, hosted Memory) error {
	if _, e := tx.Exec("INSERT INTO cloud_links VALUES(?,?,?,?) ON CONFLICT(connection,id) DO UPDATE SET revision=excluded.revision,hash=excluded.hash WHERE excluded.revision>=cloud_links.revision", connection, hosted.ID, revision, syncHash(hosted)); e != nil {
		return e
	}
	_, e := tx.Exec("INSERT INTO cloud_projects VALUES(?,?,?) ON CONFLICT(connection,id) DO UPDATE SET project=excluded.project", connection, hosted.ID, hosted.Project)
	return e
}
func syncHash(m Memory) string {
	b, _ := json.Marshal(syncPayload(m))
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func cloudHTTP(o CloudSyncOptions, path string, body any) ([]byte, error) {
	u, e := url.Parse(o.Endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("cloud endpoint must be an origin")
	}
	local := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(local && u.Scheme == "http") {
		return nil, fmt.Errorf("cloud requires HTTPS")
	}
	b, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	if len(b) > 65536 {
		return nil, fmt.Errorf("sync payload exceeds 65536 bytes")
	}
	req, e := http.NewRequest("POST", o.Endpoint+path, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.Token)
	req.Header.Set("X-Elephant-User", o.Remote.User)
	req.Header.Set("X-Elephant-Tenant", o.Remote.Tenant)
	client := o.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	// Never forward credentials to a redirect, even when a caller supplies a client.
	safe := *client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r, e := safe.Do(req)
	if e != nil {
		return nil, fmt.Errorf("sync offline; queued operations retained")
	}
	defer r.Body.Close()
	data, e := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	if e != nil || len(data) > 2<<20 {
		return nil, fmt.Errorf("invalid sync response size")
	}
	if r.StatusCode != 200 {
		return nil, fmt.Errorf("sync HTTP %d; queued operation retained (409 requires explicit conflict review)", r.StatusCode)
	}
	return data, nil
}
func cloudPost(o CloudSyncOptions, body any) (cloudResponse, error) {
	data, e := cloudHTTP(o, "/v1/sync", body)
	if e != nil {
		return cloudResponse{}, e
	}
	var out cloudResponse
	if e = json.Unmarshal(data, &out); e != nil || out.Version != 1 {
		return out, fmt.Errorf("invalid sync response")
	}
	return out, nil
}

// initLocalSync creates sync state. cloud_outbox.attempted marks payloads that
// may have reached the server; rows from older versions default to attempted.
func initLocalSync(tx *sql.Tx) error {
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS cloud_links(connection TEXT,id TEXT,revision INTEGER NOT NULL,hash TEXT NOT NULL,PRIMARY KEY(connection,id))",
		"CREATE TABLE IF NOT EXISTS cloud_outbox(connection TEXT,id TEXT,payload TEXT NOT NULL,attempted INTEGER NOT NULL DEFAULT 1,PRIMARY KEY(connection,id))",
		"CREATE TABLE IF NOT EXISTS cloud_cursors(connection TEXT PRIMARY KEY,cursor INTEGER NOT NULL)",
		"CREATE TABLE IF NOT EXISTS cloud_projects(connection TEXT,id TEXT,project TEXT NOT NULL,PRIMARY KEY(connection,id))",
	} {
		if _, e := tx.Exec(q); e != nil {
			return e
		}
	}
	var attempted int
	if e := tx.QueryRow("SELECT COUNT(*) FROM pragma_table_info('cloud_outbox') WHERE name='attempted'").Scan(&attempted); e != nil || attempted != 0 {
		return e
	}
	_, e := tx.Exec("ALTER TABLE cloud_outbox ADD COLUMN attempted INTEGER NOT NULL DEFAULT 1")
	return e
}

// SyncCloud is explicit and bounded. Hooks and MCP never call it. Outbox payloads
// and operation IDs survive process restarts; credentials remain in memory only.
func (s Store) SyncCloud(id Identity, o CloudSyncOptions) (CloudSyncResult, error) {
	var result CloudSyncResult
	u, e := url.Parse(o.Endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return result, fmt.Errorf("sync endpoint must be an origin")
	}
	local := u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(local && u.Scheme == "http") {
		return result, fmt.Errorf("sync requires HTTPS")
	}
	if o.Token == "" || o.Remote.Tenant == "" || o.Remote.User == "" {
		return result, fmt.Errorf("sync requires token and expected cloud tenant/user")
	}
	scopes := map[string]bool{}
	for _, v := range o.Scopes {
		if v != "personal" && v != "project" && v != "conversation" {
			return result, fmt.Errorf("sync supports only private scopes")
		}
		scopes[v] = true
	}
	if len(scopes) == 0 {
		return result, fmt.Errorf("explicit --sync-scopes required")
	}
	o.Scopes = nil
	for v := range scopes {
		o.Scopes = append(o.Scopes, v)
	}
	sort.Strings(o.Scopes)
	bind, _ := json.Marshal([]any{o.Endpoint, o.Remote.Tenant, o.Remote.User, id.Tenant, id.User, o.Scopes})
	connection := string(bind)
	if o.Resolve != "" && (o.ID == "" || (o.Resolve != "local" && o.Resolve != "remote")) {
		return result, fmt.Errorf("resolve requires --id and local or remote")
	}
	policy, e := s.LanguagePolicy()
	if e != nil {
		return result, e
	}
	if o.Resolve != "" {
		// Read the current remote revision before acknowledging a reviewed conflict.
		cursor := 0
		var found *cloudItem
		for pages := 0; pages < 10; pages++ {
			r, e := cloudPost(o, map[string]any{"action": "pull", "cursor": cursor, "scopes": o.Scopes})
			if e != nil {
				return result, e
			}
			for _, x := range r.Items {
				if x.Memory.ID == o.ID {
					v := x
					found = &v
				}
			}
			cursor = r.Cursor
			if !r.More {
				break
			}
		}
		if found == nil {
			return result, fmt.Errorf("conflicting remote memory not found")
		}
		e = s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
			if e := initLocalSync(tx); e != nil {
				return nil, e
			}
			var data string
			if e := tx.QueryRow("SELECT data FROM memories WHERE id=?", o.ID).Scan(&data); e != nil {
				return nil, e
			}
			var m Memory
			if e := json.Unmarshal([]byte(data), &m); e != nil {
				return nil, e
			}
			if !owned(m, id) || !scopes[m.Scope] {
				return nil, fmt.Errorf("resolution requires owned selected memory")
			}
			if o.Resolve == "remote" && m.Retired && !found.Memory.Retired {
				// Local retirement is authoritative: acknowledge the remote revision
				// and let the snapshot below send the retirement in this run.
				result.KeptLocalRetirements = append(result.KeptLocalRetirements, o.ID)
			} else if o.Resolve == "remote" {
				project := m.Project
				m = found.Memory
				m.Tenant = id.Tenant
				m.Owner = id.User
				m.Project = project
				if e := Validate(m); e != nil {
					return nil, e
				}
				report := CheckLanguage(policy, m.Lesson)
				if !report.Accepted {
					return nil, fmt.Errorf("synced lesson fails local writing policy")
				}
				m.Writing = &report
				if e := applyEvent(tx, Event{Kind: "put", Memory: &m}); e != nil {
					return nil, e
				}
			}
			if o.Resolve == "local" && found.Memory.Retired && !m.Retired {
				return nil, fmt.Errorf("remote retirement cannot be undone")
			}
			if _, e := tx.Exec("DELETE FROM cloud_outbox WHERE connection=? AND id=?", connection, o.ID); e != nil {
				return nil, e
			}
			return nil, linkCloud(tx, connection, found.Revision, found.Memory)
		})
		if e != nil {
			return result, e
		}
	}
	// Outbox state machine, per (connection, memory ID):
	//   absent -> queued(attempted=0): snapshot finds local wire state differs
	//     from the acknowledged link; the payload gets a fresh operation ID.
	//   queued(attempted=0) -> replaced: every snapshot rebuilds unattempted rows
	//     from current local state, so a later retirement supersedes a never-sent
	//     active payload.
	//   queued(attempted=0) -> queued(attempted=1): committed before network use.
	//   queued(attempted=1) -> resent byte-identically until acknowledged, since
	//     the server may have applied it and lost the acknowledgement.
	//   acknowledged -> absent: the link advances; the next round re-compares
	//     current local state and queues any newer revision (notably retirement).
	// Rounds are bounded; the write budget spans all rounds.
	budget := 1000
	for round := 0; round < 3 && budget > 0; round++ {
		queue, e := s.snapshotCloudOutbox(id, o, connection, scopes, budget)
		if e != nil {
			return result, e
		}
		result.Pending = len(queue)
		if len(queue) == 0 {
			break
		}
		for _, q := range queue {
			current := true
			e = s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
				r, e := tx.Exec("UPDATE cloud_outbox SET attempted=1 WHERE connection=? AND id=? AND payload=?", connection, q.id, q.payload)
				if e != nil {
					return nil, e
				}
				n, e := r.RowsAffected()
				current = n == 1
				return nil, e
			})
			if e != nil {
				return result, e
			}
			if !current {
				// A concurrent snapshot superseded this unsent payload.
				result.Pending--
				continue
			}
			budget--
			r, e := cloudPost(o, json.RawMessage(q.payload))
			if e != nil {
				return result, e
			}
			if r.Memory.ID != q.id || r.Revision < 1 || r.Memory.Owner != o.Remote.User || r.Memory.Tenant != o.Remote.Tenant {
				return result, fmt.Errorf("sync receipt identity mismatch")
			}
			e = s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
				if e := linkCloud(tx, connection, r.Revision, r.Memory); e != nil {
					return nil, e
				}
				_, e := tx.Exec("DELETE FROM cloud_outbox WHERE connection=? AND id=? AND payload=?", connection, q.id, q.payload)
				return nil, e
			})
			if e != nil {
				return result, e
			}
			result.Uploaded++
			result.Pending--
		}
	}
	cursor := 0
	db, e := s.database(false)
	if e != nil {
		return result, e
	}
	e = db.QueryRow("SELECT cursor FROM cloud_cursors WHERE connection=?", connection).Scan(&cursor)
	db.Close()
	if e != nil && e != sql.ErrNoRows {
		return result, e
	}
	for pages := 0; pages < 10; pages++ {
		r, e := cloudPost(o, map[string]any{"action": "pull", "cursor": cursor, "scopes": o.Scopes})
		if e != nil {
			return result, e
		}
		if r.Cursor < cursor || len(r.Items) > 128 {
			return result, fmt.Errorf("invalid sync cursor/page")
		}
		e = s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
			for _, x := range r.Items {
				m := x.Memory
				if m.Tenant != o.Remote.Tenant || m.Owner != o.Remote.User || !scopes[m.Scope] || x.Revision < 1 {
					return nil, fmt.Errorf("sync page identity/scope mismatch")
				}
				var linked int
				e := tx.QueryRow("SELECT revision FROM cloud_links WHERE connection=? AND id=?", connection, m.ID).Scan(&linked)
				if e != nil && e != sql.ErrNoRows {
					return nil, e
				}
				if x.Revision < linked {
					// A concurrent sync already acknowledged a newer revision.
					continue
				}
				var data string
				e = tx.QueryRow("SELECT data FROM memories WHERE id=?", m.ID).Scan(&data)
				if e == nil {
					var old Memory
					if e = json.Unmarshal([]byte(data), &old); e != nil {
						return nil, e
					}
					if !owned(old, id) {
						return nil, fmt.Errorf("sync ID conflicts with another local owner")
					}
					if !scopes[old.Scope] {
						return nil, fmt.Errorf("sync ID conflicts with an unselected local scope")
					}
					var hash string
					e = tx.QueryRow("SELECT hash FROM cloud_links WHERE connection=? AND id=?", connection, m.ID).Scan(&hash)
					if e != nil && e != sql.ErrNoRows {
						return nil, e
					}
					wire, e := cloudWireMemory(tx, connection, o.Remote.Tenant, old)
					if e != nil {
						return nil, e
					}
					if syncHash(wire) != hash {
						return nil, fmt.Errorf("local revision conflict; review --resolve local or remote --id %s", m.ID)
					}
					if old.Retired && !m.Retired {
						return nil, fmt.Errorf("local retirement cannot be undone")
					}
					// The hosted copy may carry an opaque project_id; keep the local one.
					m.Project = old.Project
				} else if e != sql.ErrNoRows {
					return nil, e
				}
				m.Tenant = id.Tenant
				m.Owner = id.User
				if e := Validate(m); e != nil {
					return nil, e
				}
				report := CheckLanguage(policy, m.Lesson)
				if !report.Accepted {
					return nil, fmt.Errorf("synced lesson fails local writing policy")
				}
				m.Writing = &report
				if e := applyEvent(tx, Event{Kind: "put", Memory: &m}); e != nil {
					return nil, e
				}
				if e := linkCloud(tx, connection, x.Revision, x.Memory); e != nil {
					return nil, e
				}
			}
			_, e := tx.Exec("INSERT INTO cloud_cursors VALUES(?,?) ON CONFLICT(connection) DO UPDATE SET cursor=excluded.cursor", connection, r.Cursor)
			return nil, e
		})
		if e != nil {
			return result, e
		}
		result.Downloaded += len(r.Items)
		cursor = r.Cursor
		if !r.More {
			return result, nil
		}
	}
	return result, fmt.Errorf("sync page limit reached; rerun to continue")
}

// snapshotCloudOutbox queues changed owned memories with stable operation IDs
// and returns up to limit queued payloads. Selected scopes include retired rows
// so withdrawals propagate after offline periods.
func (s Store) snapshotCloudOutbox(id Identity, o CloudSyncOptions, connection string, scopes map[string]bool, limit int) ([]cloudQueued, error) {
	queue := []cloudQueued{}
	e := s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
		if e := initLocalSync(tx); e != nil {
			return nil, e
		}
		// Unattempted payloads never reached the server; rebuild them from current
		// local state so a retirement supersedes a queued active snapshot.
		if _, e := tx.Exec("DELETE FROM cloud_outbox WHERE connection=? AND attempted=0", connection); e != nil {
			return nil, e
		}
		all, e := readOwnedSyncMemories(tx, id)
		if e != nil {
			return nil, e
		}
		for _, m := range all {
			if !scopes[m.Scope] {
				continue
			}
			var queued int
			if e = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM cloud_outbox WHERE connection=? AND id=?)", connection, m.ID).Scan(&queued); e != nil {
				return nil, e
			}
			if queued != 0 {
				// An attempted payload is resent byte-identically; once acknowledged,
				// the next round compares current local state again.
				continue
			}
			var rev int
			var hash string
			e = tx.QueryRow("SELECT revision,hash FROM cloud_links WHERE connection=? AND id=?", connection, m.ID).Scan(&rev, &hash)
			if e != nil && e != sql.ErrNoRows {
				return nil, e
			}
			wire, e := cloudWireMemory(tx, connection, o.Remote.Tenant, m)
			if e != nil {
				return nil, e
			}
			if hash == syncHash(wire) {
				continue
			}
			payload, _ := json.Marshal(map[string]any{"action": "push", "operation_id": newID(), "expected_revision": rev, "memory": syncPayload(wire)})
			if _, e = tx.Exec("INSERT INTO cloud_outbox VALUES(?,?,?,0)", connection, m.ID, string(payload)); e != nil {
				return nil, e
			}
		}
		rows, e := tx.Query("SELECT id,payload FROM cloud_outbox WHERE connection=? ORDER BY id LIMIT ?", connection, limit)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		for rows.Next() {
			var q cloudQueued
			if e = rows.Scan(&q.id, &q.payload); e != nil {
				return nil, e
			}
			queue = append(queue, q)
		}
		return nil, rows.Err()
	})
	return queue, e
}

type cloudQueued struct{ id, payload string }

func readOwnedSyncMemories(tx *sql.Tx, id Identity) ([]Memory, error) {
	rows, e := tx.Query("SELECT data FROM memories WHERE tenant=? AND owner=? ORDER BY id", id.Tenant, id.User)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		var data string
		var m Memory
		if e = rows.Scan(&data); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(data), &m); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
