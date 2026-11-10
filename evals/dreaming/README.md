# Dreaming evaluations

## Offline contract suite (no model credits)

```sh
go build -o /tmp/elephant-dream ./cmd/elephant
python3 evals/dreaming/run.py /tmp/elephant-dream --output /tmp/dream-results.json
go test -race ./...
```

Six explicit synthetic sources cover outdated pool capacity, a backfill exception,
queue idempotency, changed timeout, an unsupported source claim, and paraphrased
retrieval. Hand-authored fixture replacements are positive/negative controls.
The driver calls the real CLI and SQLite, checks no-write candidate evaluation,
then commits accepted corrections and recalls them after a fresh process starts.
The suite fails if it accepts a known bad control or rejects a known good one.
CI executes it on Linux, macOS and Windows.

These are contract checks, not a learned-model benchmark. Before/after scores use
exact source-backed fragments, not an LLM judge or executable coding tasks. Source
oracle text is in cases.json; it is not a raw-RAG comparison. Compression alone
is not success. The existing experiments/memory_tax harness supplies separate
raw-evidence/combined retrieval research; this suite does not replace it.

## Opt-in external reviewer evaluation

```sh
python3 evals/dreaming/run.py /tmp/elephant-dream \
  --reviewer-command /absolute/reviewer.json --output /tmp/model-dream-results.json
```

The JSON file is a trusted executable argument array, invoked without a shell,
in each isolated case directory. It receives `instructions`, `batch` and
`output_schema` on stdin and returns `{"replacements":[...]}` on stdout. Use the
production dreaming proposal shape; read `source.md` with read-only tools. The
harness never sends gold probe fragments or fixture replacements to the reviewer.
No-change results are valid. A model can fail an expectation without terminating
the suite: inspect `reviewer_passed` and per-case results. Nonzero process exits,
timeouts, and malformed output fail the command. This mode may spend provider
allowance. Each process is bounded to 120 seconds; results are limited to 256 KiB.
Token/currency usage is unavailable unless measured separately by the provider.

External command adapters must enforce their own read-only permissions. They
are local trusted configuration, not commands accepted from a memory. The schema
in the harness is a transport hint; production proposal validation runs through
`dream-eval`. All memories and commits remain in temporary synthetic stores.

## Real Codex adapter smoke test

```sh
ELEPHANT_LIVE_DREAM_EVAL=1 go test -run '^TestDreamLiveReviewer$' -v .
```

Requires installed/authenticated Codex, writable Codex runtime state, and support
for its nested read-only sandbox. Calls the real scheduler runner and parent
commit path against a synthetic markdown source: correct pool capacity 64 to 8,
retire the old note, preserve lineage, and leave usefulness observations at zero.
Skipped in normal CI to avoid provider spend. One smoke test is not evidence of
broad consolidation quality. Do not remove the sandbox to make it pass.

## Observed run on 9 November 2026

- Baseline Go tests passed before changes.
- Six offline controls passed, including rejection of both harmful replacements.
- MCP/CLI, storage, automatic hooks, and detached idle-watcher acceptance passed.
- Real Codex adapter did not reach inference in this Work environment. Initially
  its runtime state was read-only. With isolated writable runtime state, nested
  sandbox startup failed with `NETLINK_ROUTE socket: Operation not permitted`.
  Consequently real model correction quality is **unverified**, and no live
  quality score or provider-cost improvement is claimed.

## Next experiments

Add held-out incident families and executable future coding tasks; compare no
memory, ordinary recall, current dreaming, and each new feature independently.
Hold reader/reviewer model and recall budgets fixed. Measure total maintenance
plus foreground usage and uncertainty across task orders. Evaluate source-change
prioritization and immutable claim/span evidence as separate additions. Today's
within-page ordering does not implement either feature.

## Challenge before efficacy testing

Read [the adversarial methodology review](METHODOLOGY.md) before designing new
quality experiments. Run `python3 evals/dreaming/challenge.py` for analytic
counterexamples and hypothetical sample-size/cost arithmetic; this is not a benchmark.

Schema v2 separates reviewer time, candidate evaluation time, and total review/evaluate/commit
time (excluding fixture setup). External `model_calls` is null/unknown;
`reviewer_invocations` counts subprocesses. Provider usage remains unmeasured.
