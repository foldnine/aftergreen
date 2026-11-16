# Security policy

## Reporting a vulnerability

Please use GitHub's **private vulnerability reporting** ("Report a
vulnerability" on the Security tab of this repository). Do not open a public
issue. We aim to acknowledge reports within 5 working days.

In scope: this repository and
[foldnine/aftergreen-action](https://github.com/foldnine/aftergreen-action).

## Design boundaries

- **Trusted targets only.** `aftergreen http` and the GitHub Action fetch
  URLs supplied by repository maintainers. They are not SSRF defences. Never
  pass URLs or hostnames from pull requests, issues or other untrusted input.
- **No secret echo.** Errors and verdict reasons never include the URL, the
  response body, the expected text, or PostgreSQL row values. Local evidence
  files (`aftergreen check`) are the exception: their expected and observed
  values are printed, so keep secrets and personal data out of them.
- **PostgreSQL** checks use a read-only transaction, validated identifiers and a
  bound event-key parameter. Use a dedicated role with `SELECT` on the checked
  table only.
- **Fail closed.** Anything that cannot be observed is `UNKNOWN`, never
  `VERIFIED`.

## Supported versions

The latest release (currently v0.1.0) and `main` are supported. Fixes land on
`main` and ship in the next release.
