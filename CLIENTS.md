# Client connection recipes

Run `elephant setup` first. It emits absolute command/argument paths with the selected journal and context. Preserve other server entries when merging config. These recipes are checked against the official documentation below; this environment has not run Codex, Claude Code or Cursor as real clients. A separate subprocess MCP client validates the protocol lifecycle.

| Client | Generated content | Where to merge |
| --- | --- | --- |
| Codex | `codex-mcp.toml` | Your Codex configuration's `[mcp_servers.elephant]` table. |
| Claude Code | `mcp.json` | Project-root `.mcp.json` under `mcpServers`; or use the CLI's stdio server registration. |
| Cursor | `mcp.json` | Project `.cursor/mcp.json` under `mcpServers`, or the global `~/.cursor/mcp.json`. |
| Other MCP host | `mcp.json` | The host's local stdio server settings; translate format if needed. |
| Custom harness | CLI JSON | Run `init`/`recall`, inject `context`, call `record` and `feedback` after observed outcomes. |

Add the emitted `AGENT_INSTRUCTIONS.md` content to the project's existing agent guidance (for example AGENTS.md or CLAUDE.md, as appropriate). Keep existing policies. The generated text tells the agent when to recall, record evidence and give observed feedback; current policy takes precedence over remembered lessons.

Reload the client, approve the server if requested, and confirm all six tools. Test `profile_memory` and `init_memory` before relying on capture. MCP prompt support is optional to the user experience; direct tool requests work without slash commands. A local stdio server must run on the machine/container where the executable and project paths exist; it is not a remote URL service.

Official references, consulted 2026-10-03:

- Codex MCP: https://developers.openai.com/codex/mcp/ (redirects to the current ChatGPT developer guide).
- Claude Code MCP: https://code.claude.com/docs/en/mcp
- Cursor MCP and configuration locations: https://prod.cursor.com/docs/mcp

Client formats may change. Report actual host/version plus sanitized setup failure output during the pilot. Do not treat these recipes as cross-client certification.
