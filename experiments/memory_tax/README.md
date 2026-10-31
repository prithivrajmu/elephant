# Offline memory-tax experiments

Experimental evaluation tooling, not a new production memory backend. It reads
only the dataset path explicitly supplied. It does not discover agent histories,
open a user memory store, change hooks, upload evidence, or modify recall defaults.

## Evaluation contract

Baseline repository: `525f50e6bded96eb5a169572b9e20c073a3b46ea`.
The helper calls the production `memory.Recall` implementation directly with its
six-lesson default, scope/applicability checks and exact serialized byte budget.
It requires an explicit clock and never opens SQLite. Existing supersession is
already implemented in `duplicates.go`; no production evidence migration is added.

The included 60 synthetic cases (20 dev / 40 reserved smoke cases) exercise ten
failure categories. They share authored templates. They are not independent
real-world samples, an untouched research benchmark, or proof of coding benefit.
The fixtures are intentionally small and retrieval-friendly. Do not tune against
the reserved cases and keep calling them holdout. Collect a new grouped/time-split
sanitized-consented dataset for generalization claims.

## Run without models

From the repository root, with Go 1.26+ and Python 3.11+:

```sh
go build -o /tmp/memory-tax-baseline ./cmd/memory-tax-baseline
ELEPHANT_EVAL_BINARY=/tmp/memory-tax-baseline python3 -m unittest discover -s experiments/memory_tax -p 'test_*.py' -v
python3 experiments/memory_tax/run.py \
  --dataset experiments/memory_tax/smoke.json --split dev \
  --elephant-binary /tmp/memory-tax-baseline \
  --conditions none,elephant,raw_bm25,oracle_raw,oracle_lessons \
  --output experiments/memory_tax/local-results/dev.json
```

Never substitute a Python imitation for the `elephant` baseline. Integration
coverage is skipped unless `ELEPHANT_EVAL_BINARY` is explicitly set.

## Real local embedding and reranking models

Optional experimental dependencies are isolated from the Go product:

```sh
python3 -m venv /tmp/elephant-eval-venv
/tmp/elephant-eval-venv/bin/pip install -r experiments/memory_tax/requirements-models.txt
/tmp/elephant-eval-venv/bin/python experiments/memory_tax/run.py \
  --dataset experiments/memory_tax/smoke.json --split dev \
  --elephant-binary /tmp/memory-tax-baseline \
  --conditions none,elephant,raw_bm25,raw_hybrid,combined,fallback,reserved,semantic_lessons,oracle_raw,oracle_lessons \
  --model-cache .cache/memory-tax-models --allow-model-download \
  --budget 4000 --candidates 10 --min-rerank 0 \
  --output experiments/memory_tax/local-results/dev-models.json
```

Omit `--allow-model-download` for subsequent local-cache-only runs. Missing models
fail rather than silently falling back to lexical search. Model loading uses CPU,
two threads, BAAI/bge-small-en-v1.5 embeddings and
Xenova/ms-marco-MiniLM-L-6-v2 cross-encoder scores. The report hashes actual local
ONNX/config/tokenizer files, experiment source, helper binary, dataset, repository
revision and dirty state. Reproduction requires preserving those model artifacts,
not merely the model aliases. Model hashes are observed after loading; this is not
a supply-chain authenticity guarantee. Install/first-download time and disk cost
are outside per-query measurements; setup time and artifact hashes are recorded.

These are alternative models, not a reproduction of the paper's Qwen baseline.
The zero-logit reranker admission threshold is an experimental fixed setting,
not calibrated confidence. It remains unchanged for the reserved run. Results can
reveal threshold failures. No model choice or threshold changes production recall.

## Conditions

| Name | Behavior |
| --- | --- |
| `none` | Empty historical context |
| `elephant` | Actual production lesson recall and serialization |
| `raw_bm25` | Positive-score BM25 over authorized raw event chunks |
| `raw_hybrid` | BM25+dense top-k union, RRF ordering, cross-encoder reranking |
| `combined` | Same hybrid retrieval over independently eligible raw and lesson chunks |
| `fallback` | Production lessons; raw hybrid only if lesson recall is empty |
| `reserved` | Combined ranking, giving one highest-ranked admitted raw chunk packing priority |
| `semantic_lessons` | Hybrid lesson-only ablation with the same incident+lesson+subject search text |
| `oracle_raw` | Gold-source context, bounded by budget; diagnostic only |
| `oracle_lessons` | Gold-lesson context, bounded by budget; diagnostic only |

`fallback` is deliberately a cheap **empty-result** trigger, not a learned
sufficiency detector. A nonempty but incomplete lesson can still fail it.
`reserved` prioritizes one fitting raw chunk; it does not guarantee a fraction of
context, or bypass admission when the reranker rejects evidence.

Scope, time, availability and applicable-lesson filters execute before embedding
or reranking. Historical source events can remain searchable when their derived
lesson is inapplicable, but are labelled historical evidence, not current advice.
Tenant labels are local test scoping, not enterprise authorization.

Events never merge across sessions. Long events split at 1,024 Unicode characters
with 128-character overlap. Spans are character offsets; budgets use UTF-8 bytes.
A gold source/lesson is covered only when the selected chunks cover every required
`gold_spans[id]` interval. Without explicit gold spans, the entire document must be
covered. Source-link coverage is separately labelled and never proves a fact
survived extraction. Exact matching of gold IDs is an evidence diagnostic, not a
judge of answer quality. No-label cases require abstention.

## Optional QA, follow-up and outcome scoring

Supply explicit subprocess argv arrays; commands execute without a shell. These
adapters are user-supplied trusted programs and may access their own network or
files. Keep credentials out of argv/reports. Nothing calls an LLM by default.

```sh
python3 experiments/memory_tax/run.py \
  --dataset /path/to/sanitized-consented-cases.json --split holdout \
  --conditions raw_hybrid,combined --rounds 2 \
  --model-cache .cache/memory-tax-models \
  --qa-command '["python3","/path/to/answer_adapter.py"]' \
  --score-command '["python3","/path/to/outcome_scorer.py"]' \
  --output experiments/memory_tax/local-results/outcomes.json
```

QA reads one JSON request on stdin:

```json
{"operation":"answer","query":"...","context":"...","current_time":"2026-10-01T00:00:00Z","round":1,"allow_followup":true}
```

It returns exactly one JSON object on stdout:

```json
{"model_id":"pinned-provider-model-and-settings","answer":"...","followup_query":null,"provider_tokens":123,"billed_cost":0.001}
```

Usage fields are optional and remain null when unknown. Model identity must match
across all cases/conditions, and it should encode settings/revision. A follow-up
query is allowed only for raw BM25, raw hybrid, combined, reserved and semantic
lesson arms when `--rounds 2`. The other controls remain single round. The second
query searches the remaining chunk pool. A third query fails. First-round context
is capped at one third of the budget, and its repeated appearance in the second QA call
is charged again: total retrieved-context exposure bytes cannot exceed `--budget`.
Query, system prompt, output tokens and model-specific tokenizer counts are not
bounded by that byte limit; report actual provider usage for end-to-end costs.
Two rounds without a QA adapter fail rather than invent a follow-up query.

Gold labels are never passed to QA. A scorer, invoked after the final answer,
receives `{"operation":"score","case":{...},"answer":"..."}` and returns
`{"model_id":"scorer-version","success":true}` with optional check details.
Use executable task checks where possible. A supplied scorer's success field is
its assertion, not independently verified task completion. No coding repo is
executed by this harness. An adapter/scorer must orchestrate isolated task states
and tool budgets for actual coding trials. A controlled live-agent experiment
remains separate work; synthetic adapter protocol tests are not LLM evaluations.

## Dataset and report interpretation

`make_smoke.py` is the reproducible authoring source for `smoke.json`.
Cases carry identity, project/conversation, explicit time/features, source events,
lessons, group/split and gold IDs. Items carry source, occurrence time, original
text hash, scope/owner, `evidence_type` (observation, user_report, assistant_claim,
interpretation), availability and optional applicability/provenance fields.
Lesson records additionally require incident and evidence IDs. Source hashes
verify fixture consistency, not truth. Lesson evidence IDs identify provenance;
`gold_lessons` is a separate judgement that the lesson actually preserves the
needed information. A lesson made after the evaluation time is excluded even if
its linked source event is older.

The loader rejects cross-split incident groups, invalid content hashes, invalid
gold spans and forbidden/unavailable/future gold targets. It does not prove that
different group names are independent; dataset authors must check lineage and
repository/time splits. Histories must exist before evaluation queries are written.

Reports include source/lesson eligibility exclusions, candidates/scores, budget
omissions, selected chunk IDs, raw span coverage, source-link coverage, abstention,
paired retrieval benefit/tax against raw hybrid, elapsed work and optional QA and
scorer records. Model setup is separate. Embeddings are rebuilt per case/condition:
these are cold diagnostic costs, not warm production latency or dollar savings.
Extraction/maintenance cost for authored fixtures is unknown, not zero. There is
no automatic memory construction in this experiment.

Real-data quality conclusions require grouped paired confidence intervals,
sample-size planning, a predeclared non-inferiority/benefit criterion and actual
provider costs. This smoke runner intentionally does not manufacture significance
or cost savings from tiny synthetic data. Task success is null without a scorer.
No LongMemEval-s or real coding-task score is claimed by the included report.

## Release decision

Do not enable production evidence capture on these results. First collect a fresh
sanitized/consented historical dataset, run frozen-model outcome comparisons,
inspect disagreements and size a powered follow-up. Only a demonstrated quality
or cost/latency advantage justifies an opt-in adapter and evidence deletion,
retention, backup, access and rollback lifecycle. Automated retrieval learning,
team/raw-history sync and graph storage remain deferred.

Reference: [Memory Tax](https://arxiv.org/html/2610.34073v1), especially §6 and
Appendix A.2. FastEmbed's model API is documented by
[Qdrant](https://qdrant.tech/documentation/fastembed/fastembed-rerankers/).
