//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	memory "github.com/prithivrajmu/elephant"
)

func TestMenuBarKeepsClientIdentityWhenCallsPrecedeAttachment(t *testing.T) {
	h := newMenuBarHarness(t)
	release := make(chan struct{})
	served := make(chan error, 1)
	go func() {
		served <- h.client(t.Errorf).serve(h.service("/tmp/shared-project"), "mcp", func(observed memory.Service) error {
			observed.ObserveMCP(memory.MCPEvent{Client: memory.MCPClientInfo{Name: "omp", Version: "1.2.3", Title: "OMP"}, At: time.Now().UTC()})
			observed.ObserveMCP(memory.MCPEvent{Tool: "profile_memory", At: time.Now().UTC()})
			<-release
			return nil
		})
	}()
	h.waitReady()
	other := h.attach("/tmp/shared-project")
	if err := json.NewEncoder(other).Encode(menuBarObservation{Initialized: true, Client: memory.MCPClientInfo{Name: "claude-code", Version: "2.0"}}); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case model := <-h.ui.models:
			for _, section := range model.Items {
				if section.Title != "Sessions" || len(section.Items) != 1 || section.Items[0].Title != "shared-project" {
					continue
				}
				rows := section.Items[0].Items
				var omp, claude string
				for _, row := range rows {
					if strings.HasPrefix(row.Title, "OMP · ") && findNativeAction(row.Items, "Open Memory Palace") != "" {
						for _, activity := range row.Items {
							if activity.Title == "profile_memory · completed" {
								omp = row.Title
							}
						}
					}
					if strings.HasPrefix(row.Title, "claude-code · ") {
						claude = row.Title
					}
				}
				if omp != "" && claude != "" && omp != claude {
					other.Close()
					close(release)
					if err := <-served; err != nil {
						t.Fatal(err)
					}
					h.waitHosts()
					return
				}
			}
		case <-timer.C:
			other.Close()
			close(release)
			<-served
			h.waitHosts()
			t.Fatal("same-project sessions did not retain distinct client labels and completed-call metadata")
		}
	}
}

func TestMenuBarCopiedDiagnosticsExcludePrivateAndUntrustedFields(t *testing.T) {
	root := t.TempDir()
	secret := "private-marker-must-not-be-copied"
	svc := memory.Service{Root: filepath.Join(root, secret), Project: secret, Conversation: secret, Store: memory.Store{Path: filepath.Join(root, secret, "memory.sqlite")}, Identity: memory.Identity{Tenant: secret, User: secret, Team: secret}}
	controls := &menuBarControls{health: map[menuBarHealthKey]menuBarHealth{}}
	controls.health[healthKey(svc)] = menuBarHealth{Checked: time.Now().UTC(), Report: memory.Diagnostics{Store: svc.Store.Path, Notes: []string{secret}, Checks: []memory.Check{{Name: "database_integrity", OK: false, Detail: secret}, {Name: secret, OK: true, Detail: secret}}}}
	view := menuBarView{ID: "SAFEID123", Service: svc, Session: menuBarSession{Store: svc.Store.Path, Root: svc.Root, Project: svc.Project, Conversation: secret, Identity: svc.Identity, PID: 123, Kind: "mcp", Executable: filepath.Join(root, secret), EngineVersion: secret}, Observed: menuBarObservation{Client: memory.MCPClientInfo{Name: secret, Title: secret, Version: secret}, Tool: secret}, Connected: time.Now().UTC(), Context: context.Background()}
	text, err := controls.redactedDiagnostics(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, secret) || strings.Contains(text, root) {
		t.Fatal("clipboard diagnostics included a path, identity, raw client field, activity, or doctor detail")
	}
	var payload struct {
		Checks []struct {
			Name string
			OK   bool
		}
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Checks) != 2 || payload.Checks[0].Name != "database_integrity" || payload.Checks[0].OK || payload.Checks[1].Name != "additional_check" {
		t.Fatalf("redaction lost the actionable health result: %+v", payload.Checks)
	}
}
