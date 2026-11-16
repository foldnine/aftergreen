<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="brand/logo/aftergreen-on-dark.svg">
    <img src="brand/logo/aftergreen-light.svg" alt="AfterGreen" width="340">
  </picture>
</h1>

**Green isn't proof. Verify what actually happened.**

A green CI job or a "success" from an automation means the steps ran. It does
not mean the outcome happened. AfterGreen independently observes the outcome
and returns one of three verdicts:

| Verdict | Meaning |
| --- | --- |
| `VERIFIED` | The observation succeeded and every assertion holds |
| `FAILED` | The observation succeeded and an assertion is false |
| `UNKNOWN` | The outcome could not be observed or trusted. Never treated as a pass |

Built by [FoldNine](https://foldnine.dev). Apache-2.0.

> **Status: early preview.** No releases or binaries are published yet. Run it
> from source with Go 1.26+.

## What exists today

| Capability | Where | Status |
| --- | --- | --- |
| HTTP deployment check (status + body text) | `aftergreen http` | works |
| Local JSON evidence check | `aftergreen check` | works (demo/evaluation format) |
| PostgreSQL destination check | `aftergreen postgres` | works; read-only, one validated query shape, tested against real PostgreSQL 17 |
| GitHub Action | [foldnine/aftergreen-action](https://github.com/foldnine/aftergreen-action) | preview, unreleased |

Not built: n8n or other workflow-tool integrations, other databases, a hosted
service, a UI, retries or scheduling.

## Quick start

```bash
git clone https://github.com/foldnine/aftergreen && cd aftergreen
go run ./cmd/aftergreen demo     # writes aftergreen-demo.json (synthetic data)
go run ./cmd/aftergreen check    # exit 1: one FAILED, one UNKNOWN by design
```

### Verify a deployment over HTTP

```bash
go run ./cmd/aftergreen http \
  --url https://app.example.com/version \
  --status 200 \
  --contains "$GIT_COMMIT_SHA" \
  --timeout 10s
```

HTTP 200 alone does not prove the right version is live. A stale build or a
maintenance page can return 200 too. Expose a unique build ID and assert it with
`--contains`. Redirects are not followed. Bodies are read up to 1 MiB. See
[docs/http-check.md](docs/http-check.md).

### Verify a database write

```bash
export AFTERGREEN_POSTGRES_URL='postgres://aftergreen_reader@db:5432/app?sslmode=verify-full'
go run ./cmd/aftergreen postgres --contract examples/postgres-contract.json --key "$DEPLOY_ID"
```

AfterGreen runs one read-only, parameterized `SELECT` that it builds itself.
It never runs SQL from configuration. Use a `SELECT`-only role. See
[docs/postgres.md](docs/postgres.md).

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Every outcome verified |
| 1 | At least one outcome failed |
| 2 | Invalid input or usage; nothing was verified |
| 3 | No failures, but at least one outcome unknown |

## Local evidence format (`aftergreen check`)

```json
{"events": [{
  "id": "lead-sync-182",
  "execution_status": "SUCCESS",
  "observation_available": true,
  "expected": {"status": "qualified"},
  "actual":   {"status": "qualified"}
}]}
```

Each event needs a unique `id` and at least one `expected` field. Unknown keys
are rejected. `observation_available: false` (or missing) gives `UNKNOWN`. A
missing or different `actual` field gives `FAILED`. This command **prints
expected and observed values**, so don't put secrets or personal data in
evidence files.

## Design documents

- [docs/http-check.md](docs/http-check.md): HTTP check semantics and security boundary
- [docs/postgres.md](docs/postgres.md): PostgreSQL contract, exact SQL, least-privilege role
- [docs/adr/0001-action-engine.md](docs/adr/0001-action-engine.md): why the Action is standalone Node
- [conformance/](conformance/): shared HTTP behaviour cases for the CLI and the Action

## Development

```bash
gofmt -l . && go vet ./... && go test -race ./...
```

Tests use local fixtures only. PostgreSQL integration tests:
`scripts/test-postgres.sh` (Docker). See [CONTRIBUTING.md](CONTRIBUTING.md) and
[SECURITY.md](SECURITY.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
