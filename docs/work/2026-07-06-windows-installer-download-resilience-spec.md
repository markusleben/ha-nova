# Installer & Update Download Resilience Spec

## Problem

The documented Windows one-liner can load `install.ps1` from `raw.githubusercontent.com`
but then fail during the release bundle download with a raw `Invoke-WebRequest`
connection exception. The README command must be robust on fresh Windows systems
and must not expose an unhandled PowerShell stack trace for the normal asset
download path.

The same fragility exists on the two other download surfaces users hit in the
shipped product:

- `ha-nova update` (all platforms) downloads the release bundle through the
  relay API `httpClient`, which has a hard 15-second TOTAL response timeout.
  Any bundle download slower than 15 seconds aborts the update. The path has
  no retry, no re-download on checksum mismatch, and no User-Agent.
- `install.sh` (macOS/Linux) uses bare `curl -fsSL` with no retries, no
  connect timeout, downloads the bundle before the checksum, and surfaces raw
  curl errors instead of an HA NOVA message.

## Scope

- Harden `install.ps1` release asset downloads.
- Harden the Go bundle download in `stageBundle` (`cli/bundle_apply.go`) with
  a dedicated asset download client; the relay API client and release
  metadata lookup (`fetchLatestRelease` with its cache fallback) stay unchanged.
- Harden `install.sh` downloads to parity with `install.ps1`.
- Preserve checksum verification and bundle metadata validation everywhere.
- Keep the public commands and the `HA_NOVA_BUNDLE_URL` /
  `HA_NOVA_BUNDLE_SHA256_URL` overrides unchanged.
- Add focused installer contract and behavior tests.

## Design

- Initialize modern TLS protocols best-effort before GitHub API or asset calls.
- Use consistent download headers including a User-Agent.
- Keep progress suppression scoped to file downloads.
- Try multiple native Windows download transports before failing:
  1. `Invoke-WebRequest` with `-UseBasicParsing` when supported.
  2. .NET `HttpClient`.
  3. BITS transfer when available.
- Download the checksum first, then retry the ZIP by transport until the SHA256
  matches. A proxy HTML page, zero-byte file, partial ZIP, or captive-portal
  body from the first transport must not prevent trying the next transport.
- If all transports fail, fail with one HA NOVA error message that includes the
  asset URL and all attempted transport errors instead of leaking a raw cmdlet stack.
- Keep a test-only export guard so PowerShell behavior tests can dot-source the
  installer functions without running the full installer.

### Go update path (`cli/asset_download.go`)

- Dedicated `assetHTTPClient` with NO total timeout: a slow but progressing
  bundle download must never abort. Stalls stay bounded via dial, TLS
  handshake, and response-header timeouts.
- `downloadAssetFile`: streams to disk, sends `ha-nova-installer` User-Agent
  and octet-stream Accept, retries transient failures (network errors,
  HTTP 5xx, HTTP 429) with a short backoff, fails fast on other 4xx. A
  zero-byte result counts as a failed attempt.
- `stageBundle` downloads the checksum FIRST, then the bundle verified
  against it; a checksum mismatch discards the file and re-downloads before
  failing. Attempt errors are aggregated into one message.
- Retry pacing lives in a package variable so tests run without real sleeps.

### install.sh (macOS/Linux)

- One `fetch_url` download helper: `--fail --silent --show-error --location`
  plus `--connect-timeout` and `--retry` with `--retry-all-errors` when the
  local curl supports it (probed once; older curl falls back to plain retry).
- Checksum downloads first, then the bundle; on SHA256 mismatch the bundle is
  re-downloaded once before failing.
- Download failures produce one HA NOVA error message that names the asset
  URL instead of a raw curl abort.
- Test-only export guard (`HA_NOVA_INSTALLER_TEST_EXPORT=1`) so behavior
  tests can source the installer functions without running the installer.

## Verification

- `git diff --check`
- focused Vitest installer contracts (`installer-contract`,
  `windows-installer-contract`, `release-contract`)
- PowerShell parser check for `install.ps1`; `bash -n install.sh`
- behavior test (Windows): hash-mismatched first transport retries and
  succeeds with the next transport
- behavior test (Unix): flaky-then-healthy download server; checksum-mismatch
  first response then clean re-download succeeds
- `gofmt -l cli` clean and `go test ./...` in `cli/`, including new tests:
  transient 5xx then success, checksum mismatch then re-download success,
  hard 404 fails without retries, asset client has no total timeout
