# Design: remembering useful experience

## The model

Treat memory as case-based reasoning: retrieve an earlier situation, compare conditions, adapt a lesson, then observe the new result. This is not model training and doesn't change model weights.

The unit is **incident → reusable lesson → applicability → evidence → subsequent usefulness**. A raw transcript is evidence, not automatically a memory. An agent or human extracts a concise incident and lesson. The system keeps positive and negative experience; ordinary outcomes provide a denominator so rare dramatic incidents do not dominate the worldview.

Separate three types as the product grows:

1. Episodic: what happened and the observed outcome.
2. Procedural: how to handle a situation next time.
3. Policy/preferences: current authoritative project rules and personal workflow preferences.

The MVP stores episodes and their procedural lessons together. Project policy remains outside retrieved memory and takes precedence. A remembered preference cannot grant permission or supersede current policy. Reviewer/contributor labels are optional; avoid deriving stereotypes about people from limited incidents.

## Implemented math (version 0.3)

### 1. Hard eligibility and exact task admission

Filter tenant, visibility, retirement, team approval and applicability first. A project ID is optional, but project scope requires nonempty stored/current IDs. Conversation scope separately requires an exact nonempty conversation ID and the owner. None of these rules is a scoring penalty.

Every required dimension must match at least one authored value. Unknown required dimensions reject. Any exclusion match rejects. Mere origin-profile differences are not invented requirements. Query facts supplied through `context_features` are asserted facts, not verified production architecture.

For an explicit task, tokenize Unicode letters/digits, deduplicate, remove the documented small English stopword/boilerplate set in `memory.go`, and require at least one remaining exact term in incident, lesson or subject. If there are no content terms, abstain. Project similarity cannot rescue an unrelated task. This is lexical admission, not semantic understanding.

An explicitly requested `recall --initialize`/`init_memory` with a blank task permits profile discovery with agreement-times-coverage >= 0.15. Plain `recall` does not silently browse. Initialization with no task and no known features returns no memories.

### 2. Known agreement and coverage

For each comparable dimension, use weighted Jaccard. Let D contain dimensions with nonempty values in both the memory profile and current task profile:

    J_d = |profile_d ∩ memory_d| / |profile_d ∪ memory_d|
    S_known = Σ_(d in D) w_d J_d / Σ_(d in D) w_d
    Coverage = Σ_(d in D) w_d / Σ_(d with memory values) w_d

Use zero for empty denominators. Unknown fields reduce the profile bonus, not task relevance. Extra query-only dimensions do not dilute agreement. Initial heuristic weights remain database 2.5; framework/app 2; cloud/workflow 1.5; language/data model/style 1; reviewer/contributor 0.5. Custom dimensions default to 1. Sort dimensions before summing to make the result independent of Go map order.

### 3. BM25 ranking, not admission probability

    IDF(t) = ln(1 + (N - df(t) + 0.5) / (df(t) + 0.5))
    BM25(q,m) = Σ_t IDF(t) · tf(t,m)(k1+1)
                / [tf(t,m) + k1(1-b+b·|m|/avgLength)]
    L = BM25 / (BM25 + 2)
    R = L · (1 + 0.20 · S_known · Coverage)

Use k1=1.2 and b=0.75. Compute corpus statistics over visible, applicable memories only; sort query terms before summing. The same task-first formula applies with or without a project. With no known profile, R=L. Profile corroboration supplies at most a 20% bonus.

BM25 and its transform drift with the candidate corpus and are not calibrated correctness probabilities. In version 0.1, L below 0.15 could reject a one-term memory until an unrelated document changed IDF. Version 0.3 removes that admission threshold. This still doesn't provide synonyms, multilingual stopword sets, phrase indexes, stemming or embeddings.

### 4. Reported usefulness and age

    p_reported_helpful ~ Beta(2 + helpful, 2 + unhelpful)
    U = (2 + helpful) / (4 + helpful + unhelpful)
    F = exp(-ln(2) · ageDays / 180)
    score = R · (0.5 + 0.5U) · (0.8 + 0.2F)

For profile-only initialization, substitute S_known·Coverage for R. U is a shrunk estimate of reported usefulness under independent Bernoulli assumptions, not truth confidence. Return its observation count. Correlated peer outcomes and copied evidence need lineage before stronger statistical claims. Feedback keys encode user/run as a tuple and deduplicate repeated observations. Legacy IDs remain recognized.

The half-life is 180 days and contributes a modest soft discount. Feedback does not refresh age; approval currently updates the reviewed timestamp. Separate occurred/reviewed/validated dates later. Great/worst outcomes remain stored but receive no unvalidated ranking boost.

### 5. Diversity, conditions and context budget

    utility(m) = score(m) - 0.12 · max_selected Jaccard(words(m.lesson), words(selected.lesson))

Use an MMR-like greedy heuristic and deterministic ID tie-breaking. The coefficient is heuristic, not tuned. Complete serialization includes incident, lesson, source, project/conversation provenance, requires/excludes, match evidence, reported usefulness and observation count. Potential alternatives include accessible IDs with an explicit budget-omission warning.

Accept a complete entry only if the exact UTF-8 text plus header fits `byte_budget`. Default is 6 lessons and 4,000 bytes; caps are 20 lessons and 64,000 bytes. Long entries are skipped rather than losing applicability conditions. A tiny budget can validly return no lessons. Exact model tokenizer budgeting remains an adapter milestone.

Baseline and injection share one renderer. The baseline still represents naive full loading of all visible/applicable summaries, not comparison against an optimized competitor. Provider token counts, tool overhead, prompt caching and real task improvement are not established by this estimate.

### 6. Alternatives and graph roadmap

Same normalized subject with different lesson text is a potential alternative, not established contradiction. Subject normalization occurs on new writes. Alternatives omitted by selection remain identifiable. Version-aware constraints and reviewed supersession are future work.

Conversation provenance, optional origin project, visibility and applicability are separate. The proposed richer graph links message evidence to incidents, incidents to lessons, incidents to optional projects, and lesson application to observed task outcomes. The current record supports one incident/lesson plus an optional origin project/conversation; it is not yet a many-to-many graph database or automatic association engine.


## Learning loop and next versions

Today's extraction is human/agent-supplied. The agent-independent lifecycle is: profile → retrieve on initialization → retrieve again before a consequential decision → execute → inspect tests/review/outcome → record evidence-backed lesson → later report whether applying it helped.

Do not save every message. An optional reflection adapter can propose candidates from completed task events. It should emit typed JSON and validate incident, action, observed outcome, lesson, identifiers, conditions, version range, source and evidence quality. Proposals should distinguish observed vs inferred causes. Automatic capture of agent reasoning or generated guesses is not reliable proof of an outcome.

Next retrieval version:

- SQLite indexed metadata + FTS for local stores; embeddings optional and provider-neutral.
- Hybrid lexical and vector retrieval with Reciprocal Rank Fusion: `RRF(m)=Σ_r 1/(60+rank_r(m))`. Fuse ranks rather than adding incompatible raw BM25 and cosine scales.
- Cosine similarity `cos(q,m)=q·m/(||q|| ||m||)` is semantic resemblance, not a calibrated correctness probability. Keep hard scopes and conditions before candidate delivery.
- Version-aware invalidation, indexed candidate retrieval, bounded reranking, evidence consolidation, and exact tokenizer adapters.
- Preserve provenance for deduplicated lessons; keep the original episodes when a summary is promoted into a shared procedure.

## What to measure

The UI computes context estimates against a naive full-context baseline:

    baseline = all visible, applicable rendered summaries
    estimateTokens(text) = ceil(UTF8Bytes(text) / 4)
    avoided = max(0, Σ baselineTokenEst - Σ injectedTokenEst)

This is not a counterfactual improvement in completion time, model quality or billing. Retrieval adds overhead and can worsen outcomes. Prompt caching can alter the economics. Exact token counts require the selected model tokenizer and complete requests.

Evaluate with representative project/task pairs. Create relevance judgments and an untouched holdout, then run paired memory-on / memory-off trials with identical model, harness, task, starting repository state and tool budget. Reset persistent state between controlled conditions; prevent future outcomes from leaking into earlier evaluation tasks. Report sample sizes and uncertainty.

Retrieval metrics:

- Precision@k: useful retrieved cases / retrieved cases.
- Recall@k: retrieved relevant cases / all judged relevant cases.
- nDCG@k for graded relevance; scope leakage must remain zero.
- Applicability violations, contradiction exposure, stale advice and abstention rate.

Agent/task metrics:

- Test pass/completion rates, review rework and recurrence of known mistakes.
- End-to-end latency, tool calls, provider input/output tokens and billed cost.
- Net cost = agent execution cost + retrieval/extraction/storage cost.
- Paired time improvement = `(time_without - time_with)/time_without`; retain negative results.
- Success rate difference and bootstrap confidence intervals over paired tasks.

Keep exposure feedback separate from usefulness: retrieval isn't application; application isn't success; success doesn't prove memory caused it. A future task trace should link run → retrieved IDs → applied IDs → observed outcome → baseline/counterfactual. Today's usefulness is user-supplied feedback and is displayed as reported helpfulness.

## Enterprise peer learning

The local file exchange implements reviewable peer lesson transfer; it doesn't deliver hosted enterprise access. Production requires:

1. Authenticated identities resolved by the server, tenant isolation, per-team/project authorization before vector/text candidates are surfaced.
2. Encrypted storage and transport, audit logs, retention/physical deletion, redaction and source access controls.
3. Reviewed promotion from private incident to sanitized shared lesson; never pool all raw developer transcripts.
4. Provenance, signed/trusted imports, reviewer role enforcement, duplicate evidence detection and poisoning controls.
5. Postgres + indexed full-text/vector search, transactional imports, pagination, backups, metrics and quotas.
6. Shared lesson versions and revocation; peer-specific policy must not become global advice.

Start with a narrow, closed-beta team rollout and measure whether recurrence/rework declines before a broad rollout. Sharing should amplify validated lessons without turning a popular anecdote into organizational truth.

## Language decision

Go is a practical first choice for a small local binary, subprocess/MCP interfaces and a future concurrent service. It has a simple standard library and deployment model. The SQLite WAL implementation pins a Go SQLite driver and its transitive modules, without requiring a C compiler. Rust is a good option if you prefer its ownership model or later build a demanding retrieval engine; no measured requirement in this MVP justifies a rewrite. Python is useful for reflection experiments and evaluation scripts; TypeScript fits a rich UI/plugin ecosystem. None changes retrieval quality by itself. Choose based on integration and team fluency, then benchmark.

## Primary references checked while building

- MCP transports: https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
- MCP lifecycle: https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle
- MCP tools: https://modelcontextprotocol.io/specification/2025-06-18/server/tools
- Go installation: https://go.dev/doc/install
- Letta docs, an existing agent-memory approach: https://docs.letta.com/

This concept overlaps existing memory systems. The proposed distinction is explicit incident/outcome/applicability/usefulness tracking with transparent context budgets and evidence. It is not a claim that agent memory is a new category or that this MVP surpasses existing products.


## Fingerprint and Signals

The profiler reads bounded regular files from the project root: `package.json`, `tsconfig.json`, `go.mod`, `Cargo.toml`, `pyproject.toml`, and `.elephant.json`. It recognizes common JavaScript frameworks, language markers, AWS/Azure SDK dependencies, SQL/NoSQL drivers, and records manifest evidence. Dependency presence indicates a candidate identifier, not proof of actual production architecture.

It does not traverse all source files or infer people from Git history. Put project intent, database configuration, workflow policy, working style, reviewers and contributors in explicit labels:

```json
{
  "features": {
    "app": ["analytics-dashboard"],
    "database": ["snowflake"],
    "cloud": ["aws"],
    "workflow": ["stacked-prs", "review-required"],
    "style": ["small-prs", "tests-before-merge"],
    "reviewer": ["technical-lead"]
  }
}
```

Save this as `.elephant.json` in your project. Identifiers are case normalized; other aliases are not inferred. An optional future agent extraction step can propose labels and applicability conditions. Keep identifiers broad enough to retrieve, and use `requires`/`excludes` to prevent unsafe transfer. Each required dimension must match at least one listed value; an excluded match blocks retrieval. A missing required identifier blocks retrieval. Similar language alone is not sufficient evidence that advice is safe.
