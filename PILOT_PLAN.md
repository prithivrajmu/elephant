# Elephant pilot and full-product priorities

## Ready for local users

Astra reviewed both the pilot task list and implementation. The critical first-use work is included: prebuilt packages with checksum-verifying installers, version/doctor/selftest, safe config generation, explicit agent capture guidance, six MCP tools and two workflow prompts, valid tool schemas, bounded concurrent lock retries, preserved legacy history, dashboard connection help, applicability fields and current-fact input, unattached conversations, and stable feedback IDs.

Astra's verdict: suitable for a limited local pilot after package acceptance checks. This is not a completed enterprise product or verified compatibility with every coding agent. `VALIDATION.md` records exactly what was executed.

## Start testing with 3–5 developers

1. Each developer installs their own local package/store and connects one chosen agent.
2. Complete one real task, capture a source-backed lesson, restart and retrieve it on a related task.
3. Repeat on a second project. Try an unrelated task and a task with unknown or excluded requirements.
4. Test one no-project conversation using `--unattached`; do not invent project membership.
5. Record helpful/unhelpful only when advice was applied and the effect observed. Report capture misses explicitly.

Use `PILOT_FEEDBACK.md` as a reusable report. Pilot acceptance means successful setup and reliable memory lifecycle. No claim of efficiency improvement follows from a synthetic demo or a few positive observations.

## What to measure first

| Question | Evidence |
| --- | --- |
| Can people start? | Install/setup successes over attempts, time to first real stored lesson, host/version/platform. |
| Does memory persist? | Acknowledged IDs survive restart, related recall finds them, repeated run feedback does not duplicate. |
| Does retrieval fit? | Human judgments of relevant/missed/irrelevant lessons and whether applicability was respected. |
| Does capture happen? | Verified task outcomes versus agent captures, misses, fabricated or unsupported records. |
| Does it help? | Observed applied outcomes first; later paired tasks with/without memory controlling difficulty, agent/model and versions. |
| What does context cost? | Actual tokenizer/model usage where available; dashboard byte/4 estimates are labeled separately. |

## Next full-product tasks in order

1. **Client compatibility and usability:** native macOS/Windows execution, real Codex/Claude/Cursor sessions, actionable setup errors, release signing and automated release CI. Add only adapters justified by pilot failures.
2. **Persistence/recovery:** migrate to SQLite with versioned migrations, indexes and transactional concurrency; backup/restore/export checks, explicit physical deletion, journal compaction, crash/fault tests and scaling benchmarks. Preserve existing store/import format through a migration path.
3. **Evidence lifecycle:** separate incidents from reusable lessons, source hashes/revisions, explicit supersession, disputed claims, stale-version handling and independent evidence units. Repeated observations from one task should not become many independent votes.
4. **Retrieval evaluation:** versioned query/lesson relevance set, top-k precision/recall and false-transfer cases, calibration and budget-quality tradeoffs. Compare lexical baseline with hybrid embeddings only after this baseline exists. Add proposed label extraction with human/agent verification rather than silently inferring projects.
5. **Connection explorer:** extend the current Memory Map with independent Experience units, observation links and saved Recall Paths. Today the Map derives origins, Signals and sources from stored Memories. It does not infer project membership.
6. **Team sharing:** reviewed promotion and provenance, authenticated identities, scoped authorization, audit logs and secure sync. Use separate local stores and reviewed export/import today; tenant labels alone are not enterprise isolation.
7. **Measured efficiency:** paired task experiments including model/tool tokens, latency, quality and rework. Report uncertainty and costs of extraction/retrieval. Select a product-level target after real user baseline measurements.

Do not block the first pilot on embeddings, hosted sharing or a graph database. Do not represent the current token baseline as performance proof.
