package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// ErrDreamSetupMismatch reports automation that belongs to another session setup.
var ErrDreamSetupMismatch = errors.New("Elephant automation here uses a different store, identity or project than this session")

var errDreamNotInstalled = errors.New("Elephant automation is not installed for this project; run elephant init first")

// manualDreamWindow bounds how long a Dream Now request waits for the watcher.
const manualDreamWindow = time.Minute

// dreamScope normalizes the root and store the way InitAutomation records them.
func (s Service) dreamScope() (Service, error) {
	root, err := filepath.Abs(s.Root)
	if err != nil {
		return s, err
	}
	if resolved, e := filepath.EvalSymlinks(root); e == nil {
		root = resolved
	}
	s.Root = root
	s.Store.Path, err = filepath.Abs(s.Store.Path)
	return s, err
}

// dreamConfig reads the automation config that belongs to exactly s. found is
// false when its root has no Elephant automation. A config for another root,
// store, identity or project is ErrDreamSetupMismatch, never a fallback.
func (s Service) dreamConfig() (AutomationConfig, bool, error) {
	s, err := s.dreamScope()
	if err != nil {
		return AutomationConfig{}, false, err
	}
	c, found, err := ConfigForRoot(s.Root)
	if err != nil || !found {
		return c, found, err
	}
	if !automationMatchesService(c, s) {
		return c, true, fmt.Errorf("%w (automation root %s)", ErrDreamSetupMismatch, c.Root)
	}
	return c, true, nil
}

// dreamConfigLocked holds the installer lock and returns this Service's config
// as read under it, so a concurrent init cannot swap the context unseen.
func (s Service) dreamConfigLocked() (AutomationConfig, func(), error) {
	c, found, err := s.dreamConfig()
	if err == nil && !found {
		err = errDreamNotInstalled
	}
	if err != nil {
		return c, nil, err
	}
	unlock, err := lockIdleConfiguration(c.Root)
	if err != nil {
		return c, nil, err
	}
	if c, found, err = s.dreamConfig(); err != nil || !found {
		unlock()
		if err == nil {
			err = errDreamNotInstalled
		}
		return c, nil, err
	}
	return c, unlock, nil
}

// DreamStatus reports the dreaming schedule of exactly this Service. found is
// false when its project has no Elephant automation.
func (s Service) DreamStatus() (IdleDreamStatus, bool, error) {
	c, found, err := s.dreamConfig()
	if err != nil || !found {
		return IdleDreamStatus{}, found, err
	}
	status, err := idleScheduleStatus(c)
	return status, true, err
}

// ConfigureDream saves idle-dream settings for exactly this Service. Like
// ConfigureIdleDream it invalidates in-flight saves; it also refuses when the
// project's automation now belongs to another context.
func (s Service) ConfigureDream(config IdleDreamConfig) (IdleDreamStatus, error) {
	c, found, err := s.dreamConfig()
	if err == nil && !found {
		err = errDreamNotInstalled
	}
	if err != nil {
		return IdleDreamStatus{}, err
	}
	scope, err := s.dreamScope()
	if err != nil {
		return IdleDreamStatus{}, err
	}
	return configureIdleDream(automationPath(c.Root), config, false, &scope)
}

// manualDreamBlocker reports why Dream Now must be refused, or "".
func manualDreamBlocker(c AutomationConfig, state IdleDreamState, now time.Time) string {
	switch {
	case !c.Enabled:
		return "automation_disabled"
	case c.IdleDream == nil:
		return "not_configured"
	case state.LastResult == "running" && state.Lease != "" && state.LeaseUntil.After(now):
		return "reviewing"
	case state.ManualRequest != "" && state.ManualRequestUntil.After(now):
		return "requested"
	}
	return dreamUsageBlocker(state, *c.IdleDream, now)
}

// dreamUsageBlocker holds the gates that an explicit Dream Now keeps: the
// schedule switch, foreground work and the daily attempt budget. It skips only
// the quiet period, cooldown and pending-work gates.
func dreamUsageBlocker(state IdleDreamState, config IdleDreamConfig, now time.Time) string {
	switch {
	case !config.Enabled:
		return "disabled"
	case len(state.Active) != 0:
		return "active_sessions"
	case state.Day == now.UTC().Format("2006-01-02") && state.Attempts >= config.MaxRunsPerDay:
		return "daily_limit"
	}
	return ""
}

// pendingManualDream returns the ID of a live Dream Now request that may start.
func pendingManualDream(state IdleDreamState, config IdleDreamConfig, now time.Time) string {
	if state.ManualRequest == "" || !state.ManualRequestUntil.After(now) || dreamUsageBlocker(state, config, now) != "" {
		return ""
	}
	return state.ManualRequest
}

var manualDreamRefusals = map[string]string{
	"automation_disabled": "automatic hooks are paused for this project",
	"not_configured":      "dreaming is not configured",
	"disabled":            "the dreaming schedule is paused",
	"reviewing":           "a review is already running",
	"requested":           "a Dream Now request is already waiting to start",
	"active_sessions":     "foreground agent work is active",
	"daily_limit":         "today's review limit is used up",
}

func dreamNowRefusal(blocker string) error {
	if blocker == "" {
		return nil
	}
	return fmt.Errorf("Dream Now refused: %s", manualDreamRefusals[blocker])
}

// DreamNowRefusal is the error RequestDreamNow would return at status time,
// or nil when Dream Now is allowed.
func (s IdleDreamStatus) DreamNowRefusal() error { return dreamNowRefusal(s.ManualBlocker) }

// RequestDreamNow asks this Service's dream watcher for one review outside the
// quiet period, cooldown and pending-work gates. Every other safeguard stays:
// the configured reviewer and limits apply, the attempt counts toward the daily
// limit, and the request is refused while foreground work or another review is
// active. The returned ID appears as IdleDreamState.RunRequest once the review
// starts; an unstarted request expires after one minute.
func (s Service) RequestDreamNow() (string, error) {
	c, unlock, err := s.dreamConfigLocked()
	if err != nil {
		return "", err
	}
	defer unlock()
	id, start := newID(), false
	err = idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		now := time.Now().UTC()
		if err := dreamNowRefusal(manualDreamBlocker(c, *state, now)); err != nil {
			return err
		}
		state.ManualRequest, state.ManualRequestUntil = id, now.Add(manualDreamWindow)
		start = state.Lease == "" || !state.LeaseUntil.After(now)
		return nil
	})
	if err != nil {
		return "", err
	}
	if start {
		if err = startIdleWatcher(c, automationPath(c.Root)); err != nil {
			_ = idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
				if state.ManualRequest == id {
					state.ManualRequest, state.ManualRequestUntil = "", time.Time{}
				}
				return nil
			})
			return "", err
		}
	}
	return id, nil
}

// CancelDreamReview withdraws a waiting Dream Now request and stops the running
// review, manual or scheduled, of this Service. The run's monitor and every
// commit recheck the flag, so no replacement is saved after it is set. The
// schedule stays as it was and unreviewed work stays pending. It reports
// whether anything was cancelled.
func (s Service) CancelDreamReview() (bool, error) {
	c, found, err := s.dreamConfig()
	if err == nil && !found {
		err = errDreamNotInstalled
	}
	if err != nil {
		return false, err
	}
	cancelled := false
	err = idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		now := time.Now().UTC()
		if state.ManualRequest != "" {
			cancelled = state.ManualRequestUntil.After(now)
			state.ManualRequest, state.ManualRequestUntil = "", time.Time{}
		}
		if state.LastResult == "running" && state.RunID != "" && state.Lease != "" && state.LeaseUntil.After(now) {
			state.CancelRun, cancelled = state.RunID, true
		}
		return nil
	})
	return cancelled, err
}
