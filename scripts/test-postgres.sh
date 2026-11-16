#!/usr/bin/env bash
# Runs the PostgreSQL integration tests against a disposable local container.
# Requires Docker. Uses only synthetic data; nothing leaves this machine.
set -euo pipefail

# postgres:17-alpine, pinned by digest (also used in CI).
IMAGE="postgres@sha256:a77aa70e641402a21411b92b721c58d10f23b50ae5fdd2333efd22d166e1e5f7"
NAME="aftergreen-it-$$"
PASSWORD="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"

cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run -d --name "$NAME" -e POSTGRES_PASSWORD="$PASSWORD" -e POSTGRES_DB=aftergreen_it \
  -p 127.0.0.1::5432 "$IMAGE" >/dev/null
PORT="$(docker port "$NAME" 5432/tcp | head -1 | sed 's/.*://')"

for _ in $(seq 1 60); do
  if docker exec "$NAME" pg_isready -U postgres -d aftergreen_it -h 127.0.0.1 >/dev/null 2>&1; then break; fi
  sleep 1
done

export AFTERGREEN_TEST_POSTGRES_URL="postgres://postgres:${PASSWORD}@127.0.0.1:${PORT}/aftergreen_it?sslmode=disable"
go test -tags integration -count=1 "$@" ./...
