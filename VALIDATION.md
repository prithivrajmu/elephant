# Validation: Elephant 0.4.0-pilot

Executed 2026-10-03 in Linux amd64 with Go 1.27.1. The module declares Go 1.22; there are no external Go modules. This release has not been natively run on macOS, Windows or Linux ARM.

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

Codex, Claude Code and Cursor are not installed in this environment. Their recipes were checked against current official documentation, but an actual native client connection remains a pilot acceptance task. Cross-compilation proves a build, not native execution, OS trust approval or installer behavior on that OS. The Windows PowerShell installer has not been executed here. macOS binaries are unsigned/not notarized.

No evidence yet establishes real coding-agent quality improvement, autonomous capture reliability, exact provider token savings, billing reduction or enterprise readiness. No public repository or hosted service was published. The STE target is not a compliance score; local checks do not validate the full standard. The dashboard's byte/4 estimate compares against naive loading of all eligible summaries; it does not measure savings relative to no memory or an optimized alternative.

Storage replays an append-only journal; large-history scalability and robust crash recovery require further work. Feedback observations are supplied evidence, not independent causal measurements. Retirement preserves historical journal content and is not physical erasure. Local identity labels are trusted configuration rather than enterprise authentication.

## Reproduce from source

```sh
go test -race ./...
go vet ./...
go build -buildvcs=false -o elephant ./cmd/elephant
./elephant selftest
python3 scripts/acceptance.py ./elephant  # Python 3.11+ for tomllib
ELEPHANT_GO=/path/to/go python3 scripts/build_release.py
```

Browser QA additionally uses Playwright/Chromium in the build environment. Synthetic UI data can be reproduced separately with `python3 examples/demo.py --binary ./elephant --dir /tmp/elephant-demo`; do not import this fabricated data into real user memory.
