//go:build darwin

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

// dreamHookBudget matches the default recall budget of elephant init.
const dreamHookBudget = 4000

// dreamPollInterval paces status reads while a Dream Now review runs.
const dreamPollInterval = 500 * time.Millisecond

// menuBarDreams shows the dreaming state of one session's exact automation
// setup and runs its explicit controls. Dialog-driven changes are serialized
// per project root; status, cancel and result display never wait for them.
type menuBarDreams struct {
	mu   sync.Mutex
	busy map[string]bool
}

func newMenuBarDreams() *menuBarDreams { return &menuBarDreams{busy: map[string]bool{}} }

// dreamView is one status snapshot. found is false when the session's root has
// no automation; err covers unreadable or foreign setups.
type dreamView struct {
	found  bool
	err    error
	status memory.IdleDreamStatus
}

func loadDreamView(s memory.Service) dreamView {
	var v dreamView
	v.status, v.found, v.err = s.DreamStatus()
	return v
}

// Failed review results that need the user's attention. Cancellation, foreground
// interruption and commit rejections are normal safety outcomes.
var dreamFailures = map[string]bool{"runner_failed": true, "prepare_failed": true, "invalid_output": true, "eval_failed": true}

// interrupted reports a review whose worker stopped without recording a result.
func (v dreamView) interrupted() bool {
	return v.status.State.LastResult == "running" && !v.status.Reviewing
}

// label is the short status and whether it needs attention.
func (v dreamView) label() (string, bool) {
	st := v.status
	switch {
	case v.err != nil:
		return "Unavailable", false
	case !v.found || st.Config == nil:
		return "Not configured", false
	case st.WaitReason == "automation_disabled":
		return "Paused (automatic hooks off)", false
	case st.Reviewing:
		return "Reviewing", false
	case st.ManualBlocker == "requested":
		return "Starting review", false
	case !st.Config.Enabled:
		return "Paused", false
	case v.interrupted():
		return "Failed (review interrupted)", true
	case dreamFailures[st.State.LastResult]:
		return "Failed (" + dreamResultText(st.State) + ")", true
	}
	return "Scheduled", false
}

func (d *menuBarDreams) Items(c menuControlContext) ([]menubar.Item, bool) {
	v := loadDreamView(c.Service)
	label, attention := v.label()
	rows := []menubar.Item{menuBarRow("Status: " + label)}
	switch {
	case errors.Is(v.err, memory.ErrDreamSetupMismatch):
		rows = append(rows, menuBarRow("This project's automation belongs to another store or identity"))
	case v.err != nil:
		rows = append(rows, menuBarRow(cleanMenuText(v.err.Error(), 120)))
	case !v.found:
		rows = append(rows, menuBarRow("Needs Elephant automatic hooks in this project"), menuBarSeparator(), menuBarAction("dream/setup", "Set Up Dreaming…"))
	case v.status.Config == nil:
		rows = append(rows, menuBarSeparator(), menuBarAction("dream/configure", "Configure Dreaming…"))
	default:
		rows = append(rows, v.statusRows(time.Now())...)
		rows = append(rows, menuBarSeparator())
		rows = append(rows, v.actionRows()...)
	}
	return rows, attention
}

func (v dreamView) statusRows(now time.Time) []menubar.Item {
	st := v.status
	rows := []menubar.Item{}
	if next := dreamNextText(st, now); next != "" {
		rows = append(rows, menuBarRow("Next: "+next))
	}
	if st.State.LastResult != "" {
		last := "Last: " + dreamResultText(st.State)
		if v.interrupted() {
			last = "Last: review interrupted"
		}
		if at := dreamLastTime(st.State); !at.IsZero() {
			last += " · " + dreamTime(at, now)
		}
		rows = append(rows, menuBarRow(last))
	}
	rows = append(rows, menuBarRow(fmt.Sprintf("Reviews today: %d of %d (UTC)", st.State.Attempts, st.Config.MaxRunsPerDay)))
	if n := len(st.State.LastSkipped); n > 0 {
		rows = append(rows, menuBarRow(fmt.Sprintf("Skipped last time: %d oversized %s", n, dreamPlural(n, "memory", "memories"))))
	}
	return rows
}

var dreamBlockerTitles = map[string]string{
	"automation_disabled": "automatic hooks paused",
	"disabled":            "schedule paused",
	"reviewing":           "review running",
	"requested":           "starting",
	"active_sessions":     "foreground work active",
	"daily_limit":         "daily limit reached",
}

func (v dreamView) actionRows() []menubar.Item {
	st := v.status
	now := menubar.Item{ID: "dream/now", Title: "Dream Now…", Enabled: st.ManualBlocker == ""}
	if reason := dreamBlockerTitles[st.ManualBlocker]; reason != "" {
		now.Title = "Dream Now… (" + reason + ")"
	}
	rows := []menubar.Item{now}
	if st.Reviewing || st.ManualBlocker == "requested" {
		rows = append(rows, menuBarAction("dream/cancel", "Cancel Review"))
	}
	if st.Config.Enabled {
		rows = append(rows, menuBarAction("dream/pause", "Pause Schedule"))
	} else {
		rows = append(rows, menuBarAction("dream/resume", "Resume Schedule…"))
	}
	return append(rows,
		menuBarAction("dream/configure", "Configure…"),
		menubar.Item{ID: "dream/last", Title: "Show Last Result…", Enabled: st.State.LastResult != ""},
	)
}

func (d *menuBarDreams) Handle(c menuControlContext, action string) bool {
	switch action {
	case "dream/cancel":
		d.cancel(c)
	case "dream/last":
		d.showLast(c)
	case "dream/now":
		var id string
		d.exclusive(c, func() { id = d.requestNow(c) })
		if id != "" {
			d.waitForRun(c, id)
		}
	case "dream/setup":
		d.exclusive(c, func() { d.setup(c) })
	case "dream/configure":
		d.exclusive(c, func() { d.configure(c) })
	case "dream/pause":
		d.exclusive(c, func() { d.setEnabled(c, false) })
	case "dream/resume":
		d.exclusive(c, func() { d.setEnabled(c, true) })
	default:
		return false
	}
	return true
}

// exclusive runs fn unless another dialog-driven action holds this root.
func (d *menuBarDreams) exclusive(c menuControlContext, fn func()) {
	key := filepath.Clean(c.Service.Root)
	d.mu.Lock()
	if d.busy[key] {
		d.mu.Unlock()
		c.Inform("Dreaming", "Another dreaming action for this project is still open. Finish it first.")
		return
	}
	d.busy[key] = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.busy, key)
		d.mu.Unlock()
	}()
	fn()
}

// configured loads a view that has a schedule, or explains why not.
func (d *menuBarDreams) configured(c menuControlContext) (dreamView, bool) {
	v := loadDreamView(c.Service)
	switch {
	case v.err != nil:
		c.Inform("Dreaming", v.err.Error())
	case !v.found || v.status.Config == nil:
		c.Inform("Dreaming", "Dreaming is not configured for this session. Use Set Up Dreaming or Configure Dreaming first.")
	default:
		return v, true
	}
	return v, false
}

func (d *menuBarDreams) setup(c menuControlContext) {
	v := loadDreamView(c.Service)
	if v.err != nil {
		c.Inform("Dreaming", v.err.Error())
		return
	}
	if v.found {
		d.configureFrom(c, v)
		return
	}
	message := "Dreaming reviews memories between tasks with a trusted reviewer you choose. It needs Elephant's automatic hooks in this project, so it knows when foreground agent work stops.\n\n" +
		"Project folder: " + dreamDisplayPath(c.Service.Root) + "\n" + dreamScopeText(c.Service) + "\n\n" +
		"Installing hooks writes project-local files for the agent you choose: .elephant/automation.json, that agent's hook settings (.codex/hooks.json or .claude/settings.local.json), a managed section in AGENTS.md or CLAUDE.md, and .gitignore entries. Changed files are backed up first.\n\n" +
		"Elephant does not change host trust or MCP permissions. Restart the agent; it asks you to review and approve the hooks.\n\nInstall hooks for which agent?"
	a, err := c.Prompt(menubar.Dialog{Title: "Set Up Dreaming", Message: message, Buttons: []menubar.Button{{ID: "codex", Title: "Codex"}, {ID: "claude", Title: "Claude Code"}, {ID: "both", Title: "Both"}, {ID: "cancel", Title: "Cancel"}}, Default: "cancel", Cancel: "cancel"})
	if err != nil || (a.Choice != "codex" && a.Choice != "claude" && a.Choice != "both") {
		return
	}
	r, err := memory.InitAutomation(c.Service, c.Executable, a.Choice, dreamHookBudget)
	c.Refresh()
	if err != nil {
		c.Inform("Hooks Not Installed", err.Error())
		return
	}
	c.Inform("Hooks Installed", strings.Join(r.Next, "\n\n"))
	if v = loadDreamView(c.Service); v.err != nil || !v.found {
		c.Inform("Dreaming", "Hooks were installed, but this session cannot read the new automation setup.")
		return
	}
	d.configureFrom(c, v)
}

func (d *menuBarDreams) configure(c menuControlContext) {
	v := loadDreamView(c.Service)
	switch {
	case v.err != nil:
		c.Inform("Dreaming", v.err.Error())
	case !v.found:
		d.setup(c)
	default:
		d.configureFrom(c, v)
	}
}

// configureFrom asks for the reviewer, the limits and consent, then saves.
func (d *menuBarDreams) configureFrom(c menuControlContext, v dreamView) {
	settings := memory.DefaultIdleDreamConfig()
	current := v.status.Config
	if current != nil {
		settings = *current
		settings.Runner = append([]string(nil), current.Runner...)
	}
	settings, ok := chooseDreamReviewer(c, settings, current != nil)
	if !ok {
		return
	}
	if settings, ok = chooseDreamLimits(c, settings); !ok {
		return
	}
	a, err := c.Prompt(menubar.Dialog{Title: "Enable Dreaming?", Message: dreamConsentText(c.Service, settings) + "\n\nAutomatic reviews run in the background between tasks. You can pause the schedule or cancel a review from this menu.", Buttons: []menubar.Button{{ID: "enable", Title: "Save and Enable"}, {ID: "paused", Title: "Save Paused"}, {ID: "cancel", Title: "Cancel"}}, Default: "cancel", Cancel: "cancel"})
	if err != nil || (a.Choice != "enable" && a.Choice != "paused") {
		return
	}
	settings.Enabled = a.Choice == "enable"
	_, err = c.Service.ConfigureDream(settings)
	c.Refresh()
	switch {
	case err != nil:
		c.Inform("Dreaming Not Configured", err.Error())
	case settings.Enabled:
		c.Inform("Dreaming Enabled", "The schedule is on. A review starts after a completed agent task and the quiet period. Use Dream Now to start one sooner.")
	default:
		c.Inform("Dreaming Configured", "Settings saved. The schedule stays paused until you resume it.")
	}
}

func chooseDreamReviewer(c menuControlContext, settings memory.IdleDreamConfig, existing bool) (memory.IdleDreamConfig, bool) {
	buttons := []menubar.Button{}
	message := "Choose the trusted local reviewer. Elephant sends it a bounded batch of memories and saves only replacements that pass its own source, revision, scope, applicability and writing checks."
	if existing {
		buttons = append(buttons, menubar.Button{ID: "keep", Title: "Keep Current"})
		message += "\n\nCurrent reviewer: " + dreamReviewerText(settings)
	}
	buttons = append(buttons, menubar.Button{ID: "codex", Title: "Codex CLI"}, menubar.Button{ID: "command", Title: "Custom Command…"}, menubar.Button{ID: "cancel", Title: "Cancel"})
	a, err := c.Prompt(menubar.Dialog{Title: "Dreaming Reviewer", Message: message, Buttons: buttons, Cancel: "cancel"})
	if err != nil {
		return settings, false
	}
	switch a.Choice {
	case "keep":
		return settings, existing
	case "codex":
		path, err := exec.LookPath("codex")
		if err == nil {
			path, err = filepath.Abs(path)
		}
		if err != nil {
			c.Inform("Codex CLI Not Found", "Codex CLI is not on this app's PATH. Choose Custom Command and enter the absolute path of a reviewer.")
			return settings, false
		}
		settings.Adapter, settings.Runner = "codex", []string{path}
		return settings, true
	case "command":
		value := ""
		if settings.Adapter == "command" {
			b, _ := json.Marshal(settings.Runner)
			value = string(b)
		}
		a, err = c.Prompt(menubar.Dialog{Title: "Custom Reviewer Command", Message: "Enter the command as a JSON array of arguments that starts with an absolute executable path, for example [\"/usr/local/bin/reviewer\", \"--json\"]. No shell is used.\n\nElephant writes the review request JSON to its standard input in the project folder and reads the result JSON from its standard output.", Buttons: []menubar.Button{{ID: "ok", Title: "Continue"}, {ID: "cancel", Title: "Cancel"}}, Default: "ok", Cancel: "cancel", Input: true, Value: value})
		if err != nil || a.Choice != "ok" {
			return settings, false
		}
		var runner []string
		if err = json.Unmarshal([]byte(a.Text), &runner); err != nil || len(runner) == 0 || !filepath.IsAbs(runner[0]) {
			c.Inform("Reviewer Not Changed", "Enter a JSON array of strings whose first item is an absolute executable path.")
			return settings, false
		}
		settings.Adapter, settings.Runner = "command", runner
		return settings, true
	}
	return settings, false
}

func chooseDreamLimits(c menuControlContext, settings memory.IdleDreamConfig) (memory.IdleDreamConfig, bool) {
	message := "Edit the limits as key=value pairs:\n\n" +
		"idle: quiet time after all agent sessions stop (1m–24h)\n" +
		"cooldown: minimum time between reviews (1m–168h)\n" +
		"timeout: maximum reviewer run time (10s–15m)\n" +
		"runs: reviews per UTC day, Dream Now included (1–24)\n" +
		"memories: memories per review (1–20)\n" +
		"bytes: memory text per review (2048–64000)\n" +
		"changes: replacements per review (1–3)"
	a, err := c.Prompt(menubar.Dialog{Title: "Dreaming Limits", Message: message, Buttons: []menubar.Button{{ID: "ok", Title: "Continue"}, {ID: "cancel", Title: "Cancel"}}, Default: "ok", Cancel: "cancel", Input: true, Value: formatDreamLimits(settings)})
	if err != nil || a.Choice != "ok" {
		return settings, false
	}
	next, err := parseDreamLimits(a.Text, settings)
	if err != nil {
		c.Inform("Limits Not Changed", err.Error())
		return settings, false
	}
	return next, true
}

func formatDreamLimits(s memory.IdleDreamConfig) string {
	return fmt.Sprintf("idle=%s cooldown=%s timeout=%s runs=%d memories=%d bytes=%d changes=%d", dreamDuration(s.IdleSeconds), dreamDuration(s.CooldownSeconds), dreamDuration(s.TimeoutSeconds), s.MaxRunsPerDay, s.Limit, s.ByteBudget, s.MaxChanges)
}

// parseDreamLimits applies key=value limits to base. ConfigureDream enforces
// the allowed ranges.
func parseDreamLimits(text string, base memory.IdleDreamConfig) (memory.IdleDreamConfig, error) {
	seen := map[string]bool{}
	for _, field := range strings.Fields(text) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || seen[key] {
			return base, fmt.Errorf("use each limit once as key=value, for example idle=10m")
		}
		seen[key] = true
		switch key {
		case "idle", "cooldown", "timeout":
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 || d%time.Second != 0 || d > 30*24*time.Hour {
				return base, fmt.Errorf("%s must be a whole-second duration such as 90s, 10m or 6h", key)
			}
			seconds := int(d / time.Second)
			switch key {
			case "idle":
				base.IdleSeconds = seconds
			case "cooldown":
				base.CooldownSeconds = seconds
			default:
				base.TimeoutSeconds = seconds
			}
		case "runs", "memories", "bytes", "changes":
			n, err := strconv.Atoi(value)
			if err != nil {
				return base, fmt.Errorf("%s must be a whole number", key)
			}
			switch key {
			case "runs":
				base.MaxRunsPerDay = n
			case "memories":
				base.Limit = n
			case "bytes":
				base.ByteBudget = n
			default:
				base.MaxChanges = n
			}
		default:
			return base, fmt.Errorf("unknown limit %q", key)
		}
	}
	return base, nil
}

func (d *menuBarDreams) setEnabled(c menuControlContext, enabled bool) {
	v, ok := d.configured(c)
	if !ok || v.status.Config.Enabled == enabled {
		c.Refresh()
		return
	}
	settings := *v.status.Config
	settings.Runner = append([]string(nil), settings.Runner...)
	settings.Enabled = enabled
	if enabled {
		a, err := c.Prompt(menubar.Dialog{Title: "Resume Dreaming?", Message: dreamConsentText(c.Service, settings) + "\n\nAutomatic reviews resume after the next completed agent task and the quiet period.", Buttons: []menubar.Button{{ID: "resume", Title: "Resume"}, {ID: "cancel", Title: "Cancel"}}, Default: "cancel", Cancel: "cancel"})
		if err != nil || a.Choice != "resume" {
			return
		}
	}
	_, err := c.Service.ConfigureDream(settings)
	c.Refresh()
	switch {
	case err != nil:
		c.Inform("Dreaming", err.Error())
	case !enabled && v.status.Reviewing:
		c.Inform("Dreaming Paused", "The schedule is paused. The running review stops and saves no further replacements.")
	}
}

// requestNow asks for consent and files the Dream Now request. It returns the
// request ID, or "" when nothing was requested.
func (d *menuBarDreams) requestNow(c menuControlContext) string {
	v, ok := d.configured(c)
	if !ok {
		return ""
	}
	if err := v.status.DreamNowRefusal(); err != nil {
		c.Inform("Dreaming", err.Error())
		return ""
	}
	settings := *v.status.Config
	message := dreamConsentText(c.Service, settings) + fmt.Sprintf("\n\nThis review starts now, without waiting for the quiet period or cooldown. It stops if foreground agent work starts, and it counts toward today's limit (%d of %d used).", v.status.State.Attempts, settings.MaxRunsPerDay)
	a, err := c.Prompt(menubar.Dialog{Title: "Review Memories Now?", Message: message, Buttons: []menubar.Button{{ID: "now", Title: "Review Now"}, {ID: "cancel", Title: "Cancel"}}, Default: "cancel", Cancel: "cancel"})
	if err != nil || a.Choice != "now" {
		return ""
	}
	id, err := c.Service.RequestDreamNow()
	c.Refresh()
	if err != nil {
		c.Inform("Dreaming", err.Error())
		return ""
	}
	return id
}

// waitForRun follows one Dream Now request until it finishes, is withdrawn or
// expires, then reports the recorded outcome. Leaving the menu stops waiting,
// not the review.
func (d *menuBarDreams) waitForRun(c menuControlContext, id string) {
	ticker := time.NewTicker(dreamPollInterval)
	defer ticker.Stop()
	started, failures := false, 0
	for {
		select {
		case <-c.Context.Done():
			return
		case <-ticker.C:
		}
		st, _, err := c.Service.DreamStatus()
		if err != nil {
			if failures++; failures >= 20 {
				c.Inform("Dreaming", "Could not read the review status: "+err.Error())
				return
			}
			continue
		}
		failures = 0
		state := st.State
		switch {
		case state.RunRequest == id && state.LastResult == "running" && st.Reviewing:
			if !started {
				started = true
				c.Refresh()
			}
		case state.RunRequest == id && state.LastResult == "running":
			c.Refresh()
			c.Inform("Review Interrupted", "The dream worker stopped before it recorded a result. Nothing more will be saved from this review.")
			return
		case state.RunRequest == id:
			c.Refresh()
			c.Inform("Review Finished", dreamResultDetails(st))
			return
		case state.ManualRequest == id && state.ManualRequestUntil.After(time.Now()):
		case started:
			c.Refresh()
			c.Inform("Review Finished", "The review finished and a newer review has started. Use Show Last Result for the latest outcome.")
			return
		case state.ManualRequest == id:
			c.Refresh()
			c.Inform("Review Did Not Start", "The dream worker did not start the review within a minute. Nothing was sent to the reviewer.")
			return
		default:
			c.Refresh()
			c.Inform("Review Cancelled", "The review was cancelled before it started. Nothing was sent to the reviewer.")
			return
		}
	}
}

func (d *menuBarDreams) cancel(c menuControlContext) {
	reviewing := loadDreamView(c.Service).status.Reviewing
	cancelled, err := c.Service.CancelDreamReview()
	c.Refresh()
	switch {
	case err != nil:
		c.Inform("Dreaming", err.Error())
	case !cancelled:
		c.Inform("Dreaming", "No review is running or waiting to start.")
	case reviewing:
		c.Inform("Cancelling Review", "The reviewer is being stopped. No further replacements will be saved; replacements already saved stay saved. The schedule stays as it was.")
	}
	// A withdrawn request is reported by the Dream Now action that waits for it.
}

func (d *menuBarDreams) showLast(c menuControlContext) {
	v, ok := d.configured(c)
	if !ok {
		return
	}
	if v.status.State.LastResult == "" {
		c.Inform("Last Dreaming Result", "No review has run yet.")
		return
	}
	c.Inform("Last Dreaming Result", dreamResultDetails(v.status))
}

func dreamResultText(state memory.IdleDreamState) string {
	saved := len(state.LastSaved)
	switch state.LastResult {
	case "running":
		return "review running"
	case "saved":
		return fmt.Sprintf("saved %d %s", saved, dreamPlural(saved, "replacement", "replacements"))
	case "no_change":
		return "reviewed, no change justified"
	case "empty":
		return "nothing to review"
	case "skipped":
		return "only oversized memories left"
	case "cancelled":
		return "stopped by foreground work, a settings change or the time limit"
	case "cancelled_by_user":
		if saved > 0 {
			return fmt.Sprintf("cancelled by you after saving %d", saved)
		}
		return "cancelled by you"
	case "runner_failed":
		return "reviewer failed to run"
	case "prepare_failed":
		return "batch preparation failed"
	case "invalid_output":
		return "reviewer output failed validation"
	case "eval_failed":
		return "retrieval probe check failed"
	case "commit_rejected":
		return "a save failed a current revision, policy or activity check"
	case "configured":
		return "settings changed"
	}
	return cleanMenuText(state.LastResult, 40)
}

var dreamResultHelp = map[string]string{
	"runner_failed":  "Check that the reviewer exists and its sign-in works. Reviewer diagnostics are not kept.",
	"invalid_output": "Check the reviewer's JSON output contract.",
	"prepare_failed": "Check that the memory store is available.",
	"skipped":        "Raise the bytes limit in Configure to review the oversized memories.",
	"eval_failed":    "Nothing from the review was saved.",
}

func dreamResultDetails(st memory.IdleDreamStatus) string {
	state := st.State
	lines := []string{"Result: " + dreamResultText(state)}
	if state.LastResult == "running" && !st.Reviewing {
		lines[0] = "Result: review interrupted before it recorded a result"
	}
	if state.RunRequest != "" {
		lines = append(lines, "Started by: Dream Now")
	} else if state.RunID != "" {
		lines = append(lines, "Started by: schedule")
	}
	now := time.Now()
	if !state.LastAttempt.IsZero() {
		lines = append(lines, "Started: "+dreamTime(state.LastAttempt, now))
	}
	if !state.LastFinished.IsZero() && state.LastResult != "running" {
		lines = append(lines, "Finished: "+dreamTime(state.LastFinished, now))
	}
	if n := len(state.LastSaved); n > 0 {
		ids := state.LastSaved
		if n > 5 {
			ids = ids[:5]
		}
		lines = append(lines, fmt.Sprintf("Saved %s: %s", dreamPlural(n, "replacement", "replacements"), strings.Join(ids, ", ")))
	}
	if n := len(state.LastSkipped); n > 0 {
		needed := 0
		for _, s := range state.LastSkipped {
			needed = max(needed, s.RequiredBytes)
		}
		lines = append(lines, fmt.Sprintf("Skipped: %d oversized %s (largest needs %d bytes)", n, dreamPlural(n, "memory", "memories"), needed))
	}
	if help := dreamResultHelp[state.LastResult]; help != "" {
		lines = append(lines, "", help)
	}
	return strings.Join(lines, "\n")
}

func dreamNextText(st memory.IdleDreamStatus, now time.Time) string {
	if st.Reviewing || !st.Config.Enabled {
		return ""
	}
	at := func(why string) string {
		if st.NextEligibleAt == nil {
			return why
		}
		return "after " + dreamTime(*st.NextEligibleAt, now) + " (" + why + ")"
	}
	switch st.WaitReason {
	case "ready":
		if st.Running {
			return "eligible now"
		}
		return "eligible; starts at the next agent hook event"
	case "active_sessions":
		n := len(st.State.Active)
		return fmt.Sprintf("after foreground work stops (%d active %s)", n, dreamPlural(n, "session", "sessions"))
	case "no_pending_activity":
		return "after the next completed agent task"
	case "idle_delay":
		return at("quiet period")
	case "cooldown":
		return at("cooldown")
	case "daily_limit":
		return at("daily limit")
	}
	return ""
}

func dreamLastTime(state memory.IdleDreamState) time.Time {
	if state.LastResult != "running" && !state.LastFinished.IsZero() {
		return state.LastFinished
	}
	return state.LastAttempt
}

func dreamTime(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
}

// dreamConsentText names everything a review uses: scope and store, reviewer,
// provider usage and limits.
func dreamConsentText(s memory.Service, settings memory.IdleDreamConfig) string {
	usage := "Elephant cannot see what this command calls. Any provider usage is charged to the account the command uses."
	if settings.Adapter == "codex" {
		usage = "Each review is a model request on your Codex sign-in and counts against that account's allowance."
	}
	return dreamScopeText(s) + "\n" +
		"Reviewer: " + dreamReviewerText(settings) + "\n" +
		"Provider usage: " + usage + "\n" +
		fmt.Sprintf("Limits: each review reads at most %d %s and %d bytes of memory text, saves at most %d %s and stops after %s. At most %d %s per UTC day.\n", settings.Limit, dreamPlural(settings.Limit, "memory", "memories"), settings.ByteBudget, settings.MaxChanges, dreamPlural(settings.MaxChanges, "replacement", "replacements"), dreamDuration(settings.TimeoutSeconds), settings.MaxRunsPerDay, dreamPlural(settings.MaxRunsPerDay, "review", "reviews")) +
		fmt.Sprintf("Schedule: automatic reviews start %s after all agent sessions stop, at least %s apart.", dreamDuration(settings.IdleSeconds), dreamDuration(settings.CooldownSeconds))
}

func dreamScopeText(s memory.Service) string {
	scope := "Scope: your personal memories"
	switch {
	case s.Conversation != "":
		scope += " and the private memories of conversation " + cleanMenuText(s.Conversation, 80)
	case !s.Unattached && s.Project != "":
		scope += " and the private memories of project " + dreamDisplayPath(s.Project)
	}
	return scope + " (user " + cleanMenuText(s.Identity.User, 80) + "). Team memories and other users are excluded.\nStore: " + dreamDisplayPath(s.Store.Path)
}

func dreamReviewerText(s memory.IdleDreamConfig) string {
	if len(s.Runner) == 0 {
		return "none"
	}
	if s.Adapter == "codex" {
		return "Codex CLI at " + dreamDisplayPath(s.Runner[0]) + ", read-only sandbox without MCP servers or hooks"
	}
	text := "custom command " + dreamDisplayPath(s.Runner[0])
	if n := len(s.Runner) - 1; n > 0 {
		text += fmt.Sprintf(" with %d %s", n, dreamPlural(n, "argument", "arguments"))
	}
	return text
}

// dreamDisplayPath shortens the home directory for display only.
func dreamDisplayPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
			return "~/" + rest
		}
	}
	return p
}

// dreamDuration formats whole seconds compactly: 90s, 10m, 1h30m, 6h.
func dreamDuration(seconds int) string {
	h, m, s := seconds/3600, seconds/60%60, seconds%60
	out := ""
	if h > 0 {
		out += strconv.Itoa(h) + "h"
	}
	if m > 0 {
		out += strconv.Itoa(m) + "m"
	}
	if s > 0 || out == "" {
		out += strconv.Itoa(s) + "s"
	}
	return out
}

func dreamPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
