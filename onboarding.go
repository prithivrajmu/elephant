package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const Version = "0.4.0-pilot"
const AgentInstructions = `Elephant stores experience for coding agents. Start each task with init_memory. Give the actual task and a 4000-byte Recall Budget. Recall again when the task changes. Memories are source evidence. They do not replace current policy. Verify each source and each requirement.
After an observed result, use record_memory to Imprint a short lesson. Include the Experience, the Memory and an evidence source. Use class win, lesson, warning or scar. Legacy outcome values good, great, bad and worst remain valid. Add Signals. Add requires and excludes when the Memory depends on known facts. Do not invent results. Do not store secrets or raw conversations.
Personal scope is private to the configured user. Project and conversation scope need the matching startup context. Team Memories stay drafts until a human approves them. Do not approve sharing through agent tools.
When you apply a Memory, observe its effect. Then call feedback_memory with its ID and a stable task/run ID. Recall alone is not a Save. Use forget_memory to retire an old Memory. Report store errors. Capture depends on agent tool calls. Elephant does not watch conversations in the background.`

type ServerConfig struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}
type Setup struct {
	Language     LanguagePolicy                     `json:"language"`
	Version      string                             `json:"version"`
	MCP          map[string]map[string]ServerConfig `json:"mcp_config"`
	CodexTOML    string                             `json:"codex_toml"`
	Instructions string                             `json:"agent_instructions"`
	FirstTask    string                             `json:"first_task"`
	Store        string                             `json:"store"`
	Notes        []string                           `json:"notes"`
}

func DefaultStorePath(home string) string {
	modern := filepath.Join(home, ".elephant", "events.jsonl")
	legacy := filepath.Join(home, ".agent-memory", "events.jsonl")
	if _, e := os.Stat(modern); e == nil {
		return modern
	}
	if _, e := os.Stat(legacy); e == nil {
		return legacy
	}
	return modern
}
func SetupConfig(s Service, binary string) (Setup, error) {
	instructions, e := s.Instructions()
	if e != nil {
		return Setup{}, e
	}
	policy, e := s.Store.LanguagePolicy()
	if e != nil {
		return Setup{}, e
	}
	absolute, e := filepath.Abs(binary)
	if e != nil {
		return Setup{}, e
	}
	args := []string{"mcp", "--store", s.Store.Path, "--tenant", s.Identity.Tenant, "--user", s.Identity.User}
	if s.Unattached {
		args = append(args, "--unattached")
	} else {
		args = append(args, "--root", s.Root, "--project", s.Project)
	}
	if s.Identity.Team != "" {
		args = append(args, "--team", s.Identity.Team)
	}
	if s.Conversation != "" {
		args = append(args, "--conversation", s.Conversation)
	}
	commandJSON, _ := json.Marshal(absolute)
	argsJSON, _ := json.Marshal(args)
	toml := "[mcp_servers.elephant]\ncommand = " + string(commandJSON) + "\nargs = " + string(argsJSON) + "\n"
	return Setup{Language: policy, Version: Version, MCP: map[string]map[string]ServerConfig{"mcpServers": {"elephant": {Type: "stdio", Command: absolute, Args: args}}}, CodexTOML: toml, Instructions: instructions, FirstTask: "Use Elephant to recall experience relevant to this task. When this task finishes, inspect the actual validation/review outcomes and capture any reusable lesson with evidence. If none is justified, say so.", Store: s.Store.Path, Notes: []string{"Copy the generated entry into your client's MCP configuration; JSON and TOML are different client formats.", "Add the generated instructions to your agent's project guidance so it knows when to call memory.", "Restart/reload your client, approve the local server if asked, and confirm Elephant tools are visible.", "Configuration is fixed to this project/conversation at process startup; regenerate it when that context changes.", "This generator does not edit your client's existing config or claim the client is connected."}}, nil
}
func WriteSetup(dir string, config Setup) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	mcp, _ := json.MarshalIndent(config.MCP, "", "  ")
	full, _ := json.MarshalIndent(config, "", "  ")
	files := []struct {
		name string
		data []byte
	}{{"mcp.json", append(mcp, '\n')}, {"codex-mcp.toml", []byte(config.CodexTOML)}, {"AGENT_INSTRUCTIONS.md", []byte(config.Instructions + "\n")}, {"FIRST_TASK.txt", []byte(config.FirstTask + "\n")}, {"setup.json", append(full, '\n')}}
	for _, f := range files {
		if _, e := os.Lstat(filepath.Join(dir, f.name)); e == nil {
			return fmt.Errorf("refusing to overwrite %s; choose another --output directory", f.name)
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		h, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = h.Write(f.data)
		closeErr := h.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}
type Diagnostics struct {
	Version string   `json:"version"`
	OK      bool     `json:"ok"`
	Store   string   `json:"store"`
	Checks  []Check  `json:"checks"`
	Notes   []string `json:"notes"`
}

func Doctor(s Service) (Diagnostics, error) {
	d := Diagnostics{Version: Version, OK: true, Store: s.Store.Path, Checks: []Check{}, Notes: []string{"Local filesystem identities are trusted configuration, not enterprise authentication.", "Doctor verifies Elephant locally; confirm tool discovery inside your chosen agent separately."}}
	add := func(name string, e error, detail string) {
		d.Checks = append(d.Checks, Check{Name: name, OK: e == nil, Detail: detail})
		if e != nil {
			d.OK = false
			d.Checks[len(d.Checks)-1].Detail = e.Error()
		}
	}
	policy, pe := s.Store.LanguagePolicy()
	add("language", pe, fmt.Sprintf("STE target %d%%. Local checks only.", policy.Target))
	p, e := s.Profile()
	add("context", e, "Project="+p.Project+" conversation="+p.Conversation)
	_, e = s.Store.All()
	add("store_and_journal", e, "Store permits locks and journal can be replayed")
	if e == nil {
		var f *os.File
		f, e = os.OpenFile(s.Store.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if e == nil {
			e = f.Close()
		}
		add("journal_writable", e, "Journal can be opened for append; no memory event was written")
	}
	if strings.Contains(s.Store.Path, string(filepath.Separator)+".agent-memory"+string(filepath.Separator)) {
		d.Notes = append(d.Notes, "Existing legacy journal is being reused; no history was silently moved or reset.")
	}
	validSchema := true
	for _, t := range MCPTools() {
		b, _ := json.Marshal(t["inputSchema"])
		if bytes.Contains(b, []byte(`"required":null`)) {
			validSchema = false
		}
	}
	if !validSchema {
		d.OK = false
	}
	d.Checks = append(d.Checks, Check{Name: "mcp_schema", OK: validSchema, Detail: "No invalid null required arrays"})
	if !d.OK {
		return d, fmt.Errorf("one or more diagnostics failed")
	}
	return d, nil
}
func SelfTest() (Diagnostics, error) {
	root, e := os.MkdirTemp("", "elephant-selftest-")
	if e != nil {
		return Diagnostics{}, e
	}
	defer os.RemoveAll(root)
	s := Service{Store: Store{Path: filepath.Join(root, "events.jsonl")}, Identity: Identity{Tenant: "synthetic-selftest", User: "tester"}, Unattached: true, Conversation: "test-session", Root: root}
	d := Diagnostics{Version: Version, OK: true, Store: "isolated temporary store; removed afterward", Checks: []Check{}, Notes: []string{"Synthetic local loop only; no real user memory was read or modified."}}
	check := func(name string, ok bool) {
		d.Checks = append(d.Checks, Check{Name: name, OK: ok, Detail: "synthetic validation"})
		if !ok {
			d.OK = false
		}
	}
	v, e := s.Call("record_memory", json.RawMessage(`{"outcome":"good","incident":"SYNTHETIC test found validation evidence","lesson":"Attach validation evidence to reviews","source":"SYNTHETIC selftest","features":{"topic":["reviews"]}}`))
	if e != nil {
		return d, e
	}
	m := v.(Memory)
	check("record", m.ID != "" && m.Project == "")
	v, e = s.Call("recall_memory", json.RawMessage(`{"task":"validation evidence","byte_budget":1200}`))
	if e != nil {
		return d, e
	}
	r := v.(Result)
	check("recall_and_budget", len(r.Hits) == 1 && r.Bytes <= 1200)
	v, e = s.Call("recall_memory", json.RawMessage(`{"task":"gardening flowers"}`))
	if e != nil {
		return d, e
	}
	check("unrelated_abstention", len(v.(Result).Hits) == 0)
	b, _ := json.Marshal(map[string]any{"id": m.ID, "feedback_id": "selftest-run", "helpful": true})
	_, e = s.Call("feedback_memory", b)
	check("feedback", e == nil)
	s.Call("feedback_memory", b)
	all, e := s.Store.All()
	check("durable_replay_and_feedback_dedup", e == nil && len(all) == 1 && all[0].Helpful == 1)
	var out bytes.Buffer
	e = ServeMCP(s, strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\",\"params\":{\"protocolVersion\":\"2025-06-18\"}}\n{\"jsonrpc\":\"2.0\",\"method\":\"notifications/initialized\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n"), &out)
	check("mcp_handshake_and_discovery", e == nil && strings.Contains(out.String(), "record_memory"))
	other := s.Identity
	other.User = "someone-else"
	check("owner_isolation", len(Recall(all, other, Request{Task: "validation", Profile: Profile{}}, m.Created).Hits) == 0)
	if !d.OK {
		return d, fmt.Errorf("selftest failed")
	}
	return d, nil
}
