//go:build darwin

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

// menuBarUpdates shows cached release status for the selected session's binary
// and installs a release only after a native confirmation naming the target.
// It never restarts servers. The binary's on-disk version, not the running
// version, decides whether an install still applies.
type menuBarUpdates struct {
	cached  func(memory.Store) (memory.UpdateStatus, error)
	check   func(memory.Store) (memory.UpdateStatus, error)
	install func(context.Context, string, string) (memory.UpdateInstallResult, error)
	probe   func(context.Context, string) (string, error)

	mu       sync.Mutex
	checking map[string]bool               // store path
	busy     map[string]string             // resolved binary -> "confirm" or "install"
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
		checking: map[string]bool{},
		busy:     map[string]string{},
		versions: map[string]*menuBinaryVersion{},
		restart:  map[string]string{},
	}
}

// resolveMenuExecutable returns the real file a session's binary path names.
func resolveMenuExecutable(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("this session did not report its Elephant binary")
	}
	abs, err := filepath.Abs(p)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs)
	}
	return abs, err
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

func (u *menuBarUpdates) Items(c menuControlContext) ([]menubar.Item, bool) {
	status, statusErr := u.cached(c.Service.Store)
	target, targetErr := resolveMenuExecutable(c.Executable)
	installed, probing := "", false
	if targetErr == nil {
		installed, probing = u.binaryVersion(c, target)
	}
	u.mu.Lock()
	checking, busy, restart := u.checking[c.Service.Store.Path], u.busy[target], u.restart[target]
	u.mu.Unlock()

	var rows []menubar.Item
	info := func(title string) { rows = append(rows, menubar.Item{Title: title}) }
	switch {
	case installed != "":
		info("Installed: Elephant " + installed)
	case probing:
		info("Installed: checking version…")
	default:
		info("Installed: version unknown")
	}
	attention := false
	if restart != "" {
		info("Restart needed to use Elephant " + restart)
		attention = true
	}
	latest := ""
	switch {
	case statusErr != nil:
		info("Update status unavailable")
	case !status.Enabled || status.State == "disabled":
		info("Update checks are disabled")
	case status.State == "available" && status.Latest != nil && installed != "" && memory.UpdateApplies(installed, status.Latest.Version):
		latest = status.Latest.Version
		info("Available: Elephant " + latest + " — " + status.Latest.Summary)
		attention = true
	case status.State == "available" && status.Latest != nil && installed != "":
		info("No newer release for the installed version")
	case status.State == "available" && status.Latest != nil:
		info("Available: Elephant " + status.Latest.Version + " (installed version not confirmed)")
	case status.State == "unavailable":
		info("Last check failed")
	case status.State == "current" && installed != "" && installed != status.Installed:
		info("Last check found no release newer than Elephant " + status.Installed)
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
	if latest != "" && restart != latest {
		title := "Install Elephant " + latest + "…"
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
		target, terr := resolveMenuExecutable(c.Executable)
		if terr == nil {
			if installed, perr := u.probe(c.Context, target); perr == nil && !memory.UpdateApplies(installed, status.Latest.Version) {
				msg += fmt.Sprintf("\n\nThe binary at %s already reports Elephant %s, so there is nothing to install there.", target, installed)
			} else if perr == nil {
				msg += fmt.Sprintf("\n\nChoose Install Elephant %s… to update the binary at %s (currently %s).", status.Latest.Version, target, installed)
			}
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

func (u *menuBarUpdates) installNow(c menuControlContext) {
	status, err := u.cached(c.Service.Store)
	if err != nil || status.State != "available" || status.Latest == nil {
		c.Inform(updatesTitle, "No update is available. Choose Check for Updates first.")
		return
	}
	latest := status.Latest.Version
	target, err := resolveMenuExecutable(c.Executable)
	if err != nil {
		c.Inform(updatesTitle, "Elephant could not locate this session's binary, so nothing was installed.")
		return
	}
	u.mu.Lock()
	if u.busy[target] != "" {
		u.mu.Unlock()
		c.Inform(updatesTitle, "An update for "+target+" is already in progress.")
		return
	}
	u.busy[target] = "confirm"
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		delete(u.busy, target)
		u.mu.Unlock()
		c.Refresh()
	}()
	c.Refresh()
	// Decide from the file on disk: after an install the running processes,
	// and the cached status, still report the old version.
	installed, err := u.probe(c.Context, target)
	if err != nil {
		if c.Context.Err() == nil {
			c.Inform(updatesTitle, "Elephant could not read the version of "+target+", so nothing was installed: "+err.Error())
		}
		return
	}
	if !memory.UpdateApplies(installed, latest) {
		msg := fmt.Sprintf("The binary at %s already reports Elephant %s; Elephant %s is not newer for it. Nothing was installed.", target, installed, latest)
		u.mu.Lock()
		if u.restart[target] != "" {
			msg += " Restart running Elephant sessions to use it."
		}
		u.mu.Unlock()
		c.Inform(updatesTitle, msg)
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Replace the Elephant binary at:\n%s\n", target)
	if exe := filepath.Clean(c.Executable); exe != target {
		fmt.Fprintf(&b, "(this session's %s points to it)\n", exe)
	}
	fmt.Fprintf(&b, "\nInstalled: Elephant %s\nNew: Elephant %s — %s\n\n", installed, latest, status.Latest.Summary)
	fmt.Fprintf(&b, "Elephant downloads this platform's archive from the GitHub release, checks it against that release's SHA256SUMS, confirms the new binary reports %s, then replaces the file in one step. Releases are not signed: the checksum shows the download matches the published release, not who published it.\n\n", latest)
	fmt.Fprintf(&b, "Nothing restarts automatically. Running Elephant servers and this menu bar keep their current version until you restart them. Other Elephant binaries are not changed.")
	choice, err := c.Prompt(menubar.Dialog{
		ID: "updates/install/confirm", Title: "Install Elephant " + latest + "?", Message: b.String(),
		Buttons: []menubar.Button{{ID: "cancel", Title: "Cancel"}, {ID: "install", Title: "Install " + latest}},
		Default: "cancel", Cancel: "cancel",
	})
	if err != nil || choice.Choice != "install" || c.Context.Err() != nil {
		return
	}
	u.mu.Lock()
	u.busy[target] = "install"
	u.mu.Unlock()
	c.Refresh()
	res, err := u.install(c.Context, target, latest)
	if res.Version != "" {
		u.mu.Lock()
		u.restart[target] = res.Version
		delete(u.versions, target)
		u.mu.Unlock()
	}
	if c.Context.Err() != nil && res.Version == "" {
		return
	}
	switch {
	case res.Version == "":
		c.Inform(updatesTitle, fmt.Sprintf("Elephant %s was not installed.\n\n%s\n\nThe binary at %s was left in place.", latest, err.Error(), target))
	default:
		msg := fmt.Sprintf("Installed Elephant %s at %s (was %s).\n\nRestart your agent sessions and this menu bar to use it. Nothing was restarted automatically.", res.Version, res.Target, res.Previous)
		if err != nil {
			msg += "\n\nWarning: " + err.Error()
		}
		c.Inform("Restart Needed", msg)
	}
}
