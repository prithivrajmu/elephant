package memory

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func TestIdleEligibilityExplainsAllGates(t *testing.T) {
	now := time.Date(2026, 11, 7, 23, 59, 30, 0, time.UTC)
	midnight := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*IdleDreamState, *IdleDreamConfig)
		reason string
		next   time.Time
	}{
		{name: "ready", reason: "ready"},
		{name: "disabled", mutate: func(_ *IdleDreamState, c *IdleDreamConfig) { c.Enabled = false }, reason: "disabled"},
		{name: "active session", mutate: func(s *IdleDreamState, _ *IdleDreamConfig) { s.Active["foreground"] = true }, reason: "active_sessions"},
		{name: "already reviewed", mutate: func(s *IdleDreamState, _ *IdleDreamConfig) { s.Reviewed = s.Generation }, reason: "no_pending_activity"},
		{name: "no activity", mutate: func(s *IdleDreamState, _ *IdleDreamConfig) { s.LastActivity = time.Time{} }, reason: "no_pending_activity"},
		{name: "idle delay", mutate: func(s *IdleDreamState, _ *IdleDreamConfig) { s.LastActivity = now }, reason: "idle_delay", next: now.Add(10 * time.Minute)},
		{name: "cooldown", mutate: func(s *IdleDreamState, _ *IdleDreamConfig) { s.LastAttempt = now }, reason: "cooldown", next: now.Add(6 * time.Hour)},
		{name: "latest timing gate wins", mutate: func(s *IdleDreamState, _ *IdleDreamConfig) {
			s.LastActivity, s.LastAttempt = now, now.Add(-time.Hour)
		}, reason: "cooldown", next: now.Add(5 * time.Hour)},
		{name: "UTC daily limit", mutate: func(s *IdleDreamState, c *IdleDreamConfig) {
			s.Day, s.Attempts = now.Format("2006-01-02"), c.MaxRunsPerDay
		}, reason: "daily_limit", next: midnight},
		{name: "cooldown beyond daily reset", mutate: func(s *IdleDreamState, c *IdleDreamConfig) {
			s.Day, s.Attempts, s.LastAttempt = now.Format("2006-01-02"), c.MaxRunsPerDay, now
		}, reason: "cooldown", next: now.Add(6 * time.Hour)},
		{name: "idle delay beyond daily reset", mutate: func(s *IdleDreamState, c *IdleDreamConfig) {
			s.Day, s.Attempts, s.LastActivity = now.Format("2006-01-02"), c.MaxRunsPerDay, now
		}, reason: "idle_delay", next: now.Add(10 * time.Minute)},
		{name: "daily limit wins timing tie", mutate: func(s *IdleDreamState, c *IdleDreamConfig) {
			s.Day, s.Attempts = now.Format("2006-01-02"), c.MaxRunsPerDay
			s.LastAttempt = midnight.Add(-time.Duration(c.CooldownSeconds) * time.Second)
		}, reason: "daily_limit", next: midnight},
		{name: "old UTC day resets", mutate: func(s *IdleDreamState, c *IdleDreamConfig) {
			s.Day, s.Attempts = "2026-11-06", c.MaxRunsPerDay
		}, reason: "ready"},
		{name: "active suppresses timing estimate", mutate: func(s *IdleDreamState, _ *IdleDreamConfig) {
			s.Active["foreground"], s.LastAttempt = true, now
		}, reason: "active_sessions"},
		{name: "reviewed suppresses timing estimate", mutate: func(s *IdleDreamState, _ *IdleDreamConfig) {
			s.Reviewed, s.LastAttempt = s.Generation, now
		}, reason: "no_pending_activity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := IdleDreamState{Generation: 2, Reviewed: 1, LastActivity: now.Add(-time.Hour), Active: map[string]bool{}}
			config := DefaultIdleDreamConfig()
			if tc.mutate != nil {
				tc.mutate(&state, &config)
			}
			// The caller's time zone must not change the UTC daily cap.
			reason, next := idleEligibility(state, config, now.In(time.FixedZone("local", 5*60*60+30*60)))
			if reason != tc.reason || (next == nil) != tc.next.IsZero() || next != nil && !next.Equal(tc.next) {
				t.Fatalf("got %q %v; want %q %v", reason, next, tc.reason, tc.next)
			}
			if idleReady(state, config, now) != (tc.reason == "ready") {
				t.Fatal("scheduler readiness disagrees with status")
			}
			if next != nil {
				if idleReady(state, config, next.Add(-time.Nanosecond)) {
					t.Fatal("scheduler became ready before the reported time")
				}
				if !idleReady(state, config, *next) {
					t.Fatal("scheduler was not ready at the reported time")
				}
			}
		})
	}
}

func TestDreamScheduleStatusReportsEligibilityAndWatcherSeparately(t *testing.T) {
	c, path, _, _ := idleFixture(t)
	status, err := DreamScheduleStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready || status.WaitReason != "ready" || !status.Running || status.NextEligibleAt != nil {
		t.Fatalf("unexpected ready status: %+v", status)
	}
	lastActivity := time.Now().UTC()
	if err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		state.LastActivity = lastActivity
		state.LeaseUntil = lastActivity.Add(-time.Second)
		state.Day, state.Attempts = lastActivity.AddDate(0, 0, -1).Format("2006-01-02"), c.IdleDream.MaxRunsPerDay
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, err = DreamScheduleStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	want := lastActivity.Add(time.Duration(c.IdleDream.IdleSeconds) * time.Second)
	if status.Ready || status.Running || status.WaitReason != "idle_delay" || status.NextEligibleAt == nil || !status.NextEligibleAt.Equal(want) || status.State.Attempts != 0 {
		t.Fatalf("unexpected waiting status: %+v", status)
	}
	if err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
		state.LastActivity = lastActivity.Add(-time.Hour)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	status, err = DreamScheduleStatus(path)
	if err != nil || !status.Ready || status.Running {
		t.Fatalf("readiness depended on expired watcher lease: %+v, %v", status, err)
	}
}

func TestDreamScheduleStatusOptInAndActivityBlockers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*AutomationConfig, *IdleDreamState)
		reason string
	}{
		{name: "automation paused", change: func(c *AutomationConfig, _ *IdleDreamState) { c.Enabled = false }, reason: "automation_disabled"},
		{name: "not configured", change: func(c *AutomationConfig, _ *IdleDreamState) { c.IdleDream = nil }, reason: "not_configured"},
		{name: "schedule paused", change: func(c *AutomationConfig, _ *IdleDreamState) { c.IdleDream.Enabled = false }, reason: "disabled"},
		{name: "foreground active", change: func(_ *AutomationConfig, s *IdleDreamState) { s.Active["foreground"] = true }, reason: "active_sessions"},
		{name: "no pending task", change: func(_ *AutomationConfig, s *IdleDreamState) { s.Reviewed = s.Generation }, reason: "no_pending_activity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, path, _, _ := idleFixture(t)
			if err := idleState(c, func(_ *sql.Tx, state *IdleDreamState) error {
				tc.change(&c, state)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			writeIdleConfig(t, path, c)
			status, err := DreamScheduleStatus(path)
			if err != nil {
				t.Fatal(err)
			}
			if status.Ready || status.WaitReason != tc.reason || status.NextEligibleAt != nil {
				t.Fatalf("unexpected status: %+v", status)
			}
			encoded, err := json.Marshal(status)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["next_eligible_at"]; ok {
				t.Fatal("status advertised a start time requiring a new foreground event or configuration change")
			}
		})
	}
}
