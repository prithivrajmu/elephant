# Opt-in cloud sync and reviewed team sharing

Elephant never uploads memories automatically: hooks, MCP and startup do not
sync, and local use needs no cloud account. Sync happens only when you run these
explicit commands. They require a configured hosted service (experimental), an
OAuth access token and selected private scopes. Release checks are a separate,
default-on network call to GitHub that carries no memory or task data; see
[Update notifications](updates.md).

```sh
# Set ELEPHANT_CLOUD_TOKEN in your shell from your configured OAuth issuer.
# Keep tokens out of files, command arguments and shell history.
elephant sync --cloud https://elephant.example --cloud-tenant acme \
  --cloud-user YOUR_OAUTH_SUBJECT --sync-scopes personal,project
```

Use the same origin, cloud identity and scope selection on subsequent runs.
Changing them creates a separate synchronization binding. Project scope uploads
all your project lessons, not just the current directory; conversation scope
uploads all your conversation lessons. Team lessons and captured hook events
are excluded. Sync shares lesson content, applicability, timestamps and
retirement. Feedback counters come from the hosted service; local task votes
are not merged as counters. This avoids double-counting independent histories.

### Project IDs

The default project ID is the absolute project root, which can reveal user and
directory names. Sync never uploads a path-derived project ID (POSIX absolute,
Windows drive or UNC path). It sends an opaque, stable ID instead:
`p-` followed by the first 32 hex characters of SHA-256 over the cloud tenant,
a NUL byte and the local project ID. Explicit non-path IDs set with `--project`
are sent unchanged, so choose a cloud-safe name if you want a readable ID.
Pull keeps the local project ID for records that already exist locally; records
first imported from the cloud keep the opaque ID. Hosted recall matches
project-scope lessons by exact `project_id`, so hosted MCP clients must send the
same opaque ID, or every machine must use the same explicit `--project` ID:

```sh
echo "p-$(printf '%s\0%s' TENANT /absolute/project/root | shasum -a 256 | cut -c1-32)"
```

The hosted service fixes `project_id` for a synced memory ID. Records uploaded
by earlier versions keep the raw path they were uploaded with, and later
updates and retirements reuse it so they still apply. To stop publishing such a
path, forget the record and record the lesson again. Retirement does not
physically erase the hosted copy.

The SQLite outbox persists exact requests and operation IDs before network
access. Retry the same command after being offline, losing an acknowledgement
or restarting. A request that may have reached the service is resent
byte-for-byte until it is acknowledged. A queued request that was never sent is
rebuilt from current local state on the next run. After each acknowledgement,
sync compares current local state again and sends any newer revision, such as a
retirement made while offline, in the same run. The hosted object commits each
mutation, revision and receipt atomically. Credentials are held in memory and
are never placed in the outbox.
Expected tenant and OAuth subject headers prevent accidentally syncing to a
different signed-in account. Redirects are rejected.

A 409 leaves the operation queued. Inspect local and remote content before
choosing `--resolve remote --id ID` or `--resolve local --id ID` on the same sync
command. Remote resolution accepts that remote revision. Local resolution
acknowledges the current remote revision and sends the locally reviewed content
with a new operation ID. Another concurrent update still causes a conflict.
Retirement cannot be undone and takes precedence in conflict resolution. If the
local record is retired and the remote revision is active, `--resolve remote`
keeps the local retirement, sends it in the same run and lists the ID in
`KeptLocalRetirements`. If the remote revision is retired, `--resolve local`
cannot revive the local copy. Pull imports also enforce the local writing policy.
Scope and project/conversation identity are immutable for a synced memory ID;
use a new record ID for a different context. Pull cannot overwrite a local
memory belonging to an unselected scope.
Sync is bounded to 1,000 writes and ten pages per invocation; rerun to continue.

## Team review

Configure the `TEAM` Durable Object and the optional `TEAM_MEMBERS` secret:

```json
{"platform":{"members":["alice","bob"],"reviewers":["bob"]}}
```

The issuer must grant reviewers `memory:review`. Membership comes from server
configuration. All team operations also require `memory:read`, because their
responses contain lesson snapshots. Membership is never derived from tool
arguments or token team claims. Keep review authority
out of ordinary coding-agent credentials. Review is an operator action through
the CLI/HTTP API; it is not an exposed MCP tool. The server enforces a different
reviewer from the author, exact revision and exact snapshot digest. It cannot
prove a human was present when a privileged token was used.

First sync a selected private lesson. Then submit a JSON request using
`elephant team-submit --cloud ORIGIN --cloud-tenant TENANT --cloud-user SUBJECT
--file request.json`. Keep the same operation ID for retries:

```json
{"action":"propose","operation_id":"proposal-task-42","team":"platform","memory_id":"SYNCED_MEMORY_ID"}
```

`elephant team-pull` with the same cloud options, `--cloud-team platform` and
`--team platform` displays full proposals, author, reviewer, revision and digest.
A reviewer inspects the incident, lesson, source and conditions, then submits:

```json
{"action":"review","operation_id":"review-task-42","team":"platform","id":"PROPOSAL_ID","expected_revision":1,"digest":"EXACT_64_CHARACTER_DIGEST","approve":true}
```

Approved snapshots are staged locally as drafts with peer-review provenance.
Use `elephant list --team platform`, inspect the local lesson, then run
`elephant approve --team platform --id LOCAL_ID`. Repeated pulls preserve local
approval for the same revision. A new review revision needs fresh local approval.
Review rejection and retirement withdraw existing local copies on the next pull.

The author or reviewer withdraws a shared snapshot with `action: "retire"`, its
proposal ID, expected revision, team and stable operation ID. A private lesson
and a shared snapshot have separate lifecycles: withdrawing a private lesson
does not withdraw a previously reviewed team snapshot; withdraw the proposal
explicitly as well. Changes to a private lesson require a new proposal/review.

All team members can inspect drafts during review. Private records remain in
user objects; proposal creation copies only the selected lesson snapshot,
including its opaque project ID.
There is no background synchronization, physical erasure, automatic conflict
merge, interactive OAuth login implementation or enterprise membership service.
The pilot limits and deployment measurements in [Cloudflare architecture](cloudflare.md)
still apply. See [setup](../cloudflare/README.md) for issuer and Worker configuration.
