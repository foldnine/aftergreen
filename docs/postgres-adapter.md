# PostgreSQL verification core (experimental)

This package accepts a caller-provided `database/sql` connection backed by a PostgreSQL driver. No driver or live connection is bundled yet; CLI integration follows in a separate milestone.

## Contract

A contract identifies a destination schema, table, key column, event key, and a map of expected destination fields and **source-derived** values. Verification uses a parameterized event key, strictly validated SQL identifiers, and `LIMIT 2` to reject ambiguous matches.

Outcomes:
- `VERIFIED`: exactly one row matches every assertion.
- `FAILED`: missing row, duplicate matching rows, null or mismatched field.
- `UNKNOWN`: connection, query, or observation error. No success is inferred.

## Security

Use a restricted database role with SELECT permissions only on the required tables. The adapter explicitly begins a read-only transaction. Never use a production owner/admin account.

**No real values are emitted in mismatch reasons.** This avoids echoing tokens, customer fields, and other sensitive data into logs. SQL query text is not logged.

## Important limitations

- Unit tests use a fake SQL driver; no live PostgreSQL integration test exists yet.
- Columns are read as PostgreSQL `text`, so expected values must use canonical database text representations.
- Key correlation requires a stable source event identifier present in the destination.
- The package does not ingest n8n executions, schedule checks, retry failed reads, or connect to a database by itself.
