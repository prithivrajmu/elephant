package memory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
