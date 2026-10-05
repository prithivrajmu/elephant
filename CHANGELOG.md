# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Elephant is in
pre-release; versions carry a `-pilot` suffix and compatibility is not yet
guaranteed.

## [Unreleased]

### Public beta hardening

-

## [0.7.0-pilot]

Full notes: [docs/releases/v0.7.0-pilot.md](docs/releases/v0.7.0-pilot.md).

### Added

- Optional Cloudflare MCP service (authenticated, user-scoped Durable Objects)
  with all six memory tools available remotely.
- Explicit cloud sync with selected scopes, persistent offline outbox, stable
  retry IDs, revision conflicts and retirement propagation. No automatic upload.
- Reviewed team sharing: immutable proposals, separate reviewers, provenance,
  withdrawal and local approval before imported advice becomes active.
- `read_evidence` tool for allowlisted, read-only external MCP tools; evidence is
  untrusted and never automatically recorded.
- Backend contracts separating service operations from concrete local storage,
  with shared Go/TypeScript behavior fixtures.

### Changed

- Scoped SQL recall selects applicable candidates through SQLite indexes before
  decoding (synthetic 10,000-row selection: 87.9 ms to 4.8 ms).
- Update checks show progress and actionable messages for access, credentials,
  rate limits, connectivity and timeouts; releases are never installed
  automatically.

### Known limitations

- Packages are unsigned; ARM archives are cross-compiled.
- No hosted deployment or production load certification accompanies this release.

## [0.6.0-pilot]

Full notes: [docs/releases/v0.6.0-pilot.md](docs/releases/v0.6.0-pilot.md).

### Added

- Durable SQLite storage (WAL, FULL synchronization) with automatic JSONL
  migration that retains the original as `<store>.jsonl-backup`.
- `elephant backup` and `elephant restore` for consistent snapshots and verified
  recovery.
- Factual end-of-task status lines and cached, user-controlled release
  notifications.
- Six portable macOS/Linux/Windows archives with SHA-256 checksums.

### Fixed

- Windows drive-letter SQLite URIs.

### Known limitations

- Older binaries cannot read a migrated store.
- Memories and backups are not encrypted by Elephant.

[Unreleased]: https://github.com/prithivrajmu/elephant/compare/v0.7.0-pilot...HEAD
[0.7.0-pilot]: https://github.com/prithivrajmu/elephant/compare/v0.6.0-pilot...v0.7.0-pilot
[0.6.0-pilot]: https://github.com/prithivrajmu/elephant/releases/tag/v0.6.0-pilot
