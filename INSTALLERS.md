# Elephant installation routes

## Available now

Each portable archive contains the binary, checksum file, installer, setup launcher and docs. No Go installation is needed.

| System | Install | Configure |
| --- | --- | --- |
| macOS | Run `sh install.sh`. Or open `Setup.command` in Terminal. | The launcher runs selftest and the setup wizard. |
| Linux | Run `sh install.sh`. Or install the matching `.deb`. | Run `elephant setup --wizard`. |
| Windows | Run `install.ps1` or `Setup.ps1` in PowerShell. | The setup script runs selftest and the wizard. |

The user scripts verify package checksums. They preserve a different existing binary unless replacement is explicit. They do not edit PATH or client settings. They preserve the memory journal. A Linux system package uses the normal package-manager upgrade rules.

On macOS, use the normal organization approval process if the unsigned binary is blocked. Setup.command is a Terminal launcher, not a signed graphical installer. Windows scripts remain subject to local script policy.

## Native package work

| Route | Work included | What remains |
| --- | --- | --- |
| Debian/Ubuntu `.deb` | Build script and packages for amd64/arm64. | Native ARM and package-manager install tests. Linux amd64 package extraction is tested. |
| macOS `.pkg` | Native build recipe with `pkgbuild`. | Build/test on a Mac, Developer ID signing and notarization. No `.pkg` was built here. |
| Windows setup `.exe` | Inno Setup recipe with a per-user destination and uninstall support. | Compile/test on Windows, code signing and native architecture checks. No setup `.exe` was built here. |
| Homebrew | Recommended first public Mac/Linux package route. | A public versioned release URL, actual hashes, a tap and native audit/tests. No `brew install elephant` claim yet. |
| WinGet | Can distribute the Windows setup package. | Published installer URL, manifest/hash validation and repository submission. No WinGet listing yet. |

Build portable releases first:

```sh
python3 scripts/build_release.py
python3 scripts/build_installers.py --format deb --arch amd64
python3 scripts/build_installers.py --format deb --arch arm64

# On a Mac with pkgbuild:
python3 scripts/build_installers.py --format pkg --arch arm64
```

On Windows, install Inno Setup. Compile `packaging/elephant.iss` with `ReleaseDir` set to the extracted Windows archive. Set `Architecture=arm64` for the ARM archive. The default is `x64compatible`. This recipe is a starting point and has not been compiled in this Linux environment.

The next release should automate native tests, signing and publication. Choose the public repository/release location before package-manager publication. Do not ship manifests with invented URLs or pretend a package is already listed.

## Setup steps

`elephant setup --wizard` asks for the project or no-project context, writing target and output folder. It writes MCP settings and agent instructions. The user merges these files into existing client settings and restarts the client. The wizard does not choose credentials, overwrite client files or install a background service.

## References

- Homebrew formula structure and testing: https://docs.brew.sh/Formula-Cookbook
- WinGet manifests: https://learn.microsoft.com/en-us/windows/package-manager/package/manifest
- macOS distribution: https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution
- Inno Setup destination selection: https://jrsoftware.org/ishelp/topic_setup_defaultdirname.htm


## Version source and acceptance

Archive names, Debian and macOS package versions derive from `onboarding.go` via `scripts/release_version.py`. Windows release folders include `version.iss`; the Inno recipe reads it instead of hardcoding a version. All root Markdown docs are packaged. Run `python3 scripts/package_acceptance.py` after archive creation to verify all six archives and native Linux execution. `.github/workflows/validate.yml` adds native-platform checks and archive artifacts; it does not publish a release.
