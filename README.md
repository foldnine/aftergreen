# AfterGreen

**Green isn't proof. Verify what actually happened.**

AfterGreen is an experimental, local-first open-source outcome verification engine for automations. Built by [FoldNine](https://foldnine.dev).

> **Preview:** This milestone compares local JSON observations against declared expectations. It does **not** connect to n8n, PostgreSQL, Gmail, or any other external service yet. Never treat workflow execution status as outcome proof.

## Try it

Requires Go 1.23+.

```bash
go run ./cmd/aftergreen demo
go run ./cmd/aftergreen check
```

The demo produces an `aftergreen-demo.json` fixture. The check deliberately detects a mismatch and reports `FAILED`, so its exit status is **1**. If an outcome cannot be observed, it reports `UNKNOWN`; it never silently reports a pass.

```bash
go run ./cmd/aftergreen check --json
go test ./...
```

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Every outcome verified |
| 1 | At least one outcome failed |
| 2 | Invalid input / usage |
| 3 | No failures, but at least one outcome unknown |

### Evidence model

Each event includes a unique `id`, a reported `execution_status`, expected fields, observed fields, and an explicit `observation_available` flag. The engine sorts assertion keys for deterministic explanations. Missing observations result in `UNKNOWN`; missing or mismatched fields in available observations result in `FAILED`.

**Security:** Do not put real secrets, tokens, customer PII, or private email contents in evidence files. Demo values are synthetic.

## Roadmap

1. Deterministic local verification engine (this PR).
2. Read-only PostgreSQL adapter with event correlation and restricted permissions.
3. n8n ingestion adapter.
4. Evidence timeline UI, redaction, and reproducible proof receipts.

Apache-2.0 licensing is planned, subject to the repository's actual license file and project governance decisions.
