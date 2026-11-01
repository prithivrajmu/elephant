# Historical coding workflow pilot

This experiment measures whether retrieved context helps an agent repair two real
Elephant regressions. It complements the synthetic retrieval contracts in
`../memory_tax`; it is not a LongMemEval replication or a production-quality claim.

Each task starts from the parent of a historical fix. Five fresh participants receive
the same issue-style prompt and source snapshot: no additional context, current
Elephant recall, raw BM25, raw hybrid retrieval, or combined raw and lesson retrieval.
All receive a 4,000-byte maximum retrieved context, one retrieval round, and an
instruction-enforced 12-tool-call coding budget. The hybrid arms use the pinned
embedding and reranking models from `../memory_tax`. The fixed reranker threshold
is zero; it is not tuned on these tasks.

## Evidence and isolation

The raw corpus contains ancestor commit messages and documentation at the task's
base revision. Source revisions, timestamps and content hashes are retained.
Lessons are deterministic extracts of documentation paragraphs, not authentic
saved user memories. Commit messages are available only to the raw arms; this is
an explicit corpus difference, so the trial does not isolate retrieval algorithm
quality from representation and source coverage.

The participant sees a git-free snapshot and an opaque workspace name. It is
instructed not to read sibling directories, history, evaluator artifacts, external
sources or other agents. Boundaries and tool budgets are instruction-enforced on
a shared host, not security isolation. Model pretraining contamination cannot be
excluded. No participant receives the historical fix or evaluator test overlay.
The coordinator can inspect all artifacts and must not coach participants.

Before trials, the controller verifies that the original suite passes, the original
code fails the new regression assertions, and the historical fix passes. For the
interrupted-review task, the overlay deliberately replaces an older contradictory
expectation with the requested behavior. Exact required error/status strings are
included in the common task prompt.

Scoring rejects modifications outside each task's production-file allowlist, copies
only those production changes into a fresh base snapshot, overlays evaluator-owned
historical tests, and runs the targeted regression and full suite. Task success
requires both. Baseline hashes, task definitions, engine/controller hashes, helper
binary hash, Go version, model artifacts, context and prompt hashes are frozen.
Scoring refuses changes to frozen controller/task/engine sources. Do not modify the
controller after preparation; start a new experiment instead.

## Reproduction

Use a full Git clone, Python 3.11+ with the tar extraction filter backport (3.11.8+
recommended), Go 1.27.3, and dependencies from
`experiments/memory_tax/requirements-models.txt`. Warm Go modules before offline
participant work. Use new coordinator and participant directories for every run.

```sh
go build -o /tmp/historical-baseline ./cmd/memory-tax-baseline
python3 -m unittest discover -s experiments/historical_tasks -v
python3 experiments/historical_tasks/benchmark.py controls \
  --state /tmp/historical-coordinator --go "$(command -v go)"
python3 experiments/historical_tasks/benchmark.py prepare \
  --state /tmp/historical-coordinator --workspaces /tmp/historical-participants \
  --go "$(command -v go)" --baseline-binary /tmp/historical-baseline \
  --model-cache /tmp/historical-models --allow-model-download \
  --solver-model YOUR_MODEL
```

Give each fresh coding participant only its generated prompt. Use the manifest's
seeded launch order; do not share outcomes between participants or retry failures.
The initial pilot uses fresh agent sessions with inherited model settings; no
model-provider API wrapper is included. After each submission:

```sh
python3 experiments/historical_tasks/benchmark.py score \
  --state /tmp/historical-coordinator --go "$(command -v go)" --trial TRIAL_ID
```

Record actual run settings and participant reports separately. Unknown token usage
and billed cost must remain null. Shared-host wall times are not production latency
measurements. Run the independent reviewer after scoring and before interpretation.
Two tasks are two task groups, not ten independent samples: report per-task results
and do not infer a reliable ranking, confidence interval or production default.

Infrastructure interruptions before a valid submission are excluded and disclosed.
If workspaces are lost, restart the entire block from pristine snapshots. Never
count a quota error as a model-quality failure or selectively retry behavioral failures.

The model loader sets `ORT_DISABLE_TELEMETRY=1` and
`HF_HUB_DISABLE_TELEMETRY=1` before loading model libraries. When using libraries
outside the loader, export both settings before starting Python. ONNX Runtime's
[privacy documentation](https://github.com/microsoft/onnxruntime/blob/main/docs/Privacy.md)
explains why its process-start setting is needed before initialization. The loader
rejects an already-imported runtime unless the setting is present; use a fresh
process if the runtime was previously initialized without it.
