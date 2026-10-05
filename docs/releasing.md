# Releasing Elephant

## Version and notes

`onboarding.go` is the version source. `scripts/release_version.py` derives archive names and native package versions. Add notes at `docs/releases/vVERSION.md`, update the README's release link and validate installation instructions for each supported platform. Pilot versions publish as prereleases.

Release builds use the `toolchain go1.27.1` line in `go.mod`. CI installs that exact patch with `go-version: '1.27.1'`. `actions/setup-go` reads only the `go` directive from `go.mod`, not `toolchain`, so the workflow does not use `go-version-file`. A separate minimum-Go job runs `go vet` and `go test` on Go 1.26.x with `GOTOOLCHAIN=local`, so the `go 1.26.0` floor is compiled instead of being upgraded to the toolchain line.

Actions in `.github/workflows/validate.yml` are pinned to full commit SHAs. Dependabot is disabled; update pins and module requirements by hand.

## Reproducible packages

`scripts/build_release.py` cross-compiles with `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, and `-ldflags=-s -w -buildid=`. `SOURCE_DATE_EPOCH` comes from the environment or `git log -1 --format=%ct`. Zip entries use that UTC timestamp, sorted names, deflate compression, and fixed Unix modes (`0755` for the binary and install scripts, `0644` otherwise). Two builds of the same commit produce the same `dist/SHA256SUMS`.

## Validation and publication

The validation workflow tests Linux, macOS and Windows natively. On Ubuntu it also checks `gofmt`, verifies `go.sum`, and runs pinned `govulncheck` v1.8.0. It builds six portable ZIPs, checks every packaged file checksum and documentation link, and exercises the Linux installer. A `package-native` job then downloads those artifacts and, on each runner, verifies the matching archive (`linux-amd64`, `darwin-arm64` on current hosted macOS runners, `windows-amd64`), runs `selftest`, installs into a temporary directory, and runs `selftest` again. A secret scan reads the full git history with pinned gitleaks v8.30.1. A push to `main` publishes a new version only after the native, package, package-native, minimum-Go, secret-scan, and Cloudflare check/test/dry-run jobs succeed. Pull requests build and validate without publishing.

`main` must be branch-protected with the Validate Elephant workflow required before merge. Publication is not a substitute for that protection.

Publication targets the exact main commit tested by that workflow. `scripts/publish_release.py` creates a draft release, uploads six ZIPs plus `SHA256SUMS`, downloads and verifies the assets, then publishes the draft. If `vVERSION` is already published, the job prints a release-skipped notice, records the same notice in the step summary, and exits successfully. Bump `Version` in `onboarding.go` and add `docs/releases/vVERSION.md` before the next release. A rerun may resume a draft only when it targets the same commit; a draft that points at a different commit is a failure. For a subsequent release, change the version and add its notes in a pull request before merging.

The publishing job uses the repository's Actions token with `contents: write`; no long-lived personal token is needed.

## Local package verification

```sh
go test -race ./...
go vet ./...
python3 scripts/build_release.py
python3 scripts/package_acceptance.py
python3 scripts/package_acceptance.py --native
python3 scripts/publish_release.py --dry-run
```

The dry run checks asset inventory and checksums without contacting GitHub. `--native` executes the archive for the machine running it. Native validation on all three operating systems remains a release gate. Repeating `build_release.py` for the same commit must leave `dist/SHA256SUMS` unchanged.

## Native installer recipes

These recipes are separate from the published ZIP distribution. Signed/notarized native packages and package-manager listings require further platform testing and distribution setup.

```sh
python3 scripts/build_installers.py --format deb --arch amd64
python3 scripts/build_installers.py --format deb --arch arm64
# On macOS with pkgbuild:
python3 scripts/build_installers.py --format pkg --arch arm64
```

On Windows, compile `packaging/elephant.iss` with Inno Setup and `ReleaseDir` pointing to the Windows release folder. Set `Architecture=arm64` for ARM64; the default is `x64compatible`. Native installers preserve the README and nested documentation layout. Signing, notarization, Homebrew and WinGet publication are future work; do not advertise them as available until published and tested.

## npm package (`npx elephant-memory`)

`npm/` holds a dependency-free launcher. It downloads the GitHub release matching its own `package.json` version, verifies the SHA-256 from `SHA256SUMS`, caches the binary in `~/.elephant/bin/<tag>`, installs it to `~/.local/bin` (Windows: `%LOCALAPPDATA%\Elephant\bin`) and passes any arguments through. After the GitHub release is published, set `npm/package.json` `version` to the release version (it must equal `Version` in `onboarding.go`) and run `npm publish --access public --tag beta` from `npm/`. Publishing is manual.
