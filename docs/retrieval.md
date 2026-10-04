# SQL retrieval

Local recall selects tenant, owner, project, conversation and approved team candidates in SQL before decoding payloads. Retired records are excluded. The existing engine rechecks visibility and applicability and uses the complete eligible corpus for BM25. Ranking, alternatives, initialization, abstention, context budgets and the dashboard baseline retain their semantics. No storage migration or candidate truncation is introduced.

Run `go test -run '^$' -bench BenchmarkSQLCandidateSelection -benchmem`.
Measured on 2026-10-04 with Go 1.25.8, Linux amd64, AMD EPYC 9V74, three iterations per case, warm database, excluding setup and usage receipt writes:

| Selection | Decoded rows | Time | Allocated bytes |
| --- | ---: | ---: | ---: |
| All rows | 10,000 | 87.86 ms | 42,944,477 |
| Scoped SQL | 100 | 4.83 ms | 359,760 |
| Exact token index experiment | 1 | 0.25 ms | 3,488 |

These are synthetic selection measurements, not end-to-end recall latency or Cloudflare CPU measurements. The lexical experiment is benchmark-only: its different corpus would change document frequency and average document length. Production lexical narrowing needs indexed statistics over the complete visible/applicable corpus and an equivalent baseline metric, plus measurements of index maintenance, cold reads and real workload distributions. SQLite FTS tokenization must also be checked against Elephant's Unicode tokenizer.

Administrative exports and dashboards still load the full store. Recall cost still grows with the user's visible corpus; SQL scoping alone does not make an unbounded personal corpus constant cost.
