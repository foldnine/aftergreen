# Release plan (core CLI)

Status: **v0.1.0 published** on 2026-11-16 as a pre-release, with immutable
releases enabled. Every step marked *owner* needs explicit approval.

## What the pipeline does

`.github/workflows/release.yml`:

| Trigger | Result |
| --- | --- |
| PR touching the release workflow or build script, or manual dispatch (on any ref, including a tag) | Dry run: test, build all targets, smoke-test the linux/amd64 binary, upload a workflow artifact. Nothing is attested or published. |
| Owner pushes a `vX.Y.Z` tag on a commit that is on `main` | Same build, plus SLSA build provenance attestation (Sigstore, via `actions/attest-build-provenance`) and a **draft** GitHub Release. A human publishes it. Tags on other commits fail. |

Build properties (`scripts/build-release.sh`):

- Targets: linux, darwin and windows, each for amd64 and arm64. `CGO_ENABLED=0`, static, no system dependencies.
- Toolchain: latest patch of the `go` version in `go.mod` (`check-latest`), and no build cache.
- `-trimpath -buildvcs=false -ldflags "-s -w -buildid= -X main.version=<tag>"`. No VCS stamps, so the output doesn't depend on how the source was checked out.
- Deterministic archives: sorted entries, fixed mtime (last commit time, or `SOURCE_DATE_EPOCH`), fixed owner and modes, `umask 022`, `TZ=UTC`, `gzip -n`, `zip -X`.
- `BUILDINFO` records the version and the exact Go toolchain. It is covered by `SHA256SUMS`.
- `SHA256SUMS` covers every archive. The provenance attestation covers the same subjects.
- All workflow actions are pinned to full commit SHAs.

## Reproducing a build

Output depends only on the source commit, the version string and the Go
toolchain in `BUILDINFO`. Checked on 2026-11-16 for one commit: a git worktree
(UTC, umask 022) and a `git archive` tarball without `.git` (America/New_York,
umask 077, `SOURCE_DATE_EPOCH` set to the commit time) produced byte-identical
`SHA256SUMS`.

```bash
git clone https://github.com/foldnine/aftergreen && cd aftergreen && git checkout v0.1.0
GOTOOLCHAIN=go1.26.11 scripts/build-release.sh v0.1.0 /tmp/ag-dist   # Go version from BUILDINFO
diff /tmp/ag-dist/SHA256SUMS SHA256SUMS                               # the published file
```

Needs GNU tar, gzip, Info-ZIP zip and sha256sum (any Linux runner).
The gzip and zip implementations must match for byte-identical archives; the
binaries inside are reproducible regardless.

## Verifying a release (for users)

```bash
sha256sum --ignore-missing -c SHA256SUMS
gh attestation verify aftergreen_v0.1.0_linux_amd64.tar.gz --repo foldnine/aftergreen
```

Install methods for users, all built on the release assets: [install.md](install.md).

## Steps (owner)

1. All PRs for the release are merged, and `main` CI is green: tests, race,
   3-OS matrix, PostgreSQL integration, govulncheck.
2. Enable **immutable releases** in repository settings. Published release
   tags are then locked and can't be reused.
3. `git tag -s v0.1.0 -m v0.1.0 && git push origin v0.1.0`.
4. Review the draft release. Download one archive and verify its checksum and
   attestation as above. Then publish.

5. After publishing, follow [After a release is published](#after-a-release-is-published).

Rollback: never re-tag. Mark the release as not recommended in its notes,
publish `v0.1.1`, and publish a security advisory if needed.

## After a release is published

The install paths read the release assets by name
(`aftergreen_vX.Y.Z_<os>_<arch>.tar.gz|zip` and `SHA256SUMS`). Keep those names.

1. `scripts/install.sh` needs no change: by default it installs the newest
   published release.
2. Replace the old version in the pinned places: `docs/install.md`,
   `website/site/index.html` (see `website/README.md`), the Windows job in
   `.github/workflows/install.yml`, and the pinned-version step in the same
   workflow.
3. Regenerate the Homebrew formula and open a PR to the tap (once it exists):

   ```bash
   gh release download vX.Y.Z -R foldnine/aftergreen -p SHA256SUMS -D /tmp/ag
   packaging/homebrew/render-formula.sh vX.Y.Z /tmp/ag/SHA256SUMS > packaging/homebrew/aftergreen.rb
   ```

   CI fails if the committed formula doesn't match the release's checksums.
4. Run the Install workflow (`workflow_dispatch`) and deploy the website.

## Proposed for later (not done)

- Keyless cosign signatures of `SHA256SUMS`. The attestation already gives
  Sigstore-backed provenance; add cosign only if users ask for it.
- Publishing the Homebrew tap: see [packaging/homebrew/README.md](../packaging/homebrew/README.md).
- Switching the GitHub Action to verified binaries (see ADR 0001).

## Supported platforms

| OS | Arch | Status |
| --- | --- | --- |
| Linux | amd64, arm64 | built; amd64 smoke-tested in the release build; both installed from the release by the Install workflow, plus Alpine (musl, busybox) |
| macOS | amd64, arm64 | built; tests run in CI on macOS (arm64 runner); both installed by script and Homebrew formula in the Install workflow |
| Windows | amd64, arm64 | built; tests run in CI on Windows amd64; both downloaded, verified and run by the Install workflow |
