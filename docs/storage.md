# SQLite storage and recovery

See [SQL retrieval and benchmarks](retrieval.md) for scoped recall selection.

Elephant 0.6 uses SQLite with WAL, `synchronous=FULL`, foreign keys and a 3-second busy timeout. Independent CLI, MCP and dashboard processes share the same local store. Readers use snapshots; writers use `BEGIN IMMEDIATE`. A callback runs once, so contention does not replay hook side effects.

The pinned CGo-free `modernc.org/sqlite` v1.60.1 bundles SQLite 3.53.4, including its WAL-reset fix. The module requires Go 1.26+. CI uses Go 1.27. No database daemon, C compiler or separate SQLite installation is required.

## Paths and migration

New stores default to `~/.elephant/memories.sqlite`. If no new store exists, Elephant retains an existing modern `~/.elephant/events.jsonl` or legacy `~/.agent-memory/events.jsonl` path. A custom `--store` path also works. Store format is detected from its header, not its extension.

First access converts a JSONL store in place:

1. Acquire the legacy `<store>.lock` directory to exclude old writers.
2. Stream every event through schema version 1 into a private SQLite candidate. Invalid JSON, unknown event kinds or invalid records stop conversion.
3. Create and sync `<store>.jsonl-backup`. An existing backup must match the original byte for byte; it is never overwritten.
4. Commit and close the candidate, sync it, and replace the original path. SQLite enables WAL before normal store use.

IDs, creation/update times, source evidence, scope, approval, retirement, feedback deduplication, usage and task receipts survive conversion. Adjacent `.settings.json`, `.updates.json` and `.sessions` paths stay the same. Older binaries reject the converted format. The original backup only contains history up to migration; it cannot recover later writes.

The database holds current memory state, deduplication keys, an event audit, experiences and usage. Experience context/task and usage identity indexes serve dashboard and task-summary reads. A memory and its capture receipt commit together. Recall still scans current memories and may do quadratic alternative-advice work; dashboard usage history is not yet bounded. Automatic retention and physical erasure are not implemented.

## Sensitive content

Elephant rejects common credential formats at record time, before writing a memory. Screening covers incident, lesson, source, subject, conversation, and all feature, requirement and exclusion label keys and values. It recognizes private-key PEM headers, common vendor API/access tokens, JWTs and URLs with embedded credentials. Rejection errors name the field and credential kind without repeating the matched content; rephrase or redact the content before recording it.

This is heuristic screening, not automatic redaction: it does **not** detect PII or every secret, or establish whether a credential is valid. Bare token prefixes without a payload remain allowed. Existing stored memories still load without screening and are not retroactively redacted. Do not store secrets or regulated data in Elephant.

The default project ID is the absolute project path, stored locally as memory metadata. See [cloud sync](cloud-sync.md) for upload handling; record-time screening does not remove local paths or make metadata safe to upload.

## Backups and restore

```sh
elephant backup --store /path/to/memories.sqlite --output /path/to/NEW-backup.sqlite
elephant restore --file /path/to/backup.sqlite --output /path/to/NEW-restored.sqlite
elephant doctor --store /path/to/restored.sqlite --unattached
```

Backup uses `VACUUM INTO` to include committed WAL pages in a consistent standalone snapshot, verifies integrity/schema, syncs it and publishes it without replacing an existing destination. It sees committed state even while another writer has uncommitted changes. The destination parent directory must exist and support hard links.

Restore verifies the backup and creates a new store. It does not overwrite a live store or alter the backup. Use the restored path with `--store`; run `init` with that path to update installed project hooks. Keep the original store until recovery is verified. Back up adjacent settings, update preferences, task state and project hook configuration separately when preserving those is required; the SQLite snapshot covers memory data, audit events and receipts.

Use the backup command while Elephant is running. Copying only the main database can omit committed data still in `-wal`. Do not delete `-wal` or `-shm` files during recovery. WAL requires a local filesystem shared by processes on one host; network/shared remote filesystems are unsupported.

## Interrupted work

SQLite releases process locks after termination and recovers its journal on the next open. A transaction killed before commit rolls back; an acknowledged commit survives process termination. This does not claim to simulate a physical power failure or defective storage hardware.

If JSONL migration stops before replacement, the original remains usable for retry. A crash may leave `<store>.lock` or private `.elephant-migrate-*` files. Stop all processes using that store, preserve the original and backup, then remove only the stale migration lock and private candidates before retrying. Never remove a live process's migration lock. A partial or mismatching original backup is reported explicitly; preserve it and compare against the original before moving it aside and retrying.

Corrupt SQLite databases and unsupported schema versions fail explicitly. Run `doctor` for SQLite integrity checks. Restore a verified backup to a new path rather than resetting history automatically.

## References

- [SQLite WAL](https://www.sqlite.org/wal.html)
- [SQLite 3.53.4 fixes](https://sqlite.org/releaselog/3_51_3.html)
- [SQLite VACUUM INTO](https://www.sqlite.org/lang_vacuum.html)
- [Pinned Go driver](https://pkg.go.dev/modernc.org/sqlite@v1.48.1)
