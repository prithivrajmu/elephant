package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// IdleDreamConfig is trusted local configuration, never an MCP argument.
type IdleDreamConfig struct {
	Enabled         bool     `json:"enabled"`
	Adapter         string   `json:"adapter"`
	Runner          []string `json:"runner"`
	IdleSeconds     int      `json:"idle_seconds"`
	CooldownSeconds int      `json:"cooldown_seconds"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	MaxRunsPerDay   int      `json:"max_runs_per_day"`
	ByteBudget      int      `json:"byte_budget"`
	Limit           int      `json:"limit"`
	MaxChanges      int      `json:"max_changes"`
}

func DefaultIdleDreamConfig() IdleDreamConfig {
	return IdleDreamConfig{Enabled: true, Adapter: "codex", Runner: []string{"codex"}, IdleSeconds: 600, CooldownSeconds: 21600, TimeoutSeconds: 120, MaxRunsPerDay: 4, ByteBudget: 16000, Limit: 10, MaxChanges: 3}
}

func (c IdleDreamConfig) validate() error {
	if (c.Adapter != "codex" && c.Adapter != "command") || len(c.Runner) < 1 || len(c.Runner) > 32 || c.Runner[0] == "" {
		return fmt.Errorf("idle dreaming requires a codex or command adapter and an executable argument array")
	}
	for _, arg := range c.Runner {
		if len(arg) > 4096 {
			return fmt.Errorf("idle runner argument too long")
		}
	}
	if c.IdleSeconds < 60 || c.IdleSeconds > 86400 || c.CooldownSeconds < 60 || c.CooldownSeconds > 604800 || c.TimeoutSeconds < 10 || c.TimeoutSeconds > 900 || c.MaxRunsPerDay < 1 || c.MaxRunsPerDay > 24 || c.ByteBudget < 2048 || c.ByteBudget > 64000 || c.Limit < 1 || c.Limit > 20 || c.MaxChanges < 1 || c.MaxChanges > 3 {
		return fmt.Errorf("invalid idle limits: idle 60..86400s, cooldown 60..604800s, timeout 10..900s, daily runs 1..24, bytes 2048..64000, memories 1..20, changes 1..3")
	}
	return nil
}

type IdleDreamState struct {
	Generation   int64           `json:"generation"`
	Reviewed     int64           `json:"reviewed_generation"`
	LastActivity time.Time       `json:"last_activity"`
	Active       map[string]bool `json:"active_sessions"`
	Lease        string          `json:"lease,omitempty"`
	LeaseUntil   time.Time       `json:"lease_until,omitempty"`
	LastAttempt  time.Time       `json:"last_attempt,omitempty"`
	Day          string          `json:"day_utc,omitempty"`
	Attempts     int             `json:"attempts_today"`
	Cursor       string          `json:"cursor,omitempty"`
	LastResult   string          `json:"last_result,omitempty"`
	LastSaved    []string        `json:"last_saved_ids,omitempty"`
}

type IdleDreamStatus struct {
	Config  *IdleDreamConfig `json:"config"`
	State   IdleDreamState   `json:"state"`
	Running bool             `json:"watcher_running"`
}

const idleTable = `CREATE TABLE IF NOT EXISTS dream_idle(context TEXT PRIMARY KEY, data TEXT NOT NULL)`

func readIdleState(tx *sql.Tx, key string) (IdleDreamState, error) {
	state := IdleDreamState{Active: map[string]bool{}}
	if _, err := tx.Exec(idleTable); err != nil {
		return state, err
	}
	var data string
	err := tx.QueryRow("SELECT data FROM dream_idle WHERE context=?", key).Scan(&data)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal([]byte(data), &state)
	if state.Active == nil {
		state.Active = map[string]bool{}
	}
	return state, err
}

func writeIdleState(tx *sql.Tx, key string, state IdleDreamState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO dream_idle VALUES(?,?) ON CONFLICT(context) DO UPDATE SET data=excluded.data", key, string(b))
	return err
}

func idleState(c AutomationConfig, fn func(*sql.Tx, *IdleDreamState) error) error {
	return c.Service().Store.transactSQL(false, nil, func(tx *sql.Tx, _ []Memory, _ map[string]bool) (*Event, error) {
		state, err := readIdleState(tx, taskContext(c))
		if err != nil {
			return nil, err
		}
		if err = fn(tx, &state); err != nil {
			return nil, err
		}
		return nil, writeIdleState(tx, taskContext(c), state)
	})
}

// ConfigureIdleDream is a local opt-in. Changing settings invalidates in-flight
// saves under the same SQLite lock used by the memory journal.
func ConfigureIdleDream(path string, config IdleDreamConfig, reset bool) (IdleDreamStatus, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return IdleDreamStatus{}, err
	}
	c, err := ReadAutomation(path)
	if err != nil {
		return IdleDreamStatus{}, err
	}
	unlock, err := lockIdleConfiguration(c.Root)
	if err != nil {
		return IdleDreamStatus{}, err
	}
	defer unlock()
	c, err = ReadAutomation(path)
	if err != nil {
		return IdleDreamStatus{}, err
	}
	if err = config.validate(); err != nil {
		return IdleDreamStatus{}, err
	}
	if config.Enabled {
		runner, e := exec.LookPath(config.Runner[0])
		if e != nil {
			return IdleDreamStatus{}, fmt.Errorf("idle runner not found: %w", e)
		}
		config.Runner[0], err = filepath.Abs(runner)
		if err != nil {
			return IdleDreamStatus{}, err
		}
	}
	c.IdleDream = &config
	err = idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		state.Generation++
		state.LastActivity = time.Now().UTC()
		state.LastResult = "configured"
		if reset {
			state.Active = map[string]bool{}
		}
		// Configuration alone does not count as a completed agent task.
		state.Reviewed = state.Generation
		b, e := json.MarshalIndent(c, "", "  ")
		if e != nil {
			return e
		}
		if e = safeParents(c.Root, path); e != nil {
			return e
		}
		return atomicLocalFile(path, append(b, '\n'), 0600)
	})
	if err != nil {
		return IdleDreamStatus{}, err
	}
	if config.Enabled && c.Enabled {
		if err = startIdleWatcher(c, path); err != nil {
			return IdleDreamStatus{}, err
		}
	}
	return DreamScheduleStatus(path)
}

// Share InitAutomation's lock so a concurrent init cannot restore paused state.
func lockIdleConfiguration(root string) (func(), error) {
	path := filepath.Join(root, ".elephant", "init.lock")
	if err := os.Mkdir(path, 0700); err != nil {
		return nil, fmt.Errorf("Elephant configuration is already being updated: %w", err)
	}
	return func() { _ = os.Remove(path) }, nil
}

func DreamScheduleStatus(path string) (IdleDreamStatus, error) {
	c, err := ReadAutomation(path)
	if err != nil {
		return IdleDreamStatus{}, err
	}
	out := IdleDreamStatus{Config: c.IdleDream}
	err = idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		out.State = *state
		out.Running = state.Lease != "" && state.LeaseUntil.After(time.Now())
		if out.State.Day != time.Now().UTC().Format("2006-01-02") {
			out.State.Attempts = 0
		}
		return nil
	})
	return out, err
}

// touchIdleDream runs only for validated main-session hooks. An unmatched active
// session stays active until Stop (or explicit local reset); silence is not
// evidence that a long-running foreground task has finished.
func touchIdleDream(c AutomationConfig, session string, active bool, now time.Time) (bool, error) {
	start := false
	err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		state.Generation++
		state.LastActivity = now.UTC()
		if active {
			if len(state.Active) >= 128 && !state.Active[session] {
				return fmt.Errorf("too many active idle-dream sessions; inspect and reset abandoned sessions")
			}
			state.Active[session] = true
		} else {
			delete(state.Active, session)
		}
		start = state.Lease == "" || !state.LeaseUntil.After(now)
		return nil
	})
	return start, err
}

func notifyIdleDream(c AutomationConfig, path, session string, h HookInput, out map[string]any) error {
	if c.IdleDream == nil || !c.IdleDream.Enabled {
		return nil
	}
	active := true
	if h.Event == "Stop" && len(h.Background) == 0 && out["decision"] != "block" {
		active = false
		if h.StopActive {
			state, err := readTaskState(c, session)
			active = err != nil || !state.Completed
		}
	}
	start, err := touchIdleDream(c, session, active, time.Now())
	if err != nil || !start {
		return err
	}
	return startIdleWatcher(c, path)
}

func startIdleWatcher(c AutomationConfig, path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	cmd := exec.Command(c.Binary, "dream-worker", "--config", path)
	cmd.Dir = c.Root
	quiet, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer quiet.Close()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = quiet, quiet, quiet
	detachDreamProcess(cmd)
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("could not start idle watcher: %w", err)
	}
	return cmd.Process.Release()
}

func idleReady(state IdleDreamState, config IdleDreamConfig, now time.Time) bool {
	attempts := state.Attempts
	if state.Day != now.UTC().Format("2006-01-02") {
		attempts = 0
	}
	return config.Enabled && len(state.Active) == 0 && state.Generation > state.Reviewed && !state.LastActivity.IsZero() &&
		!now.Before(state.LastActivity.Add(time.Duration(config.IdleSeconds)*time.Second)) &&
		!now.Before(state.LastAttempt.Add(time.Duration(config.CooldownSeconds)*time.Second)) && attempts < config.MaxRunsPerDay
}

// idleCommitGuard is internal: no model or MCP caller can supply one. Every save
// rechecks activity, configuration, lease and revisions in the write transaction.
type idleCommitGuard struct {
	ConfigPath string
	Config     AutomationConfig
	Lease      string
	Generation int64
}

func validateIdleCommit(tx *sql.Tx, guard *idleCommitGuard) error {
	if guard == nil {
		return nil
	}
	c, err := ReadAutomation(guard.ConfigPath)
	if err != nil || !c.Enabled || c.IdleDream == nil || !c.IdleDream.Enabled || c.Store != guard.Config.Store || taskContext(c) != taskContext(guard.Config) || idleConfigDigest(c) != idleConfigDigest(guard.Config) {
		return fmt.Errorf("idle dreaming paused or reconfigured")
	}
	state, err := readIdleState(tx, taskContext(c))
	if err != nil {
		return err
	}
	if state.Lease != guard.Lease || !state.LeaseUntil.After(time.Now()) || state.Generation != guard.Generation || len(state.Active) != 0 {
		return fmt.Errorf("foreground activity resumed or idle lease expired")
	}
	return nil
}

func idleConfigDigest(c AutomationConfig) string {
	b, _ := json.Marshal(c.IdleDream)
	return digest(string(b))
}

func claimIdleWatcher(c AutomationConfig, now time.Time) (string, error) {
	lease := newID()
	err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		if state.Lease != "" && state.LeaseUntil.After(now) {
			lease = ""
			return nil
		}
		state.Lease, state.LeaseUntil = lease, now.Add(30*time.Second)
		return nil
	})
	return lease, err
}

// WatchIdleDream is restarted by normal hooks after a crash. A persisted lease
// keeps duplicate watcher processes from launching overlapping model runs.
func WatchIdleDream(ctx context.Context, path string) error {
	c, err := ReadAutomation(path)
	if err != nil || !c.Enabled || c.IdleDream == nil || !c.IdleDream.Enabled {
		return err
	}
	lease, err := claimIdleWatcher(c, time.Now())
	if err != nil || lease == "" {
		return err
	}
	defer idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		if state.Lease == lease {
			state.Lease, state.LeaseUntil = "", time.Time{}
		}
		return nil
	})
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		next, e := ReadAutomation(path)
		if e != nil {
			return e
		}
		if !next.Enabled || next.IdleDream == nil || !next.IdleDream.Enabled || taskContext(next) != taskContext(c) || next.Store != c.Store {
			return nil
		}
		c = next
		var ready bool
		err = idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
			if state.Lease != lease {
				return fmt.Errorf("idle watcher lease replaced")
			}
			state.LeaseUntil = time.Now().Add(30 * time.Second)
			ready = idleReady(*state, *c.IdleDream, time.Now())
			return nil
		})
		if err != nil {
			return err
		}
		if ready {
			if err = runIdleDream(ctx, path, c, lease, executeDreamRunner); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
