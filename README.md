# Elephant

Persistent experience for coding agents.

Elephant stores source-backed lessons from development work and recalls relevant experience for the next task. A local Go binary provides automatic Codex and Claude Code hooks, an MCP server, a JSON CLI, and the Memory Palace dashboard.

## Features

- Relevant recall within a configurable context budget, using task terms, project signals, and applicability conditions.
- Personal, project, conversation, and reviewed team memory scopes.
- Automatic task hooks with evidence-based lesson review and a factual one-line status.
- SQLite WAL storage, transactional receipts, legacy migration, and verified backup/restore.
- A local dashboard and optional, cached release notifications.
- No separate model key, database service, or cloud account required.

## Installation

Download the matching ZIP from [Releases](https://github.com/prithivrajmu/elephant/releases). Prebuilt binaries do not require Go. The current `0.6.0-pilot` release is a prerelease. Repository access is required while this repository is private.

| Computer | Archive platform |
| --- | --- |
| Apple Silicon Mac | `darwin-arm64` |
| Intel Mac | `darwin-amd64` |
| Linux x86-64 | `linux-amd64` |
| Linux ARM64 | `linux-arm64` |
| Windows x86-64 | `windows-amd64` |
| Windows ARM64 | `windows-arm64` |

Extract the ZIP and run its checksum-verifying installer from the extracted folder.

macOS and Linux:

```sh
sh ./install.sh
export PATH="$HOME/.local/bin:$PATH"
elephant selftest
```

Windows PowerShell:

```powershell
.\install.ps1
$env:Path = "$env:LOCALAPPDATA\Elephant\bin;$env:Path"
elephant selftest
```

The PATH changes above apply to the current terminal. See [Installation](docs/installation.md) for persistent PATH setup, authenticated downloads, upgrades, and source builds. Packages are currently unsigned.

## Quick start

For Codex or Claude Code on macOS/Linux, run this in your project:

```sh
elephant init
elephant doctor
```

Restart your agent and approve its Elephant hooks through the host's normal approval flow. Work normally: hooks recall relevant memories, observe tool metadata, and request a short lesson review after the task. A saved lesson requires evidence and an acknowledged write; a task may produce no reusable lesson.

Open the local dashboard with `elephant palace`. For Windows, other MCP clients, or explicit tool access, use `elephant setup --wizard` and follow [Client integration](docs/clients.md).

## Documentation

- [Quick start](docs/quickstart.md) and [CLI reference](docs/cli-reference.md)
- [Automatic hooks](docs/automation.md) and [client integration](docs/clients.md)
- [Memory Palace](docs/dashboard.md)
- [Storage, migration, and recovery](docs/storage.md)
- [Update notifications](docs/updates.md)
- [Architecture](docs/architecture.md), [validation](docs/validation.md), and [roadmap](docs/roadmap.md)
- [Release notes](docs/releases/v0.6.0-pilot.md)

## Security and privacy

Elephant stores lessons and tool-event metadata locally. It does not read transcripts or retain raw prompts, tool arguments, or tool output. Recalled memories are untrusted evidence; current project instructions take precedence. Local tenant/user labels are configuration, not enterprise authentication. See [Security](docs/security.md).

## Contributing

Bug reports and pull requests are welcome. Source builds require Go 1.25 or later. See [Contributing](docs/contributing.md) for development commands and review expectations.

## License

[MIT](LICENSE).
