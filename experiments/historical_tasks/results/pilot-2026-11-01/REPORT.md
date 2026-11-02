# Historical coding pilot: results

The no-memory baseline passed both historical tasks. Added memory has shown no
pass/fail improvement in this pilot. These tasks validate the harness and establish
a baseline; they do not establish equivalence or a reliable system ranking.

PASS requires a valid allowlisted submission, the protected regression tests and
the full historical suite all passing. Context sizes are UTF-8 bytes.

| Condition | Update refresh | Interrupted review | Update context bytes | Review context bytes |
|---|---|---|---:|---:|
| No added memory | PASS | PASS | 0 | 0 |
| Current Elephant | PASS | PASS | 3965 | 3536 |
| Raw BM25 | PASS | PASS | 3780 | 3963 |
| Raw hybrid | PASS | PASS | 0 | 839 |
| Combined | PASS | PASS | 0 | 1586 |

The frozen zero-logit reranker threshold excluded every candidate for the update
task in the raw-hybrid and combined arms. Those arms therefore received the same
empty historical context as the baseline, with independent solver runs. The review
combined arm included both a raw paragraph and its lesson extract; redundancy is
visible in the saved selected IDs. We did not retune retrieval on these tasks.

The coding prompts specify the requested behavior completely, and every condition
can inspect the same historical source tree and its documentation. Both tasks are
solvable without added history. A ceiling in executable success therefore leaves
little room to detect a memory benefit. Tool-call counts are participant estimates;
tokens and billed cost were unavailable and remain null. Retrieval/test wall times
on the shared host are diagnostic timings, not production latency measurements.

## Evidence and independent review

`manifest.json` freezes task definitions, source/model/helper hashes, supplied
contexts, selected IDs, prompt hashes, baseline file inventories and retrieval traces.
`controls.json` records original-suite success, original-code regression failure,
and historical-fix suite success for both tasks. Each `*-score.json` includes the
submitted production diff and evaluator logs. `participant_reports.json` separates
self-reported checks from coordinator scores. `execution.json` records interruptions
and operational manifest changes; `initial-manifest.json` preserves the first setup.

The published frozen source commit is
`29507a1a089114cf8a033f4c2a243fdedfdb4f0b`, whose tree exactly matches local source
revision `632fa259fefda5162fedd59f3319bcbfa109a22a` (tree
`824d27975df02eb8280d04014c33c81247b6a8d5`). Commit metadata differs. Final changes
outside those frozen source hashes strengthen a test and add documentation/results.
Earlier independent AI reviews found no blocking design defect and requested the
provenance and frozen-scoring checks included here. Final artifact audit is recorded
in `REVIEW.md`; it is an AI review, not a human audit.

## Limits on interpretation

- There are only two task groups and one completed submission per condition/task.
  Ten submissions are not ten independent task samples.
- Raw evidence is ancestor commit messages and base-revision documentation.
  Lessons are deterministic documentation excerpts, not authentic saved memories
  or chat history. Raw arms also have commit messages, a source-coverage difference.
- Workspace boundaries and the 12-call budget are instruction-enforced on a shared
  host. Model pretraining contamination cannot be excluded.
- Repeated quota interruptions required restarts. Three completed valid submissions
  were retained. Seven unfinished trials were restarted; partial work resurfaced
  at reused paths, so those submissions were excluded unscored and unique paths
  were allocated. The final interrupted trial received another unique clean path.
- Solver sessions inherited the host defaults; exact model identity/settings across
  resets could not be verified. This prevents a strict causal comparison. All
  accepted participants used the same task/context protocol and requested budget.
- Controls are not cryptographically bound to configuration by the CLI. They were
  freshly rerun; scoring used the recorded Go binary. Other inherited environment
  variables and evaluator test coverage remain procedural limitations.
- Passing the historical suite establishes its assertions, not exhaustive correctness
  of every requirement. No production retrieval defaults are changed by this PR.

## Next experiment

1. Keep these tasks as harness smoke checks. Assemble a larger reserved set whose
   answers depend on earlier decisions absent from the current source and prompt.
2. Use an explicitly pinned solver through a metered adapter, repeated independent
   runs, enforced workspace/tool boundaries, and automatic token/cost capture.
3. On separate development tasks, calibrate reranker abstention and compare shorter
   retrieval queries; deduplicate combined raw/lesson evidence by source provenance.
4. Freeze those choices, then compare executable success, regressions, total cost and
   task time on the reserved set before deciding whether to change Elephant defaults.
