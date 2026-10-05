# Contributing to Elephant

Thanks for helping. This page is the short version; see
[docs/contributing.md](docs/contributing.md) for the full acceptance scripts and
review expectations.

## Development setup

Install the Go version named in `go.mod`. From the repository root:

```sh
gofmt -l .          # must print nothing
go vet ./...
go test -race ./...
```

## Pull request checklist

- [ ] `gofmt -l .`, `go vet ./...` and `go test -race ./...` pass.
- [ ] New behavior or bug fixes have regression tests.
- [ ] User-visible changes update the relevant documentation and `CHANGELOG.md`.
- [ ] Memory scope checks, source evidence and byte budgets are preserved.
- [ ] No secrets, personal paths or private memories are included.

## Redaction

Never put secrets, tokens, personal or employer-specific paths, raw transcripts
or real memories in issues, pull requests, test fixtures or recorded memories.
Use synthetic data and sanitize `elephant doctor` output before sharing it.

## Security issues

Do not report vulnerabilities publicly. Follow [SECURITY.md](SECURITY.md).

## License

Elephant is MIT licensed. By submitting a contribution you agree that it is
licensed under the same [MIT License](LICENSE) (inbound = outbound).

Participation is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).
