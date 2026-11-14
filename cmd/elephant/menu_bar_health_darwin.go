//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

type menuBarHealthKey struct {
	Root, Store, Project, Conversation string
	Identity                           memory.Identity
	Unattached                         bool
}

type menuBarHealth struct {
	Checked  time.Time
	Checking bool
	Report   memory.Diagnostics
}

func healthKey(s memory.Service) menuBarHealthKey {
	return menuBarHealthKey{Root: s.Root, Store: s.Store.Path, Project: s.Project, Conversation: s.Conversation, Identity: s.Identity, Unattached: s.Unattached}
}

func (c *menuBarControls) healthItems(view menuBarView) ([]menubar.Item, bool) {
	c.mu.Lock()
	health := c.health[healthKey(view.Service)]
	c.mu.Unlock()
	state := "Health not checked"
	attention := false
	if health.Checking {
		state = "Checking local health…"
	} else if !health.Checked.IsZero() {
		state = "Healthy · checked " + health.Checked.Local().Format("15:04")
		if !health.Report.OK {
			state, attention = "Needs attention · checked "+health.Checked.Local().Format("15:04"), true
		}
	}
	check := menuBarAction("health/check", "Run Health Check…")
	check.Enabled = !health.Checking
	show := menuBarAction("health/show", "Show Last Check…")
	show.Enabled = !health.Checked.IsZero()
	return []menubar.Item{menuBarRow(state), check, show, menuBarSeparator(), menuBarAction("health/copy", "Copy Redacted Diagnostics")}, attention
}

func (c *menuBarControls) handleHealth(view menuBarView, action string) bool {
	scope := c.scope(view)
	switch action {
	case "health/check":
		key := healthKey(view.Service)
		c.mu.Lock()
		previous := c.health[key]
		if previous.Checking {
			c.mu.Unlock()
			return true
		}
		previous.Checking = true
		c.health[key] = previous
		c.mu.Unlock()
		c.requestRefresh()
		report := runMenuBarHealth(view.Service)
		c.mu.Lock()
		if scope.Context.Err() == nil {
			c.health[key] = menuBarHealth{Checked: time.Now().UTC(), Report: report}
		} else {
			previous.Checking = false
			c.health[key] = previous
		}
		c.mu.Unlock()
		c.requestRefresh()
		if scope.Context.Err() == nil {
			c.showHealth(view)
		}
	case "health/show":
		c.showHealth(view)
	case "health/copy":
		text, err := c.redactedDiagnostics(view)
		if err == nil {
			command := exec.CommandContext(scope.Context, "/usr/bin/pbcopy")
			command.Stdin = strings.NewReader(text)
			err = command.Run()
		}
		if err != nil {
			if scope.Context.Err() == nil {
				scope.Inform("Diagnostics were not copied", err.Error())
			}
		} else {
			scope.Inform("Diagnostics copied", "Copied engine/platform, connection metadata, hook/reviewer configuration status, and health check results. Full paths, identities, names, tool activity, memory text, and credentials were excluded.")
		}
	default:
		return false
	}
	return true
}

func (c *menuBarControls) showHealth(view menuBarView) {
	c.mu.Lock()
	health := c.health[healthKey(view.Service)]
	c.mu.Unlock()
	if health.Checked.IsZero() {
		c.scope(view).Inform("Health not checked", "Choose Run Health Check to inspect the local store, context, MCP schema, and installed hooks. Host approval must still be checked in the agent.")
		return
	}
	var message strings.Builder
	fmt.Fprintf(&message, "Instance: %s\nChecked: %s\nRoot: %s\nStore: %s\n\n", view.shortID(), health.Checked.Local().Format(time.RFC3339), view.Service.Root, view.Service.Store.Path)
	for _, check := range health.Report.Checks {
		status := "OK"
		if !check.OK {
			status = "FAILED"
		}
		fmt.Fprintf(&message, "%s — %s\n%s\n\n", check.Name, status, cleanMenuText(check.Detail, 500))
	}
	for _, note := range health.Report.Notes {
		fmt.Fprintf(&message, "%s\n", cleanMenuText(note, 500))
	}
	c.scope(view).Inform("Elephant Health · "+view.shortID(), message.String())
}

// Clipboard diagnostics are constructed from an allowlist, not redacted by
// searching raw doctor output. Client names and version strings are untrusted.
func (c *menuBarControls) redactedDiagnostics(view menuBarView) (string, error) {
	type check struct {
		Name string `json:"name"`
		OK   bool   `json:"ok"`
	}
	c.mu.Lock()
	health := c.health[healthKey(view.Service)]
	c.mu.Unlock()
	checks := make([]check, 0, len(health.Report.Checks))
	for _, item := range health.Report.Checks {
		name := "additional_check"
		switch item.Name {
		case "language", "context", "sqlite_wal", "database_integrity", "mcp_schema", "automation_config", "automation_binary", "automation_hooks", "codex_hooks", "claude_hooks", "dream_reviewer":
			name = item.Name
		}
		checks = append(checks, check{Name: name, OK: item.OK})
	}
	hooks, reviewer, schedule := "not_installed", "not_configured", "not_configured"
	config, found, err := menuBarProjectAutomation(view.Service)
	if err != nil {
		hooks = "configuration_unavailable_or_different_context"
	} else if found {
		hooks = "paused"
		if config.Enabled {
			hooks = "configured_host_approval_unverified"
		}
		if config.IdleDream != nil {
			reviewer, schedule = "configured", "paused"
			if config.IdleDream.Enabled {
				schedule = "enabled"
			}
		}
	}
	payload := struct {
		Schema           int       `json:"schema"`
		CompanionVersion string    `json:"companion_version"`
		SameEngine       bool      `json:"instance_matches_companion_version"`
		OS               string    `json:"os"`
		Architecture     string    `json:"architecture"`
		InstanceID       string    `json:"instance_id"`
		PID              int       `json:"pid"`
		Kind             string    `json:"connection_kind"`
		Client           string    `json:"client_category"`
		Connected        time.Time `json:"connected_at"`
		Checked          time.Time `json:"health_checked_at,omitempty"`
		Checks           []check   `json:"checks"`
		Hooks            string    `json:"automatic_hooks"`
		Reviewer         string    `json:"reviewer"`
		Schedule         string    `json:"dream_schedule"`
	}{1, memory.Version, view.Session.EngineVersion == memory.Version, runtime.GOOS, runtime.GOARCH, view.shortID(), view.Session.PID, view.Session.Kind, menuBarClientCategory(view), view.Connected, health.Checked, checks, hooks, reviewer, schedule}
	data, err := json.MarshalIndent(payload, "", "  ")
	return string(data), err
}

func menuBarClientCategory(view menuBarView) string {
	if view.Session.Kind == "palace" {
		return "memory-palace"
	}
	name := strings.ToLower(view.Observed.Client.Name)
	switch {
	case name == "omp" || strings.HasPrefix(name, "omp-"):
		return "omp"
	case name == "pi" || strings.HasPrefix(name, "pi-"):
		return "pi"
	case strings.Contains(name, "claude"):
		return "claude-code"
	case strings.Contains(name, "codex"):
		return "codex"
	default:
		return "other-mcp-client"
	}
}

func runMenuBarHealth(svc memory.Service) memory.Diagnostics {
	report, err := memory.Doctor(svc)
	if err != nil {
		report.OK = false
	}
	config, found, configErr := menuBarProjectAutomation(svc)
	if configErr != nil || !found || config.IdleDream == nil {
		return report
	}
	check := memory.Check{Name: "dream_reviewer", OK: true, Detail: "Configured reviewer executable is available. Provider authentication was not checked."}
	if len(config.IdleDream.Runner) == 0 {
		check.OK = false
	} else if _, err := exec.LookPath(config.IdleDream.Runner[0]); err != nil {
		check.OK = false
	}
	if !check.OK {
		check.Detail = "Configured reviewer executable is unavailable. Use Dreaming → Configure to select an installed reviewer. No provider call was made."
		report.OK = false
	}
	report.Checks = append(report.Checks, check)
	return report
}
