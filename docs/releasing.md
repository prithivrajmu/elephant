# Releasing Elephant

## Version and notes

`onboarding.go` is the version source. `scripts/release_version.py` derives archive names and native package versions. Add notes at `docs/releases/vVERSION.md`, update the README's release link and validate installation instructions for each supported platform. Pilot versions publish as prereleases.

The validation workflow tests Linux, macOS and Windows natively, builds six portable ZIPs, checks every packaged file checksum and documentation link, and exercises the Linux installer. A push to `main` publishes a new version only after all native, package and Cloudflare check/test/dry-run jobs succeed. Pull requests build and validate without publishing.

Publication targets the exact main commit tested by that workflow. `scripts/publish_release.py` creates a draft release, uploads six ZIPs plus `SHA256SUMS`, downloads and verifies the assets, then publishes the draft. An existing published version is left unchanged. A rerun may resume a draft only when it targets the same commit. For a subsequent release, change the version and add its notes in a pull request before merging.

The publishing job uses the repository's Actions token with `contents: write`; no long-lived personal token is needed. Repository visibility does not change: private releases require repository access.

## Local package verification

```sh
go test -race ./...
go vet ./...
python3 scripts/build_release.py
python3 scripts/package_acceptance.py
python3 scripts/publish_release.py --dry-run
```

The dry run checks asset inventory and checksums without contacting GitHub. Native validation on all three operating systems remains a release gate.

## Native installer recipes

These recipes are separate from the published ZIP distribution. Signed/notarized native packages and package-manager listings require further platform testing and distribution setup.

```sh
python3 scripts/build_installers.py --format deb --arch amd64
python3 scripts/build_installers.py --format deb --arch arm64
# On macOS with pkgbuild:
python3 scripts/build_installers.py --format pkg --arch arm64
```

On Windows, compile `packaging/elephant.iss` with Inno Setup and `ReleaseDir` pointing to the Windows release folder. Set `Architecture=arm64` for ARM64; the default is `x64compatible`. Native installers preserve the README and nested documentation layout. Signing, notarization, Homebrew and WinGet publication are future work; do not advertise them as available until published and tested.
