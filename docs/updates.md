# User status and release notifications

## One line after each task

After `elephant init`, restart the host and approve its updated hooks normally.
The end-of-task review supplies commands for recording lessons and reading task
status. The agent includes the exact status line once in its final response:

```text
Elephant: recalled 2 memories · saved 1 lesson.
Elephant: recalled 0 memories · no reusable lesson found.
Elephant: recalled 1 memory · review incomplete.
Elephant: recalled 0 memories · saved 0 lessons · 1 attempt rejected.
Elephant: recalled 0 memories · saved 0 lessons · capture failed.
Elephant: saved 1 lesson · a newer task replaced this review.
Elephant: saved 1 lesson · this review was already reported.
Elephant: automation paused.
```

Recalled counts are the unique hits delivered by the pre-task hook. Saved counts
are distinct Memory IDs acknowledged for this task. The record and its receipt
are appended together. Repeating an identical lesson can reuse a Memory ID;
repeating its save does not inflate the task count. Other tasks, sessions and
manual records do not increase this count. An old review command cannot complete
or record into a later task. One interrupted review keeps its own task, so its
lessons still save. The two lines above come from a stale `task-status` command.
They show a saved count and no recalled count. A review request alone is not completion or a save.

A rejected attempt is a lesson that Elephant refused on purpose: invalid fields,
sensitive content, the writing check or a near-duplicate. The agent gets the
reason and can resend or drop the lesson, so a rejection is not a failure. The
line shows rejections only when no lesson was saved. `capture failed` means that
a record call failed for another reason, such as a store or input error. It
stays on the line after a later save, because that save can be a different
lesson. Each event keeps a fixed reason code, never the error text.

`task-status` uses the config, hashed session and task ID supplied by the hook.
Add `--json` to inspect counts, IDs and state. `--review-complete` acknowledges
that the agent finished review; it does not independently verify lesson quality.
If the `task-status` command fails, the agent must report `Elephant: capture status
unavailable.` and the error in one sentence, rather than invent a count. The host still controls whether it runs
hooks and follows the output instructions. Native verification is separate from
the subprocess acceptance test.

## New Elephant releases

Session-start hooks check published GitHub releases at most once per 24 hours.
This check is on by default and contacts GitHub; see below for what is sent and
how to disable it. A notice includes the version, release title and upgrade link. Each version is
announced once per local journal. It never installs an update automatically.

```sh
elephant update               # Force a fresh check and print one line
elephant update --check       # Same explicit check (also works with --json)
elephant update --json        # Read cached status
elephant update --dismiss     # Dismiss the available version
elephant update --enabled=false
elephant update --enabled=true
```

The command checks availability; it does not replace the installed binary.
To install the latest release with the checksum-verified npm launcher, run
`npx --yes elephant-memory@latest`, then confirm with `elephant version`.
Restart running agents and Memory Palace to load the new binary. Older releases
require `elephant update --check` for a fresh lookup; plain `update` reads their cache.
Explicit checks print the available version and release link every time, even
when a background hook has already announced it. Disabled checks stay disabled.

Memory Palace shows cached availability, the last check time, an upgrade link,
and Check, Dismiss and Enable/Disable controls. Dashboard polling does not make
network requests. Each explicit Check forces a fresh lookup (unless checks are disabled), even within the 24-hour cache window. The button shows Checking while the request is pending and prevents duplicate clicks.

The request is a GET to the Elephant repository's GitHub releases endpoint. It
has a two-second timeout and a 256 KiB response limit; redirects are rejected.
No task text, prompts, project paths or Memories are sent. GitHub receives the
request's normal network metadata, such as your IP address, and a user agent
naming Elephant and its version. No token is needed for public releases. An
optional `ELEPHANT_GITHUB_TOKEN` environment variable raises GitHub's API rate
limit for the check. Credentials are not stored or logged. A failed check cannot
determine whether an update exists. The UI and CLI report a specific,
actionable reason for rejected credentials, rate limits, timeouts, network
failures and unreadable release metadata. Diagnostics survive
dashboard polling and restarts; credentials and remote error bodies are never saved.
The displayed timestamp is the last attempt, not proof of a successful check.

If you hit rate limits, optionally provide `ELEPHANT_GITHUB_TOKEN` to the process that
launches Elephant (including Memory Palace). A token with no special permissions is
enough for public release metadata. Restart that process after changing its
environment, then click Check again. Signing in to GitHub in a browser does not
authenticate Elephant. Do not paste a token into the dashboard, a Memory, or a
committed configuration file. An old cached failure without a reason asks for a
fresh check. Empty successful release results mean no newer matching release was
found; offline or rate-limited failures never mean the installation is current.

To opt out, run `elephant update --enabled=false` (persistent) or set
`ELEPHANT_UPDATE_CHECKS=0` for a process.

A candidate must be published, newer, on the same major version, and have the
ZIP asset for the current OS/architecture. Pilot-channel installations accept pilot or
stable releases; stable installations do not move to prereleases. RC builds
stay on the RC or stable channel. This is a release selection rule, not proof
of data-format or runtime compatibility. Read release notes before upgrading.
Archive versions derive from the Go version constant. The cache is refreshed after the installed binary version changes. A maintainer must publish
the release and its assets before availability can be announced.

Cache and dismissal settings live beside the journal in `.updates.json`; the
short-lived update lock is separate from the memory journal lock. Lookup failure
does not block the coding task or change memory data. Hook notices use the host's
`systemMessage` field, while the memory summary is agent-authored final output.

## macOS menu-bar installation

**Updates** treats Elephant as one product. It covers every distinct binary used
by a connected MCP server or Memory Palace, the menu-bar companion's own binary,
and the `elephant` found on `PATH`. Symlinks resolve to their real file, so a
binary shared by several connections counts once. The menu shows each binary's
on-disk version, the cached release status, and the last check time. Release
status comes from the oldest connection's store. Merely opening or refreshing the
menu makes no network request. **Check for Updates** uses the same explicit check
and opt-out policy as the CLI. Release notes open the canonical GitHub page.

**Install Elephant … Everywhere…** shows one native confirmation. It lists every
binary it will replace with its current version. It also lists binaries that are
already current or whose version cannot be read; those are not changed. It states
the checksum trust limits and restart impact. Cancel is the default. For each
binary, installation downloads only the matching published platform archive and
its `SHA256SUMS`, validates size/checksum/executable platform and reported
version, then atomically replaces that file. Binaries are installed one at a
time. A failure leaves that binary unchanged and does not stop the others. The
result lists each installed and failed binary, and the menu keeps offering the
release for any binary that still needs it. A directory-flush warning after
replacement reports that installation committed but durability could not be
confirmed.

Copies in the npm launcher's per-version cache (`~/.elephant/bin/<tag>`) are
listed but never replaced, because each cache directory names its version. Update
those with `npx --yes elephant-memory@VERSION`.

Releases are unsigned. A checksum proves agreement with the published asset,
not the publisher's identity. Downloads trust GitHub TLS and permit only its
release-delivery redirect hosts; credentials are not forwarded to those hosts.

After installation the menu shows **Restart Needed**. Running MCP servers and
the companion retain their old process image until explicitly restarted.
Installation never interrupts or restarts them automatically. A file that changes
during preparation is not replaced; an unwritable installation must be updated
with the tool that owns it. Linux/Windows have no native menu-bar installer.

## Verification

```sh
go test -race ./...
go vet ./...
go build -buildvcs=false -o elephant ./cmd/elephant
python3 scripts/acceptance.py ./elephant
python3 scripts/automation_acceptance.py ./elephant
python3 scripts/build_release.py
python3 scripts/package_acceptance.py
```

For native sessions, prepare an isolated fixture, approve hooks in the chosen
host normally, then run the two-task check. No trust/permission bypass is used:

```sh
python3 scripts/native_host_acceptance.py ./elephant --agent codex --workspace /absolute/path/acceptance
# Open the printed project in Codex and approve its hooks.
python3 scripts/native_host_acceptance.py ./elephant --agent codex --workspace /absolute/path/acceptance --run
```

Use `--agent claude` for Claude Code. Inspect sources and applicability yourself
after a successful native run. Logs and stores remain in the isolated workspace;
they are not telemetry or committed validation artifacts.
