# HTTP outcome verification

Run an independent check *after* your deploy reports success:

```bash
go run ./cmd/aftergreen http --url http://127.0.0.1:8080/health
go run ./cmd/aftergreen http --url https://app.example.com/version --status 200 --contains abc123 --json
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--url` | required | Absolute `http`/`https` URL; no credentials, no `#fragment` |
| `--status` | `200` | Expected status, 100–599 |
| `--contains` | empty | Literal, case-sensitive, byte-exact text required in the body |
| `--timeout` | `10s` | Whole request including body, `1s`–`60s` |
| `--json` | off | Print `{"status", "reasons", "observed_status"}` |

Exit codes: `0` verified, `1` failed, `2` invalid input, `3` unknown.

## Semantics

These semantics are pinned by [`conformance/http/cases.json`](../conformance/http/cases.json).

- One `GET`. **Redirects are not followed.** A redirect is observed as its 3xx
  status. If you want it, assert it with `--status 302`.
- Wrong status: **FAILED**, even if the body cannot be read.
- `--contains` set: the body is read (decompressed) up to **1 MiB**.
  - Text missing: FAILED.
  - Body over 1 MiB, truncated, unreadable, or timed out: **UNKNOWN**.
- No response (DNS, connection refused, TLS error, timeout): **UNKNOWN**.
- `HTTP_PROXY` / `HTTPS_PROXY` / `NO_PROXY` are honoured. TLS 1.2+ with
  system roots is required.
- No retries. No custom headers or authentication yet.

## Security and limitations

Use this only for **trusted endpoints specified by repository maintainers**.
It makes network requests from wherever it runs. It is not a sandbox and not
an SSRF defence, and it does not block private or internal addresses. Never take
URLs or hostnames from PR titles, branch names, issue content or other
user-controlled input.

Never put credentials in URLs; they are rejected. If a URL needs a token in its
query string, pass it from a secret. Output and errors never print the URL,
the response body or the `--contains` value.
