package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const releaseRepository = "prithivrajmu/elephant"
const releaseAPI = "https://api.github.com/repos/" + releaseRepository + "/releases?per_page=20"

type ReleaseUpdate struct {
	Version string `json:"version"`
	Summary string `json:"summary"`
	URL     string `json:"url"`
}
type UpdateStatus struct {
	Enabled   bool           `json:"enabled"`
	State     string         `json:"state"`
	Checked   time.Time      `json:"checked_at,omitempty"`
	Latest    *ReleaseUpdate `json:"latest,omitempty"`
	Notified  string         `json:"notified_version,omitempty"`
	Dismissed string         `json:"dismissed_version,omitempty"`
	Line      string         `json:"line,omitempty"`
	Installed string         `json:"installed_version"`
	Reason    string         `json:"reason,omitempty"`
	Message   string         `json:"message"`
}
type UpdateOptions struct {
	Check, Force, Notify, Dismiss bool
	Enabled                       *bool
}

var releaseVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(pilot|rc\.[1-9][0-9]*))?$`)

type releaseVersion struct {
	parts   [3]int
	channel string
	rc      int
}

func parseReleaseVersion(v string) (releaseVersion, bool) {
	var out releaseVersion
	m := releaseVersionPattern.FindStringSubmatch(v)
	if m == nil {
		return out, false
	}
	for i := 0; i < 3; i++ {
		n, e := strconv.Atoi(m[i+1])
		if e != nil {
			return out, false
		}
		out.parts[i] = n
	}
	out.channel = m[4]
	if strings.HasPrefix(out.channel, "rc.") {
		out.rc, _ = strconv.Atoi(strings.TrimPrefix(out.channel, "rc."))
	}
	return out, true
}
func newerRelease(a, b releaseVersion) bool {
	for i := 0; i < 3; i++ {
		if a.parts[i] != b.parts[i] {
			return a.parts[i] > b.parts[i]
		}
	}
	if a.channel == b.channel {
		return false
	}
	if a.channel == "" {
		return true
	}
	if b.channel == "" {
		return false
	}
	if strings.HasPrefix(a.channel, "rc.") && strings.HasPrefix(b.channel, "rc.") {
		return a.rc > b.rc
	}
	return false
}
func releaseText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) > 100 {
		r = r[:100]
	}
	return string(r)
}

type githubRelease struct {
	Tag        string `json:"tag_name"`
	Name       string `json:"name"`
	URL        string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

// Only bounded, locally defined diagnostics reach the cache and UI. Never expose
// remote response bodies, request URLs, or credentials through an error string.
type updateCheckError struct{ reason string }

func (e *updateCheckError) Error() string { return updateFailureMessage(e.reason) }

func updateFailureMessage(reason string) string {
	switch reason {
	case "access_required":
		return "Could not access Elephant releases. The repository may be private; set ELEPHANT_GITHUB_TOKEN for the process running Elephant, then retry."
	case "credentials_rejected":
		return "GitHub rejected the update-check credentials. Check ELEPHANT_GITHUB_TOKEN and its repository access, then retry."
	case "access_denied":
		return "GitHub denied access to Elephant releases. Check the token's repository access and any organization authorization, then retry."
	case "not_found":
		return "GitHub could not find accessible Elephant releases. Check the token's repository access; GitHub also returns this response for private repositories."
	case "rate_limited":
		return "GitHub is limiting update checks. Wait before retrying; authenticated access may provide a higher limit."
	case "timeout":
		return "The update check timed out. Check your connection and retry."
	case "network":
		return "Could not connect to GitHub to check for updates. Check your network or proxy settings and retry."
	case "invalid_metadata":
		return "GitHub returned release information Elephant could not read. Retry later."
	case "invalid_version":
		return "This Elephant build has an unrecognized version. Install a published release to enable update comparisons."
	case "server_error":
		return "GitHub could not complete the update check. Retry later."
	default:
		return "Could not verify whether an update is available. Retry the check for more details."
	}
}

func (d *UpdateStatus) describe() {
	switch d.State {
	case "unavailable":
		d.Message = updateFailureMessage(d.Reason)
	case "current":
		d.Message = "No newer compatible release found for this installation."
	case "available":
		d.Message = "A newer compatible Elephant release is available."
	case "disabled":
		d.Message = "Update checks are disabled."
	default:
		d.Message = "Updates have not been checked yet."
	}
}

func selectRelease(releases []githubRelease) (*ReleaseUpdate, error) {
	current, ok := parseReleaseVersion(Version)
	if !ok {
		return nil, &updateCheckError{"invalid_version"}
	}
	best := current
	var found *ReleaseUpdate
	for _, r := range releases {
		v, ok := parseReleaseVersion(r.Tag)
		if !ok || r.Draft || v.parts[0] != current.parts[0] || !newerRelease(v, best) {
			continue
		}
		// A stable installation never moves to prerelease; pilot and RC channels
		// only see their own prerelease channel or a stable release.
		sameChannel := v.channel == current.channel || strings.HasPrefix(v.channel, "rc.") && strings.HasPrefix(current.channel, "rc.")
		if (r.Prerelease || v.channel != "") && (current.channel == "" || !sameChannel) {
			continue
		}
		u, e := url.Parse(r.URL)
		if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/"+releaseRepository+"/releases/tag/"+r.Tag {
			continue
		}
		asset := false
		for _, a := range r.Assets {
			if a.Name == "elephant-"+strings.TrimPrefix(r.Tag, "v")+"-"+runtime.GOOS+"-"+runtime.GOARCH+".zip" {
				asset = true
			}
		}
		if !asset {
			continue
		}
		summary := releaseText(r.Name)
		if summary == "" {
			summary = "New Elephant release"
		}
		found = &ReleaseUpdate{Version: strings.TrimPrefix(r.Tag, "v"), Summary: summary, URL: r.URL}
		best = v
	}
	return found, nil
}
func fetchRelease(client *http.Client, endpoint string) (*ReleaseUpdate, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if e != nil {
		return nil, &updateCheckError{"network"}
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "elephant/"+Version)
	// Explicitly provided credentials stay in memory, never in config/cache/logs.
	if token := os.Getenv("ELEPHANT_GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, e := client.Do(req)
	if e != nil {
		var timeout net.Error
		if errors.Is(e, context.DeadlineExceeded) || errors.As(e, &timeout) && timeout.Timeout() {
			return nil, &updateCheckError{"timeout"}
		}
		return nil, &updateCheckError{"network"}
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
		return nil, &updateCheckError{reason}
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (256<<10)+1))
	if e != nil || len(b) > 256<<10 {
		return nil, &updateCheckError{"invalid_metadata"}
	}
	var releases []githubRelease
	if e = json.Unmarshal(b, &releases); e != nil || releases == nil {
		return nil, &updateCheckError{"invalid_metadata"}
	}
	return selectRelease(releases)
}
func (s Store) CachedUpdate() (UpdateStatus, error) {
	d := UpdateStatus{Enabled: true, State: "unchecked", Installed: Version}
	p := s.Path + ".updates.json"
	b, e := readConfigFile(p)
	if e != nil {
		return d, e
	}
	if len(b) > 0 {
		e = json.Unmarshal(b, &d)
	}
	d.Line = ""
	if d.Installed != Version {
		d.Installed = Version
		d.Checked = time.Time{}
		d.Latest = nil
		d.State = "unchecked"
		d.Reason = ""
	}
	if os.Getenv("ELEPHANT_UPDATE_CHECKS") == "0" {
		d.Enabled = false
		d.State = "disabled"
	}
	d.describe()
	return d, e
}
func (s Store) Updates(opts UpdateOptions) (UpdateStatus, error) {
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return s.updates(opts, client, releaseAPI, time.Now().UTC())
}
func (s Store) updates(opts UpdateOptions, client *http.Client, endpoint string, now time.Time) (UpdateStatus, error) {
	d, e := s.CachedUpdate()
	if e != nil {
		return d, e
	}
	// A process opt-out must not permanently overwrite the stored preference.
	if os.Getenv("ELEPHANT_UPDATE_CHECKS") == "0" {
		d.Enabled = false
		d.State = "disabled"
		d.Line = "Elephant: update checks disabled."
		return d, nil
	}
	if !opts.Check && !opts.Dismiss && opts.Enabled == nil {
		return d, nil
	}
	if e = os.MkdirAll(filepath.Dir(s.Path), 0700); e != nil {
		return d, e
	}
	lock := s.Path + ".updates.lock"
	if e = os.Mkdir(lock, 0700); e != nil {
		return d, fmt.Errorf("update check busy")
	}
	defer os.Remove(lock)
	d, e = s.CachedUpdate()
	if e != nil {
		return d, e
	}
	if opts.Enabled != nil {
		d.Enabled = *opts.Enabled
		d.Checked = time.Time{}
		d.State = "unchecked"
		d.Reason = ""
	}
	if os.Getenv("ELEPHANT_UPDATE_CHECKS") == "0" {
		d.Enabled = false
	}
	if !d.Enabled {
		d.State = "disabled"
		d.Line = "Elephant: update checks disabled."
	} else {
		if opts.Check && (opts.Force || d.Checked.IsZero() || now.Sub(d.Checked) >= 24*time.Hour) {
			d.Checked = now
			d.Latest = nil
			d.Reason = ""
			latest, err := fetchRelease(client, endpoint)
			if err != nil {
				d.State = "unavailable"
				var failure *updateCheckError
				if errors.As(err, &failure) {
					d.Reason = failure.reason
				}
				d.Line = "Elephant: " + updateFailureMessage(d.Reason)
			} else if latest == nil {
				d.State = "current"
				d.Line = "Elephant: no newer matching release found."
			} else {
				d.State = "available"
				d.Latest = latest
				d.Line = ""
			}
		}
		if opts.Dismiss && d.Latest != nil {
			d.Notified = d.Latest.Version
			d.Dismissed = d.Latest.Version
		}
		if d.State == "available" && d.Latest != nil {
			d.Line = ""
			if opts.Notify && d.Notified != d.Latest.Version {
				d.Line = fmt.Sprintf("Elephant update: %s available — %s. Upgrade: %s", d.Latest.Version, d.Latest.Summary, d.Latest.URL)
				d.Notified = d.Latest.Version
			}
		}
	}
	d.describe()
	saved := d
	saved.Line = ""
	b, e := json.MarshalIndent(saved, "", "  ")
	if e != nil {
		return d, e
	}
	return d, atomicLocalFile(s.Path+".updates.json", b, 0600)
}
