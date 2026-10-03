# Validation: Elephant 0.5.0-pilot


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

Original acceptance executed 2026-10-03 in Linux amd64 with Go 1.27.1. Additional macOS arm64 source-build validation is recorded below. The module declares Go 1.22; there are no external Go modules. Windows and Linux ARM native execution remain unverified.

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

Storage replays an append-only journal; large-history scalability and robust crash recovery require further work. Feedback observations are supplied evidence, not independent causal measurements. Retirement preserves historical journal content and is not physical erasure. Local identity labels are trusted configuration rather than enterprise authentication.

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
