# Homebrew formula

`aftergreen.rb` installs the prebuilt release binary for macOS and Linux
(amd64, arm64). Homebrew checks each archive against the `sha256` in the
formula before installing.

**Not published yet.** The install command `brew install foldnine/tap/aftergreen`
works only after the tap repository exists. Don't add it to the README, docs
or website before then.

## Files

| File | Purpose |
| --- | --- |
| `render-formula.sh` | Generates the formula from a release's `SHA256SUMS` |
| `aftergreen.rb` | The formula for the current release (v0.1.0) |

CI (`.github/workflows/install.yml`) installs the formula from a local tap on
macOS arm64 and Intel, runs `brew test` and `brew audit --strict`, and checks
that it matches the published `SHA256SUMS`.

## Publishing the tap (owner)

1. Create the public repository `foldnine/homebrew-tap` (Homebrew maps
   `foldnine/tap` to it).
2. Add `Formula/aftergreen.rb` from this directory, plus a short README and
   the Apache-2.0 license.
3. Check from a clean machine: `brew install foldnine/tap/aftergreen && aftergreen version`.
4. Then add the Homebrew command to `README.md`, `docs/install.md` and the
   website's Install section.

For each release, regenerate the formula (see `docs/release.md`) and copy it to
the tap.
