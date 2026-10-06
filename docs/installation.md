# Installation

## Download a prebuilt binary

Download the matching ZIP from [GitHub Releases](https://github.com/prithivrajmu/elephant/releases). No Go installation is needed. Elephant is in closed beta: releases are pre-1.0 and packages are unsigned (see [Status: Beta](../README.md#status-beta)).

| Computer | Archive suffix |
| --- | --- |
| Apple Silicon Mac | `darwin-arm64` |
| Intel Mac | `darwin-amd64` |
| Linux x86-64 | `linux-amd64` |
| Linux ARM64 | `linux-arm64` |
| Windows x86-64 | `windows-amd64` |
| Windows ARM64 | `windows-arm64` |

For example, an Apple Silicon Mac user with the GitHub CLI can download the latest release:

```sh
TAG=$(gh release list --repo prithivrajmu/elephant --limit 1 --json tagName --jq '.[0].tagName')
gh release download "$TAG" --repo prithivrajmu/elephant \
  --pattern 'elephant-*-darwin-arm64.zip'
unzip elephant-*-darwin-arm64.zip
cd elephant-*-darwin-arm64/
sh ./install.sh
export PATH="$HOME/.local/bin:$PATH"
elephant selftest
```

Alternatively, download and extract the ZIP in your browser. Each archive includes a binary, checksum-verifying installer, setup launcher, version and documentation. The release also provides `SHA256SUMS` for the ZIP downloads; compare the matching entry with `shasum -a 256 ARCHIVE.zip` or `sha256sum ARCHIVE.zip`. Checksums detect corruption; they are not a substitute for signed packages.

## Release checks and network use

By default, Elephant checks GitHub for a newer release at most once per 24 hours when a hooked session starts, and when you run `elephant update --check` or press Check in Memory Palace. The request goes to GitHub's public release metadata endpoint. GitHub receives the request's normal network metadata (such as your IP address and a user agent naming Elephant and its version). No memory, task, prompt, or project data is sent, and nothing is installed automatically. To opt out, run `elephant update --enabled=false` or set `ELEPHANT_UPDATE_CHECKS=0` for the process. See [Update notifications](updates.md).

## macOS and Linux

From the extracted folder, run `sh ./install.sh`. It installs to `~/.local/bin` without administrator access. Set `ELEPHANT_INSTALL_DIR` to use another directory. Add this line to your shell configuration (`~/.zshrc` for typical macOS terminals, `~/.bashrc` for Bash), then open a new terminal:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Use your organization's normal approval process if macOS blocks the unsigned binary. `Setup.command` is a Terminal launcher that installs, runs selftest and opens the setup wizard.

## Windows

From the extracted folder in PowerShell:

```powershell
.\install.ps1
$env:Path = "$env:LOCALAPPDATA\Elephant\bin;$env:Path"
elephant selftest
```

The default destination is `%LOCALAPPDATA%\Elephant\bin`. To persist PATH, add that directory to your **user** Path through Windows Environment Variables and open a new terminal. Scripts follow your local execution policy; if scripts are blocked, use your organization's approval process or run the extracted `elephant.exe` directly. `Setup.ps1` installs, runs selftest and opens the setup wizard.

## Connect your agent

On macOS/Linux, run `elephant init` or `elephant setup --auto` in your project. Both install hooks and run local health checks. Use `--agent codex` or `--agent claude` for one agent; reruns keep that choice. Restart the agent and approve the hooks through its normal flow. Run `elephant doctor` later to inspect the store, executable path, hook files, and guidance. On Windows or with another MCP client, run `elephant setup --wizard`, merge the generated settings into the client's configuration and restart it. Identical setup files can be generated again safely; changed files require a new output directory. See [Quick start](quickstart.md) and [Client integration](clients.md).

## Upgrade

Stop running Elephant processes, create a [verified backup](storage.md), and extract the new archive. The installers preserve a different existing binary unless replacement is explicit:

```sh
ELEPHANT_REPLACE=1 sh ./install.sh
```

On Windows, run `./install.ps1 -Replace`. Existing memory data remains in place. Version 0.6 migrates legacy JSONL journals in place to SQLite and preserves the original bytes in a `.jsonl-backup`; older binaries cannot open the migrated path. Keep the backup for rollback. See [Storage and recovery](storage.md) before moving databases or handling migration failures.

## Build from source

Go 1.25 or later is required:

```sh
git clone https://github.com/prithivrajmu/elephant.git
cd elephant
git checkout "$TAG"   # a release tag, e.g. from the Releases page
go build -trimpath -o elephant ./cmd/elephant
./elephant selftest
```

Use `-o elephant.exe` on Windows. Copy the resulting binary into a directory on PATH. See [Contributing](contributing.md) for development checks and [Releasing](releasing.md) for package recipes. Homebrew, WinGet and signed native installers are not published distribution routes yet.

`go install github.com/prithivrajmu/elephant/cmd/elephant@<tag>` is available only from the first release tag cut after the Go module path was corrected to `github.com/prithivrajmu/elephant`. It does not work for `v0.7.0-pilot` or earlier tags; use a release archive or the source build above for those.
