# Contributing

Thanks for helping. AfterGreen is deliberately small: a narrow, trustworthy
verification engine. Please open an issue before large changes.

## Development

Requires Go 1.26+.

```bash
gofmt -l .                  # must print nothing
go vet ./...
go test -race ./...         # unit + HTTP conformance tests; no network, no database
scripts/test-postgres.sh    # real PostgreSQL integration tests (needs Docker)
```

## Rules

- **Verdict semantics are the product.** Unobservable means `UNKNOWN`, never
  `VERIFIED`. A changed verdict needs a test that shows why.
- HTTP behaviour changes start in `conformance/http/cases.json` and must be
  mirrored in the GitHub Action (see `docs/adr/0001-action-engine.md`).
- No real secrets, credentials, customer data or personal data in code, tests,
  fixtures or logs. Use `.invalid` / `example.com` hosts and synthetic values.
- Tests must not depend on external websites.
- Keep dependencies minimal and pinned; explain any new one in the PR.
- Docs describe what exists today. Mark anything planned as planned.

By contributing you agree that your contributions are licensed under Apache-2.0.
