# ADR 0001: GitHub Action engine for v1

- Status: accepted (proposed by engineering, 2026-11-15; owner may overturn)
- Scope: `foldnine/aftergreen-action` v1

## Context

The HTTP check exists twice: in Go (`internal/httpcheck`, used by the CLI) and
in Node (`foldnine/aftergreen-action`, `runs.using: node24`). Two
implementations can drift. The options for the Action are:

1. **Download a published Go binary** in the Action and run it.
2. **Keep a standalone Node 24 implementation** with no dependencies.
3. Docker container action wrapping the Go binary.

## Decision

Keep the **standalone Node 24 implementation** for v1, and bind it to the Go
engine with shared conformance cases (`conformance/http/cases.json`).

## Rationale

| Concern | Node (chosen) | Go binary download | Docker action |
| --- | --- | --- | --- |
| Install friction | none; runs in the runner | none for users | Linux runners only, slow image build/pull |
| Supply chain | ~150 lines, zero deps, readable in the PR | needs released, checksummed, attested binaries that don't exist yet, plus verification code in the Action | base image provenance |
| Platforms | Linux, macOS, Windows, self-hosted | one binary per OS/arch to select correctly | Linux only |
| Network at start-up | none | download from GitHub Releases on every run | image pull |
| Drift risk | yes, mitigated by conformance cases | none | none |

The HTTP check is small and fully specified by the verdict contract. Its whole
behaviour fits in a fixture file both engines must pass. A binary download
would add a release pipeline, checksum verification and an extra network fetch
to every user's run. It would also be the Action's most security-sensitive code.
That is premature before any core release exists.

## Consequences

- Every HTTP behaviour change must update `conformance/http/cases.json` first.
  The Action repo vendors that file at a pinned core commit
  (`test/conformance/SOURCE`), and its CI fails if the copy drifts.
- Documented, accepted differences: the Action takes whole-second timeouts
  (`timeout-seconds`) while the CLI takes durations. Node's `fetch` refuses
  WHATWG "bad ports". The CLI exits `3` on UNKNOWN, while the Action fails the
  step and exposes `verdict`.
- PostgreSQL and other adapters stay **CLI-only**. They are not to be
  reimplemented in Node. Before an adapter appears in the Action, revisit this
  ADR. By then signed, checksummed CLI releases (see `docs/release.md`) make
  option 1 viable.
