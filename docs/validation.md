# Validation

## Recall and setup improvements, 2026-10-06

Measured locally on macOS arm64 (Apple M4), comparing the retrieval and store code at `56fdc8b` with this change. Values below are medians of three runs:

| Synthetic benchmark | Before | After | Allocated bytes before → after |
| --- | ---: | ---: | ---: |
| Retrieval engine, 1,000 memories | 25.6 ms | 8.4 ms | 18.7 MB → 7.2 MB |
| SQLite recall with alternatives, 100 memories | 23.4 ms | 4.8 ms | 19.5 MB → 1.9 MB |
| SQLite recall with alternatives, 1,000 memories | 1,801 ms | 34.2 ms | 1,668 MB → 19.6 MB |

The alternatives fixture deliberately gives every memory the same subject and different lesson text. It exercises the previous quadratic baseline calculation. It includes database access, ranking, baseline rendering, and a usage receipt. It excludes host startup and model calls; this stress-case speedup is not a general workload estimate. The short runs contain scheduling noise.

Reproduce the benchmarks with:

```sh
go test -run '^$' -bench '^BenchmarkRecall1000$' -benchmem -count=3
go test -run '^$' -bench '^BenchmarkRecallWithAlternatives$' -benchmem -benchtime=3x -count=3
```

A temporary comparison against the previous `Recall` implementation checked identical complete results across 1,000 seeded combinations of tasks, budgets, feedback, ages, visibility, and applicability. Permanent regression tests cover bounded alternative IDs against a full scan, oversized entries, label-equivalent record retries, independent evidence, other-owner isolation, safe setup retries, wizard project changes, failed setup policy preservation, missing-hook repair, and automatic CLI setup. Local tests do not establish real Codex/Claude approval or model-driven capture.

Final verification passed: `go test -race ./...`, `go vet ./...`, binary build, seven-check selftest, and `scripts/acceptance.py`, `scripts/storage_acceptance.py`, and `scripts/automation_acceptance.py` against the rebuilt binary. The MCP/dashboard check required permission to bind its isolated localhost test port.

## Release gates

Publication of each release requires the [validation workflow](https://github.com/prithivrajmu/elephant/actions/workflows/validate.yml) to pass native Linux/macOS/Windows race tests, vet, selftest, external MCP and storage acceptance, plus macOS/Linux hook acceptance and six-archive package checks. The release job verifies uploaded downloads before publishing. This distinguishes native binary/protocol checks from real agent-host usage and ARM installer tests.

The release preparation fixes Windows drive-letter SQLite URIs and makes the Windows test step fail immediately when tests fail. Local Linux checks additionally execute the extracted installer twice and verify the installed binary.

## SQLite WAL, version 0.6

Executed on 2026-10-03 UTC (2026-10-04 in India), Linux amd64 with Go 1.27.1:

- 49 Go test entry points, including a child-process helper, pass with race detection; vet and source build pass. New cases cover schema/WAL/FULL settings, SQLite 3.51.3, complete JSONL conversion, original-byte backup, duplicate feedback/receipt preservation, corruption and mismatching-backup rejection, interrupted conversion retry, transaction rollback, bounded busy waits without callback replay, unsupported-schema preservation, readers and backup during a write, and killed writers both before and after commit.
- Seven-check selftest, external MCP acceptance with simultaneous clients/dashboard polling, generated Codex/Claude hook commands, CLI backup/restore/doctor and the isolated synthetic demo pass. These are protocol/engine checks; native model-driven host acceptance remains as documented below.
- Backup includes acknowledged data still in WAL and excludes uncommitted writes. Restore preserves the backup bytes and refuses to replace an existing destination. No physical power-loss simulation was performed.
- Six portable archives cross-compile without CGo. Six-archive version/checksum/document checks pass. The extracted Linux amd64 binary passes selftest and generated hook/task-status acceptance. Native macOS/Windows execution is delegated to CI; cross-compilation is not native acceptance.

The fixed-inventory benchmark uses one stored memory and 1,000/10,000/100,000 synthetic recall events. The JSONL baseline decodes every historical event; SQLite opens the database and reads current memory state. Median results from three 10-iteration runs on AMD EPYC 9V74 (shared execution environment):

| Historical events | JSONL replay | SQLite state read |
| --- | ---: | ---: |
| 1,000 | 2.43 ms | 0.61 ms |
| 10,000 | 24.22 ms | 0.60 ms |
| 100,000 | 216.30 ms | 1.34 ms |

These are short storage microbenchmarks with scheduling noise, not end-to-end recall or hook guarantees. Current-memory scans, alternative-advice work, usage history and model calls are outside this comparison. Reproduce with `go test -run '^$' -bench BenchmarkSQLiteHistory -benchtime=10x -count=3`.


## Historical user updates and packaging checks

Executed 2026-10-03 on Linux amd64 with Go 1.23.12:

- All 40 Go tests pass with race detection; vet passes. New checks cover task-specific journal receipts, deduplicated counts, incomplete review, failed capture, pause, identity isolation, stale review commands, cached release checks, one notice per version, dismissal/disable, incompatible or unsafe metadata, timeout and response bounds.
- External MCP acceptance and generated Codex/Claude hook acceptance pass. Hook acceptance executes real record/status commands and verifies one-line output, no-lesson and failed-capture states. Host-agent extraction is still simulated in this check.
- Dashboard JavaScript syntax and the actual update-notice renderer pass Node checks for text rendering, dismissal, disabled state and unsafe links. A browser download failed with a truncated/invalid archive, so the new notice has not been visually verified in a real browser.
- Six portable archives build with version 0.5.0-pilot. Archive checksum, bundled-document link and version checks pass. The extracted Linux amd64 binary passes selftest and hook/task-status acceptance. Linux amd64 .deb creation, extraction, binary version and bundled update docs pass. System-wide installation was not performed.
- Archive, Debian/macOS and Inno version inputs derive from the Go binary version constant. Windows/macOS native installers were not built or executed here. The CI workflow defines native OS checks and portable archive artifacts; its remote results are not claimed before it runs.
- A normal native Codex 0.159.2 attempt timed out without completing a task. A follow-up probe reports HTTP 401 with an authentication-token parsing error. No hook trust or host permissions were bypassed. Claude Code is not installed. Native autonomous capture and final-line compliance remain unverified; use `scripts/native_host_acceptance.py` after restoring host login and normal hook trust.

At that revision, no GitHub release or hosted service was published. Update notices require published
release metadata and the matching platform archive. Release selection is not a data-format compatibility
proof. See [Update notifications](updates.md).

## Automatic memory, version 0.5

Executed 2026-10-03 in Linux amd64 with Go 1.23.12, using the current source:

- All 34 Go tests pass under `go test -race ./...`; `go vet ./...` passes.
- The source binary builds and all seven isolated `selftest` checks pass.
- `python3 scripts/automation_acceptance.py ./elephant` passes. It executes the generated Codex and Claude hook commands in separate shell processes, with paths containing spaces, quotes and shell metacharacters. It checks init idempotency, custom store/identity inheritance from nested directories, capture, one review per task, review recursion guards, the supplied record command, next-task recall, pause/resume, payload omission and visible hook failure reporting.
- Go tests also cover malformed/duplicate JSON rejection before writes, symlink config rejection, existing settings and instruction preservation, backups, concurrent event deduplication, identity isolation, hard recall budgets and unanchored capture.
- The existing external MCP acceptance passes: six tools, schemas, conditional recall, abstention, concurrent records/dashboard reads, strict writing checks and feedback deduplication.
- Dashboard JavaScript syntax and event rendering/escaping/empty-state checks pass in Node. The new Activity section was not rendered in a real browser in this run: the Chromium download failed. Earlier version 0.4 browser results below are historical, not a new UI validation claim.

Neither Codex nor Claude Code is installed in this execution environment. The new automatic hook flow is protocol-tested; a real model-driven host session remains a pilot check. The acceptance script simulates the host agent's lesson extraction. No test establishes that every real task will produce a lesson, that the lesson is correct, or that the agent will always follow the review request. Host hook approval remains a one-time user action. No new release archives or native installers were built for 0.5 in this run; use the source build.

## Historical version 0.4 validation

Original acceptance executed 2026-10-03 in Linux amd64 with Go 1.27.1. Additional macOS arm64 source-build validation is recorded below. At that historical revision the module declared Go 1.22 without external modules. The current SQLite branch requires Go 1.25+ and a pinned SQLite driver. Windows and Linux ARM native execution remain unverified.

## macOS arm64 source-build and Codex setup

Executed 2026-10-03 with Homebrew Go 1.27.1:

- `go test -race ./...` and `go vet ./...` pass.
- `go build -buildvcs=false -o elephant ./cmd/elephant` succeeds, and the native binary reports `0.4.0-pilot`.
- `./elephant selftest` passes all seven checks with an isolated temporary store.
- `python3 scripts/acceptance.py ./elephant` passes, including external MCP discovery, applicability and abstention, restart persistence, concurrent writes/dashboard reads, strict language checks, and feedback deduplication. This check needs local loopback access; the restricted shell could not run its dashboard, and the check passed outside that sandbox.
- `codex mcp add elephant -- /absolute/path/elephant mcp --root /absolute/path/project --project elephant` registers the binary in user-level Codex configuration. `codex mcp get elephant --json` confirms the enabled stdio entry.
- A native `codex app-server` session initializes and calls `mcpServerStatus/list` for Elephant. It discovers exactly `profile_memory`, `init_memory`, `recall_memory`, `record_memory`, `feedback_memory`, and `forget_memory`. This verifies native Codex tool discovery without making a model request; it does not establish autonomous lesson capture or a completed model-driven task.
- The repository now includes `AGENTS.md` with recall, evidence-backed capture and observed-feedback instructions. Existing Codex sessions need a restart to load the new connection.

These checks use a source-built macOS binary. They do not validate a macOS release archive, installer, signing or notarization. Claude Code, Pi and Cursor connections remain unverified.

## Executed checks

- 27 Go tests pass, including retrieval math, scope/tenant rules, unknown applicability, byte-budget serialization, feedback deduplication, retirement, journal corruption/locking, bounded manifest profiling, MCP initialization/tools/prompts, configuration generation and legacy-store preservation.
- `go test -race ./...` and `go vet ./...` pass.
- `selftest` passes all seven checks using an isolated temporary store.
- External Python subprocess MCP client: initialization, six-tool discovery, schema shape validation, two workflow prompts, record, conditional recall, missing-fact abstention, unrelated-task abstention, observed feedback and restart persistence pass. This validates Elephant's exposed schema shapes; it is not a full MCP conformance certification.
- Two independent MCP subprocesses concurrently record 20 distinct lessons while an actual dashboard endpoint polls. All 21 concurrency records survive replay. A further strict-policy Imprint brings the final total to 22. Repeating the same feedback observation across clients still counts once.
- JSON/TOML setup round-trip passes with paths containing spaces. Setup refuses to overwrite its existing files. Doctor can open a writable journal without writing a memory event.
- Headless Chromium: desktop/mobile rendering, library search, activity, budgeted recall, stable-ID feedback, setup help, recording requires/excludes, and current-fact recall pass. Browser restart retains conditions and inventory; unattached mode skips a nonexistent root and disables unavailable scopes. No script errors or horizontal overflow at 390px width. Memory Class labels, Map links/selection, and a persisted 100% writing target pass. A failing summary is rejected before a Memory is written. Source evidence stays exact. Preview data is explicitly synthetic.
- Go's CGO-free cross-compilation produces six platform archives. Only Linux amd64 extracted-package installation/execution is accepted in this environment. The installer verifies checksums, supports spaces and refuses a different existing binary without explicit replacement. Linux .deb control/data archives are built for amd64/arm64; amd64 extraction and execution pass. System-wide installation was not performed.

## Limits

The original Linux acceptance environment did not have Codex, Claude Code or Cursor installed. Native Codex tool discovery has since passed on macOS as recorded above; model-driven use and other client connections remain pilot acceptance tasks. Cross-compilation proves a build, not native execution, OS trust approval or installer behavior on that OS. The Windows PowerShell installer has not been executed here. macOS binaries are unsigned/not notarized.

No evidence yet establishes real coding-agent quality improvement, autonomous capture reliability, exact provider token savings, billing reduction or enterprise readiness. No public repository or hosted service was published. The STE target is not a compliance score; local checks do not validate the full standard. The dashboard's byte/4 estimate compares against naive loading of all eligible summaries; it does not measure savings relative to no memory or an optimized alternative.

The historical implementation replayed JSONL. The SQLite branch removes replay and adds transactional recovery; current-memory ranking and dashboard history remain scaling limits. Feedback observations are supplied evidence, not independent causal measurements. Retirement preserves historical journal content and is not physical erasure. Local identity labels are trusted configuration rather than enterprise authentication.

## Reproduce from source

```sh
go test -race ./...
go vet ./...
go build -buildvcs=false -o elephant ./cmd/elephant
./elephant selftest
python3 scripts/acceptance.py ./elephant  # Python 3.11+ for tomllib
python3 scripts/automation_acceptance.py ./elephant
ELEPHANT_GO=/path/to/go python3 scripts/build_release.py
```

Browser QA additionally uses Playwright/Chromium in the build environment. Synthetic UI data can be reproduced separately with `python3 examples/demo.py --binary ./elephant --dir /tmp/elephant-demo`; do not import this fabricated data into real user memory.
