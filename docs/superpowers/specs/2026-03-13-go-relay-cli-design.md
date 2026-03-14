# Go Relay CLI — Design Spec

> Historical design note: parts of this spec predate the Go-first product runtime. Current public contract is `ha-nova setup`, `ha-nova relay ...`, `ha-nova check-update`, and `ha-nova update`; raw `~/.config/ha-nova/relay` paths are migration-only shims.

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `scripts/relay.sh` (104-line Bash+curl+jq wrapper) with a single Go binary that runs on macOS, Windows, and Linux with zero external dependencies.

**Architecture:** Single-package Go CLI (`cli/`) with platform-specific keychain access via build tags. Distributed as pre-built binaries through GitHub Releases via goreleaser. macOS binaries signed and notarized via Apple Developer account.

**Tech Stack:** Go, gojq (jq-compatible filtering), go-keyring (Windows/Linux credential store), goreleaser (cross-compilation + releases)

---

## 1. Motivation

The current `relay.sh` requires Bash, curl, jq, and platform-specific credential CLIs (`security` on macOS). On Windows, this means installing Git Bash (300MB) + Node.js (100MB) + jq — an intimidating dependency chain for non-technical users.

A single Go binary (~5-8MB) eliminates all runtime dependencies. Users download one file and it works.

## 2. Scope

### In Scope

- Go binary replacing `scripts/relay.sh`
- Subcommands: `health`, `ws`, `core`, `jq`, `version`
- Platform-specific keychain: macOS (`security` CLI), Windows/Linux (`go-keyring`)
- goreleaser config for 5 targets (darwin/arm64, darwin/amd64, windows/amd64, linux/amd64, linux/arm64)
- macOS code signing + notarization
- Skill migration: all 32 `jq` calls across 9 files → `relay jq` calls (big-bang rewrite)
- Installer changes: `install.sh` / future `install.ps1` download binary from GitHub Releases
- `update.sh` changes: download binary instead of copying `relay.sh`
- Test updates for changed contract

### Out of Scope

- Windows onboarding wizard (separate project, follows after)
- Migrating `update.sh` or `version-check.sh` to Go (stay Bash)
- Changes to the Relay server on HA (stays Node.js)
- Changes to the onboarding wizard (stays Bash)
- New CLI flags beyond `-d` and `-r`

## 3. File Structure

```
cli/
├── main.go              # Entry point, command routing (~80 LOC)
├── relay.go             # HTTP client for health/ws/core (~120 LOC)
├── config.go            # Parse ~/.config/ha-nova/onboarding.env (~40 LOC)
├── keyring_darwin.go    # macOS: exec `security` CLI (~30 LOC)
├── keyring_windows.go   # Windows: go-keyring (~20 LOC)
├── keyring_linux.go     # Linux: go-keyring (~20 LOC)
├── version.go           # `version` subcommand + semver comparison for health endpoint (~60 LOC)
├── jq.go                # gojq stdin filter (~40 LOC)
├── go.mod
└── go.sum
```

Total: ~400-500 LOC Go.

## 4. CLI Contract

### Usage

```
relay <command> [args...]

Commands:
  health              GET /health + version check
  ws -d '{...}'       POST /ws with JSON payload
  core -d '{...}'     POST /core with JSON payload
  jq [-r] '<filter>'  Filter JSON from stdin (jq-compatible)
  version             Print binary version
```

### Behavior (identical to current relay.sh)

| Scenario | Behavior |
|----------|----------|
| Config file missing | stderr: `error: HA NOVA is not set up yet. Run: ha-nova setup`, exit 1 |
| Keychain token missing | stderr: `error: missing relay auth token (ha-nova.relay-auth-token)`, exit 1 |
| Health request fails | Nothing on stdout, exit 1 |
| Health OK | JSON on stdout, optionally followed by `RELAY OUTDATED` warning |
| ws/core OK | Raw JSON response on stdout, exit 0 |
| ws/core fails | exit 1 |
| jq filter error | stderr: parse error message, exit 1 |
| Timeouts | Connect: 5s, Total: 15s |

### HTTP Headers

All requests include:
- `Authorization: Bearer <token>`
- `Content-Type: application/json`

### Health Special Logic

1. `GET $RELAY_BASE_URL/health` (not POST)
2. Execute `~/.config/ha-nova/version-check` hook if executable exists (output appears before JSON)
3. Print health JSON response on stdout
4. Parse response JSON for `version` field
5. Load `version.json` (search: git root, then `~/.config/ha-nova/version.json`)
6. Semver comparison: if relay version < `min_relay_version`, print `RELAY OUTDATED` warning after JSON

### Payload Flag

Only `-d <json>` is supported for ws/core commands. This is the only flag skills use.

### jq Subcommand

Only `-r` (raw string output) is supported. This is the only jq flag skills use.

Supported filter patterns (all used in skills):
- `.data[]`, `.data.entities[]`
- `select(.field | startswith("..."))`, `select(.field | test("...";"i"))`
- `if .ok then .data else error("...") end`
- `{key1: .field1, key2: .field2}` (object construction)
- `.data.body`, `.data.unique_id` (nested field access)
- Pipe chains: `select(...) | {entity_id: .ei, name: .en}`

## 5. Config Loading

**File:** `~/.config/ha-nova/onboarding.env`

**Format:** `KEY=VALUE` pairs, optional single/double quotes around values.

**Required variable:** `RELAY_BASE_URL` (e.g., `http://192.168.1.5:8791`)

**Path resolution:** `$HOME/.config/ha-nova/onboarding.env` via `os.UserHomeDir()`.

## 6. Keychain Access

| Platform | File | Method | Compatibility |
|----------|------|--------|---------------|
| macOS | `keyring_darwin.go` | `exec.Command("security", "find-generic-password", "-a", user, "-s", "ha-nova.relay-auth-token", "-w")` | 100% compatible with existing Keychain entries written by onboarding scripts |
| Windows | `keyring_windows.go` | `go-keyring`: `keyring.Get("ha-nova.relay-auth-token", user)` | Uses Windows Credential Manager |
| Linux | `keyring_linux.go` | `go-keyring`: `keyring.Get("ha-nova.relay-auth-token", user)` | Uses Secret Service (GNOME Keyring / KWallet) |

macOS uses `security` CLI (not go-keyring) to maintain 100% backward compatibility with tokens written by the existing Bash onboarding scripts. No migration needed for existing macOS users.

## 7. Dependencies

| Module | Purpose | License |
|--------|---------|---------|
| `github.com/itchyny/gojq` | jq-compatible JSON filtering | MIT |
| `github.com/zalando/go-keyring` | Windows/Linux credential store | MIT |

No other dependencies. Standard library for HTTP, JSON, config parsing, and process execution.

## 8. Distribution

### goreleaser

**Config:** `.goreleaser.yml` in repo root. `builds[].dir` must point to `cli/` since Go source is not at the repo root.

**Targets:**
- `darwin/arm64` (macOS Apple Silicon)
- `darwin/amd64` (macOS Intel)
- `windows/amd64` (`relay.exe`)
- `linux/amd64`
- `linux/arm64`

**Release artifacts:** Binary per platform + SHA256 checksums.

### macOS Code Signing

Binary is signed with `codesign` and notarized with `notarytool` using the user's Apple Developer account. goreleaser supports this via `sign` hooks in the release config.

Result: No Gatekeeper warnings on macOS. Clean, trusted UX.

### Installer Integration

**`install.sh` (macOS):**
```bash
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac
curl -fsSL "https://github.com/markusleben/ha-nova/releases/latest/download/relay-${OS}-${ARCH}" \
  -o ~/.config/ha-nova/relay
chmod +x ~/.config/ha-nova/relay
```

**`install.ps1` (Windows, future):**
```powershell
Invoke-WebRequest -Uri "https://github.com/markusleben/ha-nova/releases/latest/download/relay-windows-amd64.exe" `
  -OutFile "$env:USERPROFILE\.config\ha-nova\relay.exe"
```

### Update Integration

`scripts/update.sh` replaces the `cp scripts/relay.sh` line with the same binary download logic as the installer.

## 9. Skill Migration

**Strategy:** Big-bang rewrite. All 32 occurrences across 9 files in one commit.

**Before:**
```bash
~/.config/ha-nova/relay ws -d '{"type":"entity_registry/list_for_display"}' \
  | jq '[.data.entities[] | select(.ei | startswith("automation."))]'
```

**After:**
```bash
~/.config/ha-nova/relay ws -d '{"type":"entity_registry/list_for_display"}' \
  | ~/.config/ha-nova/relay jq '[.data.entities[] | select(.ei | startswith("automation."))]'
```

**Files affected:**
- `skills/read/SKILL.md`
- `skills/write/SKILL.md`
- `skills/review/SKILL.md`
- `skills/entity-discovery/SKILL.md`
- `skills/helper/SKILL.md`
- `skills/fallback/SKILL.md`
- `skills/review/checks.md`
- `skills/ha-nova/relay-api.md`
- `skills/ha-nova/safe-refactoring.md`
- `skills/ha-nova/agents/resolve-agent.md` (example text, not a pipe)

**Also remove:** jq prerequisite check from `install.sh` and `scripts/onboarding/lib/ui.sh`.

## 10. Test Changes

| Test File | Change |
|-----------|--------|
| `tests/onboarding/relay-cli-contract.test.ts` | Update invocation from `bash scripts/relay.sh` to compiled binary path |
| `tests/skills/ha-nova-contract.test.ts` | Assert `cli/main.go` exists instead of `scripts/relay.sh`; update relay reference assertions from `jq` to `relay jq` |
| `tests/skills/ha-nova-skill-contract.test.ts` | Update any relay.sh existence assertions |
| `tests/onboarding/self-update-contract.test.ts` | Update assertions for relay deployment (binary download vs shell copy) |
| `tests/onboarding/installer-contract.test.ts` | Update assertions for jq prerequisite removal |
| Skill contract tests | Update `jq` pattern assertions to `relay jq` |

**No changes needed:** Doctor tests, setup tests, health endpoint tests (mock curl still works for onboarding probes which remain Bash).

## 11. Risks & Mitigations

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| gojq incompatibility with a skill filter | Low | All 32 filters are standard patterns; gojq has 99%+ jq compatibility |
| macOS Gatekeeper warning | Eliminated | Binary signed + notarized via Apple Developer account |
| Binary size too large | Low | Expected 5-8MB; gojq is ~3MB, go-keyring is tiny |
| Existing macOS Keychain entries break | None | macOS uses same `security` CLI as before |
| CI complexity from goreleaser | Low | goreleaser is mature; standard GitHub Actions integration |

## 12. What Changes for Users

| Platform | Before | After |
|----------|--------|-------|
| macOS | Needs: Node.js, jq, Bash | Needs: nothing (client-side) |
| Windows | Needs: Git Bash, Node.js, jq | Needs: nothing (client-side) |
| Linux | Not supported | Needs: nothing (client-side) |

> Note: The Relay **server** on Home Assistant still requires Node.js — it runs as an HA add-on, not on the user's machine. This table covers client-side dependencies only.

The relay CLI path (`~/.config/ha-nova/relay`) stays the same. Existing users see no difference after update — the binary is a transparent drop-in replacement.

## 13. Future Consideration

A `--jq` flag on `ws`/`core` commands (e.g., `relay ws -d '...' --jq '.data[]'`) would reduce two process invocations to one. Out of scope for this spec — can be added later without breaking changes since the pipe syntax remains valid.

## 14. Implementation Order

1. Go binary: build + unit test (isolated, no repo changes)
2. goreleaser setup + test release
3. macOS code signing + notarization
4. Skills rewrite: `jq` → `relay jq` (big-bang commit)
5. Installer/update script changes (binary download)
6. Test updates
7. Remove jq prerequisite checks
8. Release
