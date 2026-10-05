package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Keep candidate fixtures newer when the shipped version advances.
func futureReleaseTag(offset int) string {
	current, ok := parseReleaseVersion(Version)
	if !ok {
		panic("invalid installed version in test")
	}
	return fmt.Sprintf("v%d.%d.0-pilot", current.parts[0], current.parts[1]+offset)
}

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
		json.NewEncoder(w).Encode([]githubRelease{testRelease(futureReleaseTag(1))})
	}))
	defer server.Close()
	s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		d, e := s.updates(UpdateOptions{Check: true, Notify: true}, server.Client(), server.URL, now)
		if e != nil || d.State != "available" || d.Latest.Version != strings.TrimPrefix(futureReleaseTag(1), "v") {
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
	if e != nil || d.Dismissed != strings.TrimPrefix(futureReleaseTag(1), "v") {
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
		func(r *githubRelease) { r.Draft = true }, func(r *githubRelease) { r.Tag = "v1.0.0" }, func(r *githubRelease) { r.Tag = strings.TrimSuffix(futureReleaseTag(1), "-pilot") + "-rc.1" },
		func(r *githubRelease) { r.Assets = nil }, func(r *githubRelease) { r.URL = "javascript:alert(1)" }, func(r *githubRelease) { r.URL = "https://github.com.evil.test/releases" },
	} {
		r := testRelease(futureReleaseTag(1))
		change(&r)
		if d, e := selectRelease([]githubRelease{r}); e != nil || d != nil {
			t.Fatal(d, e)
		}
	}
	old := testRelease("v0.4.0-pilot")
	newest := testRelease(futureReleaseTag(2))
	d, e := selectRelease([]githubRelease{newest, old, testRelease(futureReleaseTag(1))})
	if e != nil || d.Version != strings.TrimPrefix(futureReleaseTag(2), "v") {
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

func TestUpdateFailureDiagnosticsAndRecovery(t *testing.T) {
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "")
	for _, tc := range []struct {
		name, token, reason string
		status              int
		rate                bool
	}{
		{"private without token", "", "access_required", 404, false},
		{"private with token", "private-token", "not_found", 404, false},
		{"invalid token", "private-token", "credentials_rejected", 401, false},
		{"forbidden", "private-token", "access_denied", 403, false},
		{"primary rate limit", "", "rate_limited", 403, true},
		{"rate limit", "", "rate_limited", 429, false},
		{"server error", "", "server_error", 503, false},
		{"bad metadata", "", "invalid_metadata", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ELEPHANT_GITHUB_TOKEN", tc.token)
			var recovered bool
			var requests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if recovered {
					w.Write([]byte("[]"))
					return
				}
				if tc.rate {
					w.Header().Set("X-RateLimit-Remaining", "0")
				}
				w.WriteHeader(tc.status)
				w.Write([]byte("private-token secret-server-body"))
			}))
			defer server.Close()
			s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
			now := time.Now().UTC()
			d, e := s.updates(UpdateOptions{Check: true, Force: true}, server.Client(), server.URL, now)
			if e != nil || d.State != "unavailable" || d.Reason != tc.reason || d.Message != updateFailureMessage(tc.reason) || !strings.Contains(d.Line, d.Message) {
				t.Fatal(d, e)
			}
			cached, e := s.CachedUpdate()
			if e != nil || cached.Message != d.Message || cached.Reason != d.Reason {
				t.Fatal(cached, e)
			}
			b, _ := os.ReadFile(s.Path + ".updates.json")
			if strings.Contains(string(b), "private-token") || strings.Contains(string(b), "secret-server-body") {
				t.Fatal("sensitive diagnostic persisted")
			}
			recovered = true
			d, e = s.updates(UpdateOptions{Check: true, Force: true}, server.Client(), server.URL, now.Add(time.Second))
			if e != nil || d.State != "current" || d.Reason != "" || requests != 2 {
				t.Fatal(d, e, requests)
			}
		})
	}
}

type updateRoundTrip func(*http.Request) (*http.Response, error)

func (f updateRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpdateNetworkDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		reason string
		err    error
	}{
		{"network", fmt.Errorf("sensitive-network-details")},
		{"timeout", context.DeadlineExceeded},
	} {
		client := &http.Client{Transport: updateRoundTrip(func(*http.Request) (*http.Response, error) { return nil, tc.err })}
		_, e := fetchRelease(client, "https://example.test")
		var failure *updateCheckError
		if !errors.As(e, &failure) || failure.reason != tc.reason || strings.Contains(e.Error(), "sensitive") {
			t.Fatal(e)
		}
	}
}

func TestDashboardUpdateClickForcesCheckAndPollingDoesNot(t *testing.T) {
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "")
	t.Setenv("ELEPHANT_GITHUB_TOKEN", "")
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	requests := 0
	http.DefaultTransport = updateRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != releaseAPI || r.Method != "GET" {
			t.Errorf("unexpected release request: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private response"))}, nil
	})
	s := Store{Path: filepath.Join(t.TempDir(), "events.jsonl")}
	h := DashboardHandler(Service{Store: s}, "localhost:8788", "dashboard-token")
	for _, method := range []string{"GET", "POST", "GET", "POST"} {
		req := httptest.NewRequest(method, "http://localhost:8788/api/update", strings.NewReader(`{"check":true}`))
		req.Header.Set("Authorization", "Bearer dashboard-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var d UpdateStatus
		if e := json.Unmarshal(w.Body.Bytes(), &d); e != nil {
			t.Fatal(e)
		}
		if requests > 0 && (d.Reason != "access_required" || !strings.Contains(d.Message, "ELEPHANT_GITHUB_TOKEN")) {
			t.Fatal(d)
		}
	}
	if requests != 2 {
		t.Fatalf("two clicks must make two checks, GET polling none; got %d", requests)
	}
}

func TestBetaChannelParsesAndSharesPilotLane(t *testing.T) {
	beta, ok := parseReleaseVersion("v0.8.0-beta")
	if !ok || beta.channel != "beta" {
		t.Fatal("beta tag not parsed", beta, ok)
	}
	pilot, _ := parseReleaseVersion("v0.7.0-pilot")
	if !newerRelease(beta, pilot) || newerRelease(pilot, beta) {
		t.Fatal("beta must be newer than the earlier pilot by version number")
	}
	same, _ := parseReleaseVersion("v0.8.0-pilot")
	if newerRelease(beta, same) || newerRelease(same, beta) {
		t.Fatal("same-number pilot and beta must not outrank each other")
	}
	stable, _ := parseReleaseVersion("v0.8.0")
	if !newerRelease(stable, beta) {
		t.Fatal("stable must outrank beta")
	}
	if _, ok := parseReleaseVersion("v0.8.0-beta2"); ok {
		t.Fatal("malformed beta accepted")
	}
}
