# Automatic memory

From a project directory, run:

```sh
elephant init
```

This installs the Codex and Claude Code command hooks for that directory. Use `--agent codex` or `--agent claude` to install one adapter. Use an installed binary at a stable path. Restart the agent, complete its normal project trust steps, and approve the hooks. In Codex, open `/hooks` and trust Elephant's hooks. Changing hook commands can require approval again. Elephant does not change host trust, permissions or sandbox policy.

After that, work normally. You do not need to ask for recall or remind the agent to record each task. No extra model key or MCP registration is required for this route. The coding agent already in use performs the lesson review.

## What happens

| Event | Action |
| --- | --- |
| Session start | Fingerprint the project and recall relevant project knowledge. |
| Task submitted | Recall against the current task within a 4,000-byte default budget. |
| Tool returns | Save an Experience with the tool name, status and explicit exit code when supplied. |
| Agent finishes | Request one review of observed work. The agent can save zero to three useful lessons. |
| Next related task | Recall the saved lesson if its scope, task terms and conditions match. |

The finish hook continues the agent once to perform its review. It does not repeatedly block completion. It respects the host's `stop_hook_active` guard and also keeps a local per-session review guard. A new user task resets the guard. Hook errors report a warning and let the agent continue.

The engine does not derive a lesson from an exit code. The agent uses evidence already in its context. A test failure may produce a Warning or Scar only when the agent can explain what failed and what the evidence supports. Project scope is the default in review instructions. Personal scope is for transferable knowledge. Team promotion still requires review.

## Check it, pause it

```sh
elephant automation                 # Config and events actually received
elephant experiences                # Latest 100 events in this project
elephant status                     # Memory inventory and recall activity
elephant palace                     # Activity → Automatic capture
elephant automation --enabled=false # Pause all Elephant hook actions
elephant automation --enabled=true  # Resume
```

These commands find the nearest `.elephant/automation.json`, including when run from a project subdirectory. They reuse its store and identity. Explicit CLI flags override those defaults. `automation` changes the installed project's configuration; it does not accept a different journal via `--store`.

A configured hook does not prove the host has loaded it. Received events establish that it ran. `review_requested` does not prove a Memory was saved. Inspect actual Memories and their sources to verify learning. A completed tool call does not prove a task succeeded, and recall does not automatically count as a Save.

## Local files and data

`init` merges these files while preserving other settings and instructions:

- `.elephant/automation.json`: absolute binary/root/store paths, identity, budget, adapters and pause state.
- `.codex/hooks.json`: Codex hooks.
- `.claude/settings.local.json`: Claude Code hooks, including tool failure events.
- `AGENTS.md` and `CLAUDE.md`: a bounded Elephant guidance block.
- `.gitignore`: local config and backup exclusions.

Existing changed files receive content-addressed backups beside them. Repeating init is idempotent. Invalid JSON or symlink config paths stop installation. `init` can add another adapter without removing installed ones. Remove Elephant's command entries from the host hook file and its marked guidance block to uninstall an adapter. Do not remove other hooks or policies.

The append-only store holds Experience metadata and compact Memories. Prompts are used for retrieval but are not stored. Hooks do not open transcript paths or retain tool arguments, tool output, the last assistant message or raw error text. Session IDs are hashed. Review guards live beside the journal in its `.sessions` directory. This is local trusted storage, not an authenticated enterprise service. Version 0.5 introduces `experience` journal events; older binaries will reject a journal containing these new events.

## Conversations without a project

In a working directory for the conversation, run:

```sh
elephant init --unattached --conversation research-001
```

The hooks stay bound to that directory, but Experiences have no project and the profiler skips repository manifests. Review instructions use personal scope. Signals and task terms still support recall. Unknown required facts continue to block a conditional Memory. This does not infer or assign a project from private conversations.

## Other agents and compatibility

Automatic installation currently supports macOS/Linux Codex and Claude Code hosts that implement the documented command hooks. Windows users and other agents can use `elephant setup` for MCP config and agent guidance, or connect the CLI JSON interface. Cursor and Pi are not claimed as automatic hook integrations.

The CLI `init` command now installs automation. Scripts that used the old discovery-only command must use `elephant recall --initialize`. The MCP `init_memory` tool is unchanged. If hooks already supplied Recall, agent guidance asks the agent not to repeat its routine `init_memory` call. Manual MCP calls remain available.

Hook adapters use the same engine and store as CLI/MCP. No agent-specific retrieval rules are added. Current retrieval uses task terms, fingerprint matches and applicability gates, not embeddings. Tool metadata increases journal size; large-history replay and automatic retention need further work.

## Validation

Run `go test -race ./...` and `python3 scripts/automation_acceptance.py ./elephant`. The subprocess check executes the exact generated hook and record commands, including paths with spaces, quotes and shell metacharacters. It simulates the host agent's extraction step. See [VALIDATION.md](VALIDATION.md) for native-host validation limits.

Hook contracts checked against official documentation on 2026-10-03:

- [Codex hooks](https://learn.chatgpt.com/docs/hooks)
- [Claude Code hooks](https://code.claude.com/docs/en/hooks)
