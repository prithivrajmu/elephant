package memory

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const dashboardTestHost = "127.0.0.1:7331"
const dashboardTestToken = "0123456789abcdef0123456789abcdef"

func serveDashboardTest(t *testing.T, method, host, path, origin, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	root := t.TempDir()
	id, _, _ := fixture()
	h := DashboardHandler(Service{Store: Store{Path: filepath.Join(root, "events")}, Identity: id, Root: root, Project: "app"}, dashboardTestHost, dashboardTestToken)
	r := httptest.NewRequest(method, "http://"+host+path, nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if authorization != "" {
		r.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDashboardHandlerIndexOmitsToken(t *testing.T) {
	w := serveDashboardTest(t, "GET", dashboardTestHost, "/", "", "")
	if w.Code != 200 {
		t.Fatalf("GET / = %d, want 200", w.Code)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'nonce-") || !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("missing nonce CSP: %q", csp)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
	body := w.Body.String()
	if strings.Contains(body, dashboardTestToken) {
		t.Fatal("GET / leaked the API token in the page HTML")
	}
	if strings.Contains(body, "__TOKEN__") || strings.Contains(body, "__NONCE__") {
		t.Fatal("GET / left a template placeholder in the page HTML")
	}
	if !strings.Contains(body, "Open the link printed by elephant palace") {
		t.Fatal("GET / page lacks the missing-token guidance")
	}
}

func TestDashboardHandlerAPIToken(t *testing.T) {
	for _, test := range []struct {
		name, authorization string
		want                int
	}{
		{"missing", "", 401},
		{"wrong", "Bearer " + strings.Repeat("f", len(dashboardTestToken)), 401},
		{"prefix", "Bearer " + dashboardTestToken[:8], 401},
		{"no scheme", dashboardTestToken, 401},
		{"right", "Bearer " + dashboardTestToken, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			if w := serveDashboardTest(t, "GET", dashboardTestHost, "/api/dashboard", "", test.authorization); w.Code != test.want {
				t.Fatalf("got %d want %d: %s", w.Code, test.want, w.Body.String())
			}
		})
	}
}

func TestDashboardHandlerRejectsForeignHostAndOrigin(t *testing.T) {
	auth := "Bearer " + dashboardTestToken
	for _, test := range []struct {
		name, host, path, origin string
	}{
		{"rebinding host index", "evil.example", "/", ""},
		{"rebinding host api", "evil.example:7331", "/api/dashboard", ""},
		{"foreign origin index", dashboardTestHost, "/", "https://evil.example"},
		{"foreign origin api", dashboardTestHost, "/api/dashboard", "http://localhost:7331"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if w := serveDashboardTest(t, "GET", test.host, test.path, test.origin, auth); w.Code != 403 {
				t.Fatalf("got %d want 403", w.Code)
			}
		})
	}
	if w := serveDashboardTest(t, "GET", dashboardTestHost, "/api/dashboard", "http://"+dashboardTestHost, auth); w.Code != 200 {
		t.Fatalf("same origin got %d want 200", w.Code)
	}
}

func TestDashboardURL(t *testing.T) {
	if got, want := dashboardURL("127.0.0.1:47332", "abc123"), "http://127.0.0.1:47332/#token=abc123"; got != want {
		t.Fatalf("dashboardURL = %q, want %q", got, want)
	}
}
