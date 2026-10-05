package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

type syncTransport struct {
	base http.RoundTripper
	drop bool
}

func (t *syncTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, e := t.base.RoundTrip(r)
	if e == nil && t.drop {
		t.drop = false
		response.Body.Close()
		return nil, fmt.Errorf("lost acknowledgement")
	}
	return response, e
}
func TestCloudSyncRetryConflictRetirement(t *testing.T) {
	id, _, m := fixture()
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	m.Requires = nil
	local, e := s.Record(m)
	if e != nil {
		t.Fatal(e)
	}
	other := m
	other.Scope = "project"
	other.Project = "private"
	other.Lesson = "Never upload project by default"
	s.Record(other)
	remoteID := Identity{Tenant: "hosted", User: "oauth-alice"}
	items := map[string]cloudItem{}
	receipts := map[string]cloudResponse{}
	operations := []string{}
	seq := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" || r.Header.Get("X-Elephant-User") != remoteID.User || r.Header.Get("X-Elephant-Tenant") != remoteID.Tenant {
			w.WriteHeader(403)
			return
		}
		var a struct {
			Action    string                     `json:"action"`
			Operation string                     `json:"operation_id"`
			Revision  int                        `json:"expected_revision"`
			Memory    map[string]json.RawMessage `json:"memory"`
		}
		json.NewDecoder(r.Body).Decode(&a)
		if a.Action == "push" {
			operations = append(operations, a.Operation)
			if old, ok := receipts[a.Operation]; ok {
				json.NewEncoder(w).Encode(old)
				return
			}
			data, _ := json.Marshal(a.Memory)
			var n Memory
			json.Unmarshal(data, &n)
			json.Unmarshal(a.Memory["project_id"], &n.Project)
			json.Unmarshal(a.Memory["conversation_id"], &n.Conversation)
			old := items[n.ID]
			if old.Revision != a.Revision || (old.Memory.Retired && !n.Retired) {
				w.WriteHeader(409)
				return
			}
			n.Tenant = remoteID.Tenant
			n.Owner = remoteID.User
			seq++
			v := cloudItem{Revision: old.Revision + 1, Memory: n}
			items[n.ID] = v
			out := cloudResponse{Version: 1, Revision: v.Revision, Memory: n}
			receipts[a.Operation] = out
			json.NewEncoder(w).Encode(out)
		} else {
			out := cloudResponse{Version: 1, Cursor: seq, Items: []cloudItem{}}
			for _, v := range items {
				out.Items = append(out.Items, v)
			}
			json.NewEncoder(w).Encode(out)
		}
	}))
	defer server.Close()
	transport := &syncTransport{base: server.Client().Transport, drop: true}
	client := &http.Client{Transport: transport}
	o := CloudSyncOptions{Endpoint: server.URL, Token: "secret-token", Remote: remoteID, Scopes: []string{"personal"}, Client: client}
	if r, e := s.SyncCloud(id, o); e == nil || r.Pending != 1 {
		t.Fatalf("ack loss not retained %v %v", r, e)
	}
	// A new Store instance simulates restart. The request ID must be unchanged.
	s = Store{Path: s.Path}
	r, e := s.SyncCloud(id, o)
	if e != nil || r.Uploaded != 1 || len(items) != 1 || len(operations) != 2 || operations[0] != operations[1] {
		t.Fatalf("retry %v %v %v", r, e, operations)
	}
	db, e := s.database(false)
	if e != nil {
		t.Fatal(e)
	}
	var data string
	db.QueryRow("SELECT payload FROM cloud_outbox LIMIT 1").Scan(&data)
	db.Close()
	if strings.Contains(data, "secret-token") {
		t.Fatal("credential persisted")
	}
	// A second machine imports the same cloud ID into its own local identity.
	second := Store{Path: filepath.Join(t.TempDir(), "second.sqlite")}
	secondID := Identity{Tenant: "local", User: "bob"}
	if _, e = second.SyncCloud(secondID, o); e != nil {
		t.Fatal(e)
	}
	if e = s.Retire(id, local.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.SyncCloud(id, o); e != nil {
		t.Fatal(e)
	}
	if _, e = second.SyncCloud(secondID, o); e != nil {
		t.Fatal(e)
	}
	all, _ := second.All()
	if len(all) != 1 || !all[0].Retired || all[0].Owner != secondID.User {
		t.Fatalf("retirement propagation %+v", all)
	}
	// A reviewed local conflict must not revive a remote retirement.
	if e = second.transact(func(_ []Memory, _ map[string]bool) (*Event, error) {
		n := all[0]
		n.Retired = false
		return &Event{Kind: "put", Memory: &n}, nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = second.SyncCloud(secondID, o); e == nil {
		t.Fatal("resurrection accepted")
	}
	o.Resolve = "remote"
	o.ID = local.ID
	if _, e = second.SyncCloud(secondID, o); e != nil {
		t.Fatal(e)
	}
	all, _ = second.All()
	if !all[0].Retired {
		t.Fatal("remote resolution lost retirement")
	}
}
func TestCloudSyncRequiresOptInAndRejectsRedirects(t *testing.T) {
	id, _, _ := fixture()
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	if _, e := s.SyncCloud(id, CloudSyncOptions{}); e == nil {
		t.Fatal("implicit sync accepted")
	}
	contacted := false
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted = true; io.WriteString(w, "oops") }))
	defer sink.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, sink.URL, 307) }))
	defer redirect.Close()
	_, e := cloudPost(CloudSyncOptions{Endpoint: redirect.URL, Token: "token", Remote: id}, map[string]string{"action": "pull"})
	if e == nil || contacted {
		t.Fatal("redirect forwarded token")
	}
}

func TestCloudSyncKeepsUnselectedLocalScope(t *testing.T) {
	id, _, m := fixture()
	m.Scope = "project"
	m.Project = "private"
	m.Requires = nil
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	saved, e := s.Record(m)
	if e != nil {
		t.Fatal(e)
	}
	remote := saved
	remote.Scope = "personal"
	remote.Tenant = "hosted"
	remote.Owner = "alice"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(cloudResponse{Version: 1, Cursor: 1, Items: []cloudItem{{Revision: 1, Memory: remote}}})
	}))
	defer server.Close()
	_, e = s.SyncCloud(id, CloudSyncOptions{Endpoint: server.URL, Token: "token", Remote: Identity{Tenant: "hosted", User: "alice"}, Scopes: []string{"personal"}, Client: server.Client()})
	if e == nil || !strings.Contains(e.Error(), "unselected") {
		t.Fatalf("unselected scope overwritten: %v", e)
	}
	all, _ := s.All()
	if len(all) != 1 || all[0].Scope != "project" {
		t.Fatal("local scope changed")
	}
}

// fakeCloud emulates the hosted /v1/sync and /v1/team contracts that matter for
// local sync: operation replay, expected revisions, immutable project_id and
// no resurrection. It records every request body for privacy assertions.
type fakeCloud struct {
	mu        sync.Mutex
	remote    Identity
	items     map[string]cloudItem
	itemSeq   map[string]int
	receipts  map[string]cloudResponse
	payloads  map[string]string
	proposals []TeamProposal
	seq       int
	bodies    []string
	pushes    []fakePush
	conflicts int
}
type fakePush struct {
	ID, Operation, Project string
	Retired                bool
}

func newFakeCloud(remote Identity) *fakeCloud {
	return &fakeCloud{remote: remote, items: map[string]cloudItem{}, itemSeq: map[string]int{}, receipts: map[string]cloudResponse{}, payloads: map[string]string{}}
}

// put stores a hosted revision as another machine would.
func (f *fakeCloud) put(m Memory) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m.Tenant = f.remote.Tenant
	m.Owner = f.remote.User
	f.seq++
	f.items[m.ID] = cloudItem{Revision: f.items[m.ID].Revision + 1, Memory: m}
	f.itemSeq[m.ID] = f.seq
}
func (f *fakeCloud) item(id string) cloudItem {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.items[id]
}
func (f *fakeCloud) allBodies() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.bodies, "\n")
}
func (f *fakeCloud) pushLog() []fakePush {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakePush(nil), f.pushes...)
}
func (f *fakeCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies = append(f.bodies, string(body))
	if r.Header.Get("X-Elephant-User") != f.remote.User || r.Header.Get("X-Elephant-Tenant") != f.remote.Tenant {
		w.WriteHeader(403)
		return
	}
	var a struct {
		Action    string                     `json:"action"`
		Operation string                     `json:"operation_id"`
		Revision  int                        `json:"expected_revision"`
		Cursor    int                        `json:"cursor"`
		Memory    map[string]json.RawMessage `json:"memory"`
		MemoryID  string                     `json:"memory_id"`
	}
	if json.Unmarshal(body, &a) != nil {
		w.WriteHeader(400)
		return
	}
	if r.URL.Path == "/v1/team" {
		if a.Action == "propose" {
			f.proposals = append(f.proposals, TeamProposal{ID: "proposal-" + a.MemoryID, Author: f.remote.User, Reviewer: "reviewer", Revision: 2, Digest: "digest", Approved: true, Memory: f.items[a.MemoryID].Memory})
		}
		json.NewEncoder(w).Encode(teamPage{Version: 1, Cursor: len(f.proposals), Items: f.proposals})
		return
	}
	if a.Action == "pull" {
		out := cloudResponse{Version: 1, Cursor: f.seq, Items: []cloudItem{}}
		ids := []string{}
		for id := range f.items {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if f.itemSeq[id] > a.Cursor {
				out.Items = append(out.Items, f.items[id])
			}
		}
		json.NewEncoder(w).Encode(out)
		return
	}
	var n Memory
	data, _ := json.Marshal(a.Memory)
	json.Unmarshal(data, &n)
	json.Unmarshal(a.Memory["project_id"], &n.Project)
	json.Unmarshal(a.Memory["conversation_id"], &n.Conversation)
	f.pushes = append(f.pushes, fakePush{ID: n.ID, Operation: a.Operation, Project: n.Project, Retired: n.Retired})
	if old, ok := f.receipts[a.Operation]; ok {
		if f.payloads[a.Operation] != string(body) {
			f.conflicts++
			w.WriteHeader(409)
			return
		}
		json.NewEncoder(w).Encode(old)
		return
	}
	old, exists := f.items[n.ID]
	if old.Revision != a.Revision || (exists && (old.Memory.Project != n.Project || old.Memory.Scope != n.Scope)) || (old.Memory.Retired && !n.Retired) {
		f.conflicts++
		w.WriteHeader(409)
		return
	}
	n.Tenant = f.remote.Tenant
	n.Owner = f.remote.User
	f.seq++
	v := cloudItem{Revision: old.Revision + 1, Memory: n}
	f.items[n.ID] = v
	f.itemSeq[n.ID] = f.seq
	out := cloudResponse{Version: 1, Revision: v.Revision, Memory: n}
	f.receipts[a.Operation] = out
	f.payloads[a.Operation] = string(body)
	json.NewEncoder(w).Encode(out)
}

// cloudTransport simulates being offline (no request reaches the server) or a
// lost acknowledgement (the server applies the request, the response is lost).
type cloudTransport struct {
	base    http.RoundTripper
	offline bool
	dropAck bool
}

func (t *cloudTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.offline {
		return nil, fmt.Errorf("offline")
	}
	response, e := t.base.RoundTrip(r)
	if e == nil && t.dropAck {
		t.dropAck = false
		response.Body.Close()
		return nil, fmt.Errorf("lost acknowledgement")
	}
	return response, e
}

func newCloudFixture(t *testing.T) (*fakeCloud, *cloudTransport, CloudSyncOptions) {
	t.Helper()
	remote := Identity{Tenant: "hosted", User: "oauth-alice"}
	f := newFakeCloud(remote)
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	transport := &cloudTransport{base: server.Client().Transport}
	return f, transport, CloudSyncOptions{Endpoint: server.URL, Token: "secret-token", Remote: remote, Scopes: []string{"personal", "project"}, Client: &http.Client{Transport: transport}}
}
func recordSyncFixture(t *testing.T, s Store, project, lesson string) Memory {
	t.Helper()
	_, _, m := fixture()
	m.Requires = nil
	m.Scope = "project"
	m.Project = project
	m.Lesson = lesson
	saved, e := s.Record(m)
	if e != nil {
		t.Fatal(e)
	}
	return saved
}
func localMemory(t *testing.T, s Store, id string) Memory {
	t.Helper()
	all, e := s.All()
	if e != nil {
		t.Fatal(e)
	}
	for _, m := range all {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("memory %s missing", id)
	return Memory{}
}

func TestCloudSyncResolveRemoteKeepsLocalRetirement(t *testing.T) {
	id, _, _ := fixture()
	f, _, o := newCloudFixture(t)
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	saved := recordSyncFixture(t, s, "cloud-app", "Bound query concurrency to available pool capacity")
	if _, e := s.SyncCloud(id, o); e != nil {
		t.Fatal(e)
	}
	// Another machine edits the hosted copy while this machine forgets it.
	edited := f.item(saved.ID).Memory
	edited.Lesson = "Bound query concurrency to the pool capacity"
	f.put(edited)
	if e := s.Retire(id, saved.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SyncCloud(id, o); e == nil || !strings.Contains(e.Error(), "409") {
		t.Fatalf("stale retirement was not a reviewed conflict: %v", e)
	}
	o.Resolve = "remote"
	o.ID = saved.ID
	r, e := s.SyncCloud(id, o)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.KeptLocalRetirements) != 1 || r.KeptLocalRetirements[0] != saved.ID || r.Pending != 0 {
		t.Fatalf("kept retirement not reported: %+v", r)
	}
	if !localMemory(t, s, saved.ID).Retired {
		t.Fatal("remote resolution resurrected a local retirement")
	}
	if remote := f.item(saved.ID); !remote.Memory.Retired || remote.Revision != 3 {
		t.Fatalf("retirement not sent in the resolving run: %+v", remote)
	}
}

func TestCloudSyncOfflineRetirementSupersedesQueuedSnapshot(t *testing.T) {
	id, _, _ := fixture()
	f, transport, o := newCloudFixture(t)
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	first := recordSyncFixture(t, s, "cloud-app", "Bound query concurrency to available pool capacity")
	second := recordSyncFixture(t, s, "cloud-app", "Never upload project by default")
	transport.offline = true
	if r, e := s.SyncCloud(id, o); e == nil || r.Pending != 2 || r.Uploaded != 0 {
		t.Fatalf("offline sync %+v %v", r, e)
	}
	for _, m := range []Memory{first, second} {
		if e := s.Retire(id, m.ID); e != nil {
			t.Fatal(e)
		}
	}
	transport.offline = false
	r, e := s.SyncCloud(id, o)
	if e != nil || r.Pending != 0 {
		t.Fatalf("reconnect sync %+v %v", r, e)
	}
	for _, m := range []Memory{first, second} {
		if !f.item(m.ID).Memory.Retired {
			t.Fatalf("%s still active remotely", m.ID)
		}
	}
	// Only the payload that may have reached the server is resent active; the
	// never-attempted payload is replaced by the retirement.
	active := map[string]int{}
	for _, p := range f.pushLog() {
		if !p.Retired {
			active[p.ID]++
		}
	}
	if len(active) != 1 || f.conflicts != 0 || r.Uploaded != 3 {
		t.Fatalf("offline retirement precedence: active=%v conflicts=%d result=%+v", active, f.conflicts, r)
	}
	if r, e := s.SyncCloud(id, o); e != nil || r.Uploaded != 0 || r.Pending != 0 {
		t.Fatalf("converged sync re-uploaded %+v %v", r, e)
	}
}

func TestCloudSyncLostAcknowledgementThenRetirement(t *testing.T) {
	id, _, _ := fixture()
	f, transport, o := newCloudFixture(t)
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	saved := recordSyncFixture(t, s, "cloud-app", "Bound query concurrency to available pool capacity")
	transport.dropAck = true
	if r, e := s.SyncCloud(id, o); e == nil || r.Pending != 1 {
		t.Fatalf("lost acknowledgement not retained %+v %v", r, e)
	}
	if f.item(saved.ID).Revision != 1 {
		t.Fatal("server did not apply the first push")
	}
	if e := s.Retire(id, saved.ID); e != nil {
		t.Fatal(e)
	}
	s = Store{Path: s.Path}
	r, e := s.SyncCloud(id, o)
	if e != nil || r.Uploaded != 2 || r.Pending != 0 {
		t.Fatalf("retirement after lost acknowledgement %+v %v", r, e)
	}
	pushes := f.pushLog()
	if len(pushes) != 3 || pushes[0].Operation != pushes[1].Operation || pushes[1].Retired || !pushes[2].Retired || f.conflicts != 0 {
		t.Fatalf("retry was not byte-identical before retirement: %+v conflicts=%d", pushes, f.conflicts)
	}
	if remote := f.item(saved.ID); !remote.Memory.Retired || remote.Revision != 2 {
		t.Fatalf("remote not retired %+v", remote)
	}
}

func TestCloudSyncPseudonymizesPathProjects(t *testing.T) {
	id, _, _ := fixture()
	f, _, o := newCloudFixture(t)
	root := t.TempDir()
	projectPath := filepath.Join(root, "private-repo")
	s := Store{Path: filepath.Join(root, "m.sqlite")}
	pathMemory := recordSyncFixture(t, s, projectPath, "Bound query concurrency to available pool capacity")
	explicit := recordSyncFixture(t, s, "cloud-safe-id", "Never upload project by default")
	if _, e := s.SyncCloud(id, o); e != nil {
		t.Fatal(e)
	}
	opaque := cloudProjectID(o.Remote.Tenant, projectPath)
	if opaque != cloudProjectID(o.Remote.Tenant, projectPath) || !strings.HasPrefix(opaque, "p-") || len(opaque) != 34 {
		t.Fatalf("unstable or malformed opaque project ID %q", opaque)
	}
	if got := f.item(pathMemory.ID).Memory.Project; got != opaque {
		t.Fatalf("hosted project %q, want %q", got, opaque)
	}
	if got := f.item(explicit.ID).Memory.Project; got != "cloud-safe-id" {
		t.Fatalf("explicit project changed to %q", got)
	}
	if got := localMemory(t, s, pathMemory.ID).Project; got != projectPath {
		t.Fatalf("pull replaced local project with %q", got)
	}
	// A converged rerun uploads nothing; retirement keeps the opaque ID.
	if r, e := s.SyncCloud(id, o); e != nil || r.Uploaded != 0 {
		t.Fatalf("rerun %+v %v", r, e)
	}
	if e := s.Retire(id, pathMemory.ID); e != nil {
		t.Fatal(e)
	}
	if r, e := s.SyncCloud(id, o); e != nil || r.Uploaded != 1 || !f.item(pathMemory.ID).Memory.Retired {
		t.Fatalf("retirement %+v %v", r, e)
	}
	// Another machine imports the opaque ID as the record's project.
	second := Store{Path: filepath.Join(t.TempDir(), "second.sqlite")}
	if _, e := second.SyncCloud(Identity{Tenant: "local", User: "bob"}, o); e != nil {
		t.Fatal(e)
	}
	if got := localMemory(t, second, pathMemory.ID).Project; got != opaque {
		t.Fatalf("new pulled record project %q", got)
	}
	bodies := f.allBodies()
	for _, leak := range []string{projectPath, root, "/Users/", "private-repo"} {
		if strings.Contains(bodies, leak) {
			t.Fatalf("request bodies contain local path %q", leak)
		}
	}
}

func TestCloudSyncKeepsLegacyHostedProjectPath(t *testing.T) {
	id, _, _ := fixture()
	f, _, o := newCloudFixture(t)
	projectPath := filepath.Join(t.TempDir(), "legacy-repo")
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	saved := recordSyncFixture(t, s, projectPath, "Bound query concurrency to available pool capacity")
	if _, e := s.SyncCloud(id, o); e != nil {
		t.Fatal(e)
	}
	// Rewrite state as an earlier version left it: the hosted copy carries the raw
	// path and no hosted project_id was recorded locally.
	legacy := f.item(saved.ID).Memory
	legacy.Project = projectPath
	f.mu.Lock()
	f.items[saved.ID] = cloudItem{Revision: f.items[saved.ID].Revision, Memory: legacy}
	f.mu.Unlock()
	if e := s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
		if _, e := tx.Exec("DROP TABLE cloud_projects"); e != nil {
			return nil, e
		}
		_, e := tx.Exec("UPDATE cloud_links SET hash=? WHERE id=?", syncHash(legacy), saved.ID)
		return nil, e
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.Retire(id, saved.ID); e != nil {
		t.Fatal(e)
	}
	if r, e := s.SyncCloud(id, o); e != nil || r.Uploaded != 1 {
		t.Fatalf("legacy retirement %+v %v", r, e)
	}
	if remote := f.item(saved.ID); !remote.Memory.Retired || remote.Memory.Project != projectPath || f.conflicts != 0 {
		t.Fatalf("legacy hosted project not preserved %+v conflicts=%d", remote, f.conflicts)
	}
}

func TestCloudProjectIDRecognizesPaths(t *testing.T) {
	for project, path := range map[string]bool{"/Users/alice/repo": true, `C:\Users\alice\repo`: true, "c:/repo": true, `\\server\share\repo`: true, "cloud-safe-id": false, "c:": false, "team/app": false, "": false} {
		if isPathProject(project) != path {
			t.Fatalf("isPathProject(%q) != %v", project, path)
		}
		if got := cloudProjectID("hosted", project); path == (got == project) {
			t.Fatalf("cloudProjectID(%q) = %q", project, got)
		}
	}
	if cloudProjectID("hosted", "/repo") == cloudProjectID("other", "/repo") {
		t.Fatal("opaque project IDs must be tenant-bound")
	}
}

func TestCloudSyncResendsOutboxRowsFromEarlierVersions(t *testing.T) {
	id, _, _ := fixture()
	f, _, o := newCloudFixture(t)
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	saved := recordSyncFixture(t, s, "cloud-app", "Bound query concurrency to available pool capacity")
	payload, _ := json.Marshal(map[string]any{"action": "push", "operation_id": "legacy-operation", "expected_revision": 0, "memory": syncPayload(saved)})
	if e := s.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
		if _, e := tx.Exec("CREATE TABLE cloud_outbox(connection TEXT,id TEXT,payload TEXT NOT NULL,PRIMARY KEY(connection,id))"); e != nil {
			return nil, e
		}
		bind, _ := json.Marshal([]any{o.Endpoint, o.Remote.Tenant, o.Remote.User, id.Tenant, id.User, []string{"personal", "project"}})
		_, e := tx.Exec("INSERT INTO cloud_outbox VALUES(?,?,?)", string(bind), saved.ID, string(payload))
		return nil, e
	}); e != nil {
		t.Fatal(e)
	}
	if r, e := s.SyncCloud(id, o); e != nil || r.Uploaded != 1 {
		t.Fatalf("legacy outbox %+v %v", r, e)
	}
	// A pre-upgrade queued row may have reached the server, so it is resent as is.
	if pushes := f.pushLog(); len(pushes) != 1 || pushes[0].Operation != "legacy-operation" {
		t.Fatalf("legacy queued payload not resent: %+v", pushes)
	}
}
