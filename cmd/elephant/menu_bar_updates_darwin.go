//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

// menuBarUpdates updates Elephant as one product: every binary used by a
// connected process, the companion itself, and the elephant found on each
// PATH. It installs only after one native confirmation listing every target
// and never restarts servers. Each binary's on-disk version, not the running
// version, decides which release, if any, still applies to it.
type menuBarUpdates struct {
	cached   func(memory.Store) (memory.UpdateStatus, error)
	releases func([]string) (map[string]*memory.ReleaseUpdate, error)
	install  func(context.Context, string, string) (memory.UpdateInstallResult, error)
	probe    func(context.Context, string) (string, error)
	self     func() (string, error)
	lookPath func(string) (string, error)
	home     func() (string, error)

	mu       sync.Mutex
	checking bool                          // one product release fetch at a time
	product  *menuProductReleases          // last product release fetch; never stored on disk
	busy     string                        // "", "confirm" or "install"
	versions map[string]*menuBinaryVersion // resolved binary -> on-disk version
	restart  map[string]string             // resolved binary -> version installed by this host
}

type menuBinaryVersion struct {
	info    os.FileInfo
	version string
	probing bool
}

// menuProductReleases is the companion's in-memory release result, selected
// for each on-disk version it asked about. No store's .updates.json holds it.
type menuProductReleases struct {
	checked time.Time
	asked   map[string]bool
	latest  map[string]*memory.ReleaseUpdate
	err     error
}

const updatesTitle = "Elephant Updates"

func newMenuBarUpdates() *menuBarUpdates {
	return &menuBarUpdates{
		cached:   func(s memory.Store) (memory.UpdateStatus, error) { return s.CachedUpdate() },
		releases: memory.ReleasesForBaselines,
		install:  memory.InstallUpdate,
		probe:    memory.ExecutableVersion,
		self:     os.Executable,
		lookPath: exec.LookPath,
		home:     os.UserHomeDir,
		versions: map[string]*menuBinaryVersion{},
		restart:  map[string]string{},
	}
}

// resolveMenuExecutable returns the real file a binary path names.
func resolveMenuExecutable(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("no Elephant binary path was reported")
	}
	abs, err := filepath.Abs(p)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	return abs, err
}

// targets returns the distinct resolved binaries this menu updates, plus the
// copies owned by the npm launcher. Launcher copies live in per-version cache
// directories, so replacing one in place would make that cache name lie.
func (u *menuBarUpdates) targets(c menuControlContext) (installable, launcher []string) {
	candidates := append([]string(nil), c.Executables...)
	for _, p := range c.ReportedExecutables {
		// A session reports what its own PATH lookup found; anything but an
		// absolute path would resolve against the companion's directory.
		if filepath.IsAbs(p) {
			candidates = append(candidates, p)
		}
	}
	if p, err := u.self(); err == nil {
		candidates = append(candidates, p)
	}
	if p, err := u.lookPath("elephant"); err == nil {
		candidates = append(candidates, p)
	}
	cache := ""
	if home, err := u.home(); err == nil {
		if resolved, err := filepath.EvalSymlinks(filepath.Join(home, ".elephant", "bin")); err == nil {
			cache = resolved + string(filepath.Separator)
		}
	}
	seen := map[string]bool{}
	for _, p := range candidates {
		target, err := resolveMenuExecutable(p)
		if err != nil || seen[target] {
			continue
		}
		seen[target] = true
		if cache != "" && strings.HasPrefix(target, cache) {
			launcher = append(launcher, target)
		} else {
			installable = append(installable, target)
		}
	}
	sort.Strings(installable)
	sort.Strings(launcher)
	return installable, launcher
}

// binaryVersion returns the cached on-disk version of target and starts one
// background probe when the file is new or changed. It never blocks.
func (u *menuBarUpdates) binaryVersion(c menuControlContext, target string) (version string, probing bool) {
	fi, err := os.Stat(target)
	if err != nil {
		return "", false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	v := u.versions[target]
	if v != nil && (v.probing || os.SameFile(v.info, fi) && v.info.Size() == fi.Size() && v.info.ModTime().Equal(fi.ModTime())) {
		return v.version, v.probing
	}
	u.versions[target] = &menuBinaryVersion{info: fi, probing: true}
	go func() {
		got, err := u.probe(c.Context, target)
		u.mu.Lock()
		if c.Context.Err() != nil {
			delete(u.versions, target) // retry later rather than cache a cancellation
		} else {
			if err != nil {
				got = ""
			}
			u.versions[target] = &menuBinaryVersion{info: fi, version: got}
		}
		u.mu.Unlock()
		c.Refresh()
	}()
	return "", true
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// storeStatus reads the cached status of every connected store. Any read
// error makes the whole status unavailable; any opt-out disables checks for
// the product, so one project's opt-out is never overridden by another's.
// The returned status is the store showing the newest available release, or
// else the most recently checked one.
func (u *menuBarUpdates) storeStatus(c menuControlContext) (status memory.UpdateStatus, disabled bool, err error) {
	stores := c.Stores
	if len(stores) == 0 {
		stores = []memory.Store{c.Service.Store}
	}
	disabled = c.UpdateChecksDisabled || os.Getenv("ELEPHANT_UPDATE_CHECKS") == "0"
	seen := map[string]bool{}
	first := true
	for _, s := range stores {
		if seen[s.Path] {
			continue
		}
		seen[s.Path] = true
		st, err := u.cached(s)
		if err != nil {
			return st, disabled, err
		}
		if !st.Enabled || st.State == "disabled" {
			disabled = true
		}
		checked := st.Checked
		if !first && status.Checked.After(checked) {
			checked = status.Checked
		}
		available := st.State == "available" && st.Latest != nil
		was := status.State == "available" && status.Latest != nil
		switch {
		case first,
			available && (!was || memory.UpdateApplies(status.Latest.Version, st.Latest.Version)),
			!available && !was && st.Checked.After(status.Checked):
			status = st
		}
		status.Checked = checked
		first = false
	}
	return status, disabled, nil
}

// wantRelease returns the release a binary at on-disk version v should
// install. The product release result decides once it covers v; before that
// a store's cached release applies when it is newer. needFetch reports that
// v is older than the version the stores checked as current, so "current"
// says nothing about it and the product releases must be fetched.
func wantRelease(status memory.UpdateStatus, product *menuProductReleases, v string) (rel *memory.ReleaseUpdate, needFetch bool) {
	if v == "" {
		return nil, false
	}
	covered := product != nil && product.asked[v]
	if covered && product.err == nil {
		if r := product.latest[v]; r != nil && memory.UpdateApplies(v, r.Version) {
			return r, false
		}
		return nil, false
	}
	if status.State == "available" && status.Latest != nil && memory.UpdateApplies(v, status.Latest.Version) {
		return status.Latest, false
	}
	return nil, !covered && status.State == "current" && status.Latest == nil && memory.UpdateApplies(v, status.Installed)
}

// menuUpdatePlan is one read of every target against the cached status and
// the product release result.
type menuUpdatePlan struct {
	status    memory.UpdateStatus
	err       error
	disabled  bool
	product   *menuProductReleases
	targets   []string
	launcher  []string
	installed map[string]string                // target -> on-disk version; "" when unknown
	want      map[string]*memory.ReleaseUpdate // target -> release it should install
	needFetch bool
}

func (u *menuBarUpdates) plan(c menuControlContext, version func(target string) string) menuUpdatePlan {
	p := menuUpdatePlan{installed: map[string]string{}, want: map[string]*memory.ReleaseUpdate{}}
	p.status, p.disabled, p.err = u.storeStatus(c)
	p.targets, p.launcher = u.targets(c)
	u.mu.Lock()
	p.product = u.product
	u.mu.Unlock()
	for _, target := range p.targets {
		v := version(target)
		p.installed[target] = v
		if p.err != nil || p.disabled {
			continue
		}
		rel, need := wantRelease(p.status, p.product, v)
		if rel != nil {
			p.want[target] = rel
		}
		p.needFetch = p.needFetch || need
	}
	return p
}

// baselines returns the distinct known on-disk versions, sorted.
func (p menuUpdatePlan) baselines() []string {
	seen := map[string]bool{}
	var out []string
	for _, target := range p.targets {
		if v := p.installed[target]; v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// wanted returns the distinct releases targets should install, sorted by
// version, and how many targets want each.
func (p menuUpdatePlan) wanted() (versions []string, releases map[string]*memory.ReleaseUpdate, count map[string]int) {
	releases, count = map[string]*memory.ReleaseUpdate{}, map[string]int{}
	for _, target := range p.targets {
		if rel := p.want[target]; rel != nil {
			if releases[rel.Version] == nil {
				versions = append(versions, rel.Version)
			}
			releases[rel.Version] = rel
			count[rel.Version]++
		}
	}
	sort.Strings(versions)
	return versions, releases, count
}

// newestVersion returns the version no other version in vs updates to.
func newestVersion(vs []string) string {
	newest := ""
	for _, v := range vs {
		if newest == "" || memory.UpdateApplies(newest, v) {
			newest = v
		}
	}
	return newest
}

// beginFetch claims the single product release fetch; false means one is
// already running.
func (u *menuBarUpdates) beginFetch() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.checking {
		return false
	}
	u.checking = true
	return true
}

// fetch runs the claimed product release fetch for baselines and keeps the
// result in memory only.
func (u *menuBarUpdates) fetch(baselines []string) *menuProductReleases {
	latest, err := u.releases(baselines)
	result := &menuProductReleases{checked: time.Now().UTC(), asked: map[string]bool{}, latest: latest, err: err}
	for _, v := range baselines {
		result.asked[v] = true
	}
	u.mu.Lock()
	u.product = result
	u.checking = false
	u.mu.Unlock()
	return result
}

func (u *menuBarUpdates) Items(c menuControlContext) ([]menubar.Item, bool) {
	probing := map[string]bool{}
	p := u.plan(c, func(target string) string {
		v, busy := u.binaryVersion(c, target)
		probing[target] = busy
		return v
	})
	byVersion := map[string]int{}
	checkingVersions, unknown := 0, 0
	for _, target := range p.targets {
		switch v := p.installed[target]; {
		case probing[target]:
			checkingVersions++
		case v == "":
			unknown++
		default:
			byVersion[v]++
		}
	}
	if p.needFetch && u.beginFetch() {
		baselines := p.baselines()
		go func() {
			u.fetch(baselines)
			c.Refresh()
		}()
	}
	selfPath := ""
	if self, err := u.self(); err == nil {
		selfPath, _ = resolveMenuExecutable(self)
	}
	u.mu.Lock()
	checking, busy := u.checking, u.busy
	restart := map[string]bool{}
	for _, target := range p.targets {
		if v := u.restart[target]; v != "" {
			restart[v] = true
		}
	}
	u.mu.Unlock()
	// A live process whose binary on disk now reports another version keeps
	// running the old one until it restarts.
	companionStale := false
	for _, r := range c.Running {
		target, err := resolveMenuExecutable(r.Executable)
		if err != nil {
			continue
		}
		if v := p.installed[target]; v != "" && r.Version != "" && v != r.Version {
			restart[v] = true
			companionStale = companionStale || target == selfPath
		}
	}

	var rows []menubar.Item
	info := func(title string) { rows = append(rows, menubar.Item{Title: title}) }
	installedVersions := make([]string, 0, len(byVersion))
	for v := range byVersion {
		installedVersions = append(installedVersions, v)
	}
	sort.Strings(installedVersions)
	for _, v := range installedVersions {
		if len(p.targets) == 1 {
			info("Installed: Elephant " + v)
		} else {
			info("Installed: Elephant " + v + " · " + plural(byVersion[v], "binary", "binaries"))
		}
	}
	if checkingVersions > 0 {
		info("Installed: checking versions…")
	}
	if unknown > 0 {
		info("Installed: version unknown · " + plural(unknown, "binary", "binaries"))
	}
	if len(p.launcher) > 0 {
		info("npm launcher: " + plural(len(p.launcher), "copy", "copies") + " (update with npx)")
	}
	attention := false
	if len(restart) > 0 {
		vs := make([]string, 0, len(restart))
		for v := range restart {
			vs = append(vs, v)
		}
		sort.Strings(vs)
		title := "Restart needed to use Elephant " + strings.Join(vs, ", ")
		if companionStale {
			title += " — includes this menu bar: choose Hide Menu Bar, then start a new session"
		}
		info(title)
		attention = true
	}
	wanted, releases, count := p.wanted()
	coveredAll := p.product != nil && p.product.err == nil
	for v := range byVersion {
		coveredAll = coveredAll && p.product.asked[v]
	}
	olderThanChecked := false
	for _, v := range installedVersions {
		olderThanChecked = olderThanChecked || p.status.State == "current" && p.status.Latest == nil && memory.UpdateApplies(v, p.status.Installed)
	}
	status := p.status
	switch {
	case p.err != nil:
		info("Update status unavailable")
	case p.disabled:
		info("Update checks are disabled")
	case len(wanted) > 0:
		for _, v := range wanted {
			line := "Available: Elephant " + v + " — " + releases[v].Summary
			if len(wanted) > 1 {
				line += " · " + plural(count[v], "binary", "binaries")
			}
			info(line)
		}
		attention = true
	case p.needFetch:
		info("Checking releases for older Elephant binaries…")
	case p.product != nil && p.product.err != nil && (olderThanChecked || !p.product.checked.Before(status.Checked)):
		info("Last check failed")
	case coveredAll && len(byVersion) > 0:
		info("Up to date")
	case status.State == "available" && status.Latest != nil && len(byVersion) > 0:
		info("No newer release for the installed binaries")
	case status.State == "available" && status.Latest != nil:
		info("Available: Elephant " + status.Latest.Version + " (installed versions not confirmed)")
	case status.State == "unavailable":
		info("Last check failed")
	case status.State == "current":
		info("Up to date")
	default:
		info("Not checked yet")
	}
	if p.err == nil {
		checked := status.Checked
		if p.product != nil && p.product.checked.After(checked) {
			checked = p.product.checked
		}
		if !checked.IsZero() {
			info("Last checked: " + checked.Local().Format("Jan 2, 15:04"))
		}
	}
	rows = append(rows, menubar.Item{Separator: true})
	checkTitle := "Check for Updates"
	if checking {
		checkTitle = "Checking for Updates…"
	}
	rows = append(rows, menubar.Item{ID: "updates/check", Title: checkTitle, Enabled: p.err == nil && !p.disabled && !checking})
	if notes := p.notesVersion(); notes != "" {
		rows = append(rows, menubar.Item{ID: "updates/notes", Title: "Release Notes for Elephant " + notes, Enabled: true})
	}
	if len(wanted) > 0 {
		title := "Install Elephant updates…"
		if len(wanted) == 1 {
			title = "Install Elephant " + wanted[0] + " Everywhere…"
		}
		if busy == "install" {
			title = "Installing Elephant updates…"
			if len(wanted) == 1 {
				title = "Installing Elephant " + wanted[0] + "…"
			}
		}
		rows = append(rows, menubar.Item{ID: "updates/install", Title: title, Enabled: busy == ""})
	}
	return rows, attention
}

// notesVersion is the release whose notes the menu offers: the newest one a
// target would install, or else the release the stores last saw.
func (p menuUpdatePlan) notesVersion() string {
	if wanted, _, _ := p.wanted(); len(wanted) > 0 {
		return newestVersion(wanted)
	}
	if p.err == nil && p.status.Latest != nil {
		return p.status.Latest.Version
	}
	return ""
}

func (u *menuBarUpdates) Handle(c menuControlContext, action string) bool {
	switch action {
	case "updates/check":
		u.checkNow(c)
	case "updates/notes":
		u.openNotes(c)
	case "updates/install":
		u.installNow(c)
	default:
		return false
	}
	return true
}

// probeVersion reads a target's on-disk version now, "" when unreadable.
func (u *menuBarUpdates) probeVersion(c menuControlContext) func(string) string {
	return func(target string) string {
		v, err := u.probe(c.Context, target)
		if err != nil {
			return ""
		}
		return v
	}
}

func (u *menuBarUpdates) checkNow(c menuControlContext) {
	if _, disabled, err := u.storeStatus(c); err != nil {
		c.Inform(updatesTitle, "Elephant could not read the update status: "+err.Error())
		return
	} else if disabled {
		c.Inform(updatesTitle, "Update checks are disabled, so Elephant did not contact GitHub.")
		return
	}
	if !u.beginFetch() {
		return
	}
	c.Refresh()
	targets, _ := u.targets(c)
	seen := map[string]bool{}
	var baselines []string
	for _, target := range targets {
		if v, err := u.probe(c.Context, target); err == nil && v != "" && !seen[v] {
			seen[v] = true
			baselines = append(baselines, v)
		}
	}
	sort.Strings(baselines)
	result := u.fetch(baselines)
	c.Refresh()
	if c.Context.Err() != nil {
		return
	}
	if result.err != nil {
		c.Inform(updatesTitle, "Elephant could not check for updates: "+result.err.Error())
		return
	}
	// Plan again so the message counts exactly what Install would change.
	p := u.plan(c, u.probeVersion(c))
	wanted, releases, count := p.wanted()
	pending := 0
	for _, v := range wanted {
		pending += count[v]
	}
	var msg string
	switch {
	case len(baselines) == 0:
		msg = "Elephant could not read the version of any Elephant binary, so there is nothing to compare or install."
	case len(wanted) == 0:
		msg = "No newer compatible release was found for Elephant " + strings.Join(baselines, ", ") + ", so there is nothing to install."
	case len(wanted) == 1:
		msg = fmt.Sprintf("Elephant %s is available: %s\n\nChoose Install Elephant %s Everywhere… to update %s.", wanted[0], releases[wanted[0]].Summary, wanted[0], plural(pending, "binary", "binaries"))
	default:
		var b strings.Builder
		b.WriteString("Elephant updates are available:\n")
		for _, v := range wanted {
			fmt.Fprintf(&b, "Elephant %s — %s (%s)\n", v, releases[v].Summary, plural(count[v], "binary", "binaries"))
		}
		fmt.Fprintf(&b, "\nChoose Install Elephant updates… to update %s.", plural(pending, "binary", "binaries"))
		msg = b.String()
	}
	c.Inform(updatesTitle, msg)
}

func (u *menuBarUpdates) openNotes(c menuControlContext) {
	version := u.plan(c, u.probeVersion(c)).notesVersion()
	if version == "" {
		c.Inform(updatesTitle, "No release is known yet. Choose Check for Updates first.")
		return
	}
	link, ok := memory.ReleaseNotesURL(version)
	if !ok {
		c.Inform(updatesTitle, "The cached release version is not valid. Choose Check for Updates again.")
		return
	}
	if err := c.OpenURL(link); err != nil {
		c.Inform(updatesTitle, "Could not open the release notes: "+err.Error())
	}
}

type menuInstallTarget struct {
	path, installed, want string
}

func (u *menuBarUpdates) installNow(c menuControlContext) {
	u.mu.Lock()
	if u.busy != "" {
		u.mu.Unlock()
		c.Inform(updatesTitle, "An Elephant update is already in progress.")
		return
	}
	u.busy = "confirm"
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.busy = ""
		u.mu.Unlock()
		c.Refresh()
	}()
	c.Refresh()

	// Decide from each file on disk: after an install the running processes,
	// and the cached status, still report the old version.
	unreadable := map[string]string{}
	p := u.plan(c, func(target string) string {
		v, err := u.probe(c.Context, target)
		if err != nil {
			unreadable[target] = err.Error()
			return ""
		}
		return v
	})
	if c.Context.Err() != nil {
		return
	}
	switch {
	case p.err != nil:
		c.Inform(updatesTitle, "Elephant could not read the update status: "+p.err.Error())
		return
	case p.disabled:
		c.Inform(updatesTitle, "Update checks are disabled. Nothing was installed.")
		return
	}
	latest := ""
	if p.status.State == "available" && p.status.Latest != nil {
		latest = p.status.Latest.Version
	}
	var pending, current []menuInstallTarget
	var unread []string
	for _, target := range p.targets {
		switch {
		case unreadable[target] != "":
			unread = append(unread, target+" — "+unreadable[target])
		case p.want[target] != nil:
			pending = append(pending, menuInstallTarget{target, p.installed[target], p.want[target].Version})
		default:
			current = append(current, menuInstallTarget{target, p.installed[target], ""})
		}
	}
	wanted, releases, _ := p.wanted()
	if len(pending) == 0 {
		if latest == "" && p.product == nil {
			c.Inform(updatesTitle, "No update is available. Choose Check for Updates first.")
			return
		}
		msg := "Every Elephant binary already reports the newest release it can use. Nothing was installed."
		if p.product == nil {
			msg = fmt.Sprintf("Every Elephant binary already reports Elephant %s or newer. Nothing was installed.", latest)
		}
		if len(unread) > 0 {
			msg += "\n\nElephant could not read the version of:\n" + strings.Join(unread, "\n")
		}
		u.mu.Lock()
		for _, t := range current {
			if u.restart[t.path] != "" {
				msg += "\n\nRestart running Elephant sessions to use it."
				break
			}
		}
		u.mu.Unlock()
		c.Inform(updatesTitle, msg)
		return
	}

	// One release keeps the single-version wording; several releases list
	// each binary's own target version.
	single := len(wanted) == 1
	newest := newestVersion(wanted)
	var b strings.Builder
	if single {
		fmt.Fprintf(&b, "Elephant %s — %s\n\nReplace these Elephant binaries:\n", newest, releases[newest].Summary)
		for _, t := range pending {
			fmt.Fprintf(&b, "%s (Elephant %s)\n", t.path, t.installed)
		}
	} else {
		b.WriteString("Each binary moves to the newest release its version can use:\n")
		for _, v := range wanted {
			fmt.Fprintf(&b, "Elephant %s — %s\n", v, releases[v].Summary)
		}
		b.WriteString("\nReplace these Elephant binaries:\n")
		for _, t := range pending {
			fmt.Fprintf(&b, "%s (Elephant %s → %s)\n", t.path, t.installed, t.want)
		}
	}
	if len(current) > 0 {
		b.WriteString("\nAlready current, not changed:\n")
		for _, t := range current {
			fmt.Fprintf(&b, "%s (Elephant %s)\n", t.path, t.installed)
		}
	}
	if len(unread) > 0 {
		b.WriteString("\nVersion unreadable, not changed:\n" + strings.Join(unread, "\n") + "\n")
	}
	if len(p.launcher) > 0 {
		fmt.Fprintf(&b, "\nManaged by the npm launcher, not changed:\n%s\nRun npx --yes elephant-memory@%s to update those.\n", strings.Join(p.launcher, "\n"), newest)
	}
	reports := newest
	if !single {
		reports = "the version listed for it"
	}
	fmt.Fprintf(&b, "\nFor each binary, Elephant downloads its platform's archive from the GitHub release, checks it against that release's SHA256SUMS, confirms the new binary reports %s, then replaces the file in one step. Releases are not signed: the checksum shows the download matches the published release, not who published it.\n\n", reports)
	b.WriteString("Nothing restarts automatically. Running Elephant servers and this menu bar keep their current version until you restart them.")
	title, button := "Update Elephant Everywhere to "+newest+"?", "Install "+newest
	if !single {
		title, button = "Update Elephant Everywhere?", "Install Updates"
	}
	choice, err := c.Prompt(menubar.Dialog{
		ID: "updates/install/confirm", Title: title, Message: b.String(),
		Buttons: []menubar.Button{{ID: "cancel", Title: "Cancel"}, {ID: "install", Title: button}},
		Default: "cancel", Cancel: "cancel",
	})
	if err != nil || choice.Choice != "install" || c.Context.Err() != nil {
		return
	}
	u.mu.Lock()
	u.busy = "install"
	u.mu.Unlock()
	c.Refresh()

	selfPath := ""
	if self, err := u.self(); err == nil {
		selfPath, _ = resolveMenuExecutable(self)
	}
	replacedSelf := false
	var done, failed []string
	for _, t := range pending {
		if c.Context.Err() != nil {
			failed = append(failed, t.path+" — not attempted: the update was cancelled")
			continue
		}
		res, err := u.install(c.Context, t.path, t.want)
		if res.Version == "" {
			msg := "unknown error"
			if err != nil {
				msg = err.Error()
			}
			failed = append(failed, t.path+" — "+msg+" The binary was left in place.")
			continue
		}
		u.mu.Lock()
		u.restart[t.path] = res.Version
		delete(u.versions, t.path)
		u.mu.Unlock()
		replacedSelf = replacedSelf || t.path == selfPath
		line := fmt.Sprintf("%s (was %s)", res.Target, res.Previous)
		if !single {
			line = fmt.Sprintf("%s → %s (was %s)", res.Target, res.Version, res.Previous)
		}
		if err != nil {
			line += " — warning: " + err.Error()
		}
		done = append(done, line)
	}
	installed := "Elephant " + newest
	if !single {
		installed = "Elephant updates"
	}
	var msg strings.Builder
	if len(done) > 0 {
		fmt.Fprintf(&msg, "Installed %s at:\n%s\n\n", installed, strings.Join(done, "\n"))
	}
	if len(failed) > 0 {
		fmt.Fprintf(&msg, "%s was not installed at:\n%s\n\n", installed, strings.Join(failed, "\n"))
	}
	if len(done) == 0 {
		c.Inform(updatesTitle, strings.TrimSpace(msg.String()))
		return
	}
	msg.WriteString("Restart your agent sessions and this menu bar to use it. Nothing was restarted automatically.")
	if replacedSelf {
		msg.WriteString(" This menu bar runs a replaced binary: choose Hide Menu Bar, then start a new session to restart it.")
	}
	c.Inform("Restart Needed", msg.String())
}
