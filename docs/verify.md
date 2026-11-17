# Verification contracts (`aftergreen verify`)

> **Unreleased.** `aftergreen verify` is on `main` and planned for v0.2.0.
> The published v0.1.0 binaries do not have it.

A verification contract is a YAML file that lists the outcomes a deployment
must produce. `aftergreen verify` observes each one independently and returns
one overall verdict.

```bash
aftergreen verify -f aftergreen.yaml
```

| Flag | Meaning |
| --- | --- |
| `-f`, `--file` | Contract file, default `aftergreen.yaml` |
| `--json` | Print the [JSON report](#json-report) on stdout instead of the text report. No progress lines |
| `--report FILE` | Also write the JSON report to `FILE` (replaced atomically) |

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
| `retry` | no | Repeat the observation until stable or a deadline (see [Retries](#retries)) |

### `type: http`

Uses the same engine as `aftergreen http` ([semantics](http-check.md)).

| Field | Required | Meaning |
| --- | --- | --- |
| `url` | yes | Absolute `http`/`https` URL. Supports `${VAR}` |
| `expect.status` | no | Expected status, 100–599, default `200`. Plain decimal |
| `expect.contains` | no | Literal text the body must contain. Supports `${VAR}` |
| `expect.json` | no | List of JSON assertions on the body (see [JSON assertions](#json-assertions)) |

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

## JSON assertions

```yaml
expect:
  status: 200
  json:
    - path: $.revision
      equals: "${DEPLOY_SHA}"
    - path: $.instances[0].ready
      equals: true
```

Each assertion has a `path` and an `equals`. Every assertion must hold. Up to
16 per check.

**Path syntax.** This is the whole language:

| Syntax | Selects |
| --- | --- |
| `$` | the whole document |
| `.name` | an object member. `name` uses letters, digits, `_`, `-` and doesn't start with a digit or `-` |
| `[n]` | an array element, from `0` |

There are no wildcards, filters, recursive descent, quoted keys or functions.
Keys that need other characters can't be addressed yet.

**Comparison** is exact and typed, using the YAML type of `equals`:

| `equals` | Matches |
| --- | --- |
| `"a82f19c"`, `"3"` | a JSON string with exactly that value. `${VAR}` is substituted in strings only |
| `3`, `0.5`, `1e3` | a JSON number with the same value (`3`, `3.0` and `3e0` are equal) |
| `true` / `false` | a JSON boolean |
| `null` | a JSON `null` |

`"3"` never matches `3`. Write strings in quotes when they could look like a
number or boolean.

**Verdicts:**

| Situation | Verdict |
| --- | --- |
| Status matches, every assertion holds | `VERIFIED` |
| Path not found, different type, different value | `FAILED` (`json $.revision: value differs`) |
| Body is not valid JSON: syntax error, invalid UTF-8, trailing data, empty | `FAILED` |
| Wrong status | `FAILED`. The body is not parsed |
| Duplicate object keys (the value is ambiguous) | `UNKNOWN` |
| Number too large to compare exactly (e.g. `1e99999999999`) | `UNKNOWN` |
| Nested more than 64 levels, body over 1 MiB, unreadable, timed out | `UNKNOWN` |

Reasons name the path and JSON types only, never values.

## Retries

Right after a deploy, the old build often keeps answering for a while.
`retry` observes a check repeatedly instead of once:

```yaml
retry:
  interval: 5s       # pause between observations, 1s–5m, default 5s
  deadline: 2m       # required: hard limit for the whole check, up to 30m
  consecutive: 2     # VERIFIED observations in a row required, 1–10, default 1
```

```text
00:00  HTTP 200, wrong revision      FAILED    (expected during a rollout; retry)
00:05  HTTP 200, wrong revision      FAILED
00:10  HTTP 200, expected revision   VERIFIED  1 of 2
00:15  HTTP 200, expected revision   VERIFIED  2 of 2  →  VERIFIED
```

The check ends as soon as it has `consecutive` VERIFIED observations in a row.
Otherwise it ends at `deadline`, and the **last completed observation**
decides:

| Last completed observation | Verdict |
| --- | --- |
| `FAILED` | `FAILED`: still wrong at the deadline |
| `UNKNOWN` | `UNKNOWN`: still not observable |
| `VERIFIED`, but fewer than `consecutive` in a row | `UNKNOWN`: not observed to be stable |

Details:

- A FAILED observation inside the window isn't final. It is what a rollout in
  progress looks like. A non-VERIFIED observation resets the streak.
- `deadline` is a hard limit that includes the observations themselves. An
  observation cut short by it doesn't count and doesn't replace the previous
  one. No observation starts unless a full `interval` fits before the deadline.
- `deadline` must be at least the check's `timeout`, and long enough for
  `consecutive` observations `interval` apart.
- Cancelled (Ctrl-C, SIGTERM) before the check settles: `UNKNOWN`. A
  mismatch only becomes final at the deadline.
- Configuration errors are never retried. They exit 2 before anything runs.
- Retries are opt-in per check and work for `http` and `postgres`. A check's
  worst-case duration is its `deadline`, or its `timeout` without `retry`.
  Checks run one after another, so a run's worst case is the sum.
- Progress lines (`aftergreen: <id>: attempt 2 FAILED (...), retrying in 5s`)
  go to stderr. The report on stdout shows the attempt count.
- AfterGreen only reads. Retrying never changes anything.

## Validation

The whole file is validated before anything is observed. Any problem exits
with code `2` and no verdict:

- Unknown fields, at any level. A typo can't silently drop an assertion.
- Duplicate check IDs, unknown types, fields that don't belong to the
  check's type, unsupported versions.
- Fields with no value (`contains:`, `contains: null`, `status: ~`), an
  empty `contains` and an empty `expect`. Each would otherwise silently
  remove an assertion.
- YAML anchors, aliases, merge keys (`<<`), explicit tags, duplicate keys and
  multiple documents. Non-integer versions (`1.0`). Files over 64 KiB.
- Invalid URLs (credentials, fragments, other schemes), statuses, timeouts,
  or PostgreSQL contracts (including identifier validation).

## Environment variables

`${NAME}` is substituted **only** in `deployment.revision`, `url`,
`expect.contains`, string `expect.json[].equals` values and `key`. Anywhere else it is literal text that then fails
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
At least one expected outcome was observed to be wrong.

1 verified
1 failed
0 unknown

Duration: 2.4s
```

Verdicts are always spelled out, never shown by colour alone. Reasons follow
the engines' rules: no URLs, response bodies, expected text, keys, row values
or connection strings. The deployment name and revision **are** printed.

## JSON report

`--json` prints the report on stdout. `--report FILE` writes the same bytes
to a file, for example as a CI artifact. Both can be combined with each other
and with the text output.

```json
{
  "version": 1,
  "aftergreen_version": "v0.2.0",
  "deployment": { "name": "payment-api", "revision": "a82f19c" },
  "verdict": "FAILED",
  "summary": { "verified": 1, "failed": 1, "unknown": 0 },
  "started_at": "2026-11-17T10:00:00.000Z",
  "completed_at": "2026-11-17T10:00:02.412Z",
  "duration_ms": 2412,
  "checks": [
    {
      "id": "correct-version",
      "type": "http",
      "verdict": "VERIFIED",
      "reasons": [],
      "attempts": 3,
      "duration_ms": 2398,
      "observed_status": 200
    },
    {
      "id": "release-recorded",
      "type": "postgres",
      "verdict": "FAILED",
      "reasons": ["destination record missing"],
      "attempts": 1,
      "duration_ms": 14
    }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `version` | Report format version, `1` |
| `deployment.revision` | Omitted when the contract has none |
| `verdict` | `VERIFIED`, `FAILED` or `UNKNOWN`. Matches the exit code |
| `started_at`, `completed_at` | UTC, RFC 3339 with milliseconds |
| `checks` | In contract order. `reasons` is always an array, empty when verified |
| `checks[].observed_status` | HTTP only, omitted when no response was received |

Rules:

- Stdout holds exactly one JSON document and nothing else: no colours, no
  progress, no banners. Errors go to stderr.
- Invalid configuration: nothing on stdout, no report file, exit 2.
- The report contains what the text report shows and nothing more: no URLs,
  `contains`/`equals` values, PostgreSQL keys, connection strings, response
  bodies or row values. The deployment name and revision are included.
- `--report` creates its temporary file before the first observation, so an
  unwritable path or a directory fails with exit 2 immediately. If the final write fails
  anyway, AfterGreen prints the verdict on stderr and exits 2.
- Format compatibility: fields may be added within version 1. Existing
  fields keep their name, type and meaning. Anything else needs a new
  `version`.

CI examples: [GitHub Actions](../examples/ci/github-actions.yml),
[GitLab CI](../examples/ci/gitlab-ci.yml).

## Compatibility policy

- `version: 1` files keep their meaning in every later 0.x and 1.x release.
  New optional fields may be added. Existing fields don't change meaning.
- A change that would alter a version-1 file's verdict needs a new `version`.
  Older binaries reject newer versions with exit 2. They never guess.
- Unknown fields stay errors. A file that uses a newer optional field fails
  on an older binary instead of being checked less strictly.
