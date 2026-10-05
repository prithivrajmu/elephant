# Elephant: first user in ten minutes

Elephant is a local Go binary with an embedded dashboard. Prebuilt packages need no Go installation, model key or cloud service. A source build needs Go 1.25+. Elephant is in closed beta; see [Status: Beta](../README.md#status-beta) for known limitations. Releases are gated on native Linux, macOS and Windows checks; six platform archives are cross-compiled. Native ARM installer and real model-driven host acceptance remain separate checks. See [Validation](validation.md) for the exact checks and limits.

**Network use:** by default, Elephant checks GitHub for a new release at most once per 24 hours. GitHub sees your version and normal network metadata; no memory or task data is sent. Opt out with `elephant update --enabled=false` or `ELEPHANT_UPDATE_CHECKS=0`. See [Release updates](#release-updates).

## 1. Install the package for your computer

Extract the matching archive. Apple Silicon Macs use `darwin-arm64`; Intel Macs use `darwin-amd64`; most Windows/Linux PCs use `amd64`. Keep the extracted package together.

macOS/Linux, from the extracted folder:

```sh
sh ./install.sh
"$HOME/.local/bin/elephant" version
"$HOME/.local/bin/elephant" selftest
```

The installer verifies package checksums and preserves a different existing binary. `ELEPHANT_INSTALL_DIR=/your/bin sh ./install.sh` chooses another destination. `ELEPHANT_REPLACE=1 sh ./install.sh` explicitly upgrades an existing binary. You can instead run `./elephant` directly. On macOS, follow your organization's normal approval process if the unsigned executable is blocked. This beta has no signed/notarized installer.

Windows PowerShell, from the extracted folder:

```powershell
.\install.ps1
& "$env:LOCALAPPDATA\Elephant\bin\elephant.exe" version
& "$env:LOCALAPPDATA\Elephant\bin\elephant.exe" selftest
```

If local script policy blocks the installer, use `Get-FileHash .\elephant.exe -Algorithm SHA256`, compare against `SHA256SUMS`, and run the executable directly. The installer does not change policy or PATH. Substitute your installed binary's full path for `elephant` below if it is not on PATH.

`selftest` uses an isolated temporary store, runs seven checks, and removes it. It does not seed fabricated lessons into your real history. All checks must show `ok: true` before proceeding.

### Build from a source checkout

If you have the repository rather than a prebuilt package, install Go 1.25+ and run these commands from the repository root on macOS/Linux:

```sh
go test ./...
go build -buildvcs=false -o elephant ./cmd/elephant
./elephant version
./elephant selftest
```

The binary is created at `./elephant`. Use its absolute path in [Client integration](clients.md)'s registration commands. For the commands below, substitute `./elephant` for `elephant` when running from the repository root. Package-building scripts are included, but compiled binaries and release archives are not tracked in Git. Building and running the self-test on your OS is a separate check from the recorded Linux validation.

## 2. Initialize automatic memory

Build the current source or use a 0.6 package built from it. Older 0.4 packages do not include the automatic workflow. From the project directory, run:

```sh
elephant init
# Or: elephant init --agent codex --project my-project
```

The default installs Codex and Claude Code hooks. Existing settings and instructions are preserved. Restart the agent and complete its normal trust prompts. In Codex, open `/hooks` and approve the Elephant hooks. `init` does not bypass host approval. Automatic installation currently supports macOS/Linux.

## 3. Work normally

Hooks recall useful experience before each task, record tool outcome metadata, and request one lesson review before the agent finishes. The agent records a lesson only when observed work justifies one. There is no extra model key and no need for a repeated “remember this” prompt. After review, the agent reports one factual line such as `Elephant: recalled 2 memories · saved 1 lesson.`

```sh
elephant automation   # Check installed settings and received events
elephant experiences  # Inspect the latest event metadata
```

An empty first recall is expected. Prompts, tool output and transcripts are not copied into the store. The writing target starts at 80%; use `elephant language --ste-target 100` for strict local checks. This is not a full-standard compliance score.

Use `elephant automation --enabled=false` to pause and `--enabled=true` to resume. See [Automatic hooks](automation.md) for the exact lifecycle, local files and unanchored conversations.

For other agents or Windows, use `elephant setup --wizard` and follow [Client integration](clients.md). That MCP route requires host registration and agent guidance. It does not install automatic hooks.

The default store is `~/.elephant/memories.sqlite`. Existing `events.jsonl` paths migrate in place on first access, with `<store>.jsonl-backup` preserving the original. Set a custom path with `elephant init --store /absolute/path/memories.sqlite`. Later project commands reuse it. See [Storage](storage.md) for safe backup, restore and migration recovery.

## 4. Confirm the memory survives and applies

```sh
elephant palace --root /absolute/path/project --project my-project
# Open the printed link: http://127.0.0.1:7331/#token=<per-run token>
```

The dashboard is an owner inventory with current context labeled. Inspect the source and applicability of the recorded lesson. Try a related task in Recall, then an unrelated task; unrelated recall should abstain. A lesson with `requires` only appears when current facts satisfy those conditions. Restart Elephant and confirm the lesson remains. Use a stable task/run ID for feedback, and report outcomes only after applying the advice.

You can use the CLI/UI without an MCP client. For CLI JSON, use `record --file lesson.json`, `recall --task '...'`, and `feedback --id ID --feedback-id task-123 --helpful=true`. Inject only the returned `context` field; its byte budget excludes the enclosing tool/JSON wrapper.

## Troubleshooting

- No automatic events: run `elephant automation`, check the binary path, restart the host and inspect its hook approval/settings. `init` proves installation, not host execution.

- No tools: check absolute executable path, client config format, server approval and restart. `doctor` proves local health, not client connection.
- No recall: check task terms, known facts, scope and byte budget. Similar stack alone does not make task-specific recall eligible. Current retrieval uses lexical matching, not semantic embeddings.
- Store busy: SQLite waits up to three seconds for a writer. Ordinary process crashes release SQLite locks; migration-only `.lock` recovery is documented in [Storage](storage.md).
- Corrupt store or legacy journal: preserve it and restore explicitly to a new path. Elephant does not silently discard history.
- Wrong context: regenerate setup and restart the client. Server identity/project are fixed at startup.

No authenticated multi-user hosting or physical erasure is included. Start with non-sensitive local lessons. Token-avoidance figures are estimates against loading every eligible summary, not measured productivity or provider-billing savings.


## Release updates

Use `elephant update --check` to check releases now, `--enabled=false` to disable checks, and `--dismiss` to dismiss a notice. Session-start checks are cached for 24 hours and contact GitHub by default, sending only your version and normal network metadata. Setting `ELEPHANT_UPDATE_CHECKS=0` also disables them. An optional `ELEPHANT_GITHUB_TOKEN` only raises GitHub rate limits; unavailable metadata does not mean your version is current. Memory Palace also has update controls. See [Update notifications](updates.md).
