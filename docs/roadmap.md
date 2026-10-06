# Elephant closed-beta and full-product priorities

## Ready for local users

Maintainer review covered both the beta task list and the implementation. The critical first-use work is included: prebuilt packages with checksum-verifying installers, version/doctor/selftest, safe config generation, explicit agent capture guidance, six MCP tools and two workflow prompts, valid tool schemas, bounded concurrent lock retries, preserved legacy history, dashboard connection help, applicability fields and current-fact input, unattached conversations, and stable feedback IDs.

Assessment: suitable for a closed beta of local use after package acceptance checks. This is not a completed enterprise product or verified compatibility with every coding agent. `validation.md` records exactly what was executed.

## Closed-beta testing

1. Each beta tester installs their own local package/store and connects one chosen agent.
2. Complete one real task, capture a source-backed lesson, restart and retrieve it on a related task.
3. Repeat on a second project. Try an unrelated task and a task with unknown or excluded requirements.
4. Test one no-project conversation using `--unattached`; do not invent project membership.
5. Record helpful/unhelpful only when advice was applied and the effect observed. Report capture misses explicitly.

Use [pilot-feedback.md](pilot-feedback.md) as a reusable report, filed as a GitHub issue. Beta acceptance means successful setup and reliable memory lifecycle. No claim of efficiency improvement follows from a synthetic demo or a few positive observations.

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

1. **Client compatibility and usability:** real Codex/Claude/Cursor sessions, native ARM installer execution, actionable setup errors and release signing. Native binary validation and gated portable release publication are implemented. Add only adapters justified by beta feedback.
2. **Persistence/recovery:** SQLite WAL, versioned JSONL migration, indexed receipts, verified backup/restore and synthetic end-to-end scaling measurements are implemented. Next: explicit retention and physical deletion, broader fault tests, export checks and real-host scaling evidence.
3. **Evidence lifecycle:** separate incidents from reusable lessons, source hashes/revisions, explicit supersession, disputed claims, stale-version handling and independent evidence units. Repeated observations from one task should not become many independent votes.
4. **Retrieval evaluation:** versioned query/lesson relevance set, top-k precision/recall and false-transfer cases, calibration and budget-quality tradeoffs. Compare lexical baseline with hybrid embeddings only after this baseline exists. Add proposed label extraction with human/agent verification rather than silently inferring projects.
5. **Connection explorer:** extend the current Memory Map with independent Experience units, observation links and saved Recall Paths. Today the Map derives origins, Signals and sources from stored Memories. It does not infer project membership.
6. **Team sharing:** reviewed promotion and provenance, authenticated identities, scoped authorization, audit logs and secure sync. Use separate local stores and reviewed export/import today; tenant labels alone are not enterprise isolation.
7. **Measured efficiency:** deterministic local ratchets and synthetic setup/recall/capture/Palace journey reports are implemented. Next run paired real tasks including model/tool tokens, latency, quality and rework. Report uncertainty and extraction/retrieval costs, then select a product-level target from real user baselines.

Do not block the closed beta on embeddings, hosted sharing or a graph database. Do not represent the current token baseline as performance proof.


## Next steps and acceptance criteria

The next milestone is a **verified automatic local beta**. Release notifications and one concise end-of-task status line are implemented. Validate the complete init → recall → observe → review → save → reuse loop before calling automatic learning verified.

| Priority | Work | Acceptance criterion |
| --- | --- | --- |
| 1. User updates and notifications | Task status and release checks are implemented. Validate the final one-line output in native hosts; Verify notification delivery against the published release and assets. | Status uses actual recall results and acknowledged Memory writes. A newer published compatible release produces one notice per version, with version, brief change summary and upgrade link. Disabled/offline checks never block agent work or claim the installed version is current. |
| 2. Native automatic loop | Run real Codex and Claude Code tasks on macOS/Linux. Record host/model versions and hook approval steps. Test no-lesson tasks, failures, restart, pause/resume and custom stores. | Without a repeated memory prompt, a justified lesson is saved with an ID and actual evidence, survives restart and appears on a related task. Unrelated or inapplicable tasks abstain; review does not loop. |
| 3. Reproducible distribution | Single-source versions, complete package docs, checksum checks and validation CI are implemented. Gated native-platform validation and release publication are implemented; extend acceptance to native ARM installers. | Build a clean release package, install it on each claimed platform, and complete its supported workflow. Record native acceptance separately from cross-compilation before publishing a release. |
| 4. Capture reliability and storage | Make review requested, Memory saved, no lesson justified and capture failed distinguishable. Cover interrupted review/write and safe retry. SQLite WAL, versioned JSONL migration, verified backup/restore, complete synthetic hook/recall timings and one-way performance ratchets are implemented. Add explicit retention and real-host baselines. | Restart/fault checks preserve every acknowledged Memory, retries avoid duplicate evidence, and recovery never silently discards history. Publish real-host hook and recall latency as history grows. |
| 5. Memory quality and retrieval | Add evidence revisions, reviewed supersession and stale-condition handling. Build a versioned relevance set with related, unrelated, unknown-condition and false-transfer cases. | Report capture misses, unsupported lessons, relevance/abstention judgments and budget-quality tradeoffs against the lexical baseline. Feedback from one task must not become multiple independent votes. |
| 6. Beta feedback and measured value | Collect feedback from beta testers across at least two projects and an unattached conversation. Use [Pilot feedback](pilot-feedback.md); compare paired tasks with and without memory after lifecycle acceptance. | Report setup success, time to first real Memory, persistence, capture reliability, observed usefulness, actual token usage where available, latency and rework. Include review/retrieval overhead. |

### Priority 1: concise user updates

Implemented in the current source. Re-run `elephant init`, restart the host and approve the updated hooks to load the new review instructions. See [Update notifications](updates.md) for commands and validation limits.

- **Task status:** show one line at task completion, using actual results: `Elephant: recalled 2 memories · saved 1 lesson.` A review request alone must never count as a saved lesson.
- **No lesson:** say `Elephant: recalled 0 memories · no reusable lesson found.` only after review completed. Distinguish review incomplete, capture failed and automation paused.
- **New release:** show `Elephant update: VERSION available — CHANGE SUMMARY. Upgrade: LINK` only after verifying published release metadata. Notify once per new version and allow dismissal or disabling checks.
- **Delivery:** use CLI output and the coding agent's final status line; show update availability in Memory Palace. Keep recurring task output to one line; show a release notice separately when needed.
- **Checks:** use a bounded, cached version lookup. Release checks are on by default and can be disabled; see [Update notifications](updates.md). Do not send task text or Memories with the request. Do not automatically install an update.
- **Order:** task status and version alignment are implemented; publish release metadata and platform assets before expecting release notifications. Test duplicate suppression, unavailable metadata, offline operation, disabled checks and failed Memory writes.

The full-product priorities above guide work after the closed beta. Use the release gates and current validation evidence when assessing distribution readiness.

Keep richer Memory Map connections, semantic/hybrid retrieval, authenticated Herd sharing and hosted service work after local reliability and retrieval evidence. A synthetic demo and estimated tokens avoided do not establish productivity or billing savings.
