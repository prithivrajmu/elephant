# Astra advisor review and version 0.3 decisions

The user explicitly requested an independent GPT-6 Astra advisor review. The advisor read the Go implementation, design, persistence, metrics, profiler and tests without editing the files. This document records the findings and changes made by the primary implementation agent. It is not a claim that a second model establishes mathematical correctness or real-world agent effectiveness.

## Findings fixed now

1. **Same-stack negative transfer.** The original profile score could admit a memory with no relation to an explicit task. Task retrieval now requires at least one exact non-stopword term in incident, lesson or subject. Profile agreement only adds a bounded bonus after this gate. The gate remains lexical, so broad words can still create false positives and synonyms can be missed.
2. **Corpus-dependent admission.** A one-document/one-term BM25 score normalized to about 0.126 fell below the old 0.15 threshold. Adding one unrelated document of equal length raised the unchanged memory to about 0.257 and admitted it. BM25 still ranks candidates, but its value no longer decides whether an exact task match is admitted.
3. **Unknown metadata treated as disagreement.** The new known-dimension similarity separates agreement from coverage. Unknown features reduce corroboration, not task relevance. Query-only dimensions do not dilute agreement.
4. **Conditions missing from context.** Complete requires/excludes, incident, project/conversation provenance and evidence source are now serialized within the same hard byte budget. Long complete entries are skipped, never partially truncated.
5. **Mandatory project metadata.** An explicit `--unattached` mode leaves project empty and skips ambient working-directory manifests. Project-scoped records require a real nonempty ID. Optional conversation provenance and conversation-private scope have separate nonempty access guards.
6. **Unavailable conflict alternatives.** Potential alternatives now carry up to five accessible candidate IDs plus total and omitted counts and explicitly warn that alternatives may be omitted by the budget. Different wording is not claimed to prove contradiction.
7. **Inconsistent baseline.** Baseline and injected summaries use the same renderer, including possible alternative flags and applicability conditions.
8. **Ordering and observation keys.** Feature dimensions and query terms are sorted before floating-point accumulation. Feedback keys encode user/run as a JSON tuple, avoiding ambiguous delimiter combinations. Historical simple feedback keys are still recognized for idempotency.
9. **Unvalidated drama bonus.** Great/worst outcomes no longer receive an automatic salience boost. All four outcomes remain preserved for learning and inspection.

## Minimal project-optional design implemented

Authorization uses tenant, owner, scope and team approval. Origin uses conversation ID plus the existing exact source reference. Project association is optional. Applicability uses authored requires/excludes and explicitly known current facts. These concerns remain separate.

- `personal`: private to the owner, reusable across that owner's projects and conversations.
- `conversation`: private to the owner and exact nonempty current conversation ID.
- `project`: private to the owner and exact nonempty current project ID.
- `team`: available only within the configured team after approval.

An untagged conversation is not assigned to a fake "general" project. Conversation provenance does not automatically restrict a personal lesson. A conversation-private lesson does not become globally reusable merely because its text matches.

CLI examples:

```sh
# Private reusable lesson from a discussion with no project:
./elephant record --unattached --conversation design-discussion-42 \
  --file examples/untagged-lesson.json

./elephant recall --unattached --conversation another-discussion \
  --task 'validation evidence for reviewers'

# Same mode is available to harnesses through the MCP server:
./elephant mcp --unattached --conversation design-discussion-42
```

For technical applicability without a repository, use MCP `context_features` or CLI `--context-file` containing `{"features":{"database":["snowflake"]}}`. These are asserted task facts, not automatically verified architecture. Unknown required facts reject a lesson. Setting context facts never alters permissions.

`recall --initialize` / `init_memory` without a task explicitly allows profile-based discovery. Without both a task and known features, it returns no memories. `recall` / `recall_memory` require a meaningful task and do not silently browse or load all general memories.

## Graph design visualized, not yet a graph database

The proposed connections are:

- Conversation/message evidence → supports an incident.
- Incident → supports or challenges a reusable lesson.
- Incident → belongs to zero or more projects.
- Lesson → applies under explicit topic/technical conditions.
- Agent run → retrieves/applies a lesson → produces an observed result.

The prototype still stores one incident with one lesson and an optional origin project/conversation. It does not implement multi-project inference, a graph store, automatic association or per-message extraction. The connection visualization is a proposed model with synthetic examples, not proof that an association engine is implemented.

If project associations are inferred later, each link needs source, extraction method, timestamp and review state. An inferred link must never grant project access or silently convert a personal/conversation lesson into project policy. Mixed conversations need incident-level links, not a project tag applied to the entire transcript.

## Still heuristic or deferred

- Usefulness `Beta(2+h,2+u)` assumes independent Bernoulli observations; these are self-reported and may be correlated. Its mean is reported usefulness, not truth probability. The response now includes observation count.
- BM25 normalization still changes ranking with corpus composition. Scores are not calibrated probabilities. The lexical gate is deliberately weak.
- Usefulness/age multipliers, MMR coefficient and profile weights are starting choices, not learned optimal parameters.
- Age uses the existing reviewed/updated timestamp. OccurredAt/ReviewedAt/LastValidatedAt need distinct lifecycle fields later.
- Exact duplicates, evidence independence and supersession need a reviewed consolidation pass. Repeated versions of the same incident must not be counted as independent experiments.
- Strict UTF-8 byte budgeting remains. Estimated tokens use bytes/4; exact provider tokenizers, tool overhead and billing are not modeled.
- Context avoidance compares with naive full loading of eligible lessons. It is not measured money saved, task improvement or execution speedup.
- Enterprise authentication, verified provenance, automatic extraction, distributed graph storage and graph-driven authorization are not included.

## Validation added

Regression tests cover unattached operation without a readable root, empty-project collisions, conversation isolation, explicit task relevance, corpus-independent admission, stopword abstention, known-dimension coverage, complete multibyte serialization, deterministic candidate ordering, identifiable budget-omitted alternatives, matching baseline serialization, explicit untagged task facts and no ranking boost for dramatic outcomes.

A second Astra review confirmed the focused core changes and identified unbounded alternative-ID rendering. That has been capped at five IDs per entry with total/unlisted counts, including a 150-alternative regression test. Worst-case quadratic alternative metadata work and the owner-wide management dashboard are documented.

## Elephant 0.3.0 pilot release review

GPT-6 Astra performed a second read-only implementation review. No new concrete P0/P1 blocker was found in the install/connect/record/recall/feedback code paths. The verdict was suitable for a limited local pilot after extracted-package lifecycle, concurrency and browser checks.

The initial identified blockers are now addressed: valid MCP required arrays, three-second bounded lock retries, prebuilt platform packages and checksum installers, setup/doctor/selftest, complete Elephant branding with legacy-store discovery, and an isolated first-use test. Small onboarding UI improvements expose conditions, facts, unavailable scopes and stable feedback IDs.

See PILOT_PLAN.md for the ordered full-product backlog and VALIDATION.md for executed checks. Native macOS/Windows and actual client-host interoperability remain pilot tasks; source review and cross-compilation do not prove them.
