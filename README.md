# Elephant

Persistent experience for coding agents.

> **Status: closed beta.** Elephant is a pre-1.0 project. See [Status: Beta](#status-beta) for known limitations before you rely on it.

Elephant stores source-backed lessons from development work and recalls relevant experience for the next task. A local Go binary provides automatic Codex and Claude Code hooks, an MCP server, a JSON CLI, and the Memory Palace dashboard.

## Features

- Relevant recall within a configurable context budget, using task terms, project signals, and applicability conditions.
- Personal, project, conversation, and reviewed team memory scopes.
- Automatic task hooks with evidence-based lesson review and a factual one-line status.
- SQLite WAL storage, transactional receipts, legacy migration, and verified backup/restore.
- A local dashboard and cached release notifications (a default daily GitHub check that you can disable).
- No separate model key, database service, or cloud account required for local use.
- Optional, experimental authenticated Cloudflare hosting, explicit cloud sync, reviewed team sharing, and MCP evidence connectors.

## Installation

Fastest (needs Node 18+; downloads and checksum-verifies the binary):

```sh
npx elephant-memory          # install
npx elephant-memory init     # in a project: enable automatic memory for Codex/Claude Code
npx elephant-memory connect  # pi and omp: register the MCP server and guidance (user-level, all projects)
```

pi users can instead run `pi install npm:elephant-memory`, which registers the MCP server and an `elephant` skill with no config edits.

Or one line without Node (verifies the release checksum, installs to `~/.local/bin`):

```sh
curl -fsSL https://raw.githubusercontent.com/prithivrajmu/elephant/main/scripts/get.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/prithivrajmu/elephant/main/scripts/get.ps1 | iex
```

Pin a version with `ELEPHANT_VERSION=v0.9.1`. See the [0.9.1 release notes](docs/releases/v0.9.1.md). Or install manually:

Download the matching ZIP from [Releases](https://github.com/prithivrajmu/elephant/releases). Prebuilt binaries do not require Go. Version 0.9.1 uses a regular release tag; the project's beta limitations still apply.

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

The PATH changes above apply to the current terminal. See [Installation](docs/installation.md) for persistent PATH setup, upgrades, and source builds. Packages are currently unsigned.

## Status: Beta

Elephant is in a **closed beta**. Versions are pre-1.0, and behavior, storage details, and APIs may change between releases (read the release notes before upgrading).

- Packages are **unsigned** and not notarized. Verify downloads against `SHA256SUMS`.
- **Windows has no automatic hooks.** Use the MCP setup wizard there; automatic hooks cover Codex and Claude Code on macOS and Linux.
- There is **no enterprise authentication**. Local tenant and user labels are configuration, not access control.
- The **hosted Cloudflare service is experimental**. Local use needs no cloud account, and nothing is uploaded unless you run an explicit sync command.
- There is **no automatic redaction** beyond credential-format screening, where your version provides it; that is a best-effort check, not a guarantee. Secrets, personal data, and confidential text are not otherwise removed, so review lessons before saving or sharing them.
- A **release check runs by default**: at most once per 24 hours, Elephant asks GitHub for the latest release. Opt out with `ELEPHANT_UPDATE_CHECKS=0` or `elephant update --enabled=false`. See [Update notifications](docs/updates.md).

Send feedback with the [pilot feedback template](docs/pilot-feedback.md) in a [GitHub issue](https://github.com/prithivrajmu/elephant/issues). Report vulnerabilities as described in [SECURITY.md](https://github.com/prithivrajmu/elephant/blob/main/SECURITY.md). Version history is in the [CHANGELOG](https://github.com/prithivrajmu/elephant/blob/main/CHANGELOG.md) and the [release notes](https://github.com/prithivrajmu/elephant/releases).

## Quick start

For Codex or Claude Code on macOS/Linux, run this in your project:

```sh
elephant init
```

`init` installs hooks and runs local health checks. You can also use `elephant setup --auto --agent codex` (or `claude` or `both`). Rerunning setup keeps the existing agent choice unless you pass `--agent`. Run `elephant doctor` later to check the store, hook files, guidance, and executable path.

Restart your agent and approve its Elephant hooks through the host's normal approval flow. Work normally: hooks recall relevant memories, observe tool metadata, and request a short lesson review after the task. A saved lesson requires evidence and an acknowledged write; a task may produce no reusable lesson.

Open the local dashboard with `elephant palace`. For Windows, other MCP clients, or explicit tool access, use `elephant setup --wizard` and follow [Client integration](docs/clients.md).

## Documentation

- [Quick start](docs/quickstart.md) and [CLI reference](docs/cli-reference.md)
- [Automatic hooks](docs/automation.md) and [client integration](docs/clients.md)
- [Memory Palace](docs/dashboard.md)
- [Storage, migration, and recovery](docs/storage.md)
- [Update notifications](docs/updates.md)
- [Architecture](docs/architecture.md), [validation](docs/validation.md), and [roadmap](docs/roadmap.md)
- [Release notes](https://github.com/prithivrajmu/elephant/releases), [pilot feedback template](docs/pilot-feedback.md)
- [Experimental Cloudflare MCP service](docs/cloudflare.md), [sync and sharing](docs/cloud-sync.md), and [evidence connectors](docs/mcp-evidence.md)

## Security and privacy

Elephant stores lessons and tool-event metadata locally. It does not read transcripts or retain raw prompts, tool arguments, or tool output. It makes no automatic memory upload; the only default network call is the release check described above, which sends no memory or task data. Recalled memories are untrusted evidence; current project instructions take precedence. Local tenant/user labels are configuration, not enterprise authentication. See [Security](docs/security.md).

## Contributing

Bug reports and pull requests are welcome. Source builds require Go 1.25 or later. See [Contributing](docs/contributing.md) for development commands and review expectations.

## License

[MIT](LICENSE).
