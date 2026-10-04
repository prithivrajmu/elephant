# Explicit external MCP evidence

The hosted service can act as an MCP client through Cloudflare's Agents SDK.
`read_evidence` is an explicit hosted MCP tool, also available as a strict
`POST /v1/evidence` HTTP request. It does not create a lesson or grant team approval.
Local hooks never invoke it.

Configure the `EVIDENCE` Durable Object binding and these Worker secrets:

```json
{"docs":{"url":"https://mcp.example.com/mcp","tool":"lookup","users":["alice"]}}
```

The above is `EVIDENCE_CONNECTORS`: at most eight endpoints, each with one
reviewed tool. The optional `EVIDENCE_CREDENTIALS` secret contains separately
authorized read-only credentials:

```json
{"alice":{"docs":"EXTERNAL_SERVICE_BEARER_TOKEN"}}
```

The coding client's Elephant token must have both `memory:read` and
`evidence:read`. Connector subjects come from server configuration. Caller
arguments cannot select a URL, tool, credential or different user. Only public
HTTPS endpoints without credential/query/fragment components are accepted.
The configured tool must also advertise `readOnlyHint: true` and must not be
marked destructive. Tool annotations are declarations; configure an actual
read-only credential and a trusted tool implementation to enforce permissions
at the external service.

Call `read_evidence` with:

```json
{"request_id":"task-42-source-1","connector":"docs","arguments":{"query":"Redis cache limits"},"byte_budget":8192}
```

The HTTP variant adds `"action":"read"`. Responses contain quoted untrusted
content, endpoint/tool provenance, retrieval time, hashes of the full result
and arguments, and a source reference such as
`mcp-evidence:task-42-source-1#sha256=...`. Inspect the evidence and its
applicability before using that reference in an ordinary `record_memory`.
The service does not validate that a written summary follows from its source.

Remote response streams are limited to 64 KiB while the SDK reads them; output
is limited to 4–16 KiB including provenance. Exact endpoint checks prevent SDK
requests from following a changed URL. Redirects are rejected and requests/tools
have ten second timeouts. Current stateless MCP and legacy Streamable HTTP are
tested. Standalone SSE, sampling, elicitation, long-running tasks and interactive
outbound OAuth callbacks are not exposed. Bring a separately authorized bearer
token or use an unauthenticated read-only endpoint.

Each tenant/user has a separate evidence object. The SDK manager registers,
connects, discovers and closes the connection for a read. Credentials stay in
the live custom fetch closure; connection rows are removed after the operation.
Durable receipts contain provenance and hashes, not raw arguments, content or
credentials. Up to 1,000 receipts are retained per user. Reusing a request ID
with the same connector/tool/arguments returns provenance only; request a new
ID for fresh content. A changed request under the same ID is a conflict.
Crashing before a receipt commits may repeat the external read on retry.

The endpoint is contacted only after explicit authorization. On a crash during
connection setup, the SDK can restore its credential-free connection catalog
on a later wake; metadata discovery can occur during this recovery. No tool
read is automatically replayed. The direct Durable Object lifecycle capability
is experimental in Agents 0.26.0, so the exact SDK/client versions are pinned.

Tests exercise the real SDK client against a current SDK server and a legacy
fixture, output budgets, durable receipt replay after object restart, changed
requests, access denial, unsafe tool declarations, redirects, large responses,
and absence of raw evidence/arguments/credentials in receipts.

The external service's access, rate limits and costs remain separate from
Cloudflare's free-tier quotas. Deployed CPU and cold-start measurements are
still required before treating the pilot as suitable for a larger workload.
See [Cloudflare setup](../cloudflare/README.md) and [sync/team review](cloud-sync.md).

SDK reference: https://developers.cloudflare.com/agents/model-context-protocol/apis/client-api/
