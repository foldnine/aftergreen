# Release plan (core CLI)

Status: **prepared, not executed.** No tags, releases or binaries exist yet.
Every step marked *owner* needs explicit approval.

## What the pipeline does

`.github/workflows/release.yml`:

| Trigger | Result |
| --- | --- |
| PR touching the release workflow or build script, or manual dispatch | Dry run: test, build all targets, smoke-test the linux/amd64 binary, upload a workflow artifact. Nothing is published. |
| Owner pushes a `vX.Y.Z` tag | Same build, plus SLSA build provenance attestation (Sigstore, via `actions/attest-build-provenance`) and a **draft** GitHub Release. A human publishes it. |

Build properties (`scripts/build-release.sh`):

- Targets: linux, darwin and windows, each for amd64 and arm64. `CGO_ENABLED=0`, static, no system dependencies.
- Toolchain: latest patch of the `go` version in `go.mod` (`check-latest`), and no build cache.
- `-trimpath -buildvcs=true -ldflags "-s -w -buildid= -X main.version=<tag>"`.
- Deterministic archives (sorted entries, fixed mtime/owner, `gzip -n`, `zip -X`). Two local builds of the same commit produce identical `SHA256SUMS`.
- `SHA256SUMS` covers every archive. The provenance attestation covers the same subjects.
- All workflow actions are pinned to full commit SHAs.

## Verifying a release (for users, once one exists)

```bash
sha256sum --ignore-missing -c SHA256SUMS
gh attestation verify aftergreen_v0.1.0_linux_amd64.tar.gz --repo foldnine/aftergreen
```

Until a release is published, the only supported install is from source:
`go run ./cmd/aftergreen` or `go build ./cmd/aftergreen`.

## Steps (owner)

1. All PRs for the release are merged, and `main` CI is green: tests, race,
   3-OS matrix, PostgreSQL integration, govulncheck.
2. Enable **immutable releases** in repository settings. Published release
   tags are then locked and can't be reused.
3. `git tag -s v0.1.0 -m v0.1.0 && git push origin v0.1.0`.
4. Review the draft release. Download one archive and verify its checksum and
   attestation as above. Then publish.

Rollback: never re-tag. Mark the release as not recommended in its notes,
publish `v0.1.1`, and publish a security advisory if needed.

## Proposed for later (not done)

- Keyless cosign signatures of `SHA256SUMS`. The attestation already gives
  Sigstore-backed provenance; add cosign only if users ask for it.
- Homebrew tap or `go install` docs (`go install` works today from a tag,
  without injected version info).
- Switching the GitHub Action to verified binaries (see ADR 0001).

## Supported platforms

| OS | Arch | Status |
| --- | --- | --- |
| Linux | amd64, arm64 | built; amd64 smoke-tested in CI |
| macOS | amd64, arm64 | built; tests run in CI on macOS (arm64 runner) |
| Windows | amd64, arm64 | built; tests run in CI on Windows amd64 |
