#!/usr/bin/env bash
# Reproducible cross-compiled CLI archives plus SHA256SUMS.
# Usage: scripts/build-release.sh VERSION OUTDIR
#
# Output depends only on the source tree, VERSION and the Go toolchain version
# (recorded in BUILDINFO). It does not depend on umask, timezone, checkout
# method (git clone, worktree, source tarball) or the build directory.
set -euo pipefail
version="${1:?version}"; out="${2:?outdir}"
[[ "$version" =~ ^(v[0-9]+\.[0-9]+\.[0-9]+|0\.0\.0-dryrun\+[0-9a-f]+)$ ]] || { echo "bad version: $version" >&2; exit 2; }
umask 022
export TZ=UTC LC_ALL=C CGO_ENABLED=0 GOFLAGS=-mod=readonly
rm -rf "$out"; mkdir -p "$out"
out="$(cd "$out" && pwd)"
# Fixed archive timestamp: last commit time if available, else SOURCE_DATE_EPOCH.
if [[ -n "${SOURCE_DATE_EPOCH:-}" ]]; then mtime="@${SOURCE_DATE_EPOCH}"; else mtime="$(git log -1 --format=%cI)"; fi
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  goos="${target%/*}"; goarch="${target#*/}"
  name="aftergreen_${version}_${goos}_${goarch}"
  stage="$(mktemp -d)"
  bin="aftergreen"; [[ "$goos" == windows ]] && bin="aftergreen.exe"
  # -buildvcs=false: VCS stamps vary with how the tree was checked out; the
  # version is injected explicitly instead.
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false \
    -ldflags "-s -w -buildid= -X main.version=${version}" -o "$stage/$bin" ./cmd/aftergreen
  cp LICENSE README.md "$stage/"
  chmod 0755 "$stage/$bin"; chmod 0644 "$stage/LICENSE" "$stage/README.md"
  touch -d "$mtime" "$stage/$bin" "$stage/LICENSE" "$stage/README.md"
  if [[ "$goos" == windows ]]; then
    (cd "$stage" && zip -X -q "$out/$name.zip" "$bin" LICENSE README.md)
  else
    tar --sort=name --mtime="$mtime" --owner=0 --group=0 --numeric-owner --format=gnu \
      -C "$stage" -cf - "$bin" LICENSE README.md | gzip -n -9 > "$out/$name.tar.gz"
  fi
  rm -rf "$stage"
done
printf 'version=%s\ngo=%s\n' "$version" "$(go env GOVERSION)" > "$out/BUILDINFO"
(cd "$out" && sha256sum -- * > SHA256SUMS)
cat "$out/SHA256SUMS"
