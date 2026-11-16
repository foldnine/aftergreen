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

> **Status: v0.1.0 pre-release.** Prebuilt binaries for Linux, macOS and
> Windows (amd64, arm64) with SHA256 checksums and build provenance. You don't
> need Go.

## What exists today

| Capability | Where | Status |
| --- | --- | --- |
| HTTP deployment check (status + body text) | `aftergreen http` | works |
| Local JSON evidence check | `aftergreen check` | works (demo/evaluation format) |
| PostgreSQL destination check | `aftergreen postgres` | works; read-only, one validated query shape, tested against real PostgreSQL 17 |
| GitHub Action | [foldnine/aftergreen-action](https://github.com/foldnine/aftergreen-action) | v1.0.1; HTTP checks only |

Not built: n8n or other workflow-tool integrations, other databases, a hosted
service, a UI, retries or scheduling.

## Install

macOS and Linux:

```bash
curl -fsSL https://aftergreen.foldnine.dev/install.sh | sh
```

The [script](scripts/install.sh) detects your OS and CPU, downloads the
release from GitHub, verifies its SHA256 against the release's `SHA256SUMS`
and installs to `~/.local/bin`. It stops without installing anything if the
checksum doesn't match, and it never uses `sudo`. Use `--bin-dir DIR` or
`--version vX.Y.Z` to change the defaults.

Windows (PowerShell, download and verify), direct downloads for every
platform, `go install` and checksum/provenance verification:
**[docs/install.md](docs/install.md)**.

In GitHub Actions, use [foldnine/aftergreen-action](https://github.com/foldnine/aftergreen-action)
(`uses: foldnine/aftergreen-action@v1.0.1`); there is nothing to install.

## Quick start

Your first verification, right after installing:

```bash
aftergreen http --url https://example.com --status 200 --contains "Example Domain"
# VERIFIED  HTTP outcome (HTTP 200)          exit 0

aftergreen http --url https://example.com --status 200 --contains "a82f19c"
# FAILED  HTTP outcome (HTTP 200)            exit 1
#   - response body did not contain the expected text
```

The page answers 200 both times. Only the second check notices that the text
you expected isn't there. For your own deploys, point `--url` at an endpoint
that serves the build ID, and pass that ID to `--contains`.

To see all three verdicts on synthetic data:

```bash
aftergreen demo     # writes aftergreen-demo.json
aftergreen check    # exit 1: one FAILED, one UNKNOWN by design
```

### Verify a deployment over HTTP

```bash
aftergreen http \
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
aftergreen postgres --contract examples/postgres-contract.json --key "$DEPLOY_ID"
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

- [docs/install.md](docs/install.md): every install method and how to verify a download
- [docs/http-check.md](docs/http-check.md): HTTP check semantics and security boundary
- [docs/postgres.md](docs/postgres.md): PostgreSQL contract, exact SQL, least-privilege role
- [docs/adr/0001-action-engine.md](docs/adr/0001-action-engine.md): why the Action is standalone Node
- [conformance/](conformance/): shared HTTP behaviour cases for the CLI and the Action

## Development

```bash
gofmt -l . && go vet ./... && go test -race ./...
go run ./cmd/aftergreen version    # run from source
scripts/test-install.sh            # installer tests (offline, needs python3)
```

Tests use local fixtures only. PostgreSQL integration tests:
`scripts/test-postgres.sh` (Docker). See [CONTRIBUTING.md](CONTRIBUTING.md) and
[SECURITY.md](SECURITY.md).

## License

Apache-2.0. See [LICENSE](LICENSE).
