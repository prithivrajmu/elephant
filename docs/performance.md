# Performance measurement and ratchets

Elephant measures complete local journeys and protects stable implementation
counts separately from noisy elapsed time. The method is adapted from
[Anthropic's performance sprint](https://claude.dev/blog/how-we-made-claude-ai-faster/):
select a user journey, prove that a lab proxy tracks the user metric, improve
the measured bottleneck, and lock in the win with a ceiling that can only fall.

## Reproduce the evidence

Run deterministic ratchets:

```sh
python3 scripts/update_performance_ratchets.py
```

After a verified improvement, lower eligible count ceilings to the observed
value:

```sh
python3 scripts/update_performance_ratchets.py --lower
```

The helper refuses a measured regression. It has no mode that raises a ceiling.
Review and explain any intentional ceiling increase as a normal source change.
Allocation ceilings retain headroom because compiler/runtime versions and map
layout can change allocation counts. Calibrate allocation reductions across
supported platforms; the helper does not tighten them from one host sample.
Race-instrumented tests retain the work-count checks but skip allocation
comparison. Measure production allocations with the uninstrumented helper above.

Measure complete synthetic journeys through the built binary:

```sh
python3 scripts/perf_baseline.py --sizes 100,1000,10000 --samples 10 \
  --output performance-report.json
```

The runner uses isolated temporary stores and marked synthetic lessons. It
measures setup through six-tool MCP discovery, automatic hook recall, durable
Memory capture, and Memory Palace startup/API response. The output contains the
revision, dirty-worktree flag, environment, binary hash, sample count, first-process
result, subsequent-process p50/p75/p95, and recall phase timings. Every recall
sample launches a new process; recently seeded files and uncontrolled OS caches
make these unsuitable as cold-cache measurements. Capture checks acknowledged
IDs after reopening the store. Palace checks the expected fixture inventory.
Setup and Palace currently provide one observation each, not percentiles.
The report does not emit task text or Memory
content.

For a single local recall diagnosis:

```sh
elephant recall --trace --task "query concurrency"
```

`--trace` wraps the normal result with timings for manifest profiling, store
open, transaction begin, candidate loading, rank/render, receipt write, commit,
and total store recall, including connection close. Profiling is measured
separately and is not included in the store total. Candidate and successfully
read manifest counts are included. These are
diagnostic measurements, not a stable public wire contract.

## Ratchets and timing evidence

`testdata/performance-ratchets.json` contains deterministic ceilings for:

- allocations in the 1,000-memory ranking fixture;
- visible candidates decoded by a scoped 1,000-row store;
- recall context bytes;
- bounded manifest files;
- MCP tool-schema payload bytes;
- Memory Palace memory nodes in the 120-memory fixture.

Normal tests fail when a value exceeds its ceiling. The performance workflow
runs these checks on relevant pull requests. Scheduled and manual runs also
publish a 100/1,000/10,000-memory timing report. Shared CI wall-clock values are
evidence, not hard gates; environment noise must not block a correct change.

## Local dashboard metrics

Recall receipts store local phase timing, candidate count, manifest count,
selected Memory IDs, and byte counts. They do not store the task text. Memory
Palace shows core p50/p75/p95 and the profile/store/candidate/rank phases for
each recall. Receipt and commit timings are available in explicit trace and
synthetic runner output because they are known only after the usage receipt is
serialized.

Core latency retains its historical meaning: transaction acquisition, store
open, candidate loading, ranking, rendering, and baseline accounting before the
receipt is written. End-to-end hook latency also includes binary startup, hook
state and experience writes, receipt serialization, commit, and process exit.

## Guardrails and limits

- Retrieval contracts, scope isolation, applicability, exact byte budgets,
  durability, evidence provenance, and dashboard security take precedence over
  speed.
- Elephant uploads no performance report or task data. GitHub Actions artifacts
  contain synthetic benchmark output only.
- Synthetic results do not establish agent productivity, token savings, billing
  reduction, or real-host performance.
- A lower proxy is useful only after representative elapsed-time evidence moves
  in the same direction. Remove a proxy that rewards the wrong behavior.
- Do not keep a complex optimization for a negligible result.
