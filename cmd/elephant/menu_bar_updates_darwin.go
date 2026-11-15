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

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

// menuBarUpdates updates Elephant as one product: every binary used by a
// connected process, the companion itself, and the elephant found on PATH.
// It installs only after one native confirmation listing every target and
// never restarts servers. Each binary's on-disk version, not the running
// version, decides whether an install still applies to it.
type menuBarUpdates struct {
	cached   func(memory.Store) (memory.UpdateStatus, error)
	check    func(memory.Store) (memory.UpdateStatus, error)
	install  func(context.Context, string, string) (memory.UpdateInstallResult, error)
	probe    func(context.Context, string) (string, error)
	self     func() (string, error)
	lookPath func(string) (string, error)
	home     func() (string, error)

	mu       sync.Mutex
	checking map[string]bool               // store path
	busy     string                        // "", "confirm" or "install"
	versions map[string]*menuBinaryVersion // resolved binary -> on-disk version
	restart  map[string]string             // resolved binary -> version installed by this host
}

type menuBinaryVersion struct {
	info    os.FileInfo
	version string
	probing bool
}

const updatesTitle = "Elephant Updates"

func newMenuBarUpdates() *menuBarUpdates {
	return &menuBarUpdates{
		cached: func(s memory.Store) (memory.UpdateStatus, error) { return s.CachedUpdate() },
		check: func(s memory.Store) (memory.UpdateStatus, error) {
			return s.Updates(memory.UpdateOptions{Check: true, Force: true})
		},
		install:  memory.InstallUpdate,
		probe:    memory.ExecutableVersion,
		self:     os.Executable,
		lookPath: exec.LookPath,
		home:     os.UserHomeDir,
		checking: map[string]bool{},
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

func (u *menuBarUpdates) Items(c menuControlContext) ([]menubar.Item, bool) {
	status, statusErr := u.cached(c.Service.Store)
	targets, launcher := u.targets(c)
	installed := make(map[string]string, len(targets))
	byVersion := map[string]int{}
	probing, unknown := 0, 0
	for _, target := range targets {
		v, p := u.binaryVersion(c, target)
		installed[target] = v
		switch {
		case p:
			probing++
		case v == "":
			unknown++
		default:
			byVersion[v]++
		}
	}
	u.mu.Lock()
	checking, busy := u.checking[c.Service.Store.Path], u.busy
	restart := ""
	for _, target := range targets {
		if v := u.restart[target]; v != "" {
			restart = v
		}
	}
	u.mu.Unlock()

	var rows []menubar.Item
	info := func(title string) { rows = append(rows, menubar.Item{Title: title}) }
	versions := make([]string, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	for _, v := range versions {
		if len(targets) == 1 {
			info("Installed: Elephant " + v)
		} else {
			info("Installed: Elephant " + v + " · " + plural(byVersion[v], "binary", "binaries"))
		}
	}
	if probing > 0 {
		info("Installed: checking versions…")
	}
	if unknown > 0 {
		info("Installed: version unknown · " + plural(unknown, "binary", "binaries"))
	}
	if len(launcher) > 0 {
		info("npm launcher: " + plural(len(launcher), "copy", "copies") + " (update with npx)")
	}
	attention := false
	if restart != "" {
		info("Restart needed to use Elephant " + restart)
		attention = true
	}
	latest, pending := "", 0
	if statusErr == nil && status.Latest != nil {
		for _, target := range targets {
			if v := installed[target]; v != "" && memory.UpdateApplies(v, status.Latest.Version) {
				pending++
			}
		}
	}
	switch {
	case statusErr != nil:
		info("Update status unavailable")
	case !status.Enabled || status.State == "disabled":
		info("Update checks are disabled")
	case status.State == "available" && status.Latest != nil && pending > 0:
		latest = status.Latest.Version
		info("Available: Elephant " + latest + " — " + status.Latest.Summary)
		attention = true
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
	if statusErr == nil && !status.Checked.IsZero() {
		info("Last checked: " + status.Checked.Local().Format("Jan 2, 15:04"))
	}
	rows = append(rows, menubar.Item{Separator: true})
	checkTitle := "Check for Updates"
	if checking {
		checkTitle = "Checking for Updates…"
	}
	rows = append(rows, menubar.Item{ID: "updates/check", Title: checkTitle, Enabled: statusErr == nil && status.Enabled && status.State != "disabled" && !checking})
	if statusErr == nil && status.Latest != nil {
		rows = append(rows, menubar.Item{ID: "updates/notes", Title: "Release Notes for Elephant " + status.Latest.Version, Enabled: true})
	}
	if latest != "" {
		title := "Install Elephant " + latest + " Everywhere…"
		if busy == "install" {
			title = "Installing Elephant " + latest + "…"
		}
		rows = append(rows, menubar.Item{ID: "updates/install", Title: title, Enabled: busy == ""})
	}
	return rows, attention
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

func (u *menuBarUpdates) checkNow(c menuControlContext) {
	key := c.Service.Store.Path
	u.mu.Lock()
	if u.checking[key] {
		u.mu.Unlock()
		return
	}
	u.checking[key] = true
	u.mu.Unlock()
	c.Refresh()
	status, err := u.check(c.Service.Store)
	u.mu.Lock()
	delete(u.checking, key)
	u.mu.Unlock()
	c.Refresh()
	if c.Context.Err() != nil {
		return
	}
	if err != nil {
		c.Inform(updatesTitle, "Elephant could not check for updates: "+err.Error())
		return
	}
	msg := status.Message
	if status.State == "available" && status.Latest != nil {
		msg = fmt.Sprintf("Elephant %s is available: %s", status.Latest.Version, status.Latest.Summary)
		targets, _ := u.targets(c)
		pending := 0
		for _, target := range targets {
			if installed, perr := u.probe(c.Context, target); perr == nil && memory.UpdateApplies(installed, status.Latest.Version) {
				pending++
			}
		}
		if pending == 0 {
			msg += "\n\nEvery Elephant binary already reports this version or newer, so there is nothing to install."
		} else {
			msg += fmt.Sprintf("\n\nChoose Install Elephant %s Everywhere… to update %s.", status.Latest.Version, plural(pending, "binary", "binaries"))
		}
	} else if status.State == "current" && status.Installed != "" {
		msg += " The check compared releases with Elephant " + status.Installed + "."
	}
	c.Inform(updatesTitle, msg)
}

func (u *menuBarUpdates) openNotes(c menuControlContext) {
	status, err := u.cached(c.Service.Store)
	if err != nil || status.Latest == nil {
		c.Inform(updatesTitle, "No release is known yet. Choose Check for Updates first.")
		return
	}
	link, ok := memory.ReleaseNotesURL(status.Latest.Version)
	if !ok {
		c.Inform(updatesTitle, "The cached release version is not valid. Choose Check for Updates again.")
		return
	}
	if err := c.OpenURL(link); err != nil {
		c.Inform(updatesTitle, "Could not open the release notes: "+err.Error())
	}
}

type menuInstallTarget struct {
	path, installed string
}

func (u *menuBarUpdates) installNow(c menuControlContext) {
	status, err := u.cached(c.Service.Store)
	if err != nil || status.State != "available" || status.Latest == nil {
		c.Inform(updatesTitle, "No update is available. Choose Check for Updates first.")
		return
	}
	latest := status.Latest.Version
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

	targets, launcher := u.targets(c)
	// Decide from each file on disk: after an install the running processes,
	// and the cached status, still report the old version.
	var pending, current []menuInstallTarget
	var unreadable []string
	for _, target := range targets {
		installed, err := u.probe(c.Context, target)
		if c.Context.Err() != nil {
			return
		}
		switch {
		case err != nil:
			unreadable = append(unreadable, target+" — "+err.Error())
		case memory.UpdateApplies(installed, latest):
			pending = append(pending, menuInstallTarget{target, installed})
		default:
			current = append(current, menuInstallTarget{target, installed})
		}
	}
	if len(pending) == 0 {
		msg := fmt.Sprintf("Every Elephant binary already reports Elephant %s or newer. Nothing was installed.", latest)
		if len(unreadable) > 0 {
			msg += "\n\nElephant could not read the version of:\n" + strings.Join(unreadable, "\n")
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

	var b strings.Builder
	fmt.Fprintf(&b, "Elephant %s — %s\n\nReplace these Elephant binaries:\n", latest, status.Latest.Summary)
	for _, t := range pending {
		fmt.Fprintf(&b, "%s (Elephant %s)\n", t.path, t.installed)
	}
	if len(current) > 0 {
		b.WriteString("\nAlready current, not changed:\n")
		for _, t := range current {
			fmt.Fprintf(&b, "%s (Elephant %s)\n", t.path, t.installed)
		}
	}
	if len(unreadable) > 0 {
		b.WriteString("\nVersion unreadable, not changed:\n" + strings.Join(unreadable, "\n") + "\n")
	}
	if len(launcher) > 0 {
		fmt.Fprintf(&b, "\nManaged by the npm launcher, not changed:\n%s\nRun npx --yes elephant-memory@%s to update those.\n", strings.Join(launcher, "\n"), latest)
	}
	fmt.Fprintf(&b, "\nFor each binary, Elephant downloads its platform's archive from the GitHub release, checks it against that release's SHA256SUMS, confirms the new binary reports %s, then replaces the file in one step. Releases are not signed: the checksum shows the download matches the published release, not who published it.\n\n", latest)
	b.WriteString("Nothing restarts automatically. Running Elephant servers and this menu bar keep their current version until you restart them.")
	choice, err := c.Prompt(menubar.Dialog{
		ID: "updates/install/confirm", Title: "Update Elephant Everywhere to " + latest + "?", Message: b.String(),
		Buttons: []menubar.Button{{ID: "cancel", Title: "Cancel"}, {ID: "install", Title: "Install " + latest}},
		Default: "cancel", Cancel: "cancel",
	})
	if err != nil || choice.Choice != "install" || c.Context.Err() != nil {
		return
	}
	u.mu.Lock()
	u.busy = "install"
	u.mu.Unlock()
	c.Refresh()

	var done, failed []string
	for _, t := range pending {
		if c.Context.Err() != nil {
			failed = append(failed, t.path+" — not attempted: the update was cancelled")
			continue
		}
		res, err := u.install(c.Context, t.path, latest)
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
		line := fmt.Sprintf("%s (was %s)", res.Target, res.Previous)
		if err != nil {
			line += " — warning: " + err.Error()
		}
		done = append(done, line)
	}
	var msg strings.Builder
	if len(done) > 0 {
		fmt.Fprintf(&msg, "Installed Elephant %s at:\n%s\n\n", latest, strings.Join(done, "\n"))
	}
	if len(failed) > 0 {
		fmt.Fprintf(&msg, "Elephant %s was not installed at:\n%s\n\n", latest, strings.Join(failed, "\n"))
	}
	if len(done) == 0 {
		c.Inform(updatesTitle, strings.TrimSpace(msg.String()))
		return
	}
	msg.WriteString("Restart your agent sessions and this menu bar to use it. Nothing was restarted automatically.")
	c.Inform("Restart Needed", msg.String())
}
