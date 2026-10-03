# Installation

## Download a prebuilt binary

Download the matching ZIP from [GitHub Releases](https://github.com/prithivrajmu/elephant/releases). No Go installation is needed. Releases are currently pilot prereleases and packages are unsigned. While the repository is private, downloads require repository access and an authenticated GitHub session.

| Computer | Archive suffix |
| --- | --- |
| Apple Silicon Mac | `darwin-arm64` |
| Intel Mac | `darwin-amd64` |
| Linux x86-64 | `linux-amd64` |
| Linux ARM64 | `linux-arm64` |
| Windows x86-64 | `windows-amd64` |
| Windows ARM64 | `windows-arm64` |

For example, an authorized Apple Silicon Mac user with the GitHub CLI can download the pilot release:

```sh
gh auth login
gh release download v0.6.0-pilot --repo prithivrajmu/elephant \
  --pattern 'elephant-0.6.0-pilot-darwin-arm64.zip'
unzip elephant-0.6.0-pilot-darwin-arm64.zip
cd elephant-0.6.0-pilot-darwin-arm64
sh ./install.sh
export PATH="$HOME/.local/bin:$PATH"
elephant selftest
```

Alternatively, download and extract the ZIP in your browser. Each archive includes a binary, checksum-verifying installer, setup launcher, version and documentation. The release also provides `SHA256SUMS` for the ZIP downloads; compare the matching entry with `shasum -a 256 ARCHIVE.zip` or `sha256sum ARCHIVE.zip`. Checksums detect corruption; they are not a substitute for signed packages.

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

On macOS/Linux, run `elephant init` in your project, then restart Codex or Claude Code and approve the hooks through the host's normal flow. Run `elephant doctor` to inspect setup. On Windows or with another MCP client, run `elephant setup --wizard`, merge the generated settings into the client's configuration and restart it. See [Quick start](quickstart.md) and [Client integration](clients.md).

## Upgrade

Stop running Elephant processes, create a [verified backup](storage.md), and extract the new archive. The installers preserve a different existing binary unless replacement is explicit:

```sh
ELEPHANT_REPLACE=1 sh ./install.sh
```

On Windows, run `./install.ps1 -Replace`. Existing memory data remains in place. Version 0.6 migrates legacy JSONL journals in place to SQLite and preserves the original bytes in a `.jsonl-backup`; older binaries cannot open the migrated path. Keep the backup for rollback. See [Storage and recovery](storage.md) before moving databases or handling migration failures.

## Build from source

Repository access and Go 1.25 or later are required:

```sh
git clone https://github.com/prithivrajmu/elephant.git
cd elephant
git checkout v0.6.0-pilot
go build -trimpath -o elephant ./cmd/elephant
./elephant selftest
```

Use `-o elephant.exe` on Windows. Copy the resulting binary into a directory on PATH. See [Contributing](contributing.md) for development checks and [Releasing](releasing.md) for package recipes. Homebrew, WinGet and signed native installers are not published distribution routes yet.
