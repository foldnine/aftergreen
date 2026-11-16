#!/bin/sh
# AfterGreen installer for Linux and macOS.
#
#   curl -fsSL https://aftergreen.foldnine.dev/install.sh | sh
#   curl -fsSL https://aftergreen.foldnine.dev/install.sh | sh -s -- --version v0.1.0 --bin-dir ~/bin
#
# What it does, in order:
#   1. Detects the OS (linux, darwin) and CPU architecture (amd64, arm64).
#   2. Downloads the release archive and SHA256SUMS from the GitHub release of
#      foldnine/aftergreen over HTTPS.
#   3. Verifies the archive's SHA256 against SHA256SUMS. Any mismatch, missing
#      entry or missing hash tool stops the install before anything is written.
#   4. Copies the binary into a directory you own (default ~/.local/bin) and
#      checks that it reports the expected version. No root, no sudo.
#
# It never runs sudo, never edits shell profiles and never writes outside the
# install directory and a temporary directory it removes on exit.
#
# Options (environment variable in brackets):
#   --version vX.Y.Z  release to install [AFTERGREEN_VERSION]
#                     default: the newest published release, pre-releases included
#   --bin-dir DIR     install directory [AFTERGREEN_INSTALL_DIR]
#                     default: $HOME/.local/bin
#   -h, --help        show this help
#
# The whole script is inside main(), called on the last line, so a download cut
# short can never run a partial script.
set -eu

REPO="foldnine/aftergreen"
BASE_URL_DEFAULT="https://github.com/${REPO}/releases/download"
API_URL="https://api.github.com/repos/${REPO}/releases?per_page=1"

say() { printf 'aftergreen-install: %s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }
usage_error() { say "error: $*"; say "run with --help for usage"; exit 2; }

usage() {
	cat <<'EOF_USAGE'
Install the AfterGreen CLI from a GitHub release, verified by SHA256.

usage: install.sh [--version vX.Y.Z] [--bin-dir DIR]

  --version vX.Y.Z  release to install [env AFTERGREEN_VERSION]
                    default: the newest published release, pre-releases included
  --bin-dir DIR     install directory [env AFTERGREEN_INSTALL_DIR]
                    default: $HOME/.local/bin
  -h, --help        show this help

Never uses sudo and never edits shell profiles.
EOF_USAGE
}

have() { command -v "$1" >/dev/null 2>&1; }

# fetch URL OUTFILE
fetch() {
	case "$1" in
	https://*) proto_ok=1 ;;
	*) proto_ok=0 ;;
	esac
	if have curl; then
		if [ "$proto_ok" = 1 ]; then
			curl --proto '=https' --tlsv1.2 -fsSL --retry 2 -o "$2" "$1"
		else
			curl -fsSL -o "$2" "$1"
		fi
	elif have wget; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is required"
	fi
}

# sha256 FILE -> prints the lowercase hex digest
sha256() {
	if have sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	elif have shasum; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif have openssl; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		die "no SHA256 tool found (need sha256sum, shasum or openssl); refusing to install unverified"
	fi
}

detect_os() {
	case "$(uname -s)" in
	Linux) echo linux ;;
	Darwin) echo darwin ;;
	MINGW* | MSYS* | CYGWIN*) die "on Windows, download the .zip from https://github.com/${REPO}/releases (see the README)" ;;
	*) die "unsupported OS: $(uname -s). Prebuilt binaries exist for Linux, macOS and Windows" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
	x86_64 | amd64)
		# A shell under Rosetta 2 reports x86_64 on Apple silicon; use the native build.
		if [ "$1" = darwin ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
			echo arm64
		else
			echo amd64
		fi
		;;
	aarch64 | arm64) echo arm64 ;;
	*) die "unsupported CPU architecture: $(uname -m). Prebuilt binaries exist for amd64 and arm64" ;;
	esac
}

valid_version() {
	printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'
}

latest_version() {
	fetch "$API_URL" "$1/releases.json" || die "could not query ${API_URL}. Pass --version vX.Y.Z to skip this lookup"
	tr ',' '\n' <"$1/releases.json" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
}

main() {
	version="${AFTERGREEN_VERSION:-}"
	bin_dir="${AFTERGREEN_INSTALL_DIR:-}"
	base_url="${AFTERGREEN_RELEASE_BASE_URL:-$BASE_URL_DEFAULT}"

	while [ $# -gt 0 ]; do
		case "$1" in
		--version) [ $# -ge 2 ] || usage_error "--version needs a value"; version="$2"; shift 2 ;;
		--version=*) version="${1#*=}"; shift ;;
		--bin-dir) [ $# -ge 2 ] || usage_error "--bin-dir needs a value"; bin_dir="$2"; shift 2 ;;
		--bin-dir=*) bin_dir="${1#*=}"; shift ;;
		-h | --help) usage; exit 0 ;;
		*) usage_error "unknown argument: $1" ;;
		esac
	done

	# AFTERGREEN_RELEASE_BASE_URL exists for mirrors and the installer's own tests.
	# Plain HTTP is accepted only for loopback test servers.
	case "$base_url" in
	https://* | http://127.0.0.1:* | http://localhost:*) ;;
	*) usage_error "AFTERGREEN_RELEASE_BASE_URL must be an https:// URL" ;;
	esac

	if [ -z "$bin_dir" ]; then
		[ -n "${HOME:-}" ] || usage_error "HOME is not set; pass --bin-dir DIR"
		bin_dir="$HOME/.local/bin"
	fi
	if [ -n "$version" ] && ! valid_version "$version"; then
		usage_error "version must look like v1.2.3, got: $version"
	fi

	os="$(detect_os)"
	arch="$(detect_arch "$os")"
	have tar || die "tar is required"

	tmp="$(mktemp -d 2>/dev/null || mktemp -d -t aftergreen)"
	staged=""
	trap 'rm -rf "$tmp"; [ -z "$staged" ] || rm -f "$staged"' EXIT
	trap 'exit 130' INT TERM

	if [ -z "$version" ]; then
		version="$(latest_version "$tmp")"
		valid_version "$version" || die "could not determine the newest release (got '${version}'). Pass --version vX.Y.Z"
	fi

	archive="aftergreen_${version}_${os}_${arch}.tar.gz"
	say "installing aftergreen ${version} (${os}/${arch}) into ${bin_dir}"

	fetch "${base_url}/${version}/SHA256SUMS" "$tmp/SHA256SUMS" ||
		die "could not download SHA256SUMS for ${version}. Does the release exist? https://github.com/${REPO}/releases"
	fetch "${base_url}/${version}/${archive}" "$tmp/${archive}" ||
		die "could not download ${archive}"

	expected="$(awk -v f="$archive" '$2 == f || $2 == "*" f {print $1; exit}' "$tmp/SHA256SUMS" | tr 'A-F' 'a-f')"
	printf '%s\n' "$expected" | grep -Eq '^[0-9a-f]{64}$' ||
		die "SHA256SUMS has no valid entry for ${archive}; refusing to install"
	actual="$(sha256 "$tmp/${archive}")"
	if [ "$actual" != "$expected" ]; then
		say "error: checksum mismatch for ${archive}"
		say "  expected ${expected}"
		say "  actual   ${actual}"
		die "the download is corrupt or was tampered with; nothing was installed"
	fi
	say "checksum verified (sha256 ${actual})"

	mkdir "$tmp/x"
	tar -xzf "$tmp/${archive}" -C "$tmp/x" aftergreen || die "could not extract the binary from ${archive}"
	[ -f "$tmp/x/aftergreen" ] && [ ! -L "$tmp/x/aftergreen" ] || die "${archive} does not contain a regular aftergreen file"

	mkdir -p "$bin_dir" || die "cannot create ${bin_dir}. Choose a directory you own with --bin-dir"
	[ -w "$bin_dir" ] || die "${bin_dir} is not writable. Choose a directory you own with --bin-dir (this installer never uses sudo)"

	# Stage next to the target, then rename: an existing binary is replaced
	# atomically or not at all.
	staged="${bin_dir}/.aftergreen.install.$$"
	cp "$tmp/x/aftergreen" "$staged"
	chmod 0755 "$staged"
	got="$("$staged" version 2>/dev/null || true)"
	if [ "$got" != "aftergreen ${version}" ]; then
		die "installed binary reported '${got}', expected 'aftergreen ${version}'; nothing was installed"
	fi
	mv -f "$staged" "${bin_dir}/aftergreen"
	staged=""

	say "installed ${bin_dir}/aftergreen (${got})"
	case ":${PATH:-}:" in
	*":${bin_dir}:"*) ;;
	*)
		say "note: ${bin_dir} is not on your PATH. Add it, for example:"
		say "  echo 'export PATH=\"${bin_dir}:\$PATH\"' >> ~/.profile   # then open a new shell"
		;;
	esac
	say "try it: aftergreen http --url https://example.com --status 200 --contains \"Example Domain\""
}

main "$@"
