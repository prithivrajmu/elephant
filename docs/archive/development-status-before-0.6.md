# Development status before 0.6 publication

Historical snapshot; see [release notes](../releases/v0.6.0-pilot.md) for the release.

## Product goal

Run `elephant init` once in a project, complete the host's normal hook approval, then work normally. Recall and recording should happen without repeated “remember this” prompts.

An **Experience** records an observed event. A **Memory** contains a reusable lesson with source evidence and applicability conditions. Automatic event capture and successful learning are separate:

| Stage | Current behavior |
| --- | --- |
| Initialize | Install project-local Codex and Claude Code hooks, preserve existing settings, and add agent guidance. |
| Recall | Use project Fingerprint, task terms, scope and conditions to select lessons within a default 4,000-byte Recall Budget. |
| Observe | Append tool name, event, status and structured exit code when available. Keep prompts, tool arguments, output and transcripts out of the store. |
| Review | Request one end-of-task review. The current agent can save zero to three evidence-backed lessons. Zero is valid when nothing reusable was learned. |
| Reuse | Retrieve the saved lesson on a related task. Record usefulness only after applying it and observing its effect. |

Project scope is the default for automatic lesson review. Personal scope is for transferable experience; team lessons require human review. Recalled Memories are evidence to check against the current task and instructions.

## Current state

**Version: `0.6.0-pilot` on this development branch.** This SQLite change follows the task-status and notification work in [PR #2](https://github.com/prithivrajmu/elephant/pull/2). No release is published.

| Area | Status |
| --- | --- |
| Task status and release notices | Implemented: task-specific save receipts, one-line review output, cached/dismissible release notices, CLI and dashboard controls. See [Update notifications](../updates.md). |
| Local engine, CLI, MCP and Memory Palace | Implemented; six MCP tools, scoped recall, feedback, retirement and local reviewed export/import. |
| Automatic Codex and Claude Code adapters | Implemented for macOS/Linux; generated hook commands pass subprocess acceptance. Real model-driven automatic sessions remain unverified. |
| Validation | [SQLite checks](../validation.md#sqlite-wal-version-06): migration, concurrent readers/writers, killed-writer recovery, backup/restore, race tests and protocol acceptance. |
| Native client evidence | Historical 0.4 macOS source-build and Codex MCP discovery passed. The new Codex attempt is blocked by host authentication; Claude Code is unavailable here. The automatic loop remains a native acceptance gate. |
| Distribution | Source build is the current 0.6 path. Archive and installer versions derive from the binary version; six archives and extracted Linux binary acceptance are checked. No GitHub release is published. |
| Storage | SQLite WAL, FULL synchronization, indexed receipts/usage and current memory state. JSONL migrates with a preserved backup. Retention remains planned. See [Storage](../storage.md). |

Use [Automatic hooks](../automation.md) for hook behavior and compatibility, [Validation](../validation.md) for executed checks, and [Vocabulary](../vocabulary.md) for the product vocabulary.
