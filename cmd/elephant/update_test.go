package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	memory "github.com/prithivrajmu/elephant"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpdateCommandRefreshesCachedStatus(t *testing.T) {
	t.Setenv("ELEPHANT_UPDATE_CHECKS", "")
	t.Setenv("ELEPHANT_GITHUB_TOKEN", "")
	var major, minor int
	if _, err := fmt.Sscanf(memory.Version, "%d.%d.", &major, &minor); err != nil {
		t.Fatal(err)
	}
	version := fmt.Sprintf("%d.%d.0", major, minor+1)
	url := "https://github.com/prithivrajmu/elephant/releases/tag/v" + version
	payload := fmt.Sprintf(`[{"tag_name":"v%s","name":"New release","html_url":%q,"assets":[{"name":"elephant-%s-%s-%s.zip"}]}]`, version, url, version, runtime.GOOS, runtime.GOARCH)
	oldArgs, oldOut, oldTransport := os.Args, os.Stdout, http.DefaultTransport
	t.Cleanup(func() { os.Args, os.Stdout, http.DefaultTransport = oldArgs, oldOut, oldTransport })
	for _, tc := range []struct {
		name     string
		flags    []string
		disabled bool
		requests int
	}{
		{name: "plain", requests: 2},
		{name: "explicit", flags: []string{"--check"}, requests: 2},
		{name: "json check", flags: []string{"--json", "--check"}, requests: 2},
		{name: "json cache", flags: []string{"--json"}},
		{name: "explicit cache", flags: []string{"--check=false"}},
		{name: "dismiss", flags: []string{"--dismiss"}},
		{name: "disable", flags: []string{"--enabled=false"}},
		{name: "enable", flags: []string{"--enabled=true"}},
		{name: "disabled", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			store := filepath.Join(root, "memory.sqlite")
			cache, err := json.Marshal(memory.UpdateStatus{Enabled: !tc.disabled, State: "current", Checked: time.Now().UTC(), Installed: memory.Version})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(store+".updates.json", cache, 0600); err != nil {
				t.Fatal(err)
			}
			requests := 0
			http.DefaultTransport = releaseTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				if r.Method != "GET" || r.URL.Host != "api.github.com" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload)), Header: make(http.Header)}, nil
			})
			for range 2 {
				out, err := os.CreateTemp(t.TempDir(), "stdout")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { out.Close() })
				os.Stdout = out
				os.Args = append([]string{"elephant", "update", "--root", root, "--store", store}, tc.flags...)
				if err = run(); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(out.Name())
				if err != nil {
					t.Fatal(err)
				}
				if tc.requests > 0 && (!strings.Contains(string(data), version) || !strings.Contains(string(data), url)) {
					t.Fatalf("fresh check omitted available version or link: %s", data)
				}
			}
			if requests != tc.requests {
				t.Fatalf("made %d requests, want %d", requests, tc.requests)
			}
			if _, err := os.Stat(store); !os.IsNotExist(err) {
				t.Fatalf("update command touched the memory store: %v", err)
			}
		})
	}
}
