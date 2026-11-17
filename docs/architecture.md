# Architecture

AfterGreen is one static binary. It observes outcomes and prints a verdict.
There's no daemon, no account, no hosted service and no stored history.

```text
cmd/aftergreen            flags, output, exit codes, signals, PostgreSQL connection
internal/config           aftergreen.yaml: strict decode, validation, ${VAR} substitution
internal/runner           runs checks in order, aggregates verdicts
internal/httpcheck        HTTP engine (one GET)            ← also `aftergreen http`
internal/postgres         PostgreSQL engine (one SELECT)   ← also `aftergreen postgres`
internal/verify           local evidence files             ← `aftergreen check`
```

`aftergreen verify` adds orchestration only. `config` turns a YAML file into
the engines' existing contract types (`httpcheck.Contract`,
`postgres.Contract`). `runner` calls the engines. No verification logic is
duplicated, so the one-shot commands and `verify` can't drift apart.

## Decisions

**Three verdicts, fail closed.** `UNKNOWN` means "could not observe", never
"probably fine". Aggregation is FAILED > UNKNOWN > VERIFIED. That is the same
rule the existing commands use for exit codes, and a test pins both
implementations to it (`TestExitCodeMatchesOverall`).

**Validate everything first.** The whole file is checked before the first
request: types, fields, URLs, SQL identifiers, contract files, environment
variables. Configuration mistakes exit 2 instead of turning into a
`FAILED` or `UNKNOWN` halfway through a run, and a retry can never hide them.

**Strict YAML, small schema.** YAML because people write these files by
hand and comment them. The decoder rejects unknown fields, duplicate keys,
anchors/aliases, tags and multiple documents. What's left is a flat list of
checks with a handful of fields per type. There are no expressions, no
conditionals, no includes and no templating beyond `${VAR}`. Dependency:
`go.yaml.in/yaml/v3`, the maintained continuation of `gopkg.in/yaml.v3`. It
has no dependencies of its own in the binary.

**Explicit deployment identity.** Values that identify a deployment come
from environment variables that must be set and non-empty. A PostgreSQL
check's key must be in the YAML file. The key in a contract file is ignored.
Both rules exist so an unset CI variable can't silently check an old
deployment.

**Sequential execution.** Checks run in file order, one at a time. Output and
timing are deterministic, PostgreSQL gets at most one connection, and the
maximum run time is the sum of the checks' bounds. Parallelism isn't needed
for a handful of checks.

**Injected engines.** `runner.Engines` holds the engine functions. The CLI
passes the real ones. Tests pass fakes, so aggregation, ordering and
cancellation are tested without network or database. The real engines have
their own tests, plus the HTTP conformance cases and the PostgreSQL
integration suite.

**Retries around engines, not inside them.** The engines stay one-shot.
`runner` repeats an observation under a hard deadline and decides with the
last completed one. Retries can't change the one-shot commands, and the
verdict rules for a stabilising rollout live in one place. Mid-window
mismatches aren't final. Cancellation before the deadline is `UNKNOWN`.
Being VERIFIED without enough consecutive observations is `UNKNOWN`.

**A tiny JSON path, not an expression engine.** `$`, `.name`, `[n]` and
typed `equals` cover "which revision is serving". A larger language would add
parsing risk and evaluation ambiguity for little benefit. Responses with
duplicate keys are `UNKNOWN`, because parsers disagree about which value wins.

**File containment.** Contract files are opened through `os.Root` relative to
the YAML file's directory. A contract can't read files outside it, even
through symlinks. The YAML file itself is still trusted input (see
[SECURITY.md](../SECURITY.md)).

## Out of scope

No history database, no dashboard, no hosted API, no plugin system, no
workflow engine, no Kubernetes integration. AfterGreen answers one question
per run: did this deployment produce the outcomes it was supposed to?
Progressive delivery tools such as Argo Rollouts and Flagger solve a
different problem and can call AfterGreen as a step.

The GitHub Action keeps its own Node implementation of the HTTP check
([ADR 0001](adr/0001-action-engine.md)). `verify` is CLI-only for now.
