# Memory-tax experiment: initial implementation and smoke results

31 October 2026. Experimental implementation; no production retrieval change.

## What was implemented

- Reproducible explicit-file dataset and run contract with source hashes, clock,
  scopes, applicability, evidence types, lineage, group splits and optional gold spans.
- A store-free Go helper calling the actual production recall engine.
- Raw BM25, local dense + cross-encoder raw retrieval, independent combined
  evidence/lesson retrieval, empty-lesson fallback, raw-priority packing,
  semantic lesson ablation and bounded source/lesson oracles.
- Optional external QA/scoring protocol, with at most one follow-up query and
  a total retrieved-context exposure byte limit that charges repeated context.
- Eligibility/candidate/budget traces, span-aware coverage, paired retrieval
  benefit/tax, runtime and model/source/binary/dataset manifests.
- Fourteen contract/integration tests and model-free Linux CI coverage.

No agent-history discovery, source ingestion hooks, raw-history sync, user-store
migration, automated retrieval learning or new product default was added.

## Frozen run

Baseline: `525f50e6bded96eb5a169572b9e20c073a3b46ea`.
Implementation under test: `e6d1df411e3cafbcc80ec1d0142fcd7d9b4d96f3`.
Both runs recorded a clean worktree. Thresholds, models and candidate/context
budgets were unchanged between development and reserved cases.

Python 3.12; Go 1.27.3; FastEmbed 0.7.3; CPU/two threads.
BAAI/bge-small-en-v1.5 embedding + Xenova/ms-marco-MiniLM-L-6-v2 reranker.
Actual local model hashes are in [dev.json](results/dev.json) and
[holdout.json](results/holdout.json), alongside complete case-level diagnostics.
These are alternative models, not a reproduction of the paper's Qwen baseline.
No QA model, judge or live coding agent was run. Real task success, billed cost,
extraction cost and provider token usage remain unknown.

One retrieval round, 10 candidates per lexical/dense branch, 4,000 UTF-8 context
bytes, experimental reranker admission logit >= 0. The current Elephant control
uses its existing lexical threshold and default limit of six complete lessons.

## Retrieval coverage and abstention counts

A case passes this diagnostic when all labelled evidence spans are present, or
all independently labelled sufficient lessons are present. No-answer cases pass
when the retriever abstains. This is not answer accuracy or coding-task success.

| Condition | Development (20) | Reserved smoke (40) |
| --- | ---: | ---: |
| No historical context | 2 | 4 |
| Current Elephant lessons | 10 | 20 |
| Raw BM25 | 20 | 40 |
| Raw hybrid + reranker | 18 | 36 |
| Combined lesson + raw hybrid | 18 | 36 |
| Empty-lesson source fallback | 16 | 32 |
| Combined with one raw chunk prioritized | 18 | 36 |
| Hybrid lesson-only ablation | 10 | 20 |
| Gold raw-source oracle | 20 | 40 |
| Gold lesson oracle | 12 | 24 |

Combined retrieval has **zero paired coverage wins and zero paired coverage
losses versus raw hybrid** on both splits. There is no measured coverage benefit
from adding lessons to raw retrieval in these fixtures. Joint retrieval performs
more indexing/reranking work. Because these are cold, sequential synthetic runs,
that work does not establish a warm production latency or dollar-cost difference.

## Failure analysis

- Raw hybrid rejects the paraphrase cases: `case-01`, `case-11`, `case-21`,
  `case-31`, `case-41`, `case-51`. Candidate scores show source evidence was
  considered, then rejected at the fixed zero-logit admission threshold. Raw BM25
  retains it. This is a threshold/representation failure in this experimental
  baseline, not a general result that BM25 beats dense retrieval.
- Combined retrieval does not recover those misses, since the same reranker
  admission removes the useful alternatives. Raw-priority packing cannot rescue
  a candidate already rejected before packing.
- Empty-lesson fallback still fails multi-session questions when a nonempty
  lesson omits the second required fact. Empty-result detection is insufficient
  as an evidence-sufficiency test.
- Missing, retired or inapplicable lessons leave source-only facts recoverable.
  Some gaps reflect deliberate product abstention and capture scope, not a bug in
  the current recall engine.
- The gold-lesson oracle covers more than retrieved lessons but fewer cases than
  raw-source oracle. The cases are authored to expose source/lesson differences;
  this is not an unbiased measurement of production extraction loss.

The raw BM25 diagnostic is perfect on these small, retrieval-friendly authored
cases. This identifies a weak smoke corpus for choosing a production retriever;
we should not optimize the combined system to beat a ceiling on template data.
The 20/40 split checks run mechanics; shared templates prevent interpreting it
as independent evidence of generalization. No significance interval is claimed.

## Independent review and validation

A separate AI reviewer inspected implementation and tests. It identified and
prompted fixes for production JSON key handling, vacuous empty-gold completion,
partial-chunk evidence credit, baseline lesson limit, semantic search text
alignment, manifests and repeated context accounting. The final code review
found no remaining correctness/security blockers for an experimental PR.
Its follow-up-budget recommendation was implemented: first-round context gets
one third of the cumulative exposure budget in two-round arms.

Executed successfully:

- 14 Python contracts, including the actual compiled Go helper and QA subprocess
  protocol. Protocol stubs are test doubles, not reported model outcomes.
- `go test -race ./...`, `go vet ./...`, `gofmt` and `git diff --check`.
- Product build and seven-check selftest.
- Existing external MCP, storage/recovery and automatic-hook acceptance scripts.
- Both dashboard JavaScript checks.
- 600 case/condition retrieval evaluations with real local embedding/reranker
  weights: 200 development and 400 reserved smoke evaluations.

Native interactive Codex/Claude sessions, other operating systems, live coding
outcome comparisons and full LongMemEval-s remain untested here. Hook acceptance
explicitly reports `native_host_model_run: false`.

## Decision and next work

**Keep the implementation experimental; do not enable production evidence capture.**

The tooling and synthetic baseline/prototype runs are complete. The evidence
needed to claim coding improvement is not. Next:

1. Collect a fresh sanitized/consented historical corpus before authoring queries;
   split by incident/repository lineage and time.
2. Calibrate reranker abstention on development data, including hard negatives
   and paraphrases. Freeze settings before the new evaluation split.
3. Run raw-only, current Elephant and combined arms with the same QA model and
   actual executable task checks, counting extraction/indexing/retrieval/agent
   work and repeated model exposure.
4. Inspect paired wins/losses, estimate clustered uncertainty, then size and lock
   a powered follow-up against a meaningful predeclared quality/cost criterion.
5. Consider an opt-in evidence adapter and its access/retention/deletion/backup
   lifecycle only if that evaluation demonstrates value.

Opening an experimental PR is appropriate. These results do not justify a
production backend switch or an efficacy claim.
