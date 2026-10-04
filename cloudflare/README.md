# Elephant Cloudflare pilot

An optional authenticated remote MCP service. The existing Go CLI, local SQLite WAL store and automatic hooks work independently of this package.

This package implements private personal, project and conversation memories. It serves six memory MCP tools plus the separately authorized `read_evidence` tool over stateless Streamable HTTP using `agents/mcp/server`, with SQLite-backed Durable Objects keyed by verified tenant and user identity. Memory state outlives MCP connections.

## Local development

Requires Node.js 22 or later.

```sh
cd cloudflare
npm ci
npm run dev:auth
npm run dev
```

`dev:auth` creates ignored local files with restrictive permissions. It refuses to replace an existing `.dev.vars`. The token expires after one hour. To generate another token, remove those two development files and run the command again. The development issuer is deliberately invalid for production OAuth discovery.

The MCP endpoint is `http://127.0.0.1:8787/mcp`. Configure an MCP client that supports an explicit `Authorization: Bearer TOKEN` header, using the token from `.dev-token`. Use `127.0.0.1`, matching the configured origin. Client-supplied project IDs refer to that user's private namespace.

Record a lesson through the versioned operation API:

```sh
curl http://127.0.0.1:8787/v1/operations \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $(cat .dev-token)" \
  --data '{"version":1,"tool":"record_memory","arguments":{"operation_id":"task-1-lesson-1","scope":"project","project_id":"demo","incident":"Redis cache failed.","lesson":"Use Redis cache limits.","source":"test:redis","requires":{"database":["redis"]}}}'
```

Recall after reconnecting or restarting the local Worker:

```sh
curl http://127.0.0.1:8787/v1/operations \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $(cat .dev-token)" \
  --data '{"version":1,"tool":"recall_memory","arguments":{"task":"redis","project_id":"demo","context_features":{"database":["redis"]}}}'
```

## Checks

```sh
npm run check
npm test
npm run build
```

Tests run in the local Workers runtime with real Durable Object SQLite storage. They cover access-token validation, user/context isolation, idempotency, transaction rollback, restart persistence, feedback deduplication, retirement and MCP interoperability. Shared Go/TypeScript fixtures check scope, applicability, abstention and UTF-8 budgets. A dry-run build checks packaging; it does not establish production latency or free-tier CPU compliance.

## Deployment

Use a Cloudflare Workers Free account and its `workers.dev` subdomain. No domain purchase, model API, Vectorize, D1, KV or R2 is required by this package. No account upgrade is configured.

Configure these bindings before enabling clients:

| Binding | Value |
| --- | --- |
| `PUBLIC_ORIGIN` | Exact HTTPS origin of the Worker, without a trailing slash |
| `AUTH_ISSUER` | Exact HTTPS issuer of your OAuth access tokens |
| `AUTH_AUDIENCE` | Dedicated resource audience configured at that issuer |
| `AUTH_JWKS` | JSON public JWK set containing trusted RSA verification keys |
| `AUTH_SUBJECTS` | JSON array of allowed issuer subject IDs, at most 100 |
| `TENANT_ID` | Stable deployment tenant ID |

Set public values using Wrangler variables or the dashboard. Set `AUTH_JWKS` and `AUTH_SUBJECTS` using `npx wrangler secret put NAME`. Public keys are not credentials; using secret bindings keeps the configuration out of the repository. Never upload private signing keys. Rotation requires updating the pinned public key set; retain overlapping public keys until old tokens expire.

This Worker is an OAuth protected resource, not an authorization server. Your existing issuer must provide client login/authorization and issue RS256 access tokens with `sub`, `iat`, `exp`, the configured audience, and space-separated `memory:read`/`memory:write` scopes. Tokens must be at most one hour old. The resource metadata endpoint is `/.well-known/oauth-protected-resource/mcp`. Browser login, refresh, issuer registration and any issuer charges are external to this package. Validate your chosen issuer/client combination before inviting users.

```sh
npx wrangler login
npm run build
npm run deploy
```

The deploy command is explicit. Missing auth configuration fails closed. Keep the initial subject allowlist small, validate the deployed endpoint with an MCP client, and inspect Cloudflare CPU, request, row-read and row-write metrics before expanding access. `GET /health` checks routing only; it does not assert auth or storage readiness.

## Guarantees and limits

- `operation_id` is required for record, feedback and forget. Reuse it with identical arguments on retry. A changed payload under the same ID is rejected. A committed receipt returns the original response after reconnect or eviction.
- Feedback also deduplicates by memory and `feedback_id`, preventing one observed task from creating repeated usefulness votes. Conflicting outcomes under the same feedback ID are rejected.
- Identity comes from verified access tokens. Tool arguments cannot choose a tenant or owner. Each verified user has a stable object, including their private project memories. Team scope is rejected.
- Recall uses an inverted exact-term index, hard scope/applicability checks and a bounded BM25/diversity calculation. It returns at most 128 candidate memories and reports `candidates_truncated` in structured results. Ranking statistics are computed over bounded hosted candidates, so numerical scores need not equal local full-corpus scores.
- Recall accepts at most 32 distinct content terms; a memory accepts at most 512 distinct indexed terms. Request bodies are capped at 64 KiB. Text and labels have UTF-8 limits. The pilot caps each user's object at 1,000 memories and 10,000 mutation receipts, counting retired memories. Hitting a cap rejects further operations; it never silently discards history.
- Retiring a memory hides it from recall. Historical data, tokens and receipts remain. This is not a physical deletion API.
- Hosted profiles are explicit client assertions. The Worker cannot inspect local manifests, observe local agent tools or access evidence paths on a laptop.
- Hosted writing checks do not implement the local configurable STE guide. A nonempty source is required; its presence does not prove that the source or lesson is correct.
- Local hooks, local-to-cloud sync, shared team approval, external MCP connectors and a hosted dashboard are follow-up work. Installing this package does not change `elephant init` or upload existing memories.

See [Cloudflare architecture and budget](../docs/cloudflare.md).

Opt-in [local sync and reviewed team sharing](../docs/cloud-sync.md) are available through the CLI and versioned HTTP APIs. Team scope remains excluded from MCP record/recall. Configure `TEAM_MEMBERS` and reviewer scopes separately.

The explicit `read_evidence` MCP tool uses the Agents SDK client for configured read-only sources. See [external MCP evidence](../docs/mcp-evidence.md) for credentials, budgets and provenance retention.
