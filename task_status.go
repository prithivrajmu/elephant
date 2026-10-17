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
		return "", fmt.Errorf("invalid task session: use the exact command from the review hook")
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
		return state, fmt.Errorf("no active Elephant task in this session: the next prompt starts one")
	}
	if err = json.Unmarshal(b, &state); err != nil {
		return state, err
	}
	if state.Task == "" {
		return state, fmt.Errorf("no active Elephant task in this session: the next prompt starts one")
	}
	if state.Context != taskContext(c) {
		return state, fmt.Errorf("this task belongs to a different Elephant setup: use the command from this project's review hook")
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

// reviewTarget returns the task a hook-issued review command names: the current
// task, or the previous task whose review a new prompt interrupted. Nil otherwise.
func reviewTarget(state *hookState, taskID []string) *hookState {
	if len(taskID) == 0 || taskID[0] == "" || taskID[0] == state.Task {
		return state
	}
	if state.Pending != nil && state.Pending.Task == taskID[0] {
		return state.Pending
	}
	return nil
}

// staleReview explains a task ID that is neither the current nor the pending task.
func staleReview(tx *sql.Tx, c AutomationConfig, session, task string) (string, error) {
	rows, err := tx.Query(`SELECT json_extract(data,'$.status') FROM experiences WHERE tenant=? AND user=? AND session=? AND task=?
		AND json_extract(data,'$.status') IN ('review_requested','review_completed')`, c.Identity.Tenant, c.Identity.User, session, task)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	state := ""
	for rows.Next() {
		var status string
		if err = rows.Scan(&status); err != nil {
			return "", err
		}
		if state != "already_reported" {
			state = map[string]string{"review_requested": "superseded", "review_completed": "already_reported"}[status]
		}
	}
	if err = rows.Err(); err == nil && state == "" {
		err = fmt.Errorf("unknown task ID for this session: use the --task-id from the review hook")
	}
	return state, err
}
func CaptureForTask(configPath, session string, taskID ...string) (AutomationConfig, *TaskCapture, error) {
	c, err := ReadAutomation(configPath)
	if err != nil {
		return c, nil, err
	}
	var capture *TaskCapture
	err = c.Service().Store.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
		state, e := readTaskState(c, session)
		if e != nil {
			return nil, e
		}
		if !c.Enabled {
			return nil, fmt.Errorf("automatic memory is paused: lesson not saved. Resume with elephant automation --enabled=true")
		}
		t := reviewTarget(&state, taskID)
		if t == nil {
			why, e := staleReview(tx, c, session, taskID[0])
			if e != nil {
				return nil, e
			}
			if why == "already_reported" {
				return nil, fmt.Errorf("this review was already reported: lesson not saved")
			}
			return nil, fmt.Errorf("a newer task replaced this review: lesson not saved. Run the task-status command and report its line")
		}
		capture = &TaskCapture{Session: session, Task: t.Task, Agent: t.Agent}
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
		t := reviewTarget(&state, taskID)
		task := ""
		var done *Event
		if t == nil {
			// Read-only: a stale review never completes or counts toward a later task.
			task = taskID[0]
			if d.State, e = staleReview(tx, c, session, task); e != nil {
				return nil, e
			}
		} else {
			task = t.Task
			if complete {
				if !t.Reviewed {
					return nil, fmt.Errorf("no lesson review was requested for this task: run task-status only after the review hook asks")
				}
				if !t.Completed {
					done = &Event{Kind: "experience", Experience: &Experience{ID: newID(), Identity: c.Identity, Project: c.Project, Agent: t.Agent, Session: session, Task: t.Task, Event: "LessonReview", Status: "review_completed", At: time.Now().UTC()}}
				}
				t.Completed = true
				if e = writeTaskState(c, session, state); e != nil {
					return nil, e
				}
			}
			d.Recalled = len(t.Recalled)
		}
		ownedIDs := map[string]bool{}
		for _, m := range all {
			if owned(m, c.Identity) && m.Project == c.Project && m.Conversation == c.Conversation {
				ownedIDs[m.ID] = true
			}
		}
		seen := map[string]bool{}
		failed := false
		rows, e := tx.Query("SELECT data FROM experiences WHERE tenant=? AND user=? AND session=? AND task=? ORDER BY seq", c.Identity.Tenant, c.Identity.User, session, task)
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
		case t == nil:
		case failed:
			d.State = "capture_failed"
		case !t.Completed:
			d.State = "review_incomplete"
		case d.Saved == 0:
			d.State = "no_lesson"
		default:
			d.State = "saved"
		}
		return done, nil
	})
	if err != nil {
		return d, err
	}
	saved := fmt.Sprintf("saved %d lessons", d.Saved)
	if d.Saved == 1 {
		saved = "saved 1 lesson"
	}
	switch d.State {
	case "superseded":
		d.Line = "Elephant: " + saved + " · a newer task replaced this review."
		return d, nil
	case "already_reported":
		d.Line = "Elephant: " + saved + " · this review was already reported."
		return d, nil
	}
	ending := "review incomplete"
	switch d.State {
	case "no_lesson":
		ending = "no reusable lesson found"
	case "saved":
		ending = saved
	case "capture_failed":
		ending = saved + " · capture failed"
	}
	noun := "memories"
	if d.Recalled == 1 {
		noun = "memory"
	}
	d.Line = fmt.Sprintf("Elephant: recalled %d %s · %s.", d.Recalled, noun, ending)
	return d, nil
}
