# Security policy

## Supported versions

Elephant is in public pre-release. Only the latest 0.x pre-release receives
security fixes. Please reproduce on the latest release before reporting.

## Reporting a vulnerability

Report vulnerabilities privately with
[GitHub Private Vulnerability Reporting](https://github.com/prithivrajmu/elephant/security/advisories/new).

If you cannot use GitHub, email prithivrajmu@gmail.com with the subject
"Elephant security".

Do not open public issues, pull requests or discussions for suspected
vulnerabilities. Do not include real credentials, private memories or raw
conversation data in a report; use synthetic examples.

We aim to acknowledge a report within 72 hours. Please include the affected
version, operating system, impact and minimal reproduction steps.

## Scope

In scope:

- the local memory store and backups
- generated hooks and host integration settings
- the MCP stdio server
- the Memory Palace dashboard
- the cloud sync client
- the optional Cloudflare worker (`cloudflare/`)
- installers and release packaging scripts

Out of scope:

- Local tenant, user and team labels. They scope local data and are not
  authentication or an access-control boundary.
- The fact that pilot release packages are unsigned. This is a known limitation
  recorded in the release notes.
- Anyone who already has access to the local files or processes of the user
  running Elephant (see the data-handling model).

For the data-handling and trust model, see [docs/security.md](docs/security.md).
