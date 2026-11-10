# Adversarial review of Elephant dreaming

10 November 2026 · design challenge before efficacy experiments

Status: proposed protocol, not a completed preregistration or benchmark result.
The six existing hand-authored cases remain implementation controls. They provide
no population estimate of task improvement, safety, or consolidation quality.
The live Codex adapter remains unverified in this environment. Nothing in this
review upgrades an agent-asserted source check into verified truth.

## 1. The claim we must be willing to reject

A bounded, source-grounded consolidation policy improves future coding-task
outcomes compared with both ordinary Elephant recall and strong source retrieval,
at acceptable total cost and without unacceptable loss of rare constraints.

This is a conjunction of claims, not one score. Improvement over ordinary recall
alone cannot establish improvement over raw retrieval. Smaller memory cannot
establish better task results. Source fidelity cannot establish retrievability;
retrievability cannot establish truth. A release-quality contract test cannot
establish learned-model quality.

The honest alternative hypotheses include: no improvement; benefits entirely due
to extra inference; gains limited to predictable repeated tasks; lower average
cost with worse rare-case failures; or a navigation aid being more valuable than
rewriting. Any of these can require simplifying or rejecting the design.

## 2. Information-theoretic challenge: compression can destroy the answer

Let E be authorized evidence, Q a future query, Y its correct answer, and M=C(E)
a consolidated memory. If C is a fixed transformation whose randomness supplies
no additional information about Y, conditional data processing gives:

    I(Y; M | Q) <= I(Y; E | Q).

Rewriting cannot manufacture new task information. It can make retained evidence
cheaper to find or easier for a bounded reader to use. That computational benefit
is empirical and must be distinguished from information preservation.

Counterexample: two equally likely source worlds both compress to “Use caching.”
One prohibits caching during backfill, while the other permits it. The compressed
state is identical, but the correct backfill answer differs. A reader restricted
to this state has at least 50% error on these two worlds. Arbitrarily polished
summaries and more self-reflection cannot recover the missing distinction.

Consequence: treat consolidation as a task-distribution-dependent rate/distortion
problem. Minimize expected future loss subject to a storage budget, but separately
measure loss on rare constraints. Under an unknown query distribution, no universal
lossless-short-summary guarantee exists. Original evidence rehydration changes the
assumptions and is an escape route; measure how often it is actually exercised.

## 3. The probe gate is a fallible surrogate

Current dream-eval tests case-sensitive fragments in rendered recall context.
“The capacity is 8” and “It is false that the capacity is 8” both contain
“capacity is 8”. Required phrases can also survive in an incident or source label
while the actionable rule is wrong. A rewrite can pass every known probe and fail
an unseen exception. The gate does not detect entailment or certify source truth.

Use fragment probes for deterministic contracts only. Efficacy assessment needs
independent evidence-grounded judgments and executable future-task checks. Mutation
controls must negate facts, swap entities, change units, erase exceptions, make
provenance stale, and put gold phrases only in metadata. Report which mutations each
measurement detects, rather than declaring one metric a universal verifier.

Falsifier: a semantic mutation passes the metric. This rejects the metric's
factual-certification interpretation, even if the implementation works perfectly.
The analytic counterexample in challenge.py already demonstrates that limitation.

## 4. Repeated dreaming does not create independent evidence

An original incident rewritten 100 times is still one incident. Multiplying
likelihood ratios or increasing confidence as though those rewrites were independent
would be invalid. For equally correlated measurements, the familiar mean-variance
planning expression n_eff = k / (1 + (k-1)rho) illustrates the issue: at rho=1,
100 measurements carry the mean-estimation precision of one measurement. This is
an illustration, not a calibrated confidence model for agent memories.

Represent ancestry and independent incident identity. Distinguish new observations
from transformations, source pointers from checked assertions, and agreement from
independent corroboration. Test repeated paraphrases of one wrong source versus
independently verified counterevidence. Dreaming must not manufacture feedback.

## 5. Bigger search spaces amplify selection bias

Combining paper ideas permits many variants; it also permits many opportunities to
find a flattering result by chance. For 20 independent null tests at alpha=.05,
P(at least one false positive) = 1 - .95^20 = 64.15%. Real adaptive comparisons
need not be independent, so this number is illustrative, not a general bound.

Separate development probes, a pilot for design/variance estimation, and a sealed
confirmatory set. Freeze code, model, prompts, budgets, stopping rules, and analysis
before looking at confirmatory results. Register failures and all variants tried.
Reusing the holdout to tune the selector turns it into development data. For
multiple confirmatory tests use a declared familywise correction such as Holm;
use fixed-sample inference unless a valid sequential design was declared in advance.

No differential privacy or reusable-holdout guarantee is implemented here. The
adaptive-data-analysis literature explains why ordinary holdout reuse is unsafe;
it does not automatically validate Elephant's experimental procedure.

## 6. Causal controls: what actually caused any improvement?

| Arm | Purpose |
| --- | --- |
| A: no historical context | Measures the value of historical access. |
| B: strong authorized raw-source retrieval | Tests whether authored memory adds net value. |
| C: ordinary Elephant recall | Isolates the additional value of dreaming. |
| D: C plus source validation/navigation, no rewriting | Separates evidence recovery from consolidation. |
| E: C plus matched extra reasoning, no persistent rewrite | Tests whether gains are simply additional inference. |
| F: proposed complete dreaming | Candidate policy. |
| G: B plus dreamed navigation/lessons | Tests the evidence-plus-memory combination. |

Primary comparisons: F versus C, and G versus B. Declare the task-success endpoint
and multiplicity treatment before the confirmatory run. D and E are mechanism
controls. Ablate selector, source identity, probes, and procedural abstraction on
development data; nominate the final combination before confirmatory evaluation.
Do not claim all seven arms are individually powered without a separate calculation.

Use identical authorized source pools, frozen reader model/configuration, source
cutoffs, and future tasks. The dreamer sees past evidence, never the future query
or gold answer. Grading may see the gold; policy generation may not. Source hashes
prove byte identity, not factual correctness or authority. Prompt injection and
conflicting evidence need their own outcomes.

Run isolated copies of the same initial state for each arm. A dream learned in
one arm must not reach another. Keep source-change histories and task order paired.
A later task in the same memory trajectory is correlated with earlier tasks; it
is not a fresh independent sample. Split by repository/incident family and time,
not paraphrases of the same incident. State the target population explicitly.

## 7. Statistics: use the independent unit we actually have

For paired binary task success define D_i = Y_i(dream) - Y_i(control).
Estimate delta = mean(D_i). If q is the discordance probability, then
Var(D_i) = q - delta^2. Under independent pairs, a rough two-sided 5%, 80%-power
planning approximation is:

    n ~= (1.96 + .84)^2 (q - delta^2) / delta^2.

For hypothetical delta=.05 and q=.20, this is approximately 621 paired tasks.
It is a planning estimate, not pilot evidence or an exact power calculation.
With five tasks per cluster and rho=.20 for paired differences, the rough design
effect 1+(5-1)*.20 = 1.8 suggests about 1,118 task pairs. Real cluster sizes,
effect heterogeneity, and discrete outcomes require pilot-based simulation.

Inference should respect independent repository/trajectory clusters: report paired
cluster-level effects and cluster-resampled confidence intervals when enough
independent clusters exist. Do not bootstrap individual questions from the same
trajectory and pretend the sample is independent. Few clusters require explicit
small-sample methods or an inconclusive result, not just more repeated seeds.

For zero observed harms in n independent Bernoulli opportunities, the one-sided
95% exact upper bound is 1 - .05^(1/n). Even six clean IID trials would leave an
upper bound near 39.3%. At least 299 clean independent trials would be needed to
put this bound below 1%. Our six curated fixtures are not IID trials, so this
calculation does not apply to their population safety at all. Mutation tests and
population harm estimates answer different questions.

## 8. Cost: idle time is not free

Let D be maintenance cost per cycle and C0,C1 per-task foreground costs in the
same units. For N relevant future tasks, net compute saving is:

    S(N) = N(C0-C1) - D.

If C0>C1, break-even is N*=D/(C0-C1); otherwise there is no positive break-even
from compute savings. A hypothetical maintenance cost of 100 with a saving of 2
per future task needs 50 reused queries merely to break even. Source churn can
invalidate the memory before then. Include retries, failed/no-change runs, source
fetches, probes, index work, and remote costs; do not count only accepted dreams.

Compare two regimes: fixed online context/latency budget, and matched total
maintenance-plus-online resource budgets. Report quality/cost frontiers. Avoid
combining dollars, milliseconds, tokens, and harm into an unexplained scalar.
Utility weights and acceptable harm must be declared if a scalar decision rule is
used; otherwise show the separate tradeoffs.

The initial harness started elapsed_ms after the external reviewer returned and
counted reviewer subprocess invocations as model_calls. This revision corrects
those misleading measurements: schema v2 separates reviewer_elapsed_ms,
candidate_eval_ms, and review_evaluate_commit_ms (which excludes fixture setup).
reviewer_invocations counts processes; external model_calls is unknown/null. These
measurements still omit amortized infrastructure/setup and provider usage; full
maintenance cost remains a prerequisite for cost-effectiveness experiments.

## 9. Stress the longitudinal failure modes

| Stress | What would disprove the intended behavior? |
| --- | --- |
| Rare exceptions surrounded by repetitive common cases | Compression improves the mean but loses the rare invariant. |
| Abrupt source/requirement changes | A familiar old procedure overrides current evidence. |
| Repeated bad source or copied incident | Confidence or usefulness grows without new observations. |
| No predictable reuse | Dreaming consumes resources without measurable downstream benefit. |
| Good old memory, new irrelevant material | Consolidation degrades previously correct tasks. |
| Full knowledge but poor retrieval | Rewriting is credited for gains actually caused by navigation. |
| Poisoned text with valid hashes | Provenance is mistaken for correctness or authority. |
| Many maintenance cycles without new evidence | Rules drift, lineage balloons, or exceptions disappear. |
| Multi-replacement partial commits | Intermediate memory violates a required contract. |

No-mutation/no-new-evidence runs should test semantic stability, not exact textual
idempotence. Fixed-point prose is neither necessary nor sufficient for useful
memory. A per-cycle failure probability epsilon can compound; under independent
cycles the probability of at least one failure over T cycles is
1-(1-epsilon)^T. Without independence, do not use this expression as a prediction.
Measure actual trajectory failure rates and reversibility instead.

## 10. Go/no-go rules to freeze before confirmatory testing

1. Define the target repositories/task families, outcome rubric, unit of
   randomization, budget regimes, and source availability policy.
2. Run only a development/pilot stage to establish variance, baseline failures,
   cost instrumentation, and adequate cluster/sample size. Seal new holdout data.
3. Declare the minimum practically useful effect and harm/noninferiority margins.
   A planning target of five percentage points is hypothetical, not a chosen
   release threshold. A lower confidence bound above zero establishes positive
   evidence; it does not establish a five-point minimum improvement.
4. Freeze the final policy and a machine-readable manifest of code, corpus,
   model/configuration, seeds/order, exclusions, and analysis. Check source, probe,
   and incident leakage. Provider reproducibility limitations must be recorded.
5. Evaluate once under the chosen analysis; publish all failures and uncertainty.
   Inconclusive results stay inconclusive. No quality or cost claim from fixtures.
6. Reject or narrow the architecture if source retrieval matches/beats the full
   system, navigation alone explains the gain, total cost fails to amortize, or
   tail-risk crosses the declared margin. Keep the simpler successful component.

## References and scope

- [Memory Tax](https://arxiv.org/abs/2610.34073): the raw-evidence control and risk of
  evidence loss motivate a comparison, not an assumed result for Elephant.
- [Auto-Dreamer](https://arxiv.org/abs/2605.20616): a learned consolidator trained on
  downstream outcomes is not equivalent to our prompted local reviewer.
- [MemDream](https://arxiv.org/abs/2609.34545): anticipatory probes motivate a search
  for failures; generated probes alone cannot certify unseen-task performance.
- [Preserving Statistical Validity in Adaptive Data Analysis](https://arxiv.org/abs/1411.2664):
  grounding for the holdout-reuse concern. We do not claim to implement its guarantees.

The formulas/counterexamples here are our analysis, under the assumptions stated.
Run `python3 evals/dreaming/challenge.py` to reproduce the arithmetic. That script
performs no model calls, does not run a benchmark, and supplies no empirical proof
that dreaming helps or hurts Elephant.
