# Security and privacy

## Reporting a vulnerability

Report suspected vulnerabilities privately through GitHub Private Vulnerability Reporting. See [SECURITY.md](../SECURITY.md) for supported versions, scope and response targets. Do not open public issues for vulnerabilities.

Elephant is a local tool. Anyone with access to its files or processes is trusted. Tenant/user/team labels provide local scoping; they are not authentication or an enterprise access-control boundary.

Recalled memories are untrusted evidence. They cannot authorize actions or override current project policies. Check their source and applicability before using them. Team memories remain drafts until human approval through the local CLI.

Hooks retain tool names, status, structured exit codes and hashed session identifiers. They do not read transcripts or persist raw prompts, tool arguments, tool output or raw errors. Optional release checks contact GitHub without task or memory content. Credentials supplied for private release checks are not saved in the store.

The dashboard binds to loopback and uses session-token, Host/Origin and content-security controls. Stores and backups are not encrypted by Elephant. Retirement excludes a memory from future recall but retains historical data; regulated physical erasure is not implemented.

Do not include secrets or exploitable private data in public issues. See [Storage](storage.md) for backup/recovery and [Updates](updates.md) for release-check behavior.

## Optional hosted service

The `cloudflare/` pilot derives ownership from verified access tokens and rejects team scope. Uploads require an explicit scope-selected sync command. Reviewed team sharing uses separate membership and review authority; see [cloud sync](cloud-sync.md). Configure a dedicated issuer/audience, pinned public verification keys and an explicit subject allowlist before exposing tools. See [Cloudflare deployment and limits](../cloudflare/README.md). Retired records and idempotency receipts remain stored; retirement is not physical erasure.
