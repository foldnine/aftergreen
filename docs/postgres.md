# PostgreSQL outcome verification

Checks that the row an automation claims to have written actually exists and
has the expected values. Typical uses: a sync wrote a lead, or a deploy job
recorded a release.

```bash
export AFTERGREEN_POSTGRES_URL='postgres://aftergreen_reader@db.internal:5432/app?sslmode=verify-full'
go run ./cmd/aftergreen postgres --contract examples/postgres-contract.json --key "$DEPLOY_ID"
```

Exit codes: `0` verified, `1` failed, `2` invalid input, `3` unknown.

## Contract (version 1)

```json
{
  "version": 1,
  "schema": "public",
  "table": "deploys",
  "key_column": "deploy_id",
  "key": "run-1001",
  "expect": { "version": "v1.4.2", "state": "live" }
}
```

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | Must be `1` |
| `schema` | no | Default `public` |
| `table`, `key_column` | yes | Destination table and the column holding your stable event/deploy ID |
| `key` | yes, unless `--key` is given | Event ID to look up; `--key` overrides it |
| `expect` | yes, ≥ 1 | Column → expected value, compared as PostgreSQL `text` |

Unknown fields are rejected. The connection string is **only** read from
`AFTERGREEN_POSTGRES_URL`, never from the contract or a flag. Standard libpq
URLs and key/value strings work, including `sslmode`. Use `verify-full` for
anything that is not localhost.

Identifiers must be plain names (`[A-Za-z_][A-Za-z0-9_]*`, max 63). They are
quoted, so they are **case-sensitive**: `Leads` is not `leads`.

### Exact SQL

The only statement AfterGreen runs is:

```sql
BEGIN READ ONLY;
SELECT "state"::text, "version"::text FROM "public"."deploys" WHERE "deploy_id" = $1 LIMIT 2;
ROLLBACK;
```

The event key is always a bound parameter. The connection sets
`application_name=aftergreen`. Only that startup parameter is used, so it
works behind PgBouncer.

## Verdicts

| Situation | Verdict | Reason shown |
| --- | --- | --- |
| Exactly one row, every value equal | `VERIFIED` | |
| Value differs | `FAILED` | `state: value mismatch` |
| Column is NULL | `FAILED` | `state: NULL` |
| No row | `FAILED` | `destination record missing` |
| Two or more rows | `FAILED` | `multiple destination rows match event key` |
| Connection refused, auth failed, timeout, lock wait | `UNKNOWN` | e.g. `(SQLSTATE 28P01: authentication failed)` |
| Permission denied, missing table/column, key not castable to the column type | `UNKNOWN` | SQLSTATE code |

Reasons never contain row values, server error messages, the connection string
or the SQL text. Only column names and SQLSTATE codes are shown.

Comparison is exact text equality on PostgreSQL's canonical text output. For
example, `numeric(10,2)` `120.5` is `"120.50"`, booleans are `"true"`/`"false"`,
and timestamps depend on the session `TimeZone`. A key column of another type
(e.g. `bigint`) works; PostgreSQL converts the key.

## Least-privilege role

```sql
CREATE ROLE aftergreen_reader LOGIN PASSWORD '...' NOSUPERUSER NOCREATEDB NOCREATEROLE;
GRANT CONNECT ON DATABASE app TO aftergreen_reader;
GRANT USAGE ON SCHEMA public TO aftergreen_reader;
GRANT SELECT ON public.deploys TO aftergreen_reader;
ALTER ROLE aftergreen_reader SET default_transaction_read_only = on;
ALTER ROLE aftergreen_reader SET statement_timeout = '10s';
```

Never use an owner or admin role. Keep `--timeout` (default `10s`, max `60s`)
short. A check that waits on locks is reported as `UNKNOWN`.

## Testing

Unit tests use a fake driver. Integration tests run against a real PostgreSQL
17 container with synthetic data only. They create an isolated schema and a
reader role per run.

```bash
scripts/test-postgres.sh                                              # needs Docker
AFTERGREEN_TEST_POSTGRES_URL=postgres://... go test -tags integration ./...   # your own disposable DB
```

Covered: verified, mismatch, NULL, missing, duplicate, numeric canonical text,
integer key, uncastable key, permission denied, missing table/column,
case-sensitive names, SQL-injection-shaped keys, read-only transaction, lock
wait timeout, connection refused, bad password. The CLI is also tested end to
end for exit codes and leak checks.

## Not supported yet

Composite keys, non-equality assertions, row counts, JSON path checks, other
databases, and n8n or workflow-tool ingestion. These are possible future
phases, not current features.
