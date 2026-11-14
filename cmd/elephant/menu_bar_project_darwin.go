//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

func menuBarProjectAutomation(svc memory.Service) (memory.AutomationConfig, bool, error) {
	if svc.Unattached {
		return memory.AutomationConfig{}, false, nil
	}
	path := filepath.Join(svc.Root, ".elephant", "automation.json")
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return memory.AutomationConfig{}, false, nil
		}
		return memory.AutomationConfig{}, false, err
	}
	config, err := memory.ReadAutomation(path)
	if err != nil {
		return config, true, err
	}
	if config.Root != svc.Root || config.Store != svc.Store.Path || config.Identity != svc.Identity || config.Project != svc.Project || config.Conversation != svc.Conversation || config.Unattached != svc.Unattached {
		return config, true, errMenuBarContextMismatch
	}
	return config, true, nil
}

func (c *menuBarControls) projectItems(view menuBarView) []menubar.Item {
	if view.Service.Unattached {
		return []menubar.Item{menuBarRow("No project attached")}
	}
	items := []menubar.Item{menuBarAction("project/reveal", "Reveal Project in Finder"), menuBarSeparator()}
	config, found, err := menuBarProjectAutomation(view.Service)
	if err != nil {
		return append(items, menuBarRow("Automatic hook configuration needs attention"), menuBarAction("health/check", "Inspect Configuration…"))
	}
	if !found {
		return append(items, menuBarRow("Automatic hooks not installed"), menuBarAction("project/setup", "Set Up Automatic Hooks…"))
	}
	state, toggle, action := "Automatic hooks: enabled", "Pause Automatic Hooks", "project/pause"
	if !config.Enabled {
		state, toggle, action = "Automatic hooks: paused", "Resume Automatic Hooks", "project/resume"
	}
	return append(items, menuBarRow(state), menuBarAction(action, toggle), menuBarAction("project/setup", "Repair Installed Hooks…"))
}

func (c *menuBarControls) handleProject(view menuBarView, action string) bool {
	if !strings.HasPrefix(action, "project/") {
		return false
	}
	scope := c.scope(view)
	if view.Service.Unattached {
		scope.Inform("No project attached", "This connection intentionally has no project. Project controls do not infer one from its working directory.")
		return true
	}
	switch action {
	case "project/reveal":
		if err := exec.CommandContext(scope.Context, "/usr/bin/open", "-R", view.Service.Root).Run(); err != nil && scope.Context.Err() == nil {
			scope.Inform("Project could not be revealed", err.Error())
		}
	case "project/pause", "project/resume":
		_, found, err := menuBarProjectAutomation(view.Service)
		if err == nil && !found {
			err = errors.New("automatic hooks are not installed for this project")
		}
		if err != nil {
			scope.Inform("Automatic hooks were not changed", err.Error())
			return true
		}
		enabled := action == "project/resume"
		if _, err := view.Service.SetAutomaticHooks(enabled); err != nil {
			scope.Inform("Automatic hooks were not changed", err.Error())
			return true
		}
		c.invalidateHealth(view.Service)
		c.requestRefresh()
		state := "paused"
		if enabled {
			state = "resumed"
		}
		scope.Inform("Automatic hooks "+state, "Project: "+view.projectName()+"\n\nOnly automatic hooks were "+state+". MCP stays connected and explicit memory tool calls still work. Existing agent approval requirements remain in effect.")
	case "project/setup":
		c.setupProjectHooks(view)
	default:
		return false
	}
	return true
}

func (c *menuBarControls) invalidateHealth(svc memory.Service) {
	c.mu.Lock()
	delete(c.health, healthKey(svc))
	c.mu.Unlock()
}

func (c *menuBarControls) setupProjectHooks(view menuBarView) {
	scope := c.scope(view)
	config, found, err := menuBarProjectAutomation(view.Service)
	if err != nil {
		scope.Inform("Automatic hooks were not changed", err.Error())
		return
	}
	dialog := menubar.Dialog{Title: "Set Up Automatic Hooks", Message: fmt.Sprintf("Project: %s\nStore: %s\n\nThis writes project-local hook settings and managed guidance. Unrelated settings are preserved. You must still restart the agent and approve its hooks normally. MCP permissions do not change.\n\nChoose the agent whose hooks you want to install.", view.Service.Root, view.Service.Store.Path), Buttons: []menubar.Button{{ID: "codex", Title: "Codex"}, {ID: "claude", Title: "Claude Code"}, {ID: "both", Title: "Both"}, {ID: "cancel", Title: "Cancel"}}, Default: "cancel", Cancel: "cancel"}
	agent, budget := "", 4000
	if found {
		if len(config.Agents) == 1 {
			agent = config.Agents[0]
		} else if len(config.Agents) == 2 {
			agent = "both"
		} else {
			scope.Inform("Installed hooks need inspection", "The installed adapter list is not recognized. Run Health Check before changing it.")
			return
		}
		budget = config.Budget
		dialog.Title = "Repair Installed Hooks"
		dialog.Message = fmt.Sprintf("Project: %s\n\nRepair the existing %s adapter(s), preserving the configured memory context, budget, and unrelated settings? Host approval remains your responsibility.", view.Service.Root, strings.Join(config.Agents, ", "))
		dialog.Buttons = []menubar.Button{{ID: "repair", Title: "Repair"}, {ID: "cancel", Title: "Cancel"}}
	}
	answer, err := scope.Prompt(dialog)
	if err != nil || answer.Choice == "cancel" || answer.Choice == "" {
		return
	}
	if !found {
		switch answer.Choice {
		case "codex", "claude", "both":
			agent = answer.Choice
		default:
			return
		}
	} else if answer.Choice != "repair" {
		return
	}
	// Recheck after the dialog: another client may have installed a different
	// context while the user was choosing, and must not be overwritten.
	if _, _, err := menuBarProjectAutomation(view.Service); err != nil {
		scope.Inform("Automatic hooks were not changed", err.Error())
		return
	}
	result, err := memory.InitAutomation(view.Service, view.Session.Executable, agent, budget)
	if err != nil {
		scope.Inform("Automatic hooks were not installed", err.Error())
		return
	}
	c.invalidateHealth(view.Service)
	c.requestRefresh()
	scope.Inform("Automatic hooks configured", fmt.Sprintf("Configured %s.\n\nRestart the selected agent and approve its hooks through the normal host flow. Local installation does not prove host approval or successful learning.", strings.Join(result.Agents, ", ")))
}
