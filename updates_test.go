package memory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testRelease(tag string) githubRelease {
	r := githubRelease{Tag: tag, Name: "Smaller memory status\nNew release", URL: "https://github.com/" + releaseRepository + "/releases/tag/" + tag, Prerelease: true}
	r.Assets = append(r.Assets, struct {
		Name string `json:"name"`
	}{"elephant-" + strings.TrimPrefix(tag, "v") + "-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"})
	return r
}
func TestUpdateCachingNotificationsAndControls(t *testing.T) {
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "")
	t.Setenv("ELEPHANT_GITHUB_TOKEN", "test-token")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test-token" || r.ContentLength > 0 {
			t.Error("unexpected update request")
		}
		json.NewEncoder(w).Encode([]githubRelease{testRelease("v0.7.0-pilot")})
	}))
	defer server.Close()
	s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		d, e := s.updates(UpdateOptions{Check: true, Notify: true}, server.Client(), server.URL, now)
		if e != nil || d.State != "available" || d.Latest.Version != "0.7.0-pilot" {
			t.Fatal(d, e)
		}
		if (i == 0) != (d.Line != "") {
			t.Fatal("notice duplicated or missing", d)
		}
		if strings.Contains(d.Line, "\n") {
			t.Fatal("multiline notice")
		}
	}
	if requests.Load() != 1 {
		t.Fatal("cache not used")
	}
	if cached, e := s.CachedUpdate(); e != nil || cached.Line != "" {
		t.Fatal("cached notice repeated", cached, e)
	}
	d, e := s.updates(UpdateOptions{Dismiss: true}, server.Client(), server.URL, now)
	if e != nil || d.Dismissed != "0.7.0-pilot" {
		t.Fatal(d, e)
	}
	no := false
	d, e = s.updates(UpdateOptions{Enabled: &no, Check: true, Force: true}, server.Client(), server.URL, now)
	if e != nil || d.State != "disabled" || requests.Load() != 1 {
		t.Fatal(d, e)
	}
	b, _ := os.ReadFile(s.Path + ".updates.json")
	if strings.Contains(string(b), "test-token") {
		t.Fatal("persisted credential")
	}
	if _, e = os.Stat(s.Path); !os.IsNotExist(e) {
		t.Fatal("update lookup touched memory journal")
	}
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "0")
	fresh := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	d, e = fresh.updates(UpdateOptions{Check: true}, server.Client(), server.URL, now)
	if e != nil || d.State != "disabled" || requests.Load() != 1 {
		t.Fatal(d, e)
	}
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "")
	d, e = fresh.CachedUpdate()
	if e != nil || !d.Enabled {
		t.Fatal("process opt-out persisted", d, e)
	}
}
func TestUpdateRejectsUnpublishedIncompatibleAndUnsafeMetadata(t *testing.T) {
	for _, change := range []func(*githubRelease){
		func(r *githubRelease) { r.Draft = true }, func(r *githubRelease) { r.Tag = "v1.0.0" }, func(r *githubRelease) { r.Tag = "v0.7.0-rc.1" },
		func(r *githubRelease) { r.Assets = nil }, func(r *githubRelease) { r.URL = "javascript:alert(1)" }, func(r *githubRelease) { r.URL = "https://github.com.evil.test/releases" },
	} {
		r := testRelease("v0.7.0-pilot")
		change(&r)
		if d, e := selectRelease([]githubRelease{r}); e != nil || d != nil {
			t.Fatal(d, e)
		}
	}
	old := testRelease("v0.4.0-pilot")
	newest := testRelease("v0.8.0-pilot")
	d, e := selectRelease([]githubRelease{newest, old, testRelease("v0.7.0-pilot")})
	if e != nil || d.Version != "0.8.0-pilot" {
		t.Fatal(d, e)
	}
}
func TestOfflineUpdateIsUnavailableAndDoesNotClaimCurrent(t *testing.T) {
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "private error body", http.StatusForbidden)
	}))
	defer server.Close()
	s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	d, e := s.updates(UpdateOptions{Check: true, Notify: true}, server.Client(), server.URL, time.Now())
	if e != nil || d.State != "unavailable" || d.Latest != nil || strings.Contains(d.Line, "current") || strings.Contains(d.Line, "private error") {
		t.Fatal(d, e)
	}
	d, e = s.CachedUpdate()
	if e != nil || d.State != "unavailable" {
		t.Fatal(d, e)
	}
}
func TestUpdateTimeoutAndPayloadBound(t *testing.T) {
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	start := time.Now()
	_, e := fetchRelease(server.Client(), server.URL)
	server.Close()
	if e == nil || time.Since(start) > 3*time.Second {
		t.Fatal("unbounded update request", e)
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Repeat(" ", 257<<10))) }))
	defer server.Close()
	if _, e = fetchRelease(server.Client(), server.URL); e == nil {
		t.Fatal("oversized metadata accepted")
	}
}
