# Dreaming research and delivery plan

Updated 10 November 2026. This page records the agreed research synthesis, its
challenges, and the order of implementation. Proposed capabilities are not
current product guarantees. See [current dreaming behavior](dreaming.md).

## Research synthesis

| Proposed capability | Research ingredients | Design decision |
| --- | --- | --- |
| Evidence-backed working regions | Auto-Dreamer, Claude dreaming, Memory Tax | Select related new/retrieved/negatively rated memories; retain recoverable source versions. |
| Self-testing memory | MemDream, ReasoningBank | Use independent source-backed probes and future executable tasks; separate retrieval contracts from factual correctness. |
| Adaptive maintenance | Sleep-time Compute, Auto-Dreamer | Launch reviews only when evidence/change and expected reuse justify bounded cost. |
| Conditional procedures | ReasoningBank, Auto-Dreamer | Preserve trigger, preconditions, action, exceptions and independent supporting incidents. |
| Reversible demotion | MemDream, selectionist consolidation | Investigate retrieval demotion only after rare-constraint retention and recovery tests. |
| Topic navigation | Claude linked representations, source retrieval | Connect lessons with different applicability without merging away their boundaries. |
| Observed utility | Auto-Dreamer, Memory Tax | Track later task outcomes separately from source checks and synthetic probe results. |

Paper results are motivation, not results reproduced for Elephant. A prompted
reviewer is not the GRPO-trained consolidator evaluated by Auto-Dreamer.

## What exists in PR #57

- Source-checked correction/consolidation and opt-in idle scheduling predate this PR.
- Read-only before/after `dream-eval`, an optional scheduler probe gate, and
  negative-feedback-first ordering within existing ID-selected pages.
- Six synthetic positive/negative controls, cross-platform CI, opt-in external
  reviewer evaluation, and a real Codex adapter smoke test.
- A repaired release-doc link and explicit unknown provider-call accounting.
- An adversarial mathematical/scientific review and reproducible analytic examples.

Fragment probes are not truth tests. The reviewer still asserts its source checks.
The current queue does not perform global working-region selection. Cloud sync
still cannot preserve dreaming lineage. Live inference remains unverified here:
this runtime blocks Codex's nested read-only sandbox. No protection was weakened.

## Scientific challenges that govern the next work

1. Compression may erase distinctions needed by future queries. Recoverable source
   evidence and rare-exception outcomes are required.
2. Negation can fool phrase-presence probes. Use semantic mutations and executable
   task checks before claiming factual reliability.
3. Repeated rewrites of one incident are dependent evidence. Track source ancestry;
   do not increase confidence/usefulness without independent observations.
4. Extra inference or better source navigation may explain an apparent gain.
   Compare matched-compute and navigation-without-rewriting controls.
5. Reusing probes/holdouts to choose among many variants creates selection bias.
   Separate development, pilot and sealed confirmation; publish failed variants.
6. Related tasks share memory state. Estimate effects at independent trajectory or
   repository clusters, and plan sample size with dependence accounted for.
7. Maintenance cost must amortize over real reuse before source churn invalidates
   the lesson. Report total cost and tail risk separately from memory size.

The full derivations, illustrative calculations, control arms, and rejection rules
are in the [methodology review](https://github.com/prithivrajmu/elephant/blob/feat/dream-evals/evals/dreaming/METHODOLOGY.md).
It is a proposed protocol, not a completed preregistration. Numerical planning
examples are hypothetical, not measured gains or selected release thresholds.

## Delivery sequence and gates

| Stage | Deliverable | Evidence needed to proceed |
| --- | --- | --- |
| Foundation | Complete PR #57 implementation and packaging checks | Contract/acceptance checks pass; limits documented. |
| Pilot preparation — current | Frozen source identity/spans, incident groups, split preflight, design inventory | Changed evidence, duplicate incident families and identical sources across splits are rejected. Existing fixtures remain development-only. |
| Pilot instrumentation | Complete primary comparison, model/configuration, task taxonomy, budgets and usage accounting | No missing settings; no gold queries/answers exposed to dreamer; pilot estimates variance/cost only. |
| Source grounding | Versioned evidence identity in production dreaming, with unavailable/conflicting source states | Round-trip, source-change and race tests; hashes never labelled truth verification. |
| Working regions | Bounded selection of new, retrieved, negatively rated and compatible related memories | Equal-budget comparison against ID ordering; coverage, starvation and isolation checks. |
| Conditional procedures and stronger probes | Preserve applicability/exceptions while testing future executable tasks | Independent incident support; mutation sensitivity; held-out task non-regression. |
| Confirmation | Freeze nominated policy, corpus, code, models and analysis | Adequate independent clusters; declared practical effect/harm margins; one sealed assessment. |
| Later experiments | Adaptive scheduling, reversible demotion, topic navigation | Demonstrated additional benefit beyond source recovery and matched compute. |

Do not build all seven ideas into one unidentifiable experiment. Nominate a small
combination on development data; isolate its mechanism with ablations. No benchmark
claim follows from passing a manifest preflight or analytic counterexample script.

## Pilot source-manifest contract

The [eval guide](https://github.com/prithivrajmu/elephant/blob/feat/dream-evals/evals/dreaming/README.md)
describes the executable preflight. It records exact source bytes and byte spans,
source hashes, declared incident families and dataset splits. Existing synthetic
controls are explicitly development data, with no pilot/holdout independence claim.
Preflight detects exact identity overlap and declared-family leakage; paraphrased
sources and incorrectly declared families still require review.

A pilot must record its reader/reviewer configuration, code commit, seeds, endpoint,
comparison, resource budget, analysis and stopping rule. Confirmatory data stays
sealed during pilot tuning. This tool checks structure/identity, not sufficient
statistical power, truth, chronology, or an externally registered protocol.

## Sources

- [Auto-Dreamer](https://arxiv.org/abs/2605.20616)
- [MemDream](https://arxiv.org/abs/2609.34545)
- [Sleep-time Compute](https://arxiv.org/abs/2504.13171)
- [ReasoningBank](https://arxiv.org/abs/2509.25140)
- [Memory Tax](https://arxiv.org/abs/2610.34073)
- [Claude components of agent memory](https://www.anthropic.com/research/components-of-agent-memory)
- [Preserving Statistical Validity in Adaptive Data Analysis](https://arxiv.org/abs/1411.2664)
