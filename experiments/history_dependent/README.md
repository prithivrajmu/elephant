# History-dependent evaluation follow-through

[Exact remaining manual steps](MANUAL_STEPS.md), including the supplied OpenAI adapter and private-history intake.

See [TODO.md](TODO.md) for completion and blockers. This is a separate experiment;
the historical pilot and its frozen engine/results remain unchanged.

The 36 **synthetic** coding contracts require an earlier release decision absent
from the task and source stub. Twelve development cases calibrate retrieval; 24
reserved groups are prepared for three repetitions across five conditions (360
model calls). Values are independently sampled, but all cases share a template.
They test protocol behavior, not authentic workflows or broad generalization.

The source stub requests a `contract()` function returning a literal dictionary.
The model gets the task, that stub and selected context. It never receives expected
values, gold labels, evaluator paths or a tool interface. The coordinator parses a
restricted AST and compares literal types/values: it **never runs generated code**.
Extra files, imports, decorators, parameters and nonliteral expressions are rejected.
One call, 4,000 context bytes, 4,096 patch bytes, 64 KiB response output and a
120-second transport deadline are enforced. This bounded repair format does not
replace testing a full coding agent in an OS sandbox. The host namespace sandbox
was unavailable, so no claim of process sandboxing is made.

The development calibration searches the predeclared full/short query menu and
reranker thresholds 0, -2 and -5 using raw evidence completeness and context bytes.
It never retrieves reserved cases. Combined retrieval deduplicates representations
by linked evidence, while retaining disjoint raw chunks. Corpus eligibility uses
the existing identity/time/scope checks. Saved calibration uses real local CPU
embedding/reranking models, with telemetry disabled before initialization.

## Observed results

- All 36 baseline/reference controls fail/pass as intended.
- All six retrieval configurations retrieved all 12 development decisions.
- The full query with threshold zero used 4,881 context bytes total, versus 30,033
  for the short-query configurations. It remains the selected setting.
- Seven new contract tests include 15 subprocess-adapter fixture calls. These use
  a hardcoded test response and **are not coding-model results**.
- No paid model trials ran: this environment has no provider credentials.
- Authentic raw workflow histories were unavailable; synthetic fixtures cannot
  complete that validation requirement.

## Provider transport contract

The adapter is trusted host code, not model-generated code. It reads one JSON
request from stdin and returns one JSON object on stdout:

```json
{
  "model_id": "EXACT_PROVIDER_SNAPSHOT",
  "model_settings": {"temperature": 0},
  "files": {"policy.py": "def contract():\n    return {…}\n"},
  "usage": {"input_tokens": 100, "output_tokens": 30},
  "billed_cost_usd": 0.001
}
```

Use a fresh provider conversation for each request. The response must identify the
configured snapshot and echo the decoding settings. Token counts must come from provider usage data. Confirmed cost may remain null
when a finite estimated_cost_usd and explicit cost_basis are provided; unknown
values are never reported as zero. These remain trusted adapter reports, not an independent billing audit.
The included stdlib OpenAI adapter requires a local API key and explicit rate card;
credentials are never embedded in this repository.

The adapter lock declares all runtime dependencies and explicit model settings:

```json
{
  "runtime_files": ["/absolute/path/adapter.py", "/absolute/path/provider_sdk_file.py"],
  "model_settings": {"temperature": 0}
}
```

Include every imported adapter/provider module and configuration file in the lock.
The runner hashes declared files, the executable and file-valued arguments and
rejects changes. It does not automatically discover a complete dependency closure
or freeze remote provider internals. Environment inheritance is restricted to
PATH, locale, Windows system root and provider credentials; endpoint and provider
configuration must be explicit locked file/argv settings. Credentials stay in the
process environment and are never written to manifests. The adapter remains a
trusted component with host filesystem/network access; only candidate output is
restricted to data. Use a vetted transport and isolated host for real trials.

## Reproduction

Use Python 3.11+ and the pinned dependencies in
`../memory_tax/requirements-models.txt`. Build the unchanged Go recall helper:

```sh
go build -o /tmp/history-recall ./cmd/memory-tax-baseline
python3 -m unittest discover -s experiments/history_dependent -v
python3 experiments/history_dependent/runner.py controls --state /tmp/history-next
python3 experiments/history_dependent/calibrate.py \
  --state /tmp/history-next --model-cache /path/to/models
python3 experiments/history_dependent/runner.py prepare \
  --state /tmp/history-next --helper /tmp/history-recall \
  --model-cache /path/to/models --model-id EXACT_PROVIDER_SNAPSHOT \
  --adapter-command '["/path/to/python", "/path/to/adapter.py"]' \
  --adapter-lock /path/to/adapter-lock.json --repeats 3
python3 experiments/history_dependent/runner.py run --state /tmp/history-next
```

Controls must exactly reproduce and be valid. Calibration must identify precisely
the development cases and a recorded candidate. Their exact contents are embedded
in the run manifest. Source/data/model/helper/runtime hashes, settings, cell order
and budgets are frozen. The CLI also checks `manifest.sha256` before running.
These digests detect drift under a trusted coordinator; they are not signatures
against a malicious host operator. New runs require new output directories.

Each successful transport call produces a scored result, including its usage/cost.
Wrong candidate code is a scored failure. A transport/schema error aborts the run;
previous results remain on disk and status stays incomplete. Do not interpret an
incomplete batch as a completed experiment, silently overwrite it, or retry only
behavioral failures. No inference about system ranking should be drawn until the
full predeclared run and independent audit are complete.
