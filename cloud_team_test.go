package memory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeamCloudRequiresLocalReviewAndPropagatesWithdrawal(t *testing.T) {
	id, p, m := fixture()
	m.Requires = nil
	m.Tenant = "hosted"
	m.Owner = "peer"
	m.Lesson = "Bound query concurrency."
	proposal := TeamProposal{ID: "proposal", Author: "peer", Reviewer: "reviewer", Revision: 2, Digest: "digest", Approved: true, Memory: m}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(teamPage{Version: 1, Cursor: proposal.Revision, Items: []TeamProposal{proposal}})
	}))
	defer server.Close()
	s := Store{Path: filepath.Join(t.TempDir(), "m.sqlite")}
	o := CloudSyncOptions{Endpoint: server.URL, Token: "token", Remote: Identity{Tenant: "hosted", User: "alice"}, Client: server.Client()}
	if _, e := s.PullTeamCloud(id, o, "platform"); e != nil {
		t.Fatal(e)
	}
	all, _ := s.All()
	if len(all) != 1 || all[0].Approved || all[0].Sharing.Author != "peer" {
		t.Fatalf("review staging %+v", all)
	}
	if r, e := s.Recall(id, Request{Profile: p, Task: "query"}); e != nil || len(r.Hits) != 0 {
		t.Fatal("draft was recalled")
	}
	if e := s.Approve(id, all[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.PullTeamCloud(id, o, "platform"); e != nil {
		t.Fatal(e)
	}
	all, _ = s.All()
	if !all[0].Approved {
		t.Fatal("same revision lost approval")
	}
	proposal.Revision = 3
	proposal.Approved = false
	if _, e := s.PullTeamCloud(id, o, "platform"); e != nil {
		t.Fatal(e)
	}
	all, _ = s.All()
	if all[0].Approved || !all[0].Retired {
		t.Fatal("review rejection did not withdraw local copy")
	}
	proposal.Revision = 4
	proposal.Retired = true
	if _, e := s.PullTeamCloud(id, o, "platform"); e != nil {
		t.Fatal(e)
	}
	all, _ = s.All()
	if !all[0].Retired || all[0].Sharing.Revision != 4 {
		t.Fatal("withdrawal did not propagate")
	}
}

func TestTeamCloudBodiesOmitLocalProjectPaths(t *testing.T) {
	id, _, _ := fixture()
	f, _, o := newCloudFixture(t)
	root := t.TempDir()
	projectPath := filepath.Join(root, "private-repo")
	s := Store{Path: filepath.Join(root, "m.sqlite")}
	saved := recordSyncFixture(t, s, projectPath, "Bound query concurrency to available pool capacity")
	if _, e := s.SyncCloud(id, o); e != nil {
		t.Fatal(e)
	}
	request, _ := json.Marshal(map[string]any{"action": "propose", "operation_id": "proposal-1", "team": "platform", "memory_id": saved.ID})
	if _, e := TeamCloud(o, request); e != nil {
		t.Fatal(e)
	}
	peer := Store{Path: filepath.Join(t.TempDir(), "peer.sqlite")}
	staged, e := peer.PullTeamCloud(id, o, "platform")
	if e != nil || len(staged) != 1 {
		t.Fatalf("team pull %v %v", staged, e)
	}
	if got := staged[0].Memory.Project; got != cloudProjectID(o.Remote.Tenant, projectPath) {
		t.Fatalf("team snapshot project %q", got)
	}
	bodies := f.allBodies()
	for _, leak := range []string{projectPath, root, "/Users/", "private-repo"} {
		if strings.Contains(bodies, leak) {
			t.Fatalf("team request bodies contain local path %q", leak)
		}
	}
}
