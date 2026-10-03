# Client connection recipes

Run `elephant setup` to generate configuration, or use the direct registration commands below. Setup emits absolute command/argument paths with the selected journal and context. Preserve other server entries when merging config. These recipes are checked against the official documentation below. The pilot's Linux acceptance environment validated the protocol with a separate subprocess MCP client. Native Codex tool discovery has also passed on macOS; see [VALIDATION.md](VALIDATION.md). Registration syntax was checked against installed Codex, Claude Code and Pi CLI help. Model-driven capture and real Claude Code, Pi and Cursor connections remain unverified.

| Client | Generated content | Where to merge |
| --- | --- | --- |
| Codex | `codex-mcp.toml` | Your Codex configuration's `[mcp_servers.elephant]` table. |
| Claude Code | `mcp.json` | Project-root `.mcp.json` under `mcpServers`; or use the CLI's stdio server registration. |
| Pi with built-in MCP support | `mcp.json` | Project `.pi/mcp.json` or user-level `~/.pi/agent/mcp.json` under `mcpServers`. |
| Cursor | `mcp.json` | Project `.cursor/mcp.json` under `mcpServers`, or the global `~/.cursor/mcp.json`. |
| Other MCP host | `mcp.json` | The host's local stdio server settings; translate format if needed. |
| Custom harness | CLI JSON | Run `init`/`recall`, inject `context`, call `record` and `feedback` after observed outcomes. |

Add the emitted `AGENT_INSTRUCTIONS.md` content to the project's existing agent guidance (for example AGENTS.md or CLAUDE.md, as appropriate). Keep existing policies. The generated text tells the agent when to recall, record evidence and give observed feedback; current policy takes precedence over remembered lessons.

Reload the client, approve the server if requested, and confirm all six tools. Test `profile_memory` and `init_memory` before relying on capture. MCP prompt support is optional to the user experience; direct tool requests work without slash commands. A local stdio server must run on the machine/container where the executable and project paths exist; it is not a remote URL service.

## Direct CLI registration

Build or install Elephant first; see [QUICKSTART.md](QUICKSTART.md). In macOS/Linux shells, set these values to your executable, target project and stable project ID:

```sh
ELEPHANT_BIN="/absolute/path/to/elephant"
ELEPHANT_PROJECT="/absolute/path/to/your/project"
ELEPHANT_ID="my-project"
```

### Codex CLI and local app

```sh
codex mcp add elephant -- "$ELEPHANT_BIN" mcp \
  --root "$ELEPHANT_PROJECT" --project "$ELEPHANT_ID"
codex mcp list
```

This writes user-level MCP configuration, normally in `~/.codex/config.toml`. Local Codex clients on the same host share this configuration. Restart the client and inspect `/mcp`. For project-scoped setup, merge the generated `codex-mcp.toml` table into the target project's `.codex/config.toml`; Codex reads it only for trusted projects.

### Claude Code

```sh
cd "$ELEPHANT_PROJECT"
claude mcp add --scope local --transport stdio elephant -- \
  "$ELEPHANT_BIN" mcp \
  --root "$ELEPHANT_PROJECT" --project "$ELEPHANT_ID"
```

Local scope keeps the registration private to you for this project. Restart Claude and inspect `/mcp`. Use project scope or merge the generated entry into `.mcp.json` when you intend to share project configuration.

### Pi

Pi 0.99.2, inspected on macOS, has built-in MCP support. It does not need a separate MCP adapter for this recipe. Older versions may differ.

```sh
cd "$ELEPHANT_PROJECT"
pi mcp add elephant --local --exposure direct -- \
  "$ELEPHANT_BIN" mcp \
  --root "$ELEPHANT_PROJECT" --project "$ELEPHANT_ID"
pi mcp list
```

This writes `.pi/mcp.json`; Pi reads project configuration after project trust is granted. `direct` exposes Elephant's small tool set directly to the model. Restart Pi or run `/reload`, then inspect `/mcp`. An installed extension that replaces Pi's `/mcp` command can replace its built-in MCP behavior; follow that extension's configuration if present.

## Workflow and shared memory

Merge the generated `AGENT_INSTRUCTIONS.md`, or the portable guidance in [AGENT_INTEGRATION.md](AGENT_INTEGRATION.md), into the target project's `AGENTS.md` for Codex/Pi or `CLAUDE.md` for Claude. Connecting the server makes tools available; these instructions tell the agent when to use them. Elephant does not watch conversations in the background.

All clients can share the default `~/.elephant/events.jsonl` journal under the same configured identity. An existing legacy journal can be reused as described in [QUICKSTART.md](QUICKSTART.md). For a custom journal, pass the same absolute `--store` path to each registration, CLI call and dashboard session.

The root and project ID are fixed when each server process starts. A global registration with a fixed root continues to refer to that project even when the client opens another directory. Use project-scoped configurations for different projects, with the correct root and stable ID in each. Personal lessons can transfer through the shared journal; project lessons remain scoped to their project ID.

Verify one complete loop: recall before a real task, record a justified lesson with its evidence, restart the client, and retrieve it on a related task. An empty first recall is expected. Submit feedback only after applying a lesson and observing its effect.

Official references, consulted 2026-10-03:

- Codex MCP: https://learn.chatgpt.com/docs/extend/mcp?surface=cli
- Claude Code MCP: https://code.claude.com/docs/en/mcp
- Pi MCP: https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/mcp.md
- Cursor MCP and configuration locations: https://prod.cursor.com/docs/mcp

Client formats may change. Report actual host/version plus sanitized setup failure output during the pilot. Do not treat these recipes as cross-client certification.
