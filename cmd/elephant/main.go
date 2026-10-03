package main

import (
	"bytes"
	"encoding/json"
	memory "example.com/elephant"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: elephant <version|doctor|selftest|setup|language|fingerprint|init|automation|experiences|task-status|update|hook|recall|remember|imprint|status|inspect|why|scars|map|stats|palace|feedback|forget|approve|mcp|export|import> [flags]; see QUICKSTART.md")
	}
	command := os.Args[1]
	if command == "version" {
		fmt.Println("Elephant " + memory.Version)
		return nil
	}
	if command == "help" || command == "--help" || command == "-h" {
		fmt.Println("Elephant " + memory.Version + "\nPersistent experience for coding agents.\nCommands: version, doctor, selftest, setup, language, fingerprint, init, automation, experiences, task-status, update, recall, remember, imprint, status, inspect, why, scars, map, stats, palace, feedback, forget, approve, mcp, export, import.\nLegacy aliases: profile, record, list, ui.\nStart with: elephant init --root /path/to/project\nUse elephant <command> --help for flags.")
		return nil
	}
	originalCommand := command
	aliases := map[string]string{"fingerprint": "profile", "remember": "record", "imprint": "record", "status": "list", "palace": "ui", "why": "recall"}
	if alias, ok := aliases[command]; ok {
		command = alias
	}
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	agent := f.String("agent", "both", "automatic adapter: codex, claude or both")
	configPath := f.String("config", "", "automation config path (hook command)")
	enabled := f.Bool("enabled", true, "pause or resume installed automatic memory")
	taskSession := f.String("task-session", "", "hashed task session supplied by a hook")
	taskID := f.String("task-id", "", "task receipt ID supplied by a hook")
	reviewComplete := f.Bool("review-complete", false, "acknowledge that the requested lesson review completed")
	jsonOutput := f.Bool("json", false, "structured task/update status instead of one line")
	updateCheck := f.Bool("check", false, "check published releases now")
	updateDismiss := f.Bool("dismiss", false, "dismiss the current release notice")
	initialize := f.Bool("initialize", false, "include project conventions in an explicit recall")
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	db := f.String("store", memory.DefaultStorePath(home), "journal path (existing legacy store is reused)")
	steTarget := f.Int("ste-target", 0, "STE writing target, 80 to 100; default 80. This is not a compliance score")
	text := f.String("text", "", "summary for local language checks")
	output := f.String("output", "", "setup directory (default: ./elephant-setup); existing files are preserved")
	wizard := f.Bool("wizard", false, "show the interactive setup steps")
	root := f.String("root", ".", "project directory")
	project := f.String("project", "", "stable project ID (default absolute root)")
	unattached := f.Bool("unattached", false, "no project: skip cwd/manifest profiling")
	conversation := f.String("conversation", "", "conversation provenance ID; required for conversation scope")
	tenant := f.String("tenant", "local", "trusted local tenant label")
	user := f.String("user", "me", "trusted local user label")
	team := f.String("team", "", "trusted local team label")
	task := f.String("task", "", "current task")
	contextFile := f.String("context-file", "", "JSON file with explicit current features (max 128 KiB)")
	budget := f.Int("budget", 4000, "hard context byte budget")
	limit := f.Int("limit", 6, "max lessons")
	file := f.String("file", "-", "input/output JSON path, - for stdio")
	id := f.String("id", "", "memory ID")
	feedbackID := f.String("feedback-id", "", "stable task/run ID")
	helpful := f.Bool("helpful", true, "observed usefulness")
	port := f.Int("port", 7331, "loopback UI port")
	if err := f.Parse(os.Args[2:]); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected positional argument %q; boolean flags use --helpful=false", f.Arg(0))
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	seenFlags := map[string]bool{}
	f.Visit(func(v *flag.Flag) { seenFlags[v.Name] = true })
	if command == "hook" {
		payload, e := io.ReadAll(io.LimitReader(os.Stdin, (1<<20)+1))
		if e != nil {
			return e
		}
		out, e := memory.RunHook(*configPath, *agent, bytes.NewReader(payload))
		if e != nil {
			fmt.Fprintln(os.Stderr, "Elephant automatic memory:", e)
			out = map[string]any{"systemMessage": "Elephant automatic memory failed. Run elephant automation and elephant doctor. See hook stderr for details."}
		}
		if e == nil {
			var h memory.HookInput
			if json.Unmarshal(payload, &h) == nil && h.Event == "SessionStart" && h.AgentID == "" {
				if c, err := memory.ReadAutomation(*configPath); err == nil && c.Enabled {
					if u, err := c.Service().Store.Updates(memory.UpdateOptions{Check: true, Notify: true}); err == nil && u.State == "available" && u.Line != "" {
						out["systemMessage"] = u.Line
					}
				}
			}
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	if command == "task-status" {
		r, e := memory.TaskSummary(*configPath, *taskSession, *reviewComplete, *taskID)
		if e != nil {
			return e
		}
		if *jsonOutput {
			return json.NewEncoder(os.Stdout).Encode(r)
		}
		fmt.Println(r.Line)
		return nil
	}
	if resolved, e := filepath.EvalSymlinks(abs); e == nil {
		abs = resolved
	}
	if c, found, e := memory.ConfigForRoot(abs); e != nil {
		return e
	} else if found && (command != "init" || c.Root == abs) {
		abs = c.Root
		if !seenFlags["store"] {
			*db = c.Store
		}
		if !seenFlags["tenant"] {
			*tenant = c.Identity.Tenant
		}
		if !seenFlags["user"] {
			*user = c.Identity.User
		}
		if !seenFlags["team"] {
			*team = c.Identity.Team
		}
		if !seenFlags["project"] && !seenFlags["unattached"] {
			*project = c.Project
			*unattached = c.Unattached
		}
		if !seenFlags["conversation"] {
			*conversation = c.Conversation
		}
		if !seenFlags["budget"] {
			*budget = c.Budget
		}
	}
	if *unattached && *project != "" {
		return fmt.Errorf("--unattached and --project are mutually exclusive")
	}
	if *project == "" && !*unattached {
		*project = abs
	}
	if *tenant == "" || *user == "" {
		return fmt.Errorf("tenant and user required")
	}
	svc := memory.Service{Store: memory.Store{Path: *db}, Identity: memory.Identity{Tenant: *tenant, User: *user, Team: *team}, Root: abs, Project: *project, Unattached: *unattached, Conversation: *conversation}
	storeAbs, e := filepath.Abs(svc.Store.Path)
	if e != nil {
		return e
	}
	svc.Store.Path = storeAbs
	if command == "record" && *taskSession != "" {
		c, capture, e := memory.CaptureForTask(*configPath, *taskSession, *taskID)
		if e != nil {
			return e
		}
		svc = c.Service()
		svc.Task = capture
	}
	if *steTarget != 0 {
		if _, e := svc.Store.SetLanguagePolicy(*steTarget); e != nil {
			return e
		}
	}
	printJSON := func(v any) error { enc := json.NewEncoder(os.Stdout); enc.SetIndent("", "  "); return enc.Encode(v) }
	input := func() ([]byte, error) {
		if *file == "-" {
			return io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
		}
		data, e := os.ReadFile(*file)
		if len(data) > 1<<20 {
			return nil, fmt.Errorf("input too large")
		}
		return data, e
	}
	switch command {
	case "update":
		var change *bool
		if seenFlags["enabled"] {
			change = enabled
		}
		r, e := svc.Store.Updates(memory.UpdateOptions{Check: *updateCheck, Force: *updateCheck, Notify: true, Dismiss: *updateDismiss, Enabled: change})
		if e != nil {
			return e
		}
		if *jsonOutput {
			return printJSON(r)
		}
		if r.Line != "" {
			fmt.Println(r.Line)
		} else {
			fmt.Printf("Elephant: update status %s.\n", r.State)
		}
		return nil
	case "init":
		binary, e := os.Executable()
		if e != nil {
			return e
		}
		r, e := memory.InitAutomation(svc, binary, *agent, *budget)
		if e != nil {
			return e
		}
		return printJSON(r)
	case "automation":
		var change *bool
		if seenFlags["enabled"] {
			change = enabled
		}
		r, e := memory.Automation(abs, change)
		if e != nil {
			return e
		}
		return printJSON(r)
	case "experiences":
		p, e := svc.Profile()
		if e != nil {
			return e
		}
		d, e := svc.Store.Dashboard(svc.Identity, p)
		if e != nil {
			return e
		}
		return printJSON(map[string]any{"total": d.ExperienceCount, "recent": d.Experiences})
	case "language":
		policy, e := svc.Store.LanguagePolicy()
		if e != nil {
			return e
		}
		if *text != "" {
			r := memory.CheckLanguage(policy, *text)
			if err := printJSON(r); err != nil {
				return err
			}
			if !r.Accepted {
				return fmt.Errorf("summary fails local STE checks")
			}
			return nil
		}
		return printJSON(policy)
	case "inspect", "scars", "map", "stats":
		p, e := svc.Profile()
		if e != nil {
			return e
		}
		d, e := svc.Store.Dashboard(svc.Identity, p)
		if e != nil {
			return e
		}
		switch command {
		case "map":
			return printJSON(memory.BuildMemoryMap(d.Memories))
		case "stats":
			return printJSON(map[string]any{"memories": len(d.Memories), "recalls": len(d.Usage), "helpful": d.Helpful, "unhelpful": d.Unhelpful, "estimated_tokens_avoided": d.EstimatedTokensAvoided, "recall_tokens_injected_estimate": d.InjectedTokens, "baseline": "all eligible memory summaries; not historical conversations"})
		case "scars":
			scars := []memory.Memory{}
			for _, m := range d.Memories {
				if !m.Retired && memory.MemoryClass(m) == "scar" {
					scars = append(scars, m)
				}
			}
			return printJSON(scars)
		case "inspect":
			if *id == "" {
				return fmt.Errorf("inspect requires --id")
			}
			for _, m := range d.Memories {
				if m.ID == *id {
					return printJSON(m)
				}
			}
			return fmt.Errorf("memory not found or not visible")
		}
		return nil
	case "doctor":
		d, e := memory.Doctor(svc)
		if err := printJSON(d); err != nil {
			return err
		}
		return e
	case "selftest":
		d, e := memory.SelfTest()
		if err := printJSON(d); err != nil {
			return err
		}
		return e
	case "setup":
		binary, e := os.Executable()
		if e != nil {
			return e
		}
		if *wizard {
			return memory.SetupWizard(svc, binary, *output, os.Stdin, os.Stdout)
		}
		config, e := memory.SetupConfig(svc, binary)
		if e != nil {
			return e
		}
		if *output == "" {
			*output = "elephant-setup"
		}
		dir, e := filepath.Abs(*output)
		if e != nil {
			return e
		}
		if e = memory.WriteSetup(dir, config); e != nil {
			return e
		}
		return printJSON(map[string]any{"ok": true, "output": dir, "config": config})
	case "profile":
		p, e := svc.Profile()
		if e != nil {
			return e
		}
		return printJSON(p)
	case "recall":
		if originalCommand == "why" && *task == "" {
			return fmt.Errorf("why requires --task; it explains Recall for the current task")
		}
		p, e := svc.Profile()
		if e != nil {
			return e
		}
		if *contextFile != "" {
			data, e := os.ReadFile(*contextFile)
			if e != nil {
				return e
			}
			if len(data) > 128<<10 {
				return fmt.Errorf("context file too large")
			}
			var context struct {
				Features map[string][]string `json:"features"`
			}
			if e = json.Unmarshal(data, &context); e != nil {
				return e
			}
			if e = memory.ValidateLabels(context.Features); e != nil {
				return e
			}
			for k, v := range context.Features {
				p.Features[k] = v
			}
		}
		r, e := svc.Store.Recall(svc.Identity, memory.Request{Profile: p, Task: *task, ByteBudget: *budget, Limit: *limit, Initialize: *initialize})
		if e != nil {
			return e
		}
		if originalCommand == "why" && *id != "" {
			for _, h := range r.Hits {
				if h.ID == *id {
					return printJSON(h)
				}
			}
			return printJSON(map[string]any{"id": *id, "selected": false, "reason": "Memory was not selected for this task, context, scope and budget.", "recall": r})
		}
		return printJSON(r)
	case "record":
		b, e := input()
		if e != nil {
			if svc.Task != nil {
				_ = svc.Store.ObserveCaptureFailure(svc.Identity, svc.Project, svc.Task)
			}
			return e
		}
		v, e := svc.Call("record_memory", b)
		if e != nil {
			if svc.Task != nil {
				t := svc.Task
				_ = svc.Store.ObserveCaptureFailure(svc.Identity, svc.Project, t)
			}
			return e
		}
		return printJSON(v)
	case "feedback":
		return svc.Store.Feedback(svc.Identity, *project, *id, *feedbackID, *helpful, *conversation)
	case "forget":
		return svc.Store.Retire(svc.Identity, *id)
	case "approve":
		return svc.Store.Approve(svc.Identity, *id)
	case "list":
		p, e := svc.Profile()
		if e != nil {
			return e
		}
		d, e := svc.Store.Dashboard(svc.Identity, p)
		if e != nil {
			return e
		}
		return printJSON(d)
	case "mcp":
		return memory.ServeMCP(svc, os.Stdin, os.Stdout)
	case "ui":
		return memory.ServeDashboard(svc, *port)
	case "export":
		data, e := memory.ExportTeam(svc)
		if e != nil {
			return e
		}
		b, e := json.MarshalIndent(data, "", "  ")
		if e != nil {
			return e
		}
		if *file == "-" {
			_, e = os.Stdout.Write(append(b, '\n'))
			return e
		}
		return os.WriteFile(*file, append(b, '\n'), 0600)
	case "import":
		b, e := input()
		if e != nil {
			return e
		}
		var bundle []memory.Memory
		if e = json.Unmarshal(b, &bundle); e != nil {
			return e
		}
		ids, e := memory.ImportTeam(svc, bundle)
		if e != nil {
			return e
		}
		return printJSON(ids)
	}
	return fmt.Errorf("unknown command %q", command)
}
