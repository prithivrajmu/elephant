//go:build !windows

package memory

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// Synthetic release fixtures: shell scripts stand in for binaries, and a
// private platform reader marks them, so no real Elephant binary is touched.
func fakeElephant(version, marker string) []byte {
	return []byte("#!/bin/sh\n# " + marker + "\necho 'Elephant " + version + "'\n")
}

func releaseArchive(t *testing.T, entry string, body []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range []string{strings.Split(entry, "/")[0] + "/README.md", entry} {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o755)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if name == entry {
			w.Write(body)
		} else {
			w.Write([]byte("docs"))
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type releaseFixture struct {
	version  string
	archive  []byte
	sums     string
	redirect string
	started  chan struct{}
	hold     chan struct{}
	requests atomic.Int32
	server   *httptest.Server
}

func newReleaseFixture(t *testing.T, version string, binary []byte) *releaseFixture {
	t.Helper()
	f := &releaseFixture{version: version}
	stem := "elephant-" + version + "-" + runtime.GOOS + "-" + runtime.GOARCH
	f.archive = releaseArchive(t, stem+"/elephant", binary)
	sum := sha256.Sum256(f.archive)
	f.sums = hex.EncodeToString(sum[:]) + "  " + stem + ".zip\n" + strings.Repeat("0", 64) + "  elephant-" + version + "-other-arch.zip\n"
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		base := "/repos/" + releaseRepository + "/releases/"
		switch r.URL.Path {
		case base + "tags/v" + f.version:
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("release metadata request lacked the API token")
			}
			notes, _ := ReleaseNotesURL(f.version)
			json.NewEncoder(w).Encode(map[string]any{"tag_name": "v" + f.version, "html_url": notes, "prerelease": strings.Contains(f.version, "-"), "assets": []releaseAsset{
				{ID: 7, Name: stem + ".zip", Size: int64(len(f.archive))}, {ID: 8, Name: "SHA256SUMS", Size: int64(len(f.sums))}}})
		case base + "assets/7":
			if r.Header.Get("Accept") != "application/octet-stream" {
				t.Error("asset requested without octet-stream")
			}
			target := "/delivery/archive?sig=secret"
			if f.redirect != "" {
				target = f.redirect
			}
			http.Redirect(w, r, target, http.StatusFound)
		case base + "assets/8":
			io.WriteString(w, f.sums)
		case "/delivery/archive":
			if r.Header.Get("Authorization") != "" {
				t.Error("API token forwarded to the delivery host")
			}
			if f.started != nil {
				close(f.started)
				f.started = nil
			}
			if f.hold != nil {
				select {
				case <-f.hold:
				case <-r.Context().Done():
					return
				}
			}
			w.Write(f.archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *releaseFixture) installer() *releaseInstaller {
	in := newReleaseInstaller()
	in.api = f.server.URL + "/repos/" + releaseRepository
	u, _ := url.Parse(f.server.URL)
	in.redirect = map[string]bool{u.Host: true}
	client := *f.server.Client()
	client.CheckRedirect = in.checkRedirect
	in.client = &client
	in.platform = func(p string) (string, string, error) {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", "", err
		}
		if bytes.Contains(b, []byte("WRONGPLATFORM")) {
			return runtime.GOOS, "other", nil
		}
		return runtime.GOOS, runtime.GOARCH, nil
	}
	return in
}

func installTarget(t *testing.T) (string, []byte) {
	t.Helper()
	t.Setenv("ELEPHANT_GITHUB_TOKEN", "test-token")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "elephant")
	old := fakeElephant("1.2.0", "old")
	if err := os.WriteFile(target, old, 0o755); err != nil {
		t.Fatal(err)
	}
	return target, old
}

func assertTargetIntact(t *testing.T, target string, want []byte) {
	t.Helper()
	if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("target changed: %q %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".elephant-update-") {
			t.Fatal("staging left behind:", e.Name())
		}
	}
}

func TestInstallUpdateReplacesBinaryAtomically(t *testing.T) {
	target, old := installTarget(t)
	link := filepath.Join(filepath.Dir(target), "elephant-link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	running, err := os.Open(target) // stands in for a running process's file
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()
	newBinary := fakeElephant("1.3.0", "new")
	f := newReleaseFixture(t, "1.3.0", newBinary)
	in := f.installer()
	res, err := in.install(context.Background(), link, "1.3.0")
	if err != nil || res != (UpdateInstallResult{Target: target, Previous: "1.2.0", Version: "1.3.0"}) {
		t.Fatal(res, err)
	}
	assertTargetIntact(t, target, newBinary)
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced instead of its target", err)
	}
	if fi, err := os.Stat(target); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatal("mode not preserved", err)
	}
	if v, err := ExecutableVersion(context.Background(), target); err != nil || v != "1.3.0" {
		t.Fatal(v, err)
	}
	if got, err := io.ReadAll(running); err != nil || !bytes.Equal(got, old) {
		t.Fatal("open handle lost the previous binary", err)
	}
	// A repeated request is decided by the binary on disk, without networking.
	before := f.requests.Load()
	if _, err := in.install(context.Background(), target, "1.3.0"); err == nil || !strings.Contains(err.Error(), "not a newer") || f.requests.Load() != before {
		t.Fatal("repeated install not refused locally", err)
	}
}

func TestInstallUpdateRejectsAndPreservesTarget(t *testing.T) {
	for _, tc := range []struct {
		name, version, want string
		binary              []byte
		change              func(*releaseFixture, *releaseInstaller, string)
		offline             bool
	}{
		{name: "checksum", want: "checksum", change: func(f *releaseFixture, _ *releaseInstaller, _ string) {
			f.sums = strings.Repeat("a", 64) + f.sums[64:]
		}},
		{name: "unlisted", want: "does not list", change: func(f *releaseFixture, _ *releaseInstaller, _ string) {
			f.sums = f.sums[strings.Index(f.sums, "\n")+1:]
		}},
		{name: "version mismatch", binary: fakeElephant("1.4.0", "liar"), want: "reports version 1.4.0"},
		{name: "wrong platform", binary: fakeElephant("1.3.0", "WRONGPLATFORM"), want: "platform"},
		{name: "corrupt archive", want: "invalid", change: func(f *releaseFixture, _ *releaseInstaller, _ string) {
			f.archive = []byte("not a zip archive")
			sum := sha256.Sum256(f.archive)
			f.sums = hex.EncodeToString(sum[:]) + f.sums[64:]
		}},
		{name: "unexpected redirect", want: "Could not connect", change: func(f *releaseFixture, _ *releaseInstaller, _ string) {
			f.redirect = "https://example.invalid/archive?sig=secret"
		}},
		{name: "downgrade", version: "1.1.0", want: "not a newer", offline: true},
		{name: "lane change", version: "1.3.0-beta", want: "not a newer", offline: true},
		{name: "changed during install", want: "changed during the update", change: func(_ *releaseFixture, in *releaseInstaller, target string) {
			probe := in.version
			in.version = func(ctx context.Context, p string) (string, error) {
				if p != target {
					os.WriteFile(target, fakeElephant("1.2.0", "replaced by another tool"), 0o755)
				}
				return probe(ctx, p)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, old := installTarget(t)
			version := tc.version
			if version == "" {
				version = "1.3.0"
			}
			binary := tc.binary
			if binary == nil {
				binary = fakeElephant(version, "new")
			}
			f := newReleaseFixture(t, version, binary)
			in := f.installer()
			if tc.change != nil {
				tc.change(f, in, target)
			}
			_, err := in.install(context.Background(), target, version)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
			if tc.offline != (f.requests.Load() == 0) {
				t.Fatal("unexpected network use:", f.requests.Load())
			}
			if tc.name == "changed during install" {
				old = fakeElephant("1.2.0", "replaced by another tool")
			}
			assertTargetIntact(t, target, old)
		})
	}
}

func TestInstallUpdateCancellationLeavesTargetIntact(t *testing.T) {
	target, old := installTarget(t)
	f := newReleaseFixture(t, "1.3.0", fakeElephant("1.3.0", "new"))
	f.started, f.hold = make(chan struct{}), make(chan struct{})
	started := f.started
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.installer().install(ctx, target, "1.3.0")
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	assertTargetIntact(t, target, old)
}

func TestInstallUpdateSerializesPerTarget(t *testing.T) {
	target, _ := installTarget(t)
	newBinary := fakeElephant("1.3.0", "new")
	f := newReleaseFixture(t, "1.3.0", newBinary)
	f.started, f.hold = make(chan struct{}), make(chan struct{})
	started := f.started
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := f.installer().install(context.Background(), target, "1.3.0"); first <- err }()
	<-started
	go func() { _, err := f.installer().install(context.Background(), target, "1.3.0"); second <- err }()
	select {
	case err := <-second:
		t.Fatal("second install did not wait:", err)
	default:
	}
	close(f.hold)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err == nil || !strings.Contains(err.Error(), "not a newer") {
		t.Fatal("second install repeated the replacement:", err)
	}
	assertTargetIntact(t, target, newBinary)
}

func TestExecutableVersionIsIsolatedAndStrict(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FAKE_VERSION", "1.0.0")
	for name, script := range map[string]string{
		"env":   "echo \"Elephant $FAKE_VERSION\"",
		"extra": "echo 'Elephant 1.0.0'; echo 'more output'",
		"v":     "echo 'Elephant v1.0.0'",
		"fail":  "echo 'Elephant 1.0.0'; exit 3",
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if v, err := ExecutableVersion(context.Background(), p); err == nil {
			t.Fatal(name, "accepted", v)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if goos, goarch, err := executablePlatform(exe); err != nil || goos != runtime.GOOS || goarch != runtime.GOARCH {
		t.Fatal(goos, goarch, err)
	}
	if _, _, err := executablePlatform(filepath.Join(dir, "env")); err == nil {
		t.Fatal("script accepted as a platform binary")
	}
}

func TestUpdateAppliesUsesReleaseLanes(t *testing.T) {
	for _, tc := range []struct {
		installed, candidate string
		want                 bool
	}{
		{"1.2.0", "1.3.0", true}, {"1.2.0", "1.2.0", false}, {"1.3.0", "1.2.0", false}, {"1.2.0", "2.0.0", false},
		{"1.2.0", "1.3.0-beta", false}, {"1.2.0-pilot", "1.3.0-beta", true}, {"1.2.0-rc.1", "1.2.0-rc.2", true}, {"1.2.0-rc.1", "1.2.0", true}, {"junk", "1.3.0", false},
	} {
		if got := UpdateApplies(tc.installed, tc.candidate); got != tc.want {
			t.Error(tc, got)
		}
	}
	if u, ok := ReleaseNotesURL("1.3.0"); !ok || u != fmt.Sprintf("https://github.com/%s/releases/tag/v1.3.0", releaseRepository) {
		t.Fatal(u)
	}
	if _, ok := ReleaseNotesURL("1.3.0/../../x"); ok {
		t.Fatal("unsafe version accepted")
	}
}
