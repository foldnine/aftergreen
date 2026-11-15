# Conformance cases

`http/cases.json` defines the HTTP verification behaviour of AfterGreen in a
language-neutral way. Both engines must pass every case:

- Go: `go test ./internal/httpcheck -run TestConformance`
- Action: `node --test test/conformance.test.mjs` in
  [foldnine/aftergreen-action](https://github.com/foldnine/aftergreen-action),
  which vendors this file at a pinned commit.

Change behaviour here first, then update both engines.

## Format (schema_version 1)

Each case has:

- `name`: unique, kebab-case.
- `server` (optional): what a local fixture server on `127.0.0.1` returns.
  - `status`: HTTP status code.
  - `body` (UTF-8 text), or `body_bytes` (total length filled with `X`) plus an optional `body_suffix`.
  - `location`: `Location` header (redirect target, a path on the same server).
  - `gzip`: gzip-encode the body if the client accepts gzip.
  - `truncate`: promise 100 bytes more than sent in `Content-Length`, then close the connection.
  - `delay_ms`: wait before sending headers.
  - `body_delay_ms`: send headers, then wait before sending the body.
- `url` (optional): defaults to `{base}/check`. `{base}` is the fixture server,
  `{unreachable}` an address on `127.0.0.1` with nothing listening. Other
  values are used literally; they never reach the network.
- `contract`: `expected_status` (int), `contains` (string, optional),
  `timeout_seconds` (int).
- `want`:
  - `verdict`: `VERIFIED`, `FAILED`, `UNKNOWN`, or `INVALID` (the contract
    must be rejected before any request).
  - `observed_status`: int, or `null` for no response; omitted means not checked.
  - `redirect_target_requested`: `false` asserts that only `/check` was requested.

Every non-`VERIFIED` result must carry at least one reason. Reason wording is
not part of the contract.
