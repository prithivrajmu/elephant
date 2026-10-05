# CLI reference

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

Retirement removes future retrieval, but retains the historical event audit. It is **not physical deletion**. Do not use this prototype to store secrets or data requiring regulated erasure.

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

SQLite WAL allows readers to retain a consistent snapshot during a write. Writers use `BEGIN IMMEDIATE`, wait up to three seconds for contention, and commit memory state, deduplication keys, audit events and task receipts together with `synchronous=FULL`. Normal reads do not replay historical events. SQLite recovers interrupted transactions; failed migration never replaces the original journal. [Storage](storage.md) documents backup/restore and the remaining manual migration-lock recovery step.

Recall still scans current memories. Alternative-advice metadata can add worst-case O(memories²) baseline work, and the dashboard still loads the user's usage history. This change removes journal replay, not every scaling limit. Peer imports validate up front but commit individual records; rerun after a partial import to deduplicate.

## Conversations without a project

Use `--unattached --conversation CONVERSATION_ID` with record, recall, MCP or UI. The project field stays empty and the profiler never scans the ambient working directory. Personal memories remain private and reusable across your conversations; conversation scope restricts them to the exact conversation ID. `recall --task` requires an exact content-term match, while `recall --initialize` without a task explicitly allows known-profile discovery. See [Architecture](architecture.md) for the retrieval math and the connection roadmap.

```sh
./elephant record --unattached --conversation design-talk --file examples/untagged-lesson.json
./elephant recall --unattached --conversation other-talk --task "validation evidence for reviewers"
./elephant ui --unattached --conversation design-talk
```

For known technical facts without a repository, MCP retrieval accepts `context_features`; CLI retrieval accepts `--context-file` with `{"features":{"database":["snowflake"]}}`. Missing required facts fail closed. These asserted facts never grant permission. Automatic hooks can request evidence-based extraction from the current agent; the engine does not extract from transcripts itself. Conversation project inference and multi-project graph storage remain proposed extensions.

Conversation scope isolates recall and feedback. The owner management dashboard intentionally lists all owned memories across projects/conversations, as well as currently visible shared memories. It is not a conversation-only management view.
