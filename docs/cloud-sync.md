# Opt-in cloud sync and reviewed team sharing

Local installation and hooks remain offline by default. These commands require
an explicitly configured hosted pilot, OAuth access token and selected private
scopes. They do not run automatically from hooks, MCP or startup.

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

The SQLite outbox persists exact requests and operation IDs before network
access. Retry the same command after being offline, losing an acknowledgement
or restarting. The hosted object commits each mutation, revision and receipt
atomically. Credentials are held in memory and are never placed in the outbox.
Expected tenant and OAuth subject headers prevent accidentally syncing to a
different signed-in account. Redirects are rejected.

A 409 leaves the operation queued. Inspect local and remote content before
choosing `--resolve remote --id ID` or `--resolve local --id ID` on the same sync
command. Remote resolution accepts that remote revision. Local resolution
acknowledges the current remote revision and sends the locally reviewed content
with a new operation ID. Another concurrent update still causes a conflict.
Retirement cannot be undone. Pull imports also enforce the local writing policy.
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
user objects; proposal creation copies only the selected lesson snapshot.
There is no background synchronization, physical erasure, automatic conflict
merge, interactive OAuth login implementation or enterprise membership service.
The pilot limits and deployment measurements in [Cloudflare architecture](cloudflare.md)
still apply. See [setup](../cloudflare/README.md) for issuer and Worker configuration.
