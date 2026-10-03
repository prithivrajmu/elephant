# Elephant: first user in ten minutes

Elephant 0.4.0-pilot is a local Go binary with an embedded dashboard. Prebuilt packages need no Go installation, model key or cloud service. A source build needs Go 1.22+. This release is for a small local pilot. Only Linux x86-64 has been executed in our acceptance environment; the other packages are cross-compiled.

## 1. Install the package for your computer

Extract the matching archive. Apple Silicon Macs use `darwin-arm64`; Intel Macs use `darwin-amd64`; most Windows/Linux PCs use `amd64`. Keep the extracted package together.

macOS/Linux, from the extracted folder:

```sh
sh ./install.sh
"$HOME/.local/bin/elephant" version
"$HOME/.local/bin/elephant" selftest
```

The installer verifies package checksums and preserves a different existing binary. `ELEPHANT_INSTALL_DIR=/your/bin sh ./install.sh` chooses another destination. `ELEPHANT_REPLACE=1 sh ./install.sh` explicitly upgrades an existing binary. You can instead run `./elephant` directly. On macOS, follow your organization's normal approval process if the unsigned executable is blocked. This pilot has no signed/notarized installer.

Windows PowerShell, from the extracted folder:

```powershell
.\install.ps1
& "$env:LOCALAPPDATA\Elephant\bin\elephant.exe" version
& "$env:LOCALAPPDATA\Elephant\bin\elephant.exe" selftest
```

If local script policy blocks the installer, use `Get-FileHash .\elephant.exe -Algorithm SHA256`, compare against `SHA256SUMS`, and run the executable directly. The installer does not change policy or PATH. Substitute your installed binary's full path for `elephant` below if it is not on PATH.

`selftest` uses an isolated temporary store, runs seven checks, and removes it. It does not seed fabricated lessons into your real history. All checks must show `ok: true` before proceeding.

### Build from a source checkout

If you have the repository rather than a prebuilt package, install Go 1.22+ and run these commands from the repository root on macOS/Linux:

```sh
go test ./...
go build -buildvcs=false -o elephant ./cmd/elephant
./elephant version
./elephant selftest
```

The binary is created at `./elephant`. Use its absolute path in [CLIENTS.md](CLIENTS.md)'s registration commands. For the commands below, substitute `./elephant` for `elephant` when running from the repository root. Package-building scripts are included, but compiled binaries and release archives are not tracked in Git. Building and running the self-test on your OS is a separate check from the recorded Linux validation.

## 2. Choose a real project and generate setup

For guided steps, run `elephant setup --wizard`. On macOS, `Setup.command` installs Elephant and starts the wizard. On Windows, use `Setup.ps1`. See `INSTALLERS.md` for native package routes.

The writing target starts at 80%. Use `elephant language --ste-target 100` for strict local checks. This is not a full-standard compliance score. See `LANGUAGE_POLICY.md`.

```sh
elephant doctor --root /absolute/path/project --project my-project
elephant setup --root /absolute/path/project --project my-project \
  --output /absolute/path/elephant-setup
```

Use a stable project ID. Setup creates JSON/TOML connection config, agent instructions and a first-task prompt. It preserves existing files; use a new output folder when changing context. The binary, root and journal paths in the config are absolute, including paths with spaces.

For a conversation without a project:

```sh
elephant doctor --unattached --conversation research-001
elephant setup --unattached --conversation research-001 --output elephant-research-setup
```

This skips working-directory manifests. Personal lessons can transfer; conversation lessons require the same conversation ID. Supply known technical facts in `context_features` for conditional lessons. Unknown requirements block retrieval.

The default journal is `~/.elephant/events.jsonl`. If only the previous `~/.agent-memory/events.jsonl` exists, Elephant reuses it. Nothing is moved. When both exist, the Elephant path wins. Use `--store /absolute/path/events.jsonl` consistently to select a different journal.

## 3. Connect your agent and teach it the workflow

See [CLIENTS.md](CLIENTS.md) for exact Codex, Claude Code and Pi registration commands, or merge the generated server entry into existing client configuration. Add `AGENT_INSTRUCTIONS.md` to your agent's project guidance, then reload/restart the client. The setup command does not modify client settings automatically.

Confirm six tools are visible: `profile_memory`, `init_memory`, `recall_memory`, `record_memory`, `feedback_memory`, `forget_memory`. Two MCP prompts are also exposed: `initmemory` and `memory_review`. The client's slash-command presentation varies.

Ask the agent:

> Use Elephant to recall experience for this task before starting. After the task, inspect actual tests/review outcomes and capture one reusable lesson with its source if justified. Report the stored memory ID. If a retrieved lesson was applied, submit feedback only after observing its effect, with a stable run ID.

Give it a real task. An empty first recall is expected. There is no background transcript watcher: capture depends on the agent calling the tool. Ask `memory_review` at task end if capture was missed.

## 4. Confirm the memory survives and applies

```sh
elephant palace --root /absolute/path/project --project my-project
# Visit http://127.0.0.1:7331
```

The dashboard is an owner inventory with current context labeled. Inspect the source and applicability of the recorded lesson. Try a related task in Recall, then an unrelated task; unrelated recall should abstain. A lesson with `requires` only appears when current facts satisfy those conditions. Restart Elephant and confirm the lesson remains. Use a stable task/run ID for feedback, and report outcomes only after applying the advice.

You can use the CLI/UI without an MCP client. For CLI JSON, use `record --file lesson.json`, `recall --task '...'`, and `feedback --id ID --feedback-id task-123 --helpful=true`. Inject only the returned `context` field; its byte budget excludes the enclosing tool/JSON wrapper.

## Troubleshooting

- No tools: check absolute executable path, client config format, server approval and restart. `doctor` proves local health, not client connection.
- No recall: check task terms, known facts, scope and byte budget. Similar stack alone does not make task-specific recall eligible. V0.3 uses lexical matching, not semantic embeddings.
- Store busy: Elephant retries for three seconds. After a crashed process, verify all users of that store have stopped before removing the journal's `.lock` directory. Never remove a live process's lock.
- Corrupt journal: keep a copy and restore/repair explicitly. Elephant fails closed; it does not discard history.
- Wrong context: regenerate setup and restart the client. Server identity/project are fixed at startup.

No authenticated multi-user hosting or physical erasure is included. Start with non-sensitive local lessons. Token-avoidance figures are estimates against loading every eligible summary, not measured productivity or provider-billing savings.
