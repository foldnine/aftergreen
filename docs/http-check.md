# HTTP outcome verification (preview)

Run an independent check *after* your deploy job reports success:

```bash
go run ./cmd/aftergreen http --url http://127.0.0.1:8080/health --status 200
go run ./cmd/aftergreen http --url https://example.org/version --status 200 --contains abc123 --json
```

Exit codes: `0` verified, `1` assertion failed, `2` invalid input, `3` observation unavailable or inconclusive.

## What is checked

- GET response status equals the configured expected status.
- An optional **literal** response-body substring appears in the response.
- Transport failure, response-reading errors, or an oversized response produce **UNKNOWN**.
- Redirects are **not followed**, so a redirect status cannot silently confirm the destination.
- Timeout defaults to 10 seconds (max 60 seconds); body inspection is limited to 1 MiB.

## Security and limitations

Use this only for **trusted endpoints specified by repository maintainers**. The CLI performs network requests and is not a sandbox or an SSRF defense for untrusted URLs. Do not accept URL/hostname values directly from untrusted PR titles, issue content, or user-controlled input. Avoid protected internal endpoints and never place credentials inside URLs. The checker prints neither response bodies nor expected body values, to reduce accidental disclosure.

HTTP 200 alone does not prove a successful deployment. Prefer a version endpoint (or an immutable deployment ID) with a matching `--contains` assertion. Body-substring checking is a starting point; JSON path/schema assertions belong to a later milestone.

This feature is not yet published as a GitHub Action. Marketplace publication requires a separate public repository and a release.
