# Memory Palace

## Memory Palace

Start the dashboard with `elephant palace`. It prints one link to the terminal that started it:

```text
Elephant Memory Palace: http://127.0.0.1:7331/#token=<per-run token>
```

Open that exact link. The token is new for each `elephant palace` run and authorizes every `/api/*` request, so do not share or paste the link. The token travels in the URL fragment (`#token=`), which the browser does not send to the server. The page stores it in `sessionStorage` for reloads in that tab and removes it from the address bar. Opening `http://127.0.0.1:7331/` without the link shows "Open the link printed by elephant palace" and makes no API calls. The page HTML does not contain the token, so other local users or processes cannot read it with a plain `GET /`. Restart `elephant palace` to get a new token; old links then stop working.

The embedded, dependency-free UI includes:

- Active memory count, draft/retired totals, and a 14-day cumulative memory chart.
- Searchable incident cards with source, project fingerprint and applicability.
- Observed helpful/unhelpful feedback, retrieval counts and p95 core retrieval latency.
- Estimated context tokens avoided against loading all visible, applicable summaries.
- A recall playground with byte budget and outcome feedback controls.
- A form to Imprint a Memory with source evidence.
- A Memory Map with origin, Signal, Memory and evidence Trails.
- A writing target control for local summary checks.

The loopback UI uses the per-run token from the printed link, strict Host/Origin checks, no CORS, escaped user content and a restrictive script CSP. These controls are for local use. Anyone with access to the local process/files is trusted; tenant/user labels are not authentication. The page refreshes every 10 seconds. Retrieval activity omits task text and stores lesson IDs, byte counts and core latency. Core latency includes transaction acquisition, memory loading and ranking, but excludes event serialization, fsync, model inference and agent work.

**Token savings are estimates, not measured provider billing reductions.** The estimate is `ceil(bytes / 4)`; it varies with language, text, tokenizer, tool wrappers and prompt caching. The baseline intentionally assumes naive full loading of eligible summaries. This is not a comparison against an optimized competing retrieval layer. Useful memory may add tokens compared with no memory. Quality and execution efficiency are not claimed by this dashboard.

## Synthetic demo

```sh
python3 examples/demo.py --binary ./elephant
./elephant palace --root .demo/project --project demo-dashboard \
  --store .demo/memories.sqlite --team demo-platform
```

All demo incidents and outcomes are invented and marked **SYNTHETIC DEMO**. They demonstrate the UI, not actual product effectiveness. The demo is isolated from your real store. Delete `.demo` to reset it. The ZIP does not include a compiled platform-specific binary; build for your machine.
