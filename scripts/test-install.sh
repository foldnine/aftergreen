#!/bin/sh
# Offline tests for scripts/install.sh against a fake release on a loopback
# HTTP server. Needs python3. Usage: scripts/test-install.sh [SHELL]
# SHELL is the interpreter for install.sh (default: sh).
# pass() and fail() always succeed, so "A && pass || fail" is safe here.
# sh_bin is split on purpose so "busybox sh" works.
# shellcheck disable=SC2015,SC2086
set -eu

sh_bin="${1:-sh}"
root="$(cd "$(dirname "$0")/.." && pwd)"
installer="$root/scripts/install.sh"
work="$(mktemp -d)"
server_pid=""
cleanup() { [ -z "$server_pid" ] || kill "$server_pid" 2>/dev/null || true; rm -rf "$work"; }
trap cleanup EXIT

case "$(uname -s)" in Linux) os=linux ;; Darwin) os=darwin ;; *) echo "unsupported OS" >&2; exit 2 ;; esac
case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; *) echo "unsupported arch" >&2; exit 2 ;; esac
if [ "$os" = darwin ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then arch=arm64; fi

sha() { if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi; }

# make_release VERSION REPORTED_VERSION: a fake release whose binary prints
# "aftergreen REPORTED_VERSION".
make_release() {
	dir="$work/srv/$1"
	mkdir -p "$dir" "$work/pkg"
	printf '#!/bin/sh\necho "aftergreen %s"\n' "$2" >"$work/pkg/aftergreen"
	chmod 755 "$work/pkg/aftergreen"
	a="aftergreen_$1_${os}_${arch}.tar.gz"
	(cd "$work/pkg" && tar -czf "$dir/$a" aftergreen)
	printf '%s  %s\n' "$(sha "$dir/$a")" "$a" >"$dir/SHA256SUMS"
}

make_release v9.9.0 v9.9.0                         # good
make_release v9.9.1 v9.9.1                         # tampered after checksumming
printf 'x' >>"$work/srv/v9.9.1/aftergreen_v9.9.1_${os}_${arch}.tar.gz"
make_release v9.9.2 v9.9.2                         # no entry for this platform
: >"$work/srv/v9.9.2/SHA256SUMS"
make_release v9.9.3 v1.0.0                         # binary reports wrong version

port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])')"
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$work/srv" >/dev/null 2>&1 &
server_pid=$!
i=0
until python3 -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:$port/v9.9.0/SHA256SUMS')" 2>/dev/null; do
	i=$((i + 1)); [ "$i" -lt 50 ] || { echo "server did not start" >&2; exit 1; }; sleep 0.1
done
base="http://127.0.0.1:$port"

fails=0
pass() { printf 'ok    %s\n' "$1"; }
fail() { printf 'FAIL  %s\n' "$1"; fails=$((fails + 1)); }

# run NAME EXPECTED_EXIT BIN_DIR [installer args...]
run() {
	name="$1" want="$2" bin="$3"; shift 3
	set +e
	AFTERGREEN_RELEASE_BASE_URL="$base" $sh_bin "$installer" --bin-dir "$bin" "$@" >"$work/out" 2>&1
	got=$?
	set -e
	if [ "$got" = "$want" ]; then pass "$name (exit $got)"; else fail "$name: exit $got, want $want"; sed 's/^/      /' "$work/out"; fi
}

b="$work/bin1"
run "installs a verified release" 0 "$b" --version v9.9.0
[ "$("$b/aftergreen" version)" = "aftergreen v9.9.0" ] && pass "binary installed and runs" || fail "binary missing or wrong"
[ -z "$(find "$b" -name '.aftergreen.install.*')" ] && pass "no staging leftovers" || fail "staging file left behind"

run "rejects a tampered archive" 1 "$b" --version v9.9.1
grep -q "checksum mismatch" "$work/out" && pass "reports checksum mismatch" || fail "no mismatch message"
[ "$("$b/aftergreen" version)" = "aftergreen v9.9.0" ] && pass "existing binary untouched after mismatch" || fail "existing binary changed"

run "rejects a release without a checksum entry" 1 "$b" --version v9.9.2
grep -q "no valid entry" "$work/out" && pass "reports missing entry" || fail "no missing-entry message"

run "rejects a binary that reports the wrong version" 1 "$b" --version v9.9.3
[ "$("$b/aftergreen" version)" = "aftergreen v9.9.0" ] && pass "existing binary untouched after version mismatch" || fail "existing binary changed"
[ -z "$(find "$b" -name '.aftergreen.install.*')" ] && pass "no staging leftovers after failure" || fail "staging file left behind"

b2="$work/bin2"
run "fails on a missing release" 1 "$b2" --version v9.8.7
[ ! -e "$b2/aftergreen" ] && pass "nothing installed for a missing release" || fail "file written for missing release"

run "rejects a malformed version" 2 "$b2" --version 9.9.0
run "rejects unknown arguments" 2 "$b2" --sudo
run "creates a nested install directory" 0 "$work/a/b/c" --version v9.9.0

set +e
AFTERGREEN_RELEASE_BASE_URL="http://example.com" $sh_bin "$installer" --version v9.9.0 --bin-dir "$b2" >/dev/null 2>&1
got=$?
set -e
[ "$got" = 2 ] && pass "refuses a non-HTTPS remote base URL (exit 2)" || fail "accepted http:// remote base URL (exit $got)"

# Piped like the documented one-liner.
b3="$work/bin3"
AFTERGREEN_RELEASE_BASE_URL="$base" AFTERGREEN_VERSION=v9.9.0 AFTERGREEN_INSTALL_DIR="$b3" $sh_bin <"$installer" >"$work/out" 2>&1 &&
	[ "$("$b3/aftergreen" version)" = "aftergreen v9.9.0" ] && pass "works when piped to $sh_bin via env vars" || { fail "piped install"; cat "$work/out"; }

if [ "$fails" -ne 0 ]; then echo "$fails failure(s)"; exit 1; fi
echo "all installer tests passed ($sh_bin, $os/$arch)"
