package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

// TaskCapture is trusted CLI context, never supplied through Memory JSON or MCP.
type TaskCapture struct{ Session, Task, Agent string }
type TaskStatus struct {
	Recalled  int      `json:"recalled"`
	Saved     int      `json:"saved"`
	MemoryIDs []string `json:"memory_ids"`
	State     string   `json:"state"`
	Line      string   `json:"line"`
}

var sessionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s Store) ObserveCaptureFailure(id Identity, project string, t *TaskCapture) error {
	return s.observe(Experience{ID: newID(), Identity: id, Project: project, Agent: t.Agent, Session: t.Session, Task: t.Task, Event: "LessonReview", Status: "capture_failed", At: time.Now().UTC()})
}
func taskContext(c AutomationConfig) string {
	b, _ := json.Marshal([]string{c.Root, c.Project, c.Conversation, c.Identity.Tenant, c.Identity.User, c.Identity.Team})
	return digest(string(b))
}
func taskStatePath(c AutomationConfig, session string) (string, error) {
	if !sessionPattern.MatchString(session) {
		return "", fmt.Errorf("invalid task session")
	}
	p := c.Store + ".sessions/" + session + ".json"
	if err := safeParents(filepath.Dir(c.Store), p); err != nil {
		return "", err
	}
	return p, nil
}
func readTaskState(c AutomationConfig, session string) (hookState, error) {
	var state hookState
	p, err := taskStatePath(c, session)
	if err != nil {
		return state, err
	}
	b, err := readConfigFile(p)
	if err != nil {
		return state, err
	}
	if len(b) == 0 {
		return state, fmt.Errorf("no active task")
	}
	if err = json.Unmarshal(b, &state); err != nil {
		return state, err
	}
	if state.Context != taskContext(c) || state.Task == "" {
		return state, fmt.Errorf("task context does not match installed project")
	}
	return state, nil
}
func writeTaskState(c AutomationConfig, session string, state hookState) error {
	p, err := taskStatePath(c, session)
	if err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return atomicLocalFile(p, b, 0600)
}
func CaptureForTask(configPath, session string, taskID ...string) (AutomationConfig, *TaskCapture, error) {
	c, err := ReadAutomation(configPath)
	if err != nil {
		return c, nil, err
	}
	var capture *TaskCapture
	err = c.Service().Store.transactState(func(_ []Memory, _ map[string]bool) (*Event, error) {
		state, e := readTaskState(c, session)
		if e != nil {
			return nil, e
		}
		if !c.Enabled {
			return nil, fmt.Errorf("automatic memory is paused")
		}
		if len(taskID) > 0 && taskID[0] != "" && state.Task != taskID[0] {
			return nil, fmt.Errorf("task changed before capture")
		}
		capture = &TaskCapture{Session: session, Task: state.Task, Agent: state.Agent}
		return nil, nil
	})
	return c, capture, err
}
func (c AutomationConfig) taskStatusCommand(session, task string) string {
	return shellQuote(c.Binary) + " task-status --config " + shellQuote(automationPath(c.Root)) + " --task-session " + shellQuote(session) + " --task-id " + shellQuote(task) + " --review-complete"
}

// Counts journal receipts for this task, not inventory growth or requested review.
func TaskSummary(configPath, session string, complete bool, taskID ...string) (TaskStatus, error) {
	d := TaskStatus{MemoryIDs: []string{}, State: "review_incomplete"}
	c, err := ReadAutomation(configPath)
	if err != nil {
		return d, err
	}
	if !c.Enabled {
		d.State = "paused"
		d.Line = "Elephant: automation paused."
		return d, nil
	}
	s := c.Service().Store
	err = s.transactSQL(true, nil, func(tx *sql.Tx, all []Memory, _ map[string]bool) (*Event, error) {
		state, e := readTaskState(c, session)
		if e != nil {
			return nil, e
		}
		if len(taskID) > 0 && taskID[0] != "" && state.Task != taskID[0] {
			return nil, fmt.Errorf("task changed before status acknowledgement")
		}
		if complete {
			if !state.Reviewed {
				return nil, fmt.Errorf("lesson review has not been requested")
			}
			state.Completed = true
			if e = writeTaskState(c, session, state); e != nil {
				return nil, e
			}
		}
		d.Recalled = len(state.Recalled)
		ownedIDs := map[string]bool{}
		for _, m := range all {
			if owned(m, c.Identity) && m.Project == c.Project && m.Conversation == c.Conversation {
				ownedIDs[m.ID] = true
			}
		}
		seen := map[string]bool{}
		failed := false
		rows, e := tx.Query("SELECT data FROM experiences WHERE tenant=? AND user=? AND session=? AND task=? ORDER BY seq", c.Identity.Tenant, c.Identity.User, session, state.Task)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		for rows.Next() {
			var data string
			var x Experience
			if e = rows.Scan(&data); e != nil {
				return nil, e
			}
			if e = json.Unmarshal([]byte(data), &x); e != nil {
				return nil, e
			}
			if x.Status == "capture_failed" {
				failed = true
			}
			if x.Status == "memory_saved" && ownedIDs[x.MemoryID] && !seen[x.MemoryID] {
				seen[x.MemoryID] = true
				d.MemoryIDs = append(d.MemoryIDs, x.MemoryID)
			}
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		d.Saved = len(d.MemoryIDs)
		switch {
		case failed:
			d.State = "capture_failed"
		case !state.Completed:
			d.State = "review_incomplete"
		case d.Saved == 0:
			d.State = "no_lesson"
		default:
			d.State = "saved"
		}
		return nil, nil
	})
	if err != nil {
		return d, err
	}
	ending := "review incomplete"
	switch d.State {
	case "no_lesson":
		ending = "no reusable lesson found"
	case "saved":
		ending = fmt.Sprintf("saved %d lessons", d.Saved)
		if d.Saved == 1 {
			ending = "saved 1 lesson"
		}
	case "capture_failed":
		ending = fmt.Sprintf("saved %d lessons · capture failed", d.Saved)
	}
	noun := "memories"
	if d.Recalled == 1 {
		noun = "memory"
	}
	d.Line = fmt.Sprintf("Elephant: recalled %d %s · %s.", d.Recalled, noun, ending)
	return d, nil
}
