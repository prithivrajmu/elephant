package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// Experience contains observation metadata. Tool text, prompts and transcripts
// are deliberately absent. A completed tool call is not proof of a useful lesson.
type Experience struct {
	ID       string    `json:"id"`
	Identity Identity  `json:"identity"`
	Project  string    `json:"project,omitempty"`
	Agent    string    `json:"agent"`
	Session  string    `json:"session"`
	Event    string    `json:"event"`
	Tool     string    `json:"tool,omitempty"`
	Status   string    `json:"status"`
	ExitCode *int      `json:"exit_code,omitempty"`
	At       time.Time `json:"at"`
	Task     string    `json:"task,omitempty"`
	MemoryID string    `json:"memory_id,omitempty"`
}

type HookInput struct {
	Event      string          `json:"hook_event_name"`
	Session    string          `json:"session_id"`
	Turn       string          `json:"turn_id"`
	CWD        string          `json:"cwd"`
	Prompt     string          `json:"prompt"`
	Tool       string          `json:"tool_name"`
	ToolID     string          `json:"tool_use_id"`
	Response   json.RawMessage `json:"tool_response"`
	StopActive bool            `json:"stop_hook_active"`
	AgentID    string          `json:"agent_id"`
	Background []any           `json:"background_tasks"`
}
type hookState struct {
	Reviewed  bool     `json:"reviewed"`
	Completed bool     `json:"completed,omitempty"`
	Task      string   `json:"task,omitempty"`
	Context   string   `json:"context,omitempty"`
	Agent     string   `json:"agent,omitempty"`
	Recalled  []string `json:"recalled,omitempty"`
	// Signal marks a task with a failed tool call or a file edit; only those get a review.
	Signal bool `json:"signal,omitempty"`
}

// editTools change files. Claude names first, then Codex.
var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true, "apply_patch": true}

func (s Store) observe(x Experience) error {
	return s.transactSQL(false, []string{"experience:" + x.ID}, func(_ *sql.Tx, _ []Memory, seen map[string]bool) (*Event, error) {
		if seen["experience:"+x.ID] {
			return nil, nil
		}
		return &Event{Kind: "experience", Experience: &x}, nil
	})
}
func validLabel(s string) bool {
	if len(s) > 256 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// RunHook implements the common command-hook contract. It neither reads host
// transcripts nor invokes another model. The host agent performs semantic review.
func RunHook(configPath, agent string, input io.Reader) (map[string]any, error) {
	out := map[string]any{}
	c, err := ReadAutomation(configPath)
	if err != nil {
		return out, err
	}
	if !c.Enabled {
		return out, nil
	}
	if !containsString(c.Agents, agent) {
		return out, fmt.Errorf("adapter not installed")
	}
	b, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil {
		return out, err
	}
	if len(b) > 1<<20 {
		return out, fmt.Errorf("hook input exceeds 1 MiB")
	}
	var h HookInput
	if err = json.Unmarshal(b, &h); err != nil {
		return out, fmt.Errorf("invalid hook input: %w", err)
	}
	if h.AgentID != "" {
		return out, nil
	} // Main session only; avoid subagent review loops.
	switch h.Event {
	case "SessionStart", "UserPromptSubmit", "PostToolUse", "PostToolUseFailure", "Stop":
	default:
		return out, nil
	}
	if h.Session == "" || !validLabel(h.Session) || !validLabel(h.Tool) || !validLabel(h.ToolID) {
		return out, fmt.Errorf("invalid hook identifiers")
	}
	if h.CWD == "" {
		return out, fmt.Errorf("hook cwd required")
	}
	cwd, err := filepath.EvalSymlinks(h.CWD)
	if err != nil {
		return out, err
	}
	rel, err := filepath.Rel(c.Root, cwd)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return out, fmt.Errorf("hook cwd is outside the configured project")
	}
	if h.Event == "UserPromptSubmit" && strings.HasPrefix(strings.TrimSpace(h.Prompt), reviewPrefix) {
		return out, nil
	}
	svc := c.Service()
	sessionKeyBytes, _ := json.Marshal([]string{c.Identity.Tenant, c.Identity.User, c.Project, agent, h.Session})
	sessionKey := digest(string(sessionKeyBytes))
	x := Experience{ID: newID(), Identity: c.Identity, Project: c.Project, Agent: agent, Session: sessionKey, Event: h.Event, Status: "received", At: time.Now().UTC()}
	statePath := c.Store + ".sessions/" + sessionKey + ".json"
	switch h.Event {
	case "PostToolUse", "PostToolUseFailure":
		x.Tool = h.Tool
		x.Status = "completed"
		if h.ToolID != "" {
			x.ID = digest(sessionKey + "\x00" + h.Event + "\x00" + h.ToolID)
		}
		if h.Event == "PostToolUseFailure" {
			x.Status = "failed"
		}
		// Only explicit structured exit codes are interpreted. Never parse prose or
		// assume that a returned tool response proves the engineering task succeeded.
		var response struct {
			ExitCode  *int `json:"exit_code"`
			CamelCode *int `json:"exitCode"`
		}
		if json.Unmarshal(h.Response, &response) == nil {
			x.ExitCode = response.ExitCode
			if x.ExitCode == nil {
				x.ExitCode = response.CamelCode
			}
			if x.ExitCode != nil && *x.ExitCode != 0 {
				x.Status = "failed"
			}
		}
		if x.Status == "failed" || editTools[h.Tool] {
			err = svc.Store.transactState(func(_ []Memory, _ map[string]bool) (*Event, error) {
				var state hookState
				b, e := readConfigFile(statePath)
				if e != nil {
					return nil, e
				}
				if len(b) > 0 {
					if e = json.Unmarshal(b, &state); e != nil {
						return nil, e
					}
				}
				if state.Signal {
					return nil, nil
				}
				state.Signal = true
				if e = safeParents(filepath.Dir(c.Store), statePath); e != nil {
					return nil, e
				}
				b, _ = json.Marshal(state)
				return nil, atomicLocalFile(statePath, b, 0600)
			})
			if err != nil {
				return out, err
			}
		}
		return out, svc.Store.observe(x)
	case "Stop":
		// ponytail: a long-lived background task (e.g. a dev server) defers the review until the next task resets state.
		if h.StopActive || len(h.Background) > 0 {
			return out, nil
		}
		review := false
		err = svc.Store.transactState(func(_ []Memory, _ map[string]bool) (*Event, error) {
			var state hookState
			b, e := readConfigFile(statePath)
			if e != nil {
				return nil, e
			}
			if len(b) > 0 {
				if e = json.Unmarshal(b, &state); e != nil {
					return nil, e
				}
			}
			if state.Reviewed || !state.Signal {
				return nil, nil
			}
			state.Reviewed = true
			if state.Task == "" {
				state.Task = newID()
				state.Context = taskContext(c)
				state.Agent = agent
			}
			x.Task = state.Task
			if e = safeParents(filepath.Dir(c.Store), statePath); e != nil {
				return nil, e
			}
			b, _ = json.Marshal(state)
			if e = atomicLocalFile(statePath, b, 0600); e != nil {
				return nil, e
			}
			review = true
			x.Status = "review_requested"
			return &Event{Kind: "experience", Experience: &x}, nil
		})
		if err != nil || !review {
			return out, err
		}
		scope := "project"
		if c.Unattached {
			scope = "personal"
		}
		record := shellQuote(c.Binary) + " remember --config " + shellQuote(configPath) + " --task-session " + shellQuote(sessionKey) + " --task-id " + shellQuote(x.Task)
		out["decision"] = "block"
		reason := reviewPrefix + " Save 0-3 lessons from this task: a tested approach, a verified fix, or a confirmed convention. Use only observed evidence; no secrets, no transcripts. A tool exit code alone is not a lesson. For each lesson, pipe JSON on stdin to:\n" + record +
			"\nFields: incident, lesson, source (real test, review or tool evidence), class (win, lesson, warning or scar), scope (" + scope + " by default; personal only if transferable, never team), features (signal name to string array), optional requires/excludes. Then run this and put its one-line output once in your final response:\n" + c.taskStatusCommand(sessionKey, x.Task)
		out["reason"] = reason
		return out, nil
	case "UserPromptSubmit":
		err = svc.Store.transactState(func(_ []Memory, _ map[string]bool) (*Event, error) {
			if e := safeParents(filepath.Dir(c.Store), statePath); e != nil {
				return nil, e
			}
			x.Task = newID()
			return nil, writeTaskState(c, sessionKey, hookState{Task: x.Task, Context: taskContext(c), Agent: agent})
		})
		if err != nil {
			return out, err
		}
	}
	if err = svc.Store.observe(x); err != nil {
		return out, err
	}
	profileStart := time.Now()
	p, err := svc.Profile()
	if err != nil {
		return out, err
	}
	result, err := svc.Store.Recall(c.Identity, Request{Profile: p, Task: h.Prompt, Initialize: h.Event == "SessionStart", ByteBudget: c.Budget, Limit: 6, profileMS: elapsedMS(profileStart), manifestFiles: profileManifestCount(p)})
	if err != nil {
		return out, err
	}
	if result.Context != "" {
		out["hookSpecificOutput"] = map[string]any{"hookEventName": h.Event, "additionalContext": result.Context}
	}
	if h.Event == "UserPromptSubmit" {
		err = svc.Store.transactState(func(_ []Memory, _ map[string]bool) (*Event, error) {
			state, e := readTaskState(c, sessionKey)
			if e != nil {
				return nil, e
			}
			if state.Task != x.Task {
				return nil, nil
			}
			state.Recalled = []string{}
			for _, hit := range result.Hits {
				state.Recalled = append(state.Recalled, hit.ID)
			}
			return nil, writeTaskState(c, sessionKey, state)
		})
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// ConfigForRoot returns the nearest installed project without crossing its root.
func ConfigForRoot(root string) (AutomationConfig, bool, error) {
	for {
		p := automationPath(root)
		if _, err := os.Lstat(p); err == nil {
			c, e := ReadAutomation(p)
			return c, true, e
		} else if !os.IsNotExist(err) {
			return AutomationConfig{}, false, err
		}
		parent := filepath.Dir(root)
		if parent == root {
			break
		}
		root = parent
	}
	return AutomationConfig{}, false, nil
}
