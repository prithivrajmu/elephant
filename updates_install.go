package memory

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Release installation trusts GitHub's TLS delivery and the SHA256SUMS asset of
// the same release. Releases are not signed, so a matching checksum proves the
// archive is the published asset, not who published it.
const (
	releaseRepoAPI        = "https://api.github.com/repos/" + releaseRepository
	releaseMetadataLimit  = 1 << 20
	releaseChecksumLimit  = 64 << 10
	releaseArchiveLimit   = 96 << 20
	releaseBinaryLimit    = 192 << 20
	releaseArchiveEntries = 4096
	releaseInstallTimeout = 10 * time.Minute
	executableProbeTime   = 10 * time.Second
	executableProbeOutput = 256
)

// GitHub serves release asset bytes through a signed redirect to one of these
// hosts; no other redirect target is followed.
var releaseDeliveryHosts = []string{"objects.githubusercontent.com", "release-assets.githubusercontent.com"}

// UpdateInstallResult describes one completed binary replacement. Processes
// already running keep the previous version until they are restarted.
type UpdateInstallResult struct {
	Target   string `json:"target"`
	Previous string `json:"previous_version"`
	Version  string `json:"version"`
}

// UpdateApplies reports whether candidate is a newer release on the same major
// version and release lane as installed, using the update-check rules. It
// rejects equal versions and downgrades.
func UpdateApplies(installed, candidate string) bool {
	cur, ok := parseReleaseVersion(installed)
	v, ok2 := parseReleaseVersion(candidate)
	return ok && ok2 && releaseApplies(cur, v, false)
}

func releaseApplies(cur, v releaseVersion, prerelease bool) bool {
	if v.parts[0] != cur.parts[0] || !newerRelease(v, cur) {
		return false
	}
	same := v.channel == cur.channel || strings.HasPrefix(v.channel, "rc.") && strings.HasPrefix(cur.channel, "rc.") || isPilotLane(v.channel) && isPilotLane(cur.channel)
	return !((prerelease || v.channel != "") && (cur.channel == "" || !same))
}

// ReleaseNotesURL returns the canonical GitHub release page for version.
func ReleaseNotesURL(version string) (string, bool) {
	if _, ok := parseReleaseVersion(version); !ok {
		return "", false
	}
	return "https://github.com/" + releaseRepository + "/releases/tag/v" + strings.TrimPrefix(version, "v"), true
}

// ExecutableVersion runs `path version` with an empty working directory, a
// minimal environment and no input, and returns the release version it
// reports. It never reads Elephant config or stores.
func ExecutableVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, executableProbeTime)
	defer cancel()
	dir, err := os.MkdirTemp("", "elephant-version-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, path, "version")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir, "LC_ALL=C", "ELEPHANT_UPDATE_CHECKS=0"}
	out := &cappedBuffer{limit: executableProbeOutput}
	cmd.Stdout = out
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("the Elephant binary did not report its version in time")
		}
		return "", fmt.Errorf("the Elephant binary could not report its version")
	}
	line := strings.TrimSuffix(strings.TrimSuffix(out.buf.String(), "\n"), "\r")
	version, ok := strings.CutPrefix(line, "Elephant ")
	if _, valid := parseReleaseVersion(version); !ok || out.over || !valid || strings.HasPrefix(version, "v") {
		return "", fmt.Errorf("the Elephant binary reported an unrecognized version")
	}
	return version, nil
}

// cappedBuffer keeps at most limit bytes; it is not an io.ReaderFrom, so exec
// cannot bypass the cap.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	keep := p
	if room := b.limit - b.buf.Len(); len(keep) > room {
		b.over = true
		keep = keep[:max(room, 0)]
	}
	b.buf.Write(keep)
	return len(p), nil
}

// InstallUpdate replaces the explicitly chosen Elephant executable with the
// published release version. It installs only a newer release on the same lane
// for the target's own platform, verifies the archive checksum and the new
// binary's reported version, and swaps the file atomically in its directory.
// Failures before replacement leave the target unchanged. Once replaced,
// result.Version is set even if flushing the directory reports a warning.
func InstallUpdate(ctx context.Context, target, version string) (UpdateInstallResult, error) {
	return newReleaseInstaller().install(ctx, target, version)
}

type releaseInstaller struct {
	api      string
	client   *http.Client
	redirect map[string]bool
	version  func(context.Context, string) (string, error)
	platform func(string) (string, string, error)
}

func newReleaseInstaller() *releaseInstaller {
	in := &releaseInstaller{api: releaseRepoAPI, redirect: map[string]bool{"api.github.com": true}, version: ExecutableVersion, platform: executablePlatform}
	for _, h := range releaseDeliveryHosts {
		in.redirect[h] = true
	}
	in.client = &http.Client{CheckRedirect: in.checkRedirect}
	return in
}

func (in *releaseInstaller) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 3 || req.URL.Scheme != "https" || req.URL.User != nil || !in.redirect[req.URL.Host] {
		return errors.New("unexpected release redirect")
	}
	// Credentials are for the GitHub API only, never for delivery hosts.
	req.Header.Del("Authorization")
	return nil
}

var (
	releaseInstallMu    sync.Mutex
	releaseInstallLocks = map[string]chan struct{}{}
)

// lockInstall serializes installs per resolved target within this process.
func lockInstall(ctx context.Context, target string) (func(), error) {
	releaseInstallMu.Lock()
	ch := releaseInstallLocks[target]
	if ch == nil {
		ch = make(chan struct{}, 1)
		releaseInstallLocks[target] = ch
	}
	releaseInstallMu.Unlock()
	select {
	case ch <- struct{}{}:
		return func() { <-ch }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type releaseAsset struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func (in *releaseInstaller) install(ctx context.Context, target, version string) (UpdateInstallResult, error) {
	var res UpdateInstallResult
	want, ok := parseReleaseVersion(version)
	if !ok || strings.HasPrefix(version, "v") {
		return res, errors.New("the requested update version is not a valid Elephant release version")
	}
	ctx, cancel := context.WithTimeout(ctx, releaseInstallTimeout)
	defer cancel()
	abs, err := filepath.Abs(target)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	if err != nil {
		return res, fmt.Errorf("could not locate the Elephant binary: %w", err)
	}
	res.Target = abs
	unlock, err := lockInstall(ctx, abs)
	if err != nil {
		return res, err
	}
	defer unlock()
	before, err := os.Lstat(abs)
	if err != nil {
		return res, fmt.Errorf("could not inspect the Elephant binary: %w", err)
	}
	if !before.Mode().IsRegular() {
		return res, errors.New("the selected Elephant binary is not a regular file")
	}
	// The binary on disk, not the running process, decides whether this
	// update still applies; a repeated request after success is refused.
	res.Previous, err = in.version(ctx, abs)
	if err != nil {
		return res, err
	}
	cur, _ := parseReleaseVersion(res.Previous)
	if !releaseApplies(cur, want, false) {
		return res, fmt.Errorf("Elephant %s is not a newer compatible release for the installed %s; nothing was changed", version, res.Previous)
	}
	goos, goarch, err := in.platform(abs)
	if err != nil {
		return res, err
	}
	if goos != runtime.GOOS {
		return res, errors.New("the selected Elephant binary was built for a different operating system")
	}
	exe := "elephant"
	if goos == "windows" {
		exe += ".exe"
	}
	stem := "elephant-" + version + "-" + goos + "-" + goarch
	archive, sums, err := in.release(ctx, version, want, cur, stem+".zip")
	if err != nil {
		return res, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(abs), ".elephant-update-")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return res, errors.New("Elephant cannot write to the folder containing this binary; update it with the tool that installed it")
		}
		return res, fmt.Errorf("could not stage the update: %w", err)
	}
	defer os.RemoveAll(stage)
	var list bytes.Buffer
	if err = in.download(ctx, sums, &list); err != nil {
		return res, err
	}
	digest, err := releaseChecksum(list.Bytes(), archive.Name)
	if err != nil {
		return res, err
	}
	zipPath := filepath.Join(stage, archive.Name)
	if err = in.downloadFile(ctx, archive, zipPath, digest); err != nil {
		return res, err
	}
	staged := filepath.Join(stage, exe)
	if err = extractReleaseBinary(zipPath, stem+"/"+exe, staged, before.Mode().Perm()|0o500); err != nil {
		return res, err
	}
	if g, a, e := in.platform(staged); e != nil || g != goos || a != goarch {
		return res, errors.New("the release binary does not match this binary's platform; nothing was changed")
	}
	got, err := in.version(ctx, staged)
	if err != nil {
		return res, err
	}
	if got != version {
		return res, fmt.Errorf("the release binary reports version %s instead of %s; nothing was changed", got, version)
	}
	if err = ctx.Err(); err != nil {
		return res, err
	}
	// Refuse to replace a binary that another tool changed meanwhile.
	now, err := os.Lstat(abs)
	if err != nil || !os.SameFile(before, now) || now.Size() != before.Size() || !now.ModTime().Equal(before.ModTime()) {
		return res, errors.New("the Elephant binary changed during the update; nothing was replaced")
	}
	// Rename gives the path a new file; running processes keep the old one.
	if err = os.Rename(staged, abs); err != nil {
		return res, fmt.Errorf("could not replace the Elephant binary: %w", err)
	}
	res.Version = version
	if err = syncDirectory(filepath.Dir(abs)); err != nil {
		return res, fmt.Errorf("installed Elephant %s, but could not confirm the folder was flushed to disk: %w", version, err)
	}
	return res, nil
}

// release reads the tag's metadata and returns its expected archive and
// checksum assets after repeating the update check's publication rules.
func (in *releaseInstaller) release(ctx context.Context, version string, want, cur releaseVersion, name string) (archive, sums releaseAsset, err error) {
	var body bytes.Buffer
	if err = in.get(ctx, in.api+"/releases/tags/v"+version, "application/vnd.github+json", releaseMetadataLimit, &body); err != nil {
		return
	}
	var r struct {
		Tag        string         `json:"tag_name"`
		URL        string         `json:"html_url"`
		Draft      bool           `json:"draft"`
		Prerelease bool           `json:"prerelease"`
		Assets     []releaseAsset `json:"assets"`
	}
	notes, _ := ReleaseNotesURL(version)
	if json.Unmarshal(body.Bytes(), &r) != nil || r.Tag != "v"+version || r.URL != notes {
		return archive, sums, &updateCheckError{"invalid_metadata"}
	}
	if r.Draft || !releaseApplies(cur, want, r.Prerelease) {
		return archive, sums, errors.New("that release is not published for this installation's release lane; nothing was changed")
	}
	for _, a := range r.Assets {
		switch a.Name {
		case name:
			archive = a
		case "SHA256SUMS":
			sums = a
		}
	}
	if archive.ID <= 0 || archive.Size <= 0 || archive.Size > releaseArchiveLimit {
		return archive, sums, errors.New("the release has no usable archive for this binary's platform; nothing was changed")
	}
	if sums.ID <= 0 || sums.Size <= 0 || sums.Size > releaseChecksumLimit {
		return archive, sums, errors.New("the release has no usable SHA256SUMS file; nothing was changed")
	}
	return archive, sums, nil
}

func (in *releaseInstaller) get(ctx context.Context, endpoint, accept string, limit int64, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return &updateCheckError{"network"}
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "elephant/"+Version)
	if token := os.Getenv("ELEPHANT_GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := in.client.Do(req)
	if err != nil {
		// Transport errors embed signed delivery URLs; report a fixed reason.
		var timeout net.Error
		switch {
		case ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded):
			return ctx.Err()
		case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout():
			return &updateCheckError{"timeout"}
		}
		return &updateCheckError{"network"}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		reason := "server_error"
		switch {
		case res.StatusCode == 429 || res.StatusCode == 403 && (res.Header.Get("X-RateLimit-Remaining") == "0" || res.Header.Get("Retry-After") != ""):
			reason = "rate_limited"
		case res.StatusCode == 401:
			reason = "credentials_rejected"
		case res.StatusCode == 403:
			reason = "access_denied"
		case res.StatusCode == 404 && os.Getenv("ELEPHANT_GITHUB_TOKEN") == "":
			reason = "access_required"
		case res.StatusCode == 404:
			reason = "not_found"
		}
		return &updateCheckError{reason}
	}
	n, err := io.Copy(w, io.LimitReader(res.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errWriteFailed) {
			return err
		}
		return errors.New("the release download was interrupted; nothing was changed")
	}
	if n > limit {
		return errors.New("the release download is larger than expected; nothing was changed")
	}
	return nil
}

var errWriteFailed = errors.New("could not write the downloaded release; nothing was changed")

type stagedWriter struct{ f *os.File }

func (w stagedWriter) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	if err != nil {
		err = errWriteFailed
	}
	return n, err
}

func (in *releaseInstaller) download(ctx context.Context, a releaseAsset, w io.Writer) error {
	return in.get(ctx, in.api+"/releases/assets/"+strconv.FormatInt(a.ID, 10), "application/octet-stream", a.Size, w)
}

func (in *releaseInstaller) downloadFile(ctx context.Context, a releaseAsset, path string, digest []byte) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("could not stage the update: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	counter := &countingWriter{}
	if err = in.download(ctx, a, io.MultiWriter(stagedWriter{f}, h, counter)); err != nil {
		return err
	}
	if counter.n != a.Size || !bytes.Equal(h.Sum(nil), digest) {
		return errors.New("the downloaded archive does not match the release checksum; nothing was changed")
	}
	return nil
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// releaseChecksum returns the single SHA-256 listed for name.
func releaseChecksum(list []byte, name string) ([]byte, error) {
	var found []byte
	for _, line := range strings.Split(string(list), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || strings.TrimPrefix(f[1], "*") != name {
			continue
		}
		d, err := hex.DecodeString(f[0])
		if err != nil || len(d) != sha256.Size || found != nil {
			return nil, errors.New("the release SHA256SUMS entry for this archive is invalid; nothing was changed")
		}
		found = d
	}
	if found == nil {
		return nil, errors.New("the release SHA256SUMS does not list this archive; nothing was changed")
	}
	return found, nil
}

// extractReleaseBinary writes only the expected executable entry and syncs it.
func extractReleaseBinary(archive, entry, dest string, mode fs.FileMode) error {
	invalid := errors.New("the release archive is invalid; nothing was changed")
	z, err := zip.OpenReader(archive)
	if err != nil {
		return invalid
	}
	defer z.Close()
	if len(z.File) > releaseArchiveEntries {
		return invalid
	}
	var bin *zip.File
	for _, f := range z.File {
		if f.Name == entry {
			if bin != nil {
				return invalid
			}
			bin = f
		}
	}
	if bin == nil || !bin.Mode().IsRegular() || bin.UncompressedSize64 == 0 || bin.UncompressedSize64 > releaseBinaryLimit {
		return invalid
	}
	r, err := bin.Open()
	if err != nil {
		return invalid
	}
	defer r.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("could not stage the update: %w", err)
	}
	defer out.Close()
	n, err := io.Copy(stagedWriter{out}, io.LimitReader(r, releaseBinaryLimit+1))
	if errors.Is(err, errWriteFailed) {
		return err
	}
	// archive/zip verifies the CRC-32 and declared size at end of entry.
	if err != nil || n > releaseBinaryLimit || uint64(n) != bin.UncompressedSize64 {
		return invalid
	}
	if err = out.Chmod(mode); err == nil {
		err = out.Sync()
	}
	if err == nil {
		err = out.Close()
	}
	if err != nil {
		return fmt.Errorf("could not stage the update: %w", err)
	}
	return nil
}

// executablePlatform reads the executable header of path and returns the
// GOOS/GOARCH pair it targets. Universal and unknown binaries are rejected.
func executablePlatform(path string) (string, string, error) {
	unknown := errors.New("could not confirm the platform of the Elephant binary")
	if f, err := macho.Open(path); err == nil {
		defer f.Close()
		switch f.Cpu {
		case macho.CpuAmd64:
			return "darwin", "amd64", nil
		case macho.CpuArm64:
			return "darwin", "arm64", nil
		}
		return "", "", unknown
	}
	if f, err := elf.Open(path); err == nil {
		defer f.Close()
		switch {
		case f.OSABI != elf.ELFOSABI_NONE && f.OSABI != elf.ELFOSABI_LINUX:
		case f.Machine == elf.EM_X86_64:
			return "linux", "amd64", nil
		case f.Machine == elf.EM_AARCH64:
			return "linux", "arm64", nil
		}
		return "", "", unknown
	}
	if f, err := pe.Open(path); err == nil {
		defer f.Close()
		switch f.Machine {
		case pe.IMAGE_FILE_MACHINE_AMD64:
			return "windows", "amd64", nil
		case pe.IMAGE_FILE_MACHINE_ARM64:
			return "windows", "arm64", nil
		}
	}
	return "", "", unknown
}
