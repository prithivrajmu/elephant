//go:build darwin

package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	memory "github.com/prithivrajmu/elephant"
	"github.com/prithivrajmu/elephant/internal/menubar"
)

type menuControlContext struct {
	Context     context.Context
	Service     memory.Service
	Executable  string
	Executables []string // every connected process's binary; product-wide updates only
	// Product-wide updates only. Stores holds every connected store; when
	// empty, Service.Store is the only store. ReportedExecutables are the
	// elephant paths each session found on its own PATH.
	Stores               []memory.Store
	ReportedExecutables  []string
	UpdateChecksDisabled bool                // any connected process set ELEPHANT_UPDATE_CHECKS=0
	Running              []menuRunningBinary // live processes and the version each runs
	InstanceID           string
	Prompt               func(menubar.Dialog) (menubar.Action, error)
	Inform               func(title, message string)
	Refresh              func()
	OpenURL              func(string) error
}

// menuRunningBinary is one live process: the binary it started from and the
// engine version it runs, which stays fixed until it restarts.
type menuRunningBinary struct {
	Executable, Version string
}

type menuBarView struct {
	ID        string
	Session   menuBarSession
	Observed  menuBarObservation
	Service   memory.Service
	Connected time.Time
	Nickname  string
	Context   context.Context
	Sequence  uint64
	// Elsewhere is sessionRunsElsewhere, computed once per refresh.
	Elsewhere bool
}

func (v menuBarView) projectName() string {
	if v.Session.Unattached {
		return "No project"
	}
	name := v.Session.Project
	if name == "" || filepath.IsAbs(name) {
		name = filepath.Base(v.Session.Root)
	}
	return cleanMenuText(name, 70)
}

func (v menuBarView) clientName() string {
	if v.Session.Kind == "palace" {
		return "Memory Palace"
	}
	if !v.Observed.Initialized {
		return "MCP client (identifying…)"
	}
	if v.Observed.Client.Title != "" {
		return v.Observed.Client.Title
	}
	if v.Observed.Client.Name != "" {
		return v.Observed.Client.Name
	}
	return "Unknown MCP client"
}

// menuBarIdleAfter is how long a client may go without a tool call before the
// menu lists it as idle. Hosts such as the Codex app-server keep MCP processes
// alive long after their thread ended, so a live socket does not prove use.
const menuBarIdleAfter = 2 * time.Hour

func (v menuBarView) lastActive() time.Time {
	if v.Observed.At.IsZero() {
		return v.Connected
	}
	return v.Observed.At
}

// idle reports a session with no tool call for menuBarIdleAfter. The Memory
// Palace serves a dashboard rather than tool calls, so it is never idle.
func (v menuBarView) idle(now time.Time) bool {
	return v.Session.Kind != "palace" && now.Sub(v.lastActive()) > menuBarIdleAfter
}

// sessionRunsElsewhere reports an MCP process started outside its configured
// project root, which happens when a user-level client config pins
// --root/--project. A working directory inside the root is normal. The client
// resolves Cwd itself, so only the root is resolved here.
func sessionRunsElsewhere(s menuBarSession) bool {
	if s.Kind != "mcp" || s.Unattached || s.Cwd == "" || s.Root == "" {
		return false
	}
	root := s.Root
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	rel, err := filepath.Rel(root, s.Cwd)
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (v menuBarView) label() string {
	label := cleanMenuText(v.clientName(), 36) + " · " + v.shortID()
	if v.Elsewhere {
		label += " · running in " + cleanMenuText(filepath.Base(v.Session.Cwd), 30)
	}
	if v.Nickname != "" {
		label = v.Nickname + " · " + label
	}
	return label
}

func (v menuBarView) shortID() string {
	if len(v.ID) > 6 {
		return strings.ToUpper(v.ID[:6])
	}
	return strings.ToUpper(v.ID)
}

type menuBarGroup struct {
	key   string
	title string
	views []menuBarView
}

type menuBarControls struct {
	ctx     context.Context
	host    *menuBarHost
	models  chan menubar.Model
	changed chan struct{}
	updates *menuBarUpdates
	dreams  *menuBarDreams

	mu       sync.Mutex
	revision uint64
	dialog   *menubar.Dialog
	pending  map[string]chan menubar.Action
	gate     chan struct{}
	health   map[menuBarHealthKey]menuBarHealth
}

func newMenuBarControls(ctx context.Context, host *menuBarHost) *menuBarControls {
	return &menuBarControls{ctx: ctx, host: host, models: make(chan menubar.Model, 1), changed: make(chan struct{}, 1), updates: newMenuBarUpdates(), dreams: newMenuBarDreams(), pending: map[string]chan menubar.Action{}, gate: make(chan struct{}, 1), health: map[menuBarHealthKey]menuBarHealth{}}
}

func (c *menuBarControls) requestRefresh() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

func (c *menuBarControls) views() []menuBarView {
	c.host.mu.Lock()
	defer c.host.mu.Unlock()
	views := make([]menuBarView, 0, len(c.host.sessions))
	for peer := range c.host.sessions {
		views = append(views, menuBarView{ID: peer.id, Session: peer.session, Observed: peer.observed, Service: peer.svc, Connected: peer.connected, Nickname: peer.nickname, Context: peer.ctx, Sequence: peer.seq, Elsewhere: sessionRunsElsewhere(peer.session)})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Sequence < views[j].Sequence })
	return views
}

func groupMenuBarViews(views []menuBarView) []menuBarGroup {
	groups := make([]menuBarGroup, 0)
	indices := make(map[string]int)
	for _, view := range views {
		key := view.Session.Root + "\x00" + view.Session.Project
		if view.Session.Unattached {
			key = "\x00unattached"
		}
		index, exists := indices[key]
		if !exists {
			index = len(groups)
			indices[key] = index
			groups = append(groups, menuBarGroup{key: key, title: view.projectName()})
		}
		groups[index].views = append(groups[index].views, view)
	}
	// Two repositories can share the same basename. Show a parent-folder hint,
	// without placing full personal paths in the normal menu.
	counts := make(map[string]int)
	for _, group := range groups {
		counts[group.title]++
	}
	for i := range groups {
		if counts[groups[i].title] > 1 {
			root := groups[i].views[0].Session.Root
			groups[i].title += " · " + cleanMenuText(filepath.Base(filepath.Dir(root)), 30)
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].title == groups[j].title {
			return groups[i].key < groups[j].key
		}
		return groups[i].title < groups[j].title
	})
	return groups
}

func menuBarRow(title string) menubar.Item { return menubar.Item{Title: title} }
func menuBarAction(id, title string) menubar.Item {
	return menubar.Item{ID: id, Title: title, Enabled: true}
}
func menuBarSubmenu(title string, items []menubar.Item) menubar.Item {
	return menubar.Item{Title: title, Enabled: true, Items: items}
}
func menuBarSeparator() menubar.Item { return menubar.Item{Separator: true} }

func scopeMenuBarItems(items []menubar.Item, instanceID string) []menubar.Item {
	result := make([]menubar.Item, len(items))
	for i, item := range items {
		if item.ID != "" {
			item.ID = "session/" + instanceID + "/" + item.ID
		}
		if len(item.Items) != 0 {
			item.Items = scopeMenuBarItems(item.Items, instanceID)
		}
		result[i] = item
	}
	return result
}

func (c *menuBarControls) refresh() {
	views := c.views()
	groups := groupMenuBarViews(views)
	var sessions, dreams, health, projects []menubar.Item
	attention := false
	liveHealth := make(map[menuBarHealthKey]bool, len(views))
	now := time.Now()
	var idleRows []menubar.Item
	active := 0
	for _, group := range groups {
		var sessionRows, dreamRows, healthRows, projectRows []menubar.Item
		for _, view := range group.views {
			liveHealth[healthKey(view.Service)] = true
			idle := view.idle(now)
			status := "Connected · PID " + fmt.Sprint(view.Session.PID)
			if idle {
				status += " · last active " + now.Sub(view.lastActive()).Round(time.Minute).String() + " ago"
			}
			sessionItems := []menubar.Item{menuBarRow(status)}
			if view.Elsewhere {
				sessionItems = append(sessionItems, menuBarRow("Started in "+cleanMenuText(filepath.Base(view.Session.Cwd), 40)+", but configured for "+view.projectName()))
			}
			sessionItems = append(sessionItems, menuBarAction("open", "Open Memory Palace"), menuBarAction("details", "Instance Details…"), menuBarAction("rename", "Rename Instance…"), menuBarAction("health/copy", "Copy Redacted Diagnostics"))
			if view.Observed.Tool != "" {
				outcome := "completed"
				if view.Observed.Failed {
					outcome = "failed"
				}
				sessionItems = append(sessionItems, menuBarSeparator(), menuBarRow(view.Observed.Tool+" · "+outcome), menuBarRow(view.Observed.At.Local().Format("15:04:05")))
			}
			row := menuBarSubmenu(view.label(), scopeMenuBarItems(sessionItems, view.ID))
			if idle {
				row.Title = group.title + " · " + row.Title
				idleRows = append(idleRows, row)
			} else {
				active++
				sessionRows = append(sessionRows, row)
			}
			scope := c.scope(view)
			dreamItems, dreamAttention := c.dreams.Items(scope)
			dreamRows = append(dreamRows, menuBarSubmenu(view.label(), scopeMenuBarItems(dreamItems, view.ID)))
			healthItems, healthAttention := c.healthItems(view)
			healthRows = append(healthRows, menuBarSubmenu(view.label(), scopeMenuBarItems(healthItems, view.ID)))
			projectRows = append(projectRows, menuBarSubmenu(view.label(), scopeMenuBarItems(c.projectItems(view), view.ID)))
			attention = attention || !idle && (dreamAttention || healthAttention)
		}
		if len(sessionRows) > 0 {
			sessions = append(sessions, menuBarSubmenu(group.title, sessionRows))
		}
		dreams = append(dreams, menuBarSubmenu(group.title, dreamRows))
		health = append(health, menuBarSubmenu(group.title, healthRows))
		projects = append(projects, menuBarSubmenu(group.title, projectRows))
	}
	if len(idleRows) > 0 {
		sessions = append(sessions, menuBarSeparator(), menuBarSubmenu(fmt.Sprintf("Idle (%d) · no tool call for %s", len(idleRows), menuBarIdleAfter), idleRows))
	}
	var updates []menubar.Item
	if len(views) > 0 {
		var updateAttention bool
		updates, updateAttention = c.updates.Items(c.productScope(views))
		attention = attention || updateAttention
	}
	versions := []string{memory.Version}
	for _, view := range views {
		versions = append(versions, cleanMenuText(view.Session.EngineVersion, 40))
	}
	items := []menubar.Item{menuBarRow(menuBarProductTitle(versions, len(views))), menuBarSubmenu("Sessions", sessions), menuBarSeparator(), menuBarSubmenu("Dreaming", dreams), menuBarSubmenu("Updates", updates), menuBarSubmenu("Health & Diagnostics", health), menuBarSubmenu("Project Controls", projects)}
	c.mu.Lock()
	for key := range c.health {
		if !liveHealth[key] {
			delete(c.health, key)
		}
	}
	c.revision++
	model := menubar.Model{Revision: c.revision, Tooltip: fmt.Sprintf("Elephant · %d active, %d idle", active, len(idleRows)), Attention: attention, Items: items, Dialog: c.dialog}
	c.mu.Unlock()
	select {
	case c.models <- model:
	default:
		select {
		case <-c.models:
		default:
		}
		select {
		case c.models <- model:
		case <-c.ctx.Done():
		}
	}
}

func (c *menuBarControls) scope(view menuBarView) menuControlContext {
	ctx := view.Context
	return menuControlContext{Context: ctx, Service: view.Service, Executable: view.Session.Executable, InstanceID: view.ID, Prompt: func(dialog menubar.Dialog) (menubar.Action, error) { return c.prompt(ctx, dialog) }, Inform: func(title, message string) {
		_, _ = c.prompt(ctx, menubar.Dialog{Title: title, Message: message, Buttons: []menubar.Button{{ID: "ok", Title: "OK"}}, Default: "ok", Cancel: "ok"})
	}, Refresh: c.requestRefresh, OpenURL: c.host.openURL}
}

// menuBarProductTitle names every Elephant version running now: the companion
// and each connected engine. Blank versions are dropped.
func menuBarProductTitle(versions []string, connections int) string {
	seen := map[string]bool{}
	var unique []string
	for _, v := range versions {
		if v != "" && !seen[v] {
			seen[v] = true
			unique = append(unique, v)
		}
	}
	sort.Strings(unique)
	return fmt.Sprintf("Elephant %s · %d connections", strings.Join(unique, ", "), connections)
}

// productScope addresses Elephant as a whole: every connected binary and the
// elephant each session finds on its PATH, release status from every
// connected store, and the version each live process runs. It uses the host
// context so an update survives any single connection ending.
func (c *menuBarControls) productScope(views []menuBarView) menuControlContext {
	executables := make([]string, 0, len(views))
	var reported []string
	var stores []memory.Store
	var running []menuRunningBinary
	seenStore := map[string]bool{}
	disabled := false
	now := time.Now()
	for _, view := range views {
		executables = append(executables, view.Session.Executable)
		if view.Session.ElephantPath != "" {
			reported = append(reported, view.Session.ElephantPath)
		}
		if !seenStore[view.Service.Store.Path] {
			seenStore[view.Service.Store.Path] = true
			stores = append(stores, view.Service.Store)
		}
		// An idle process is not in use, so its older build must not hold
		// "restart needed" and attention for as long as the client keeps it.
		if !view.idle(now) {
			running = append(running, menuRunningBinary{Executable: view.Session.Executable, Version: view.Session.EngineVersion})
		}
		disabled = disabled || view.Session.UpdateChecksDisabled
	}
	if self, err := c.updates.self(); err == nil {
		running = append(running, menuRunningBinary{Executable: self, Version: memory.Version})
	}
	ctx := c.ctx
	return menuControlContext{Context: ctx, Service: views[0].Service, Executables: executables, Stores: stores, ReportedExecutables: reported, UpdateChecksDisabled: disabled, Running: running, Prompt: func(dialog menubar.Dialog) (menubar.Action, error) { return c.prompt(ctx, dialog) }, Inform: func(title, message string) {
		_, _ = c.prompt(ctx, menubar.Dialog{Title: title, Message: message, Buttons: []menubar.Button{{ID: "ok", Title: "OK"}}, Default: "ok", Cancel: "ok"})
	}, Refresh: c.requestRefresh, OpenURL: c.host.openURL}
}

func (c *menuBarControls) prompt(ctx context.Context, dialog menubar.Dialog) (menubar.Action, error) {
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return menubar.Action{}, ctx.Err()
	}
	defer func() { <-c.gate }()
	if err := ctx.Err(); err != nil {
		return menubar.Action{}, err
	}
	dialog.ID = "dialog/" + rand.Text()
	answer := make(chan menubar.Action, 1)
	c.mu.Lock()
	c.dialog, c.pending[dialog.ID] = &dialog, answer
	c.mu.Unlock()
	c.requestRefresh()
	defer func() {
		c.mu.Lock()
		delete(c.pending, dialog.ID)
		if c.dialog != nil && c.dialog.ID == dialog.ID {
			c.dialog = nil
		}
		c.mu.Unlock()
		c.requestRefresh()
	}()
	select {
	case action := <-answer:
		return action, nil
	case <-ctx.Done():
		return menubar.Action{}, ctx.Err()
	case <-c.ctx.Done():
		return menubar.Action{}, c.ctx.Err()
	}
}

// Called by the native pipe reader. It only routes responses or starts tracked
// work; native event delivery never waits for a dialog, network, or provider.
func (c *menuBarControls) action(action menubar.Action) {
	c.mu.Lock()
	pending := c.pending[action.ID]
	c.mu.Unlock()
	if pending != nil {
		select {
		case pending <- action:
		default:
		}
		return
	}
	if action.ID == menubar.HideAction {
		return
	}
	if strings.HasPrefix(action.ID, "updates/") {
		if views := c.views(); len(views) > 0 {
			c.start(func() { c.updates.Handle(c.productScope(views), action.ID) })
		}
		return
	}
	parts := strings.SplitN(action.ID, "/", 3)
	if len(parts) != 3 || parts[0] != "session" {
		return
	}
	var selected menuBarView
	for _, view := range c.views() {
		if view.ID == parts[1] {
			selected = view
			break
		}
	}
	if selected.ID == "" {
		return
	}
	c.start(func() { c.handle(selected, parts[2]) })
}

func (c *menuBarControls) start(work func()) {
	c.host.mu.Lock()
	if c.host.closing {
		c.host.mu.Unlock()
		return
	}
	c.host.wg.Add(1)
	c.host.mu.Unlock()
	go func() {
		defer c.host.wg.Done()
		work()
	}()
}

func (c *menuBarControls) handle(view menuBarView, action string) {
	scope := c.scope(view)
	if scope.Context.Err() != nil {
		return
	}
	switch action {
	case "open":
		url, err := c.host.dashboardURL(view.ID)
		if err == nil {
			err = scope.OpenURL(url)
		}
		if err != nil && scope.Context.Err() == nil {
			scope.Inform("Memory Palace unavailable", err.Error())
		}
	case "details":
		conversation := cleanMenuText(view.Session.Conversation, 160)
		if conversation == "" {
			conversation = "Not supplied by the client/configuration"
		}
		last := "No memory tool calls observed on this connection"
		if view.Observed.Tool != "" {
			last = view.Observed.Tool + " at " + view.Observed.At.Local().Format(time.RFC3339)
			if view.Observed.Failed {
				last += " (failed)"
			}
		}
		scope.Inform("Elephant Instance · "+view.shortID(), fmt.Sprintf("Project: %s\nClient: %s\nClient version: %s\nEngine: %s\nPID: %d\nConnected: %s\nConversation: %s\nRoot: %s\nWorking directory: %s\nStore: %s\nExecutable: %s\nLast activity: %s\n\nConnected does not mean an active chat or foreground task. Nicknames last for this connection.", view.projectName(), view.clientName(), view.Observed.Client.Version, cleanMenuText(view.Session.EngineVersion, 40), view.Session.PID, view.Connected.Local().Format(time.RFC3339), conversation, view.Session.Root, cleanMenuText(view.Session.Cwd, 300), view.Session.Store, view.Session.Executable, last))
	case "rename":
		answer, err := scope.Prompt(menubar.Dialog{Title: "Rename Instance", Message: "A nickname distinguishes windows on the same project. It lasts until this connection ends.", Buttons: []menubar.Button{{ID: "save", Title: "Save"}, {ID: "cancel", Title: "Cancel"}}, Default: "save", Cancel: "cancel", Input: true, Value: view.Nickname})
		if err == nil && answer.Choice == "save" {
			c.host.mu.Lock()
			for peer := range c.host.sessions {
				if peer.id == view.ID {
					peer.nickname = cleanMenuText(answer.Text, 40)
				}
			}
			c.host.mu.Unlock()
			c.requestRefresh()
		}
	default:
		if c.dreams.Handle(scope, action) || c.handleHealth(view, action) || c.handleProject(view, action) {
			return
		}
	}
}

var errMenuBarContextMismatch = errors.New("installed automatic hooks use another store, identity, project, or conversation; select the matching instance instead")
