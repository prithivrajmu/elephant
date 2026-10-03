# 🐘 Elephant

**Persistent experience for coding agents.**

**Agents forget. Elephants don’t.**

Elephant stores useful experience from earlier agent work. It reads the current environment and recalls relevant Memories. A Recall Budget limits the returned text. The engine works through CLI JSON or MCP. It has no model-provider dependency.

Version 0.4 is a local pilot. The agent must record an observed result. Elephant does not extract lessons from whole conversations in the background. No model key is needed.

See [BRAND.md](BRAND.md) for the product terms. See [LANGUAGE_POLICY.md](LANGUAGE_POLICY.md) for the default 80% writing target. Set it to 100% to enforce the local checks. This setting does not prove full ASD-STE100 compliance.

## Start today

Use the prebuilt package for your OS: [QUICKSTART.md](QUICKSTART.md) walks through installation, an isolated self-test, connecting your agent, and recording/retrieving the first real lesson. No Go installation is required for these packages.

```sh
elephant version
elephant selftest
elephant doctor --root /path/to/project --project my-dashboard
elephant setup --root /path/to/project --project my-dashboard --output elephant-setup
elephant palace --root /path/to/project --project my-dashboard
```

Merge the generated client config and agent instructions, restart the client, then confirm the tools are visible. See [CLIENTS.md](CLIENTS.md) and [PILOT_PLAN.md](PILOT_PLAN.md). The dashboard runs at http://127.0.0.1:7331. The first run is empty; lessons require observed evidence and an explicit record call.

New installs use `~/.elephant/events.jsonl`; an existing legacy `~/.agent-memory/events.jsonl` is reused when no modern journal exists. Pass `--store /absolute/path/events.jsonl` consistently for another journal. All paths in generated setup are absolute. `--budget` counts UTF-8 bytes of returned experience text; only inject the `context` field from CLI JSON, not its match metadata.

For source builds, install Go 1.22+ and run `go test ./...` then `go build -buildvcs=false -o elephant ./cmd/elephant`. No external Go modules are required.

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

## Validation

```sh
go test ./...
go test -race ./...
go vet ./...
go test -run '^$' -bench BenchmarkRecall1000 -benchmem ./...
```

Tests cover scope/tenant isolation, applicability, budgets, ranking, conflicts, feedback deduplication, persistence, corruption/locks, manifest extraction, MCP lifecycle, UI boundaries and peer review. See [DESIGN.md](DESIGN.md) for math, growth strategy, evaluation and enterprise requirements.

MIT licensed. No public repository has been created or published by this deliverable.

## Conversations without a project

Use `--unattached --conversation CONVERSATION_ID` with record, recall, MCP or UI. The project field stays empty and the profiler never scans the ambient working directory. Personal memories remain private and reusable across your conversations; conversation scope restricts them to the exact conversation ID. `recall --task` requires an exact content-term match, while `init` without a task explicitly allows known-profile discovery. See [ASTRA_REVIEW.md](ASTRA_REVIEW.md) for findings, revised math, examples and the connection roadmap.

```sh
./elephant record --unattached --conversation design-talk --file examples/untagged-lesson.json
./elephant recall --unattached --conversation other-talk --task "validation evidence for reviewers"
./elephant ui --unattached --conversation design-talk
```

For known technical facts without a repository, MCP retrieval accepts `context_features`; CLI retrieval accepts `--context-file` with `{"features":{"database":["snowflake"]}}`. Missing required facts fail closed. These asserted facts never grant permission. Conversation project inference, multi-project graph storage and automatic extraction are proposed extensions, not included functionality.

Conversation scope isolates recall and feedback. The owner management dashboard intentionally lists all owned memories across projects/conversations, as well as currently visible shared memories. It is not a conversation-only management view.
