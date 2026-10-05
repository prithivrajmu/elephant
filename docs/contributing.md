# Contributing

Open an issue with a reproducible example or a pull request describing the problem and resulting behavior. Include the operating system, Elephant version, client version where relevant, and the validation command. Remove credentials, private lessons and raw conversation data from reports.

## Development

Go 1.26+ is required; CI uses Go 1.27. Python 3.11+ runs the acceptance scripts. Node runs dashboard renderer checks. The pinned SQLite driver does not require a C compiler for release builds.

From the repository root:

```sh
go test -race ./...
go vet ./...
go build -buildvcs=false -o elephant ./cmd/elephant
./elephant selftest
python3 scripts/acceptance.py ./elephant
python3 scripts/storage_acceptance.py ./elephant
python3 scripts/automation_acceptance.py ./elephant
node scripts/dashboard_update_check.js
```

Automatic hook acceptance runs on macOS/Linux. Windows uses the MCP and storage acceptance scripts. These use isolated synthetic stores.

## Review expectations

Keep memory scope checks, source evidence, hard byte budgets and deterministic recall intact. Preserve existing client settings during integration changes. Add meaningful regression coverage for persistence, concurrency or compatibility changes. Update documentation alongside user-visible behavior. Do not claim native model behavior or performance improvements without measurements.

See [Architecture](architecture.md), [Validation](validation.md) and [Release process](releasing.md).
