# Pilot readiness review — 2026-10-04

## Verdict

The direction is sound for a controlled pilot. Local-first storage, scoped SQL
selection before decoding, a backend interface, user-scoped Durable Objects,
transactional retry receipts and explicit reviewed sharing address real problems.
The next milestone is demonstrated user value and deployment reliability. More
connectors, embeddings or UI features should wait for those results.

This is a source review and automated validation run, not an independent security
audit, MCP certification, native model-driven acceptance or production load test.

## What was validated

- Rechecked GitHub: PRs [#6](https://github.com/prithivrajmu/elephant/pull/6),
  [#7](https://github.com/prithivrajmu/elephant/pull/7),
  [#8](https://github.com/prithivrajmu/elephant/pull/8) and
  [#9](https://github.com/prithivrajmu/elephant/pull/9) are open, mergeable and green
  at their current heads. They have not been merged or deployed.
- Re-ran TypeScript checks, all 30 Workers-runtime tests and Wrangler dry-run on
  the evidence branch, which includes hosted storage, sync and team sharing.
  The dry-run bundle is approximately 374 KiB gzip.
- Combined the hosted stack with scoped SQL retrieval and the update-check fix.
  Go 1.25.8 race tests, vet, source build, external MCP acceptance, SQLite
  migration/backup/restore checks and generated-hook acceptance passed on Linux.
- Separately validated the update fix against main: race tests, vet, selftest,
  MCP/storage/hook acceptance and the actual dashboard JavaScript in Node passed.
  Tests exercise forced network checks, cache-only polling, specific diagnostics,
  credential/body exclusion, recovery, pending feedback and duplicate clicks.
  This run does not claim visual browser validation.

## Findings

| Area | Finding | Decision |
| --- | --- | --- |
| Local retrieval | Scoped SQL removes unrelated rows before decoding; full eligible BM25 statistics and applicability remain. Indexed lexical narrowing is an experiment. | Merge #7 before hosted rollout. Measure complete recall latency and relevance before narrowing production candidates further. |
| Hosted architecture | Current handlers, identity isolation, receipts, restart persistence and unauthorized access are tested. Hosted recall bounds its candidate corpus. | Suitable for a limited pilot; do not claim numerical ranking parity or proven free-tier suitability. |
| Sync and sharing | Explicit opt-in, durable outbox retries, conflicts, retirement and separate human review are covered. Shared snapshots have their own withdrawal lifecycle. | Validate with two real installations and two team members before broad rollout. |
| Evidence connectors | Allowlisted read-only operations, output bounds and provenance are covered; direct SDK lifecycle composition is experimental. | Keep optional and restrict the first pilot to one trusted connector. |
| Product value | Synthetic acceptance proves protocol and storage behavior, not reliable agent extraction or useful advice on real tasks. | Real host sessions and a labeled evaluation set are the next product gate. |
| Operations | Production issuer configuration, deployed latency/CPU, retention and bounded receipt exhaustion are not validated in production. | Measure and document these before expanding the pilot. |

## Update-check incident

The dashboard button already calls the local API with `check: true`; the API sets
`Force: true`, bypassing the 24-hour cache. GET polling reads cached status only.
Every fetch failure previously collapsed to `unavailable`; the UI displayed the
raw state rather than useful guidance.

The repository is private. An unauthenticated request to the exact release-list
endpoint returned HTTP 404, while the connected GitHub account could read
`v0.6.0-pilot` with seven assets. Missing local credentials are therefore a likely
cause of the reported symptom; the user's machine response was not inspected.
A browser GitHub login does not supply credentials to the Elephant process.

The fix retains stable state names and adds safe reason/message fields. Access,
credentials, rate limits, timeouts, connectivity and malformed responses produce
specific guidance. The dashboard shows progress, prevents duplicate requests and
labels the timestamp as the last attempt. Successful retries clear old reasons.
Cached errors stay informative; no credentials or remote error bodies are stored.
See [Update notifications](updates.md) for private-pilot configuration.

## Next steps, in order

1. **Deliver the local fix.** Merge the update-check fix and #7. Prepare a new
   version and release notes after the selected changes pass release checks.
   The current version is still `0.6.0-pilot`; the publisher deliberately leaves
   an existing published tag unchanged. Merging alone does not upgrade installs.
   Provide authorized private-pilot release access, or separately decide on
   public release distribution. Do not embed a shared GitHub token.
2. **Prove the native loop.** Run real tasks in supported Codex/Claude hosts:
   initialize, capture an evidence-backed lesson, restart, recall on a related
   task, and abstain on unrelated/unknown-condition tasks. Record host/model
   versions and actual acknowledged Memory IDs. Include no-lesson/failure paths.
3. **Evaluate retrieval quality.** Commit a versioned set of related, unrelated,
   contradictory and unknown-condition queries. Measure relevance, missed
   lessons, false transfers, budget adherence and end-to-end p50/p95 latency.
   Compare bounded hosted results with local recall; do not use selection-only
   benchmarks as a user latency or token-savings claim.
4. **Run a bounded hosted pilot.** Merge the hosted stack in order #6 → #8 → #9,
   retargeting dependent PRs as their bases merge. Configure the issuer and use
   two accounts to verify isolation, reconnects, sync conflicts and withdrawal.
   Measure deployed CPU, latency, rows read/written and storage at expected load.
   Decide retention/receipt-cap behavior before long-running use. Free-tier
   suitability remains unproven until measured on the deployed service.
5. **Expand only with evidence.** Start with 3–5 developers and two projects.
   Track setup success, time to first useful lesson, capture misses, helpful and
   harmful recalls, recovery and actual overhead. Keep sharing/connectors opt-in.

No active Elephant hook or MCP tool was available in this Work Mode session;
validation continued without reading or recording project Memories.
