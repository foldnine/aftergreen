# Homebrew formula

`aftergreen.rb` installs the prebuilt release binary for macOS and Linux
(amd64, arm64). Homebrew checks each archive against the `sha256` in the
formula before installing.

Published in [foldnine/homebrew-tap](https://github.com/foldnine/homebrew-tap)
as `Formula/aftergreen.rb`: `brew install foldnine/tap/aftergreen`.

## Files

| File | Purpose |
| --- | --- |
| `render-formula.sh` | Generates the formula from a release's `SHA256SUMS` |
| `aftergreen.rb` | The formula for the current release (v0.1.0) |

CI (`.github/workflows/install.yml`) installs the formula from a local tap on
macOS arm64 and Intel, runs `brew test` and `brew audit --strict`, and checks
that it matches the published `SHA256SUMS`.

## Updating the tap

For each release, regenerate the formula (see `docs/release.md`) and open a PR
to foldnine/homebrew-tap that replaces `Formula/aftergreen.rb` with it. The
tap's CI installs it on macOS and Linux before merge.
