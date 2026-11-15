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
- Observed helpful/unhelpful feedback, retrieval counts, core p50/p75/p95, and local phase timings.
- Estimated context tokens that recall added, with the all-summaries baseline as a comparison. This is not a saving against using no memory.
- A recall playground with byte budget and outcome feedback controls.
- A form to Imprint a Memory with source evidence.
- A brain-inspired Memory Palace with four regions for wins, lessons, warnings and scars.
- Selectable memory nodes, shared-signal or same-project connections, and zoom controls.
- Search and class filters across all active memories, with a scrollable full-text list.
- A separately scrollable inspector for lessons, incidents, evidence and applicability.
- Responsive layouts, keyboard-accessible nodes and dialogs, and wrapped long text.
- A writing target control for local summary checks.

The palace is the default view. Its graph shows up to 120 matching active memories;
the list includes every match. Search by lesson, incident, evidence, project, subject
or signal to find older memories. Connections are a sparse sample of actual shared
signals or matching nonempty projects. They do not imply causation or biological
brain activity. Node size reflects helpful feedback. Retired memories remain in
the Library. Selection and zoom survive unchanged ten-second refreshes.

Engine update controls are under **Engine settings & updates**. The Library,
Overview and Activity views remain available in the navigation.

Run `node scripts/dashboard_palace_check.js` for graph and interaction regression
checks, and `node scripts/dashboard_update_check.js` for update-control checks.

The loopback UI uses the per-run token from the printed link, strict Host/Origin checks, no CORS, escaped user content and a restrictive script CSP. These controls are for local use. Anyone with access to the local process/files is trusted; tenant/user labels are not authentication. The page refreshes every 10 seconds. Retrieval activity omits task text and stores lesson IDs, byte counts, candidate/manifest counts and local phase timings. Core latency includes transaction acquisition, store open, candidate loading, ranking and baseline rendering, but excludes receipt serialization, commit/fsync, model inference and agent work. See [Performance measurement and ratchets](performance.md).

**Token savings are estimates, not measured provider billing reductions.** The estimate is `ceil(bytes / 4)`; it varies with language, text, tokenizer, tool wrappers and prompt caching. The baseline intentionally assumes naive full loading of eligible summaries. This is not a comparison against an optimized competing retrieval layer. Useful memory may add tokens compared with no memory. Quality and execution efficiency are not claimed by this dashboard.

## macOS menu bar

On macOS, `elephant mcp` and `elephant palace` show a monochrome elephant in the
menu bar while they run. Concurrent processes share one icon. Short CLI commands
and automatic hooks alone do not keep an icon running.

- **Sessions** groups connections by project, with client names and short instance
  IDs. Pick a connection to open its token-protected Memory Palace, inspect its
  process/version/conversation metadata, or give it a temporary nickname. Its
  dashboard uses a random loopback port and that exact connection's context.
- Connections are not necessarily chats or foreground tasks. Client identity
  comes from MCP initialization; conversation IDs appear only when supplied.
  Last activity records a tool name, time, and success/failure, never arguments,
  output, prompts, error text, or memory content.
- **Dreaming** exposes status, next eligibility, last results, bounded manual
  review, configuration, pause/resume, and cancellation for the selected context.
  Review/setup/resume dialogs ask for consent before provider use or hook setup.
  See [Dreaming](dreaming.md#macos-menu-controls) for the remaining safety gates.
- **Updates** treats Elephant as one product. It reads cached availability, checks
  on request, and offers one confirmed, checksum-verified installation into every
  Elephant binary in use: connected processes, the companion, and `elephant` on
  `PATH`. npm launcher cache copies are not replaced. Running processes are never
  restarted automatically. See [Updates](updates.md#macos-menu-bar-installation).
- **Health & Diagnostics** checks the store, context, hooks, and configured
  reviewer executable. Provider authentication and host approval remain unchecked.
  Clipboard diagnostics use an allowlist that excludes paths, identities, names,
  memory text, activity content, and credentials.
- **Project Controls** reveals the project in Finder, pauses/resumes automatic
  hooks, or explicitly sets up/repairs the chosen agent's hooks. Pausing hooks does
  not disable explicit MCP calls. Controls refuse another store/identity context.
- Dialogs use the supplied Elephant artwork, also used in the dashboard and README.
  The monochrome menu-bar silhouette is unchanged.
- **Hide Menu Bar** closes only the companion and its dashboard listeners. It does
  not stop MCP servers or agent sessions. A new Elephant process can show it again.
- The companion exits when the last connected Elephant process exits, including
  after an abrupt disconnect.

Disable the icon for a process with:

```sh
ELEPHANT_MENU_BAR=0 elephant mcp
```

For an MCP client, set `ELEPHANT_MENU_BAR` to `0` in that server's environment.
Icon failures warn on stderr without changing MCP protocol output. The companion
uses macOS's built-in AppKit/JXA runtime. The first launch prepares a private cached
app bundle with system tools; later launches reuse it. It needs a logged-in desktop
session, not Swift, Xcode, or an additional package. Linux and Windows behavior is unchanged.

Existing processes keep the executable they started with. After installing this
build, hide an older companion and restart the intended MCP server with the new
binary. No agent process is stopped or replaced by the menu bar.

## Synthetic demo

```sh
python3 examples/demo.py --binary ./elephant
./elephant palace --root .demo/project --project demo-dashboard \
  --store .demo/memories.sqlite --team demo-platform
```

All demo incidents and outcomes are invented and marked **SYNTHETIC DEMO**. They demonstrate the UI, not actual product effectiveness. The demo is isolated from your real store. Delete `.demo` to reset it. The ZIP does not include a compiled platform-specific binary; build for your machine.
