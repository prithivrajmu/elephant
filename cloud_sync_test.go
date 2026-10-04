package memory

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
