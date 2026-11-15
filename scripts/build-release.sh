#!/usr/bin/env bash
# Reproducible cross-compiled CLI archives plus SHA256SUMS.
# Usage: scripts/build-release.sh VERSION OUTDIR
set -euo pipefail
version="${1:?version}"; out="${2:?outdir}"
[[ "$version" =~ ^(v[0-9]+\.[0-9]+\.[0-9]+|0\.0\.0-dryrun\+[0-9a-f]+)$ ]] || { echo "bad version: $version" >&2; exit 2; }
rm -rf "$out"; mkdir -p "$out"
out="$(cd "$out" && pwd)"
export CGO_ENABLED=0 GOFLAGS=-mod=readonly
mtime="$(git log -1 --format=%cI)"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  goos="${target%/*}"; goarch="${target#*/}"
  name="aftergreen_${version}_${goos}_${goarch}"
  stage="$(mktemp -d)"
  bin="aftergreen"; [[ "$goos" == windows ]] && bin="aftergreen.exe"
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=true \
    -ldflags "-s -w -buildid= -X main.version=${version}" -o "$stage/$bin" ./cmd/aftergreen
  cp LICENSE README.md "$stage/"
  if [[ "$goos" == windows ]]; then
    (cd "$stage" && TZ=UTC touch -d "$mtime" "$bin" LICENSE README.md && zip -X -q "$out/$name.zip" "$bin" LICENSE README.md)
  else
    tar --sort=name --mtime="$mtime" --owner=0 --group=0 --numeric-owner \
      -C "$stage" -cf - "$bin" LICENSE README.md | gzip -n > "$out/$name.tar.gz"
  fi
  rm -rf "$stage"
done
(cd "$out" && sha256sum -- * > SHA256SUMS)
cat "$out/SHA256SUMS"
