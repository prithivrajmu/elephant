# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Elephant is in
pre-1.0 development; compatibility is not yet guaranteed. Versions from 0.9.1 use a
regular release tag; earlier releases used `-beta` or `-pilot` suffixes.

## [0.10.0]

Full notes: [docs/releases/v0.10.0.md](docs/releases/v0.10.0.md).

### Changed

- `record_memory` and `remember` refuse a lesson that is similar to one you can
  already recall here, including your personal lessons, and list up to three of them.
  Before, only a byte-identical lesson in the same scope was caught. Resend with
  `supersedes` to retire the listed lessons in the same save, or with `distinct_from`
  to save a different lesson. In a replay of 159 real saves, the check caught 72% of
  repeated or updated lessons; 9% of saves were refused with no duplicate listed.

### Security

- Release builds use Go 1.27.3. It fixes GO-2026-6809 in `crypto/x509`, which
  `govulncheck` reports against Go 1.27.2.

## [0.9.2]

Full notes: [docs/releases/v0.9.2.md](docs/releases/v0.9.2.md).

### Fixed

- A review that a new prompt interrupts now saves its lessons under its own task.
  Before, `remember` and `task-status` failed with "task changed before capture"
  and the lesson was lost.
- A stale `task-status` command now prints a read-only line instead of failing.
  The new states are `superseded` and `already_reported`.
- When a hook fails, Claude Code now shows the error text. Before, it showed only
  "See hook stderr for details".
- Dashboard and `stats` no longer present "context tokens avoided" as a saving. The
  figure compares against loading every memory, not against using no memory.

### Security

- Release builds use Go 1.27.2. It fixes nine standard-library vulnerabilities that
  `govulncheck` reports against Go 1.27.1 in `net/http`, `crypto/tls` and
  `net/textproto`, which Elephant's cloud sync and dashboard use.

### Added

- Local recall phase traces, p50/p75/p95 Memory Palace metrics, deterministic
  performance ratchets, and synthetic setup/recall/capture/Palace journey reports.
- A scheduled performance workflow that publishes scale evidence without using
  noisy wall-clock timing as a pull-request gate.
- `doctor` reports agent guidance that is older than the installed version, with
  the command that updates it.
- Releases publish when a `vX.Y.Z` tag is pushed, not on every merge to `main`.

### Changed

- Recall receipts now retain local profile, store-open, candidate-load and
  rank/render timings plus bounded work counts. Task text remains excluded.
- Unreviewed edits and failed tool calls carry to the next task when the host
  skips the finish hook, for example after a user interrupt. The next finish hook
  then requests the review.
- Refusals from `remember` and `task-status` say what happened and what to do.
- The guidance text now tells the agent to report `capture status unavailable`
  with the error in one sentence. Run `elephant init` again to update it.
- `saved 1 lesson` is no longer written as `saved 1 lessons` after a capture failure.

## [0.9.1]

Full notes: [docs/releases/v0.9.1.md](docs/releases/v0.9.1.md).

### Fixed

- Plain `elephant update` checks published releases immediately instead of reading
  a cached result that can predate a new release.
- Repeated explicit checks show the available version and release link, even
  after a background hook has already announced the release.
- Cached JSON reads and update preferences remain offline unless `--check` is set.

## [0.9.0-beta]

Full notes: [docs/releases/v0.9.0-beta.md](docs/releases/v0.9.0-beta.md).

### Changed

- Faster local recall with cached word comparisons, early budget filtering, and
  indexed alternative-advice metadata. Ranking and applicability rules are preserved.
- Memory saves query matching evidence within the owner's scope instead of decoding
  the entire store. Equivalent label sets no longer create duplicate retry records.
- Memory Palace now includes the interactive brain atlas merged in PR #30.

### Added

- `setup --auto` installs project hooks and runs local diagnostics.
- `doctor` checks the configured binary, hooks, and agent guidance.
- The Memory Palace validation script now runs in release CI.

### Fixed

- Setup reruns preserve the selected agent and check store access before installing hooks.
- Repeated MCP file setup fills missing files without replacing edited content.
- Wizard project defaults follow a changed root, and failed file generation leaves
  the store's writing policy unchanged.

## [0.8.0-beta]

Full notes: [docs/releases/v0.8.0-beta.md](docs/releases/v0.8.0-beta.md).

First closed beta with public distribution, credential-format screening,
third-party notices, beta-channel updates, cloud-sync retirement protection,
and a checksum-verified npm launcher with pi/omp integration.

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

[Unreleased]: https://github.com/prithivrajmu/elephant/compare/v0.10.0...HEAD
[0.10.0]: https://github.com/prithivrajmu/elephant/compare/v0.9.2...v0.10.0
[0.9.2]: https://github.com/prithivrajmu/elephant/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/prithivrajmu/elephant/compare/v0.9.0-beta...v0.9.1
[0.9.0-beta]: https://github.com/prithivrajmu/elephant/compare/v0.8.0-beta...v0.9.0-beta
[0.8.0-beta]: https://github.com/prithivrajmu/elephant/compare/v0.7.0-pilot...v0.8.0-beta
[0.7.0-pilot]: https://github.com/prithivrajmu/elephant/compare/v0.6.0-pilot...v0.7.0-pilot
[0.6.0-pilot]: https://github.com/prithivrajmu/elephant/releases/tag/v0.6.0-pilot
