# Installing AfterGreen

The current release is **v0.1.0**, a pre-release. It ships prebuilt, static
binaries for Linux, macOS and Windows on amd64 and arm64. You don't need Go.

| Method | Platforms |
| --- | --- |
| [Install script](#install-script-linux-and-macos) | Linux, macOS |
| [PowerShell download](#windows) | Windows |
| [Direct download](#direct-downloads) | all |
| [GitHub Action](#github-actions) | GitHub-hosted and self-hosted runners |
| [From source](#from-source) | anywhere Go 1.26+ runs |

Every method is exercised by the [Install workflow](../.github/workflows/install.yml)
on Linux (amd64, arm64, Alpine), macOS (Apple silicon, Intel) and Windows
(amd64, arm64), and again every week against the published release.

## Install script (Linux and macOS)

```bash
curl -fsSL https://aftergreen.foldnine.dev/install.sh | sh
```

What [`install.sh`](../scripts/install.sh) does:

1. Detects the OS (`linux`, `darwin`) and CPU (`amd64`, `arm64`). A Rosetta
   shell on Apple silicon gets the native arm64 build.
2. Downloads the archive and `SHA256SUMS` from the GitHub release over HTTPS.
3. Verifies the archive's SHA256 against `SHA256SUMS`. A mismatch, a missing
   entry or no available hash tool (`sha256sum`, `shasum` or `openssl`) stops
   the install before anything is written.
4. Copies the binary into `~/.local/bin`, checks that it reports the expected
   version and only then moves it into place, so an existing install is
   replaced completely or not at all.

It never uses `sudo`, never edits shell profiles and writes nothing outside the
install directory and a temporary directory that it removes. It needs only
`sh`, `curl` or `wget`, `tar` and a SHA256 tool, all present on stock macOS and
mainstream Linux distributions, including Alpine.

Options:

| Flag | Environment variable | Default |
| --- | --- | --- |
| `--version vX.Y.Z` | `AFTERGREEN_VERSION` | the newest published release, pre-releases included |
| `--bin-dir DIR` | `AFTERGREEN_INSTALL_DIR` | `$HOME/.local/bin` |

```bash
# A specific version into another directory
curl -fsSL https://aftergreen.foldnine.dev/install.sh | sh -s -- --version v0.1.0 --bin-dir "$HOME/bin"
```

If the install directory is not on your `PATH`, the script says so and prints
the line to add. Re-run the script to upgrade. To uninstall, delete the binary
(`rm ~/.local/bin/aftergreen`).

To read the script before running it:

```bash
curl -fsSLO https://aftergreen.foldnine.dev/install.sh
less install.sh
sh install.sh
```

The script on the website is a byte-for-byte copy of
[`scripts/install.sh`](../scripts/install.sh); CI fails if they differ.

**What the checksum does and does not prove.** It proves the archive is the one
listed in the release's `SHA256SUMS`. Both files come from the same GitHub
release, so it does not protect against a compromised release. For that, check
the build provenance attestation (below), which ties each archive to the
release workflow run that built it.

## Windows

PowerShell, for amd64. On Arm, replace `amd64` with `arm64`.

```powershell
$v = "v0.1.0"; $a = "aftergreen_${v}_windows_amd64.zip"
$base = "https://github.com/foldnine/aftergreen/releases/download/$v"
Invoke-WebRequest "$base/$a" -OutFile $a
Invoke-WebRequest "$base/SHA256SUMS" -OutFile SHA256SUMS
$expected = ((Get-Content SHA256SUMS) | Where-Object { $_ -like "*  $a" }).Split(" ")[0]
if ((Get-FileHash $a -Algorithm SHA256).Hash.ToLower() -ne $expected) { throw "checksum mismatch" }
Expand-Archive $a -DestinationPath "$env:LOCALAPPDATA\aftergreen" -Force
& "$env:LOCALAPPDATA\aftergreen\aftergreen.exe" version
```

Add `%LOCALAPPDATA%\aftergreen` to your user `PATH` to run `aftergreen` from any
directory. Upgrade by running the block again with the new version. Uninstall
by deleting that directory.

## Direct downloads

From the [v0.1.0 release](https://github.com/foldnine/aftergreen/releases/tag/v0.1.0):

| OS | amd64 | arm64 |
| --- | --- | --- |
| Linux | [tar.gz](https://github.com/foldnine/aftergreen/releases/download/v0.1.0/aftergreen_v0.1.0_linux_amd64.tar.gz) | [tar.gz](https://github.com/foldnine/aftergreen/releases/download/v0.1.0/aftergreen_v0.1.0_linux_arm64.tar.gz) |
| macOS | [tar.gz](https://github.com/foldnine/aftergreen/releases/download/v0.1.0/aftergreen_v0.1.0_darwin_amd64.tar.gz) | [tar.gz](https://github.com/foldnine/aftergreen/releases/download/v0.1.0/aftergreen_v0.1.0_darwin_arm64.tar.gz) (Apple silicon) |
| Windows | [zip](https://github.com/foldnine/aftergreen/releases/download/v0.1.0/aftergreen_v0.1.0_windows_amd64.zip) | [zip](https://github.com/foldnine/aftergreen/releases/download/v0.1.0/aftergreen_v0.1.0_windows_arm64.zip) |

Each archive holds the `aftergreen` binary (`aftergreen.exe` on Windows),
`LICENSE` and `README.md`. Verify before running:

```bash
# Linux
sha256sum --ignore-missing -c SHA256SUMS
# macOS
grep aftergreen_v0.1.0_darwin_arm64.tar.gz SHA256SUMS | shasum -a 256 -c
# Build provenance (any OS, needs the GitHub CLI)
gh attestation verify aftergreen_v0.1.0_linux_amd64.tar.gz --repo foldnine/aftergreen
```

Then extract and put the binary on your `PATH`:

```bash
tar -xzf aftergreen_v0.1.0_linux_amd64.tar.gz aftergreen
mkdir -p ~/.local/bin && mv aftergreen ~/.local/bin/
```

The macOS binaries are not notarized by Apple. If macOS refuses to run one that
you downloaded with a browser, remove the quarantine flag:
`xattr -d com.apple.quarantine aftergreen`. Downloads made by the install
script or `curl` are not quarantined.

## GitHub Actions

Nothing to install. The [AfterGreen Action](https://github.com/foldnine/aftergreen-action)
runs the HTTP check inside a workflow:

```yaml
- name: Verify the deployed build is live
  uses: foldnine/aftergreen-action@v1.0.1
  with:
    url: https://app.example.com/version
    expected-status: '200'
    contains: ${{ github.sha }}
```

`v1.0.1` is an immutable release. `v1` follows the latest `v1.x.y`. The Action
supports HTTP checks only; for PostgreSQL in CI, install the CLI with the
install script in a `run:` step.

## Homebrew

Not available yet. A tested formula is ready in
[`packaging/homebrew/`](../packaging/homebrew/) and will be published to
`foldnine/homebrew-tap`. This section will show the install command once
`brew install foldnine/tap/aftergreen` works.

## From source

```bash
go install github.com/foldnine/aftergreen/cmd/aftergreen@v0.1.0
```

Needs Go 1.26+. A binary built this way reports `aftergreen dev` from
`aftergreen version`, because release builds inject the version at build time.

## After installing

```bash
aftergreen version
aftergreen http --url https://example.com --status 200 --contains "Example Domain"
```

The second command should print `VERIFIED  HTTP outcome (HTTP 200)` and exit
`0`. Continue with the [Quick start](../README.md#quick-start).
