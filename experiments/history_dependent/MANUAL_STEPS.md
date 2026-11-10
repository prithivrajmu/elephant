# Two manual inputs remain

The provider adapter and setup helper are implemented. The previous PR commit's
full GitHub validation suite passed. These new changes have local transport tests;
no paid provider call or authentic-data experiment has been made.

## 1. Configure the model run locally

Use a machine where the repository, Go helper and pinned model cache are available.
You need an API project with available credit, an exact accessible model snapshot
ID, its current standard token prices, and an API key. Do not paste the key into
chat or commit it. This runner currently supplies an OpenAI Responses adapter;
it is not using the ChatGPT app's signed-in session.

From the repository root:

```sh
mkdir -p .private-evals
# In Bash, enter the key without echoing it or placing it in shell history:
read -rsp 'OpenAI API key: ' OPENAI_API_KEY
export OPENAI_API_KEY
printf '\n'
```

Create `.private-evals/pricing.json`. Replace each null with the price in USD per
one million tokens for the chosen snapshot. Do not use zero for an unknown price:

```json
{
  "input_usd_per_million": null,
  "cached_input_usd_per_million": null,
  "output_usd_per_million": null
}
```

Create and activate a local Python environment, then download the pinned retrieval
models with telemetry disabled before importing the runtime:

```sh
python3 -m venv .private-evals/venv
source .private-evals/venv/bin/activate
python3 -m pip install -r experiments/memory_tax/requirements-models.txt
export ORT_DISABLE_TELEMETRY=1 HF_HUB_DISABLE_TELEMETRY=1
python3 - <<'PYMODELS'
from fastembed import TextEmbedding
from fastembed.rerank.cross_encoder import TextCrossEncoder
cache = '.private-evals/models'
TextEmbedding('BAAI/bge-small-en-v1.5', cache_dir=cache, threads=2)
TextCrossEncoder('Xenova/ms-marco-MiniLM-L-6-v2', cache_dir=cache, threads=2)
PYMODELS
```

Keep that environment activated for all subsequent commands. Use the Go version
required by the repository's `go.mod`, then build the helper and prepare:

```sh
go build -o .private-evals/recall ./cmd/memory-tax-baseline
python3 experiments/history_dependent/setup_provider.py \
  --state .private-evals/provider-run-01 \
  --model-id EXACT_PROVIDER_SNAPSHOT \
  --pricing .private-evals/pricing.json \
  --helper .private-evals/recall \
  --model-cache .private-evals/models \
  --max-estimated-usd 5
```

Setup performs local controls/calibration and freezes the run. It makes no paid API
calls. The example $5 estimate limit is user-adjustable. It stops before subsequent
calls once accounted cost reaches the limit; **one in-flight call can exceed it**.
It is not a provider-enforced hard billing cap. Estimates depend on the supplied
rate card and measured usage. Confirmed billing remains null for this adapter.

Start the paid run explicitly, with the same environment:

```sh
python3 experiments/history_dependent/runner.py run \
  --state .private-evals/provider-run-01
```

The batch has 360 scheduled calls. Each request has no tools or conversation
continuation and sets `store=false`. The fixed destination is the official
`https://api.openai.com/v1/responses`; configured proxies and redirects are disabled.
A model mismatch, quota or transport error stops the batch rather than silently
switching models or retrying. Completed malformed/truncated answers are scored as
failures with usage retained. The task stub and retrieved context go to the API.
Do not supply private real histories to this synthetic run.

The adapter follows the [official Responses API reference](https://developers.openai.com/api/reference/python/resources/responses/methods/create).
If a selected model rejects a decoding option, choose supported settings before
freezing a new run. `--model-settings` accepts JSON; the default only specifies
`max_output_tokens: 2048`.

## 2. Supply a small, redacted authentic history sample

Start with a few completed **Elephant coding tasks**: history written before the
task, the later issue/prompt, the Git base and accepted fix, and regression tests.
Include successful and failed incidents. Keep related attempts in one group;
reserve whole groups before calibrating anything. Do not include credentials,
customer/patient data, unrelated conversations or hidden reasoning.

Save the intake under `.private-evals/history.json`, which Git ignores. The minimal
shape below is a template, not an authentic dataset; replace every placeholder:

```json
{
  "classification": "authentic_private",
  "redacted": true,
  "approved_for_external_model": false,
  "tasks": [{
    "id": "task-001",
    "group": "incident-001",
    "split": "dev",
    "base_revision": "FULL_40_CHARACTER_BASE_COMMIT",
    "fix_revision": "FULL_40_CHARACTER_FIX_COMMIT",
    "allowed_changes": ["relative/production.go"],
    "test_overlay": ["relative/production_test.go"],
    "prompt": "The later task as originally requested",
    "cutoff": "2026-10-01T10:00:00Z",
    "history": [{
      "id": "turn-001",
      "at": "2026-09-30T10:00:00Z",
      "role": "user",
      "text": "Relevant earlier decision or observed incident, manually redacted"
    }]
  }]
}
```

Check it without uploading:

```sh
python3 experiments/history_dependent/history_intake.py .private-evals/history.json
```

The validator checks schema, timestamps, safe paths and split boundaries. It does
not prove authenticity, redaction, Git ancestry, history dependence or test quality.
It never sends text to a provider. Leave `approved_for_external_model` false unless
you want the selected redacted context used in a future external model evaluation.
This flag is a recorded preference, not automatic permission to upload.

Once a sample is available, it still needs task-specific executable controls and
review before becoming a scored authentic benchmark. Existing historical source
snapshots and synthetic contracts cannot replace that step. Private data and local
provider runs must stay outside the public PR; share only reviewed aggregate results.
