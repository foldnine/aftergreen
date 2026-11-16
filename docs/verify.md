# Verification contracts (`aftergreen verify`)

> **Unreleased.** `aftergreen verify` is on `main` and planned for v0.2.0.
> The published v0.1.0 binaries do not have it.

A verification contract is a YAML file that lists the outcomes a deployment
must produce. `aftergreen verify` observes each one independently and returns
one overall verdict.

```bash
aftergreen verify -f aftergreen.yaml
```

`-f` / `--file` defaults to `aftergreen.yaml` in the current directory.

## Example

```yaml
version: 1

deployment:
  name: payment-api
  revision: "${DEPLOY_SHA}"

checks:
  - id: correct-version
    type: http
    url: https://api.example.com/version
    expect:
      status: 200
      contains: "${DEPLOY_SHA}"

  - id: release-recorded
    type: postgres
    contract: checks/deploy.json
    key: "${DEPLOY_ID}"
```

A runnable local demo is in [examples/verification/](../examples/verification/).

## Format (version 1)

### Top level

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | Must be `1` |
| `deployment.name` | yes | Shown in the report. Literal text, ≤ 100 bytes |
| `deployment.revision` | no | Shown in the report. Supports `${VAR}`, ≤ 200 bytes. Must not be secret |
| `checks` | yes | 1 to 64 checks, run in file order |

### Common check fields

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | yes | Unique. 1–64 letters, digits, `.`, `_`, `-`; starts with a letter or digit |
| `type` | yes | `http` or `postgres` |
| `timeout` | no | Per observation, `1s`–`60s`, default `10s`. Must include a unit |

### `type: http`

Uses the same engine as `aftergreen http` ([semantics](http-check.md)).

| Field | Required | Meaning |
| --- | --- | --- |
| `url` | yes | Absolute `http`/`https` URL. Supports `${VAR}` |
| `expect.status` | no | Expected status, 100–599, default `200`. Plain decimal |
| `expect.contains` | no | Literal text the body must contain. Supports `${VAR}` |

### `type: postgres`

Uses the same engine as `aftergreen postgres` ([semantics](postgres.md)).

| Field | Required | Meaning |
| --- | --- | --- |
| `contract` | yes | Path to a PostgreSQL contract JSON file, relative to this YAML file |
| `key` | yes | The row key for this deployment. Supports `${VAR}` |

The connection string is read from `AFTERGREEN_POSTGRES_URL`, only if the
file has a postgres check. The contract file's own `key` is **ignored**. The
key must come from the YAML file, so a fixed key can never verify a row left
by an earlier deployment.

## Validation

The whole file is validated before anything is observed. Any problem exits
with code `2` and no verdict:

- Unknown fields, at any level. A typo can't silently drop an assertion.
- Duplicate check IDs, unknown types, fields that don't belong to the
  check's type, unsupported versions.
- YAML anchors, aliases, explicit tags, duplicate keys and multiple
  documents. Files over 64 KiB.
- Invalid URLs (credentials, fragments, other schemes), statuses, timeouts,
  or PostgreSQL contracts (including identifier validation).

## Environment variables

`${NAME}` is substituted **only** in `deployment.revision`, `url`,
`expect.contains` and `key`. Anywhere else it is literal text that then fails
validation where it matters.

- `NAME` uses letters, digits and underscores. There are no defaults, no
  `${NAME:-x}` and no command substitution. Nothing is passed to a shell.
- An unset **or empty** variable is an error (exit 2). An unset `DEPLOY_SHA`
  must never turn into a check that passes against an old deployment.
- `$$` is a literal `$`. Any other `$` is an error.
- Substitution happens once, after YAML parsing. Substituted values are not
  expanded again and never appear in error messages.

## Files

Relative `contract` paths resolve against the YAML file's directory, not the
working directory. A contract must be inside that directory: absolute paths,
`..` that leaves it, and symlinks that point out of it are rejected.

## Verdicts and exit codes

Each check gets its own verdict from its engine. The overall verdict:

1. Any check `FAILED`: **FAILED** (exit `1`)
2. Otherwise any check `UNKNOWN`: **UNKNOWN** (exit `3`)
3. All checks `VERIFIED`: **VERIFIED** (exit `0`)

Invalid configuration: no verdict, exit `2`.

Checks run one at a time in file order. A failing check does not stop later
checks: every outcome is reported. On Ctrl-C or SIGTERM, the running check
and every later check are reported `UNKNOWN`.

## Output

```text
AFTERGREEN
─────────────────────────────────────

Deployment: payment-api
Revision:   a82f19c

VERIFIED  correct-version
FAILED    release-recorded
            - destination record missing

─────────────────────────────────────

Deployment verification FAILED

1 verified
1 failed
0 unknown

Duration: 2.4s
```

Verdicts are always spelled out, never shown by colour alone. Reasons follow
the engines' rules: no URLs, response bodies, expected text, keys, row values
or connection strings. The deployment name and revision **are** printed.

## Compatibility policy

- `version: 1` files keep their meaning in every later 0.x and 1.x release.
  New optional fields may be added. Existing fields don't change meaning.
- A change that would alter a version-1 file's verdict needs a new `version`.
  Older binaries reject newer versions with exit 2. They never guess.
- Unknown fields stay errors. A file that uses a newer optional field fails
  on an older binary instead of being checked less strictly.
