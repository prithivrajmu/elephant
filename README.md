# 🐘 Elephant

**Persistent experience for coding agents.**

**Agents forget. Elephants don’t.**

Elephant is a local, repository-aware experience layer for coding agents. It recalls relevant lessons before work, records tool-event metadata during work, and asks the current agent to save useful lessons from observed results. CLI JSON and MCP share one engine and journal. No extra model key or model-provider dependency is required.

## Product goal

Run `elephant init` once in a project, complete the host's normal hook approval, then work normally. Recall and recording should happen without repeated “remember this” prompts.

An **Experience** records an observed event. A **Memory** contains a reusable lesson with source evidence and applicability conditions. Automatic event capture and successful learning are separate:

| Stage | Current behavior |
| --- | --- |
| Initialize | Install project-local Codex and Claude Code hooks, preserve existing settings, and add agent guidance. |
| Recall | Use project Fingerprint, task terms, scope and conditions to select lessons within a default 4,000-byte Recall Budget. |
| Observe | Append tool name, event, status and structured exit code when available. Keep prompts, tool arguments, output and transcripts out of the journal. |
| Review | Request one end-of-task review. The current agent can save zero to three evidence-backed lessons. Zero is valid when nothing reusable was learned. |
| Reuse | Retrieve the saved lesson on a related task. Record usefulness only after applying it and observing its effect. |

Project scope is the default for automatic lesson review. Personal scope is for transferable experience; team lessons require human review. Recalled Memories are evidence to check against the current task and instructions.

## Current state

**Version: `0.5.0-pilot`. Automatic-memory implementation is merged into `main` through [PR #1](https://github.com/prithivrajmu/elephant/pull/1).**

| Area | Status |
| --- | --- |
| Local engine, CLI, MCP and Memory Palace | Implemented; six MCP tools, scoped recall, feedback, retirement and local reviewed export/import. |
| Automatic Codex and Claude Code adapters | Implemented for macOS/Linux; generated hook commands pass subprocess acceptance. Real model-driven automatic sessions remain unverified. |
| Validation | [Recorded 0.5 checks](VALIDATION.md#automatic-memory-version-05): 34 Go tests with race checks, vet, isolated self-test, MCP acceptance and simulated hook lifecycle. |
| Native client evidence | Historical 0.4 macOS source-build and Codex MCP discovery passed. This does not validate the new 0.5 automatic loop. |
| Distribution | Source build is the current 0.5 path. No GitHub release is published; archive and installer scripts still contain 0.4 versions. |
| Storage | Append-only JSONL with lock retries and corruption detection. Indexed storage, retention and automated recovery remain planned. |

Use [AUTOMATION.md](AUTOMATION.md) for hook behavior and compatibility, [VALIDATION.md](VALIDATION.md) for executed checks, and [BRAND.md](BRAND.md) for the product vocabulary.

## Start today

Build the current source with Go 1.22+ from the repository root. There are no external Go modules.

```sh
go test ./...
go build -buildvcs=false -o elephant ./cmd/elephant
./elephant version
./elephant selftest
./elephant doctor --root /absolute/path/project --project my-project
./elephant init --root /absolute/path/project --project my-project
```

Keep the binary at a stable absolute path: installed hooks refer to it. The default installs both adapters; add `--agent codex` or `--agent claude` to choose one. Restart your agent in the target project and complete its normal hook approval. In Codex, use `/hooks`. This route does not require separate MCP registration.

After a real task, inspect the same project's events and Memories:

```sh
./elephant automation --root /absolute/path/project
./elephant experiences --root /absolute/path/project
./elephant status --root /absolute/path/project
./elephant palace --root /absolute/path/project
```

The Memory Palace runs at http://127.0.0.1:7331. An empty first recall is expected. Received events prove that hooks ran; `review_requested` proves only that review was requested. Inspect a saved Memory ID and source, restart the client, then confirm recall on a related task and abstention on an unrelated task.

Pause with `automation --root /absolute/path/project --enabled=false`; resume with `--enabled=true`. Windows and other agents use `setup --wizard` plus MCP registration or CLI JSON. That route relies on agent guidance for capture. See [QUICKSTART.md](QUICKSTART.md) and [CLIENTS.md](CLIENTS.md).

New installs use `~/.elephant/events.jsonl`; an existing legacy `~/.agent-memory/events.jsonl` is reused when no modern journal exists. Set a custom journal during init with `--store /absolute/path/events.jsonl`. Later commands from the installed project or with its `--root` reuse that config. Use a stable project ID across checkouts.

**0.5 compatibility:** CLI `init` now installs automation. Replace old discovery-only scripts with `recall --initialize`; MCP `init_memory` is unchanged. Older binaries reject journals containing the new `experience` events, so preserve a backup before upgrading and do not reuse that journal with an older binary.

`--budget` counts UTF-8 bytes of returned experience text. For CLI JSON, inject only the `context` field; match metadata and tool wrappers are outside that budget. Current retrieval is lexical, with applicability gates; semantic embeddings are not implemented.

Memory summaries use an 80% writing target by default. Set 100% for strict local checks. This is a product setting, not a measured compliance score; see [LANGUAGE_POLICY.md](LANGUAGE_POLICY.md).

### Record a real lesson

```sh
./elephant remember --root /path/to/project --project my-dashboard \
  --file examples/lesson.json
```

Replace the example incident and source with actual evidence. Default scope is **personal**, which allows reuse in your other projects. Choose **project** for local reviewer preferences, workflow policy, or anything that must not transfer. Choose **conversation** for private advice restricted to the current conversation ID. Choose **team** for a draft that needs review.

```json
{
  "scope": "personal",
  "outcome": "worst",
  "incident": "Unbounded parallel queries exhausted the warehouse pool.",
  "lesson": "Bound active query concurrency to the available pool capacity and observe queue wait time before increasing it.",
  "source": "incident-report-42",
  "subject": "warehouse-concurrency",
  "features": {
    "language": ["typescript"],
    "framework": ["nextjs"],
    "app": ["analytics-dashboard"],
    "database": ["snowflake"]
  },
  "requires": {"database": ["snowflake"]},
  "excludes": {"style": ["offline-only"]}
}
```

All four outcomes are retained: `good`, `great`, `bad`, `worst`. All four outcomes remain recorded; dramatic outcomes receive no unvalidated ranking boost. **Usefulness** means whether applying the lesson subsequently helped, not whether the original incident succeeded.

After applying a retrieved lesson:

```sh
./elephant feedback --project my-dashboard --id MEMORY_ID \
  --feedback-id task-123 --helpful=true
```

Use a stable observation ID. Repeating the same user's feedback ID for the same memory is idempotent. Feedback is supplied evidence, not independently verified causal attribution. To retire an obsolete lesson:

```sh
./elephant forget --id MEMORY_ID
```

Retirement removes future retrieval, but retains the append-only historical journal. It is **not physical deletion**. Do not use this prototype to store secrets or data requiring regulated erasure.

## Harness-agnostic integration

There are two portable interfaces: CLI JSON for any harness that can run a subprocess, and a minimal **MCP stdio** server for clients supporting MCP tools. No dependency on a particular model, IDE, coding agent, or LLM provider. It does not install itself into your current ChatGPT session.

Example MCP client configuration (the client's configuration location varies):

```json
{
  "mcpServers": {
    "elephant": {
      "type": "stdio",
      "command": "/absolute/path/elephant",
      "args": [
        "mcp", "--root", "/absolute/path/project",
        "--project", "my-dashboard",
        "--store", "/absolute/path/events.jsonl",
        "--tenant", "local", "--user", "me"
      ]
    }
  }
}
```

Tools: `profile_memory`, `init_memory`, `recall_memory`, `record_memory`, `feedback_memory`, `forget_memory`. `init_memory` profiles the configured root and retrieves lessons. Re-query when the task changes rather than re-injecting everything. The MCP client controls whether and when calls happen. A literal `/initmemory` command is harness-specific; map it to `init_memory` in the harness or use the portable instruction text in [AGENT_INTEGRATION.md](AGENT_INTEGRATION.md).

The server's project root, tenant, user and team are fixed at startup; agent arguments cannot change identity or approve team drafts. MCP follows newline-delimited JSON-RPC over stdio, negotiates supported protocol versions, and implements initialization, ping, tools/list, tools/call, prompts/list and prompts/get. It exposes `initmemory` and `memory_review` workflow prompts. It is a deliberately small implementation, not a claim of certification across every MCP client. Tested with an automated stdio smoke test; exercise your chosen client's configuration before relying on it.

## Fingerprint and Signals

The profiler reads bounded regular files from the project root: `package.json`, `tsconfig.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, and `.elephant.json`. It recognizes common JavaScript frameworks, language markers, AWS/Azure SDK dependencies, SQL/NoSQL drivers, and records manifest evidence. Dependency presence indicates a candidate identifier, not proof of actual production architecture.

It does not traverse all source files or infer people from Git history. Put project intent, database configuration, workflow policy, working style, reviewers and contributors in explicit labels:

```json
{
  "features": {
    "app": ["analytics-dashboard"],
    "database": ["snowflake"],
    "cloud": ["aws"],
    "workflow": ["stacked-prs", "review-required"],
    "style": ["small-prs", "tests-before-merge"],
    "reviewer": ["technical-lead"]
  }
}
```

Save this as `.elephant.json` in your project. Identifiers are case normalized; other aliases are not inferred. An optional future agent extraction step can propose labels and applicability conditions. Keep identifiers broad enough to retrieve, and use `requires`/`excludes` to prevent unsafe transfer. Each required dimension must match at least one listed value; an excluded match blocks retrieval. A missing required identifier blocks retrieval. Similar language alone is not sufficient evidence that advice is safe.

## Memory Palace

The embedded, dependency-free UI includes:

- Active memory count, draft/retired totals, and a 14-day cumulative memory chart.
- Searchable incident cards with source, project fingerprint and applicability.
- Observed helpful/unhelpful feedback, retrieval counts and p95 core retrieval latency.
- Estimated context tokens avoided against loading all visible, applicable summaries.
- A recall playground with byte budget and outcome feedback controls.
- A form to Imprint a Memory with source evidence.
- A Memory Map with origin, Signal, Memory and evidence Trails.
- A writing target control for local summary checks.

The loopback UI uses a process session token, strict Host/Origin checks, no CORS, escaped user content and a restrictive script CSP. These controls are for local use. Anyone with access to the local process/files is trusted; tenant/user labels are not authentication. The page refreshes every 10 seconds. Retrieval activity omits task text and stores lesson IDs, byte counts and core latency. Core latency includes journal replay/ranking, but excludes event serialization, fsync, model inference and agent work.

**Token savings are estimates, not measured provider billing reductions.** The estimate is `ceil(bytes / 4)`; it varies with language, text, tokenizer, tool wrappers and prompt caching. The baseline intentionally assumes naive full loading of eligible summaries. This is not a comparison against an optimized competing retrieval layer. Useful memory may add tokens compared with no memory. Quality and execution efficiency are not claimed by this dashboard.

## Local Herd sharing

To create and approve a team draft, consistently use `--tenant`, `--user` and `--team`:

```sh
./elephant record --team platform --file my-team-lesson.json
./elephant approve --team platform --id MEMORY_ID
./elephant export --team platform --file approved-team-lessons.json

# A peer imports into their own store/identity. Imports are drafts again.
./elephant import --user peer --team platform \
  --file approved-team-lessons.json
./elephant approve --user peer --team platform --id IMPORTED_ID
```

`my-team-lesson.json` must have `"scope":"team"`. Source and origin project remain attached; importing records who accepted custody in the receiving store. Approval is a trusted human CLI operation, absent from MCP and the UI. Approval signifies review, not cryptographic provenance. File imports preserve evidence but don't verify authorship. Do not put different untrusted enterprise users in this same local file store.

The journal serializes writers with a lock directory and fsyncs appended events. Busy readers/writers retry for up to three seconds before returning a clear error. Corrupt/truncated logs fail closed; they are not silently discarded. If a writer crashes leaving `events.jsonl.lock`, verify it stopped before removing that directory. The MVP replays the journal in O(events) and scans memories for relevance. Alternative-advice metadata currently adds worst-case O(memories²) work to baseline estimation; use SQLite with indexes for the next local milestone, and authenticated Postgres for enterprise service deployment. Large peer imports are validated up front but individual records are appended separately: an I/O failure can leave a partial import. Rerun safely to deduplicate it.

## Synthetic demo

```sh
python3 examples/demo.py --binary ./elephant
./elephant palace --root .demo/project --project demo-dashboard \
  --store .demo/events.jsonl --team demo-platform
```

All demo incidents and outcomes are invented and marked **SYNTHETIC DEMO**. They demonstrate the UI, not actual product effectiveness. The demo is isolated from your real store. Delete `.demo` to reset it. The ZIP does not include a compiled platform-specific binary; build for your machine.

## Next steps and acceptance criteria

The next milestone is a **verified automatic local pilot**. Prioritize a complete init → recall → observe → review → save → reuse loop.

| Priority | Work | Acceptance criterion |
| --- | --- | --- |
| 1. Native automatic loop | Run real Codex and Claude Code tasks on macOS/Linux. Record host/model versions and hook approval steps. Test no-lesson tasks, failures, restart, pause/resume and custom stores. | Without a repeated memory prompt, a justified lesson is saved with an ID and actual evidence, survives restart and appears on a related task. Unrelated or inapplicable tasks abstain; review does not loop. |
| 2. Reproducible 0.5 distribution | Align binary, archive, installer and package versions; ship automation and referenced integration docs. Add CI for Go/race/vet, MCP and hook acceptance, plus release checksums. | Build a clean 0.5 package, install it on each claimed platform, and complete its supported workflow. Record native acceptance separately from cross-compilation before publishing a release. |
| 3. Capture reliability and storage | Make review requested, Memory saved, no lesson justified and capture failed distinguishable. Cover interrupted review/write and safe retry. Benchmark growing event histories, then migrate to indexed SQLite with versioned migration, backup/restore and retention. | Restart/fault checks preserve every acknowledged Memory, retries avoid duplicate evidence, and recovery never silently discards history. Publish end-to-end hook and recall latency as history grows. |
| 4. Memory quality and retrieval | Add evidence revisions, reviewed supersession and stale-condition handling. Build a versioned relevance set with related, unrelated, unknown-condition and false-transfer cases. | Report capture misses, unsupported lessons, relevance/abstention judgments and budget-quality tradeoffs against the lexical baseline. Feedback from one task must not become multiple independent votes. |
| 5. Developer pilot and measured value | Test with 3–5 developers across two projects and an unattached conversation. Use [PILOT_FEEDBACK.md](PILOT_FEEDBACK.md); compare paired tasks with and without memory after lifecycle acceptance. | Report setup success, time to first real Memory, persistence, capture reliability, observed usefulness, actual token usage where available, latency and rework. Include review/retrieval overhead. |

The existing [PILOT_PLAN.md](PILOT_PLAN.md) gives broader evaluation and product priorities. Its prebuilt-package readiness describes earlier pilot work; use the 0.5 state and release gates above for current automation.

Keep richer Memory Map connections, semantic/hybrid retrieval, authenticated Herd sharing and hosted service work after local reliability and retrieval evidence. A synthetic demo and estimated tokens avoided do not establish productivity or billing savings.

## Validation

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -bench BenchmarkRecall1000 -benchmem ./...
```

Tests cover scope/tenant isolation, applicability, budgets, ranking, conflicts, feedback deduplication, persistence, corruption/locks, manifest extraction, MCP lifecycle, UI boundaries and peer review. See [DESIGN.md](DESIGN.md) for math, growth strategy, evaluation and enterprise requirements.

MIT licensed. The repository is currently private; no hosted service is included.

## Conversations without a project

Use `--unattached --conversation CONVERSATION_ID` with record, recall, MCP or UI. The project field stays empty and the profiler never scans the ambient working directory. Personal memories remain private and reusable across your conversations; conversation scope restricts them to the exact conversation ID. `recall --task` requires an exact content-term match, while `recall --initialize` without a task explicitly allows known-profile discovery. See [ASTRA_REVIEW.md](ASTRA_REVIEW.md) for findings, revised math, examples and the connection roadmap.

```sh
./elephant record --unattached --conversation design-talk --file examples/untagged-lesson.json
./elephant recall --unattached --conversation other-talk --task "validation evidence for reviewers"
./elephant ui --unattached --conversation design-talk
```

For known technical facts without a repository, MCP retrieval accepts `context_features`; CLI retrieval accepts `--context-file` with `{"features":{"database":["snowflake"]}}`. Missing required facts fail closed. These asserted facts never grant permission. Automatic hooks can request evidence-based extraction from the current agent; the engine does not extract from transcripts itself. Conversation project inference and multi-project graph storage remain proposed extensions.

Conversation scope isolates recall and feedback. The owner management dashboard intentionally lists all owned memories across projects/conversations, as well as currently visible shared memories. It is not a conversation-only management view.
