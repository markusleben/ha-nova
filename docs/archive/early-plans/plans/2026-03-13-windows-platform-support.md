# Windows Platform Support Implementation Plan

> Historical planning note: superseded by the Go-first runtime cutover. Current public contract is native `install.ps1` / `install.sh`, `ha-nova setup`, `ha-nova relay ...`, and `ha-nova update` without Git Bash in the end-user path.

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make HA NOVA install and run on Windows (Git Bash) with the same easy onboarding experience as macOS.

**Architecture:** Platform abstraction via 6-function API (`platform/{macos,windows}.sh`), auto-detecting dispatcher, inline credential dispatch in standalone `relay.sh`. Git Bash is the Windows prerequisite — no native PowerShell rewrite. DPAPI-encrypted files replace macOS Keychain. All changes are backwards-compatible for existing macOS users.

**Tech Stack:** Bash (Git Bash on Windows), PowerShell 5.1+ (DPAPI only), Vitest, GitHub Actions `windows-latest`

**Branch:** `feat/windows-support` (test branch — not merged until everything works cleanly)

**UX Constraint:** Windows onboarding must feel identical to macOS. Same one-liner install, same interactive wizard, same number of steps. Zero extra prerequisites beyond Git for Windows (which includes Git Bash, curl, sed, grep).

---

## Chunk 1: Foundation (no Windows machine needed)

### Task 1: Create branch and add `.gitattributes`

**Why first:** Without LF enforcement, every `.sh` file breaks on Windows clone (`\r: command not found`). This is a Phase 0 showstopper.

**Files:**
- Create: `.gitattributes`

- [ ] **Step 1: Create the feature branch**

```bash
git checkout -b feat/windows-support
```

- [ ] **Step 2: Create `.gitattributes`**

```
# Force LF line endings for all shell scripts and hooks
*.sh text eol=lf
hooks/session-start text eol=lf
scripts/onboarding/bin/ha-nova text eol=lf
```

- [ ] **Step 3: Verify no CRLF files exist in repo**

Run: `git ls-files --eol | grep 'w/crlf'`
Expected: No output (no working-tree files with CRLF)

- [ ] **Step 4: Commit**

```bash
git add .gitattributes
git commit -m "chore: add .gitattributes to enforce LF for shell scripts"
```

---

### Task 2: Create `safe_sed_i` helper and fix all `sed -i ''` occurrences

**Why:** `sed -i ''` is macOS-only. GNU sed (Git Bash, Linux) requires `sed -i` without the empty string. 7 occurrences across 2 dev scripts.

**Files:**
- Modify: `scripts/dev-sync.sh` (4 occurrences)
- Modify: `scripts/bump-version.sh` (2 occurrences)

- [ ] **Step 1: Add `safe_sed_i` function to `scripts/dev-sync.sh`**

Add near the top (after `set -euo pipefail`):

```bash
safe_sed_i() {
  if [[ "$(uname -s)" == "Darwin" ]]; then
    sed -i '' "$@"
  else
    sed -i "$@"
  fi
}
```

Replace all 4 `sed -i '' ...` calls with `safe_sed_i ...` (drop the `''`).

- [ ] **Step 2: Add `safe_sed_i` function to `scripts/bump-version.sh`**

Same function, replace all 2 `sed -i '' ...` calls.

- [ ] **Step 3: Test locally**

Run: `bash scripts/bump-version.sh 0.1.11` (same version = no-op)
Expected: No errors, no changes

- [ ] **Step 4: Commit**

```bash
git add scripts/dev-sync.sh scripts/bump-version.sh
git commit -m "fix: make sed -i portable across macOS/GNU sed"
```

---

### Task 3: Add `copy_to_clipboard` as 6th platform function

**Why:** `pbcopy` is used 8x in `macos-lib.sh` and 2x in `lib/ui.sh` — all outside the platform API. Windows equivalent is `clip.exe`.

**Files:**
- Modify: `scripts/onboarding/platform/macos.sh`
- Modify: `scripts/onboarding/lib/ui.sh` (2 occurrences)
- Modify: `scripts/onboarding/macos-lib.sh` (8 occurrences, but these use `command -v pbcopy` guard — replace with platform function)

- [ ] **Step 1: Add `copy_to_clipboard` to `platform/macos.sh`**

```bash
copy_to_clipboard() {
  local value="$1"
  if command -v pbcopy >/dev/null 2>&1; then
    printf '%s' "$value" | pbcopy
    return 0
  fi
  return 1
}
```

- [ ] **Step 2: Replace `pbcopy` calls in `lib/ui.sh`**

In `wait_for_enter_or_copy()` (line 234), replace:
```bash
      if command -v pbcopy >/dev/null 2>&1; then
        printf '%s' "$secret" | pbcopy
```
with:
```bash
      if copy_to_clipboard "$secret"; then
```

- [ ] **Step 3: Replace `pbcopy` calls in `macos-lib.sh`**

Replace all 6 patterns of:
```bash
if command -v pbcopy >/dev/null 2>&1; then
  printf '%s' "$relay_auth_token" | pbcopy
```
with:
```bash
if copy_to_clipboard "$relay_auth_token"; then
```

And the 2 patterns with `"$token"` variant similarly.

Also replace 2 occurrences at lines 794-795 in the retry prompt section.

- [ ] **Step 4: Test the macOS onboarding still works**

Run: `npm test -- tests/onboarding/setup-fresh-install.test.ts`
Expected: All tests pass (macOS)

- [ ] **Step 5: Commit**

```bash
git add scripts/onboarding/platform/macos.sh scripts/onboarding/lib/ui.sh scripts/onboarding/macos-lib.sh
git commit -m "refactor: extract clipboard to platform API (copy_to_clipboard)"
```

---

### Task 4: Create platform dispatcher

**Why:** Central dispatch that auto-detects the platform and sources the right `platform/*.sh`. Supports `HA_NOVA_PLATFORM_OVERRIDE` env var for testing.

**Files:**
- Create: `scripts/onboarding/lib/platform.sh`

- [ ] **Step 1: Create the dispatcher**

```bash
#!/usr/bin/env bash
# Platform dispatcher — auto-detects OS and sources the matching platform module.
# Set HA_NOVA_PLATFORM_OVERRIDE=windows|macos to force a platform (testing only).
set -euo pipefail

_ha_nova_detect_platform() {
  if [[ -n "${HA_NOVA_PLATFORM_OVERRIDE:-}" ]]; then
    echo "${HA_NOVA_PLATFORM_OVERRIDE}"
    return
  fi
  case "$(uname -s)" in
    Darwin)           echo "macos" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *)                echo "unsupported" ;;
  esac
}

_HA_NOVA_PLATFORM="$(_ha_nova_detect_platform)"

_platform_file="${BASH_SOURCE[0]%/*}/../platform/${_HA_NOVA_PLATFORM}.sh"

if [[ ! -f "$_platform_file" ]]; then
  echo "[ha-nova] Unsupported platform: $(uname -s)" >&2
  echo "[ha-nova] HA NOVA supports macOS and Windows (Git Bash)." >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$_platform_file"
```

- [ ] **Step 2: Commit**

```bash
git add scripts/onboarding/lib/platform.sh
git commit -m "feat: add platform dispatcher with auto-detection and test override"
```

---

### Task 5: Create `platform/windows.sh`

**Why:** The 6-function Windows platform implementation. Uses DPAPI via PowerShell for credentials, `cmd.exe /c start` for browser, `clip.exe` for clipboard.

**Files:**
- Create: `scripts/onboarding/platform/windows.sh`

- [ ] **Step 1: Create the Windows platform module**

```bash
#!/usr/bin/env bash
# Platform-specific functions for Windows (Git Bash / MSYS2).
# Sourced by lib/platform.sh — do not execute directly.
set -euo pipefail

_win_cred_file() {
  local service="$1"
  # Store credentials alongside config — same dir, predictable location.
  echo "${HOME}/.config/ha-nova/.${service}.enc"
}

require_platform() {
  if [[ -z "${HA_NOVA_PLATFORM_OVERRIDE:-}" ]]; then
    case "$(uname -s)" in
      MINGW*|MSYS*|CYGWIN*) ;;
      *) die "This script requires Windows (Git Bash). Detected: $(uname -s)" ;;
    esac
  fi
}

store_keychain_secret() {
  local service="$1"
  local value="$2"
  local cred_file
  cred_file="$(_win_cred_file "$service")"

  mkdir -p "$(dirname "$cred_file")"

  local win_path
  win_path="$(cygpath -w "$cred_file" 2>/dev/null || echo "$cred_file")"

  # Use base64 intermediary to prevent PowerShell injection via token values.
  local b64
  b64="$(printf '%s' "$value" | base64)"

  powershell.exe -NoProfile -NonInteractive -Command "
    \$bytes = [Convert]::FromBase64String('${b64}')
    \$plain = [System.Text.Encoding]::UTF8.GetString(\$bytes)
    \$ss = ConvertTo-SecureString -String \$plain -AsPlainText -Force
    ConvertFrom-SecureString -SecureString \$ss | Out-File -FilePath '${win_path}' -Encoding UTF8 -NoNewline
  " 2>/dev/null || {
    # Fallback: store as-is if PowerShell/DPAPI unavailable (CI mock scenario)
    printf '%s' "$value" > "$cred_file"
  }
  chmod 600 "$cred_file"
}

read_keychain_secret() {
  local service="$1"
  local cred_file
  cred_file="$(_win_cred_file "$service")"

  [[ -f "$cred_file" ]] || { echo ""; return 0; }

  local win_path
  win_path="$(cygpath -w "$cred_file" 2>/dev/null || echo "$cred_file")"

  local token
  token="$(powershell.exe -NoProfile -NonInteractive -Command "
    \$encrypted = Get-Content -Path '${win_path}' -Raw
    \$ss = ConvertTo-SecureString -String \$encrypted
    [System.Runtime.InteropServices.Marshal]::PtrToStringAuto(
      [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR(\$ss)
    )
  " 2>/dev/null | tr -d '\r')" || {
    # Fallback: read plaintext (CI mock scenario)
    token="$(cat "$cred_file" 2>/dev/null)"
  }

  echo "$token"
}

delete_keychain_secret_if_exists() {
  local service="$1"
  local cred_file
  cred_file="$(_win_cred_file "$service")"
  rm -f "$cred_file" 2>/dev/null || true
}

open_browser() {
  local url="$1"
  # Skip browser launch when stdin is piped (non-interactive / test mode).
  if [[ ! -t 0 ]]; then return 0; fi
  cmd.exe /c start "" "$url" 2>/dev/null || true
}

copy_to_clipboard() {
  local value="$1"
  if command -v clip.exe >/dev/null 2>&1; then
    printf '%s' "$value" | clip.exe
    return 0
  fi
  return 1
}
```

- [ ] **Step 2: Commit**

```bash
git add scripts/onboarding/platform/windows.sh
git commit -m "feat: add Windows platform module (DPAPI credentials, browser, clipboard)"
```

---

### Task 6: Wire `macos-lib.sh` to use platform dispatcher

**Why:** Switch from hardcoded `source platform/macos.sh` to the auto-detecting dispatcher. This is the single-line change that enables multi-platform support in the onboarding flow.

**Files:**
- Modify: `scripts/onboarding/macos-lib.sh` (line 10)

- [ ] **Step 1: Replace the source line**

Change line 10 from:
```bash
source "${SCRIPT_DIR}/platform/macos.sh"
```
to:
```bash
source "${SCRIPT_DIR}/lib/platform.sh"
```

- [ ] **Step 2: Fix hardcoded macOS strings in `macos-lib.sh`**

Line 537: `"this Mac"` → `"this computer"`
Line 779: `"this Mac"` → `"this computer"`
Line 70: `"Missing relay auth token in Keychain"` → `"Missing relay auth token. Run setup first."`
Line 104: `"Keychain token found"` → `"Auth token found"`
Line 106: `"Keychain token missing"` → `"Auth token missing"`
Line 560: `"Using existing relay auth token from Keychain."` → `"Using existing relay auth token."`
Line 566: `"Saved to Keychain."` → `"Saved securely."`
Line 580: `"Saved to Keychain — safe even if you quit now."` → `"Saved securely — safe even if you quit now."`
Line 816: `"Token stored in macOS Keychain"` → `"Token stored securely"`
Line 868: `require_cmd security` → remove this line (platform module handles its own deps)
Line 908: `require_cmd security` → remove this line
Line 920: `require_cmd security` → remove this line
Line 1001: `require_cmd security` → remove this line

- [ ] **Step 3: Test macOS still works**

Run: `npm test -- tests/onboarding/`
Expected: All existing tests pass unchanged

- [ ] **Step 4: Commit**

```bash
git add scripts/onboarding/macos-lib.sh
git commit -m "refactor: wire onboarding to platform dispatcher instead of hardcoded macOS"
```

---

### Task 7: Add platform dispatch to `relay.sh` (inline)

**Why:** `relay.sh` is standalone-deployed as `~/.config/ha-nova/relay` — it can't source library files. The credential read must be inlined. This is the critical path: 52+ skill invocations depend on it.

**Files:**
- Modify: `scripts/relay.sh` (lines 12-14)

- [ ] **Step 1: Replace the hardcoded Keychain call with inline platform dispatch**

Replace lines 12-14:
```bash
RELAY_AUTH_TOKEN="$(security find-generic-password \
  -a "$USER" -s "ha-nova.relay-auth-token" -w 2>/dev/null)" \
  || { echo "error: missing Keychain token ha-nova.relay-auth-token" >&2; exit 1; }
```

with:
```bash
# Platform-dispatched credential read (relay.sh is standalone — no library sourcing)
_relay_service="ha-nova.relay-auth-token"
case "$(uname -s)" in
  Darwin)
    RELAY_AUTH_TOKEN="$(security find-generic-password \
      -a "$USER" -s "$_relay_service" -w 2>/dev/null)" || true
    ;;
  MINGW*|MSYS*|CYGWIN*)
    _cred_file="${HOME}/.config/ha-nova/.${_relay_service}.enc"
    if [[ -f "$_cred_file" ]]; then
      _win_path="$(cygpath -w "$_cred_file" 2>/dev/null || echo "$_cred_file")"
      RELAY_AUTH_TOKEN="$(powershell.exe -NoProfile -NonInteractive -Command "
        \$encrypted = Get-Content -Path '${_win_path}' -Raw
        \$ss = ConvertTo-SecureString -String \$encrypted
        [System.Runtime.InteropServices.Marshal]::PtrToStringAuto(
          [System.Runtime.InteropServices.Marshal]::SecureStringToBSTR(\$ss)
        )
      " 2>/dev/null | tr -d '\r')" || {
        RELAY_AUTH_TOKEN="$(cat "$_cred_file" 2>/dev/null)" || true
      }
    fi
    ;;
  *)
    echo "error: unsupported platform: $(uname -s)" >&2; exit 1
    ;;
esac

if [[ -z "${RELAY_AUTH_TOKEN:-}" ]]; then
  echo "error: missing credential ($_relay_service). Run: ha-nova setup" >&2
  exit 1
fi
```

- [ ] **Step 2: Test relay CLI still works on macOS**

Run: `npm test -- tests/onboarding/relay-cli-contract.test.ts`
Expected: All tests pass

- [ ] **Step 3: Commit**

```bash
git add scripts/relay.sh
git commit -m "feat: add platform-dispatched credential read to relay CLI"
```

---

### Task 8: Add platform dispatch to `uninstall.sh`

**Why:** `uninstall.sh` calls `security` directly on lines 85, 121-122, bypassing the platform API.

**Files:**
- Modify: `scripts/onboarding/uninstall.sh`

- [ ] **Step 1: Replace hardcoded `security` calls**

Replace lines 79-94 (the entire relay probe block) with:

```bash
# Probe relay BEFORE deleting config/token (platform-dispatched)
relay_still_running="0"
relay_base_url=""
config_file="${HOME}/.config/ha-nova/onboarding.env"
if [[ -f "$config_file" ]]; then
  relay_base_url="$(grep -E '^RELAY_BASE_URL=' "$config_file" 2>/dev/null | head -1 | sed "s/^RELAY_BASE_URL=//" | tr -d "'" | tr -d '"' || true)"
fi

relay_token=""
case "$(uname -s)" in
  Darwin)
    relay_token="$(security find-generic-password -s "ha-nova.relay-auth-token" -w 2>/dev/null || true)"
    ;;
  MINGW*|MSYS*|CYGWIN*)
    # DPAPI-encrypted file — can't read without PowerShell. Just check if relay is up without auth.
    if [[ -n "$relay_base_url" ]]; then
      http_code="$(curl -sS --connect-timeout 2 --max-time 4 \
        -o /dev/null -w "%{http_code}" \
        "${relay_base_url%/}/health" 2>/dev/null || true)"
      if [[ "$http_code" == "200" || "$http_code" == "401" ]]; then
        relay_still_running="1"
      fi
    fi
    ;;
esac

if [[ -z "$relay_token" && "$relay_still_running" == "0" && -n "$relay_base_url" ]]; then
  : # Skip authenticated probe — no token available
elif [[ -n "$relay_base_url" && -n "$relay_token" ]]; then
  http_code="$(curl -sS --connect-timeout 2 --max-time 4 \
    -H "Authorization: Bearer ${relay_token}" \
    -o /dev/null -w "%{http_code}" \
    "${relay_base_url%/}/health" 2>/dev/null || true)"
  if [[ "$http_code" == "200" ]]; then
    relay_still_running="1"
  fi
fi
```

Replace lines 120-124 (Keychain removal section):
```bash
if security find-generic-password -s "ha-nova.relay-auth-token" >/dev/null 2>&1; then
  security delete-generic-password -s "ha-nova.relay-auth-token" >/dev/null 2>&1 || true
  log "Removed Keychain entry: ha-nova.relay-auth-token"
  removed=$((removed + 1))
fi
```
with:
```bash
# Remove stored credential (platform-dispatched)
case "$(uname -s)" in
  Darwin)
    if security find-generic-password -s "ha-nova.relay-auth-token" >/dev/null 2>&1; then
      security delete-generic-password -s "ha-nova.relay-auth-token" >/dev/null 2>&1 || true
      log "Removed Keychain entry: ha-nova.relay-auth-token"
      removed=$((removed + 1))
    fi
    ;;
  MINGW*|MSYS*|CYGWIN*)
    _cred_file="${HOME}/.config/ha-nova/.ha-nova.relay-auth-token.enc"
    if [[ -f "$_cred_file" ]]; then
      rm -f "$_cred_file"
      log "Removed credential file: $_cred_file"
      removed=$((removed + 1))
    fi
    ;;
esac
```

Also update the banner text (line 61) from `"Keychain entry"` to `"Stored credentials"`.

- [ ] **Step 2: Commit**

```bash
git add scripts/onboarding/uninstall.sh
git commit -m "feat: add platform dispatch to uninstall credential cleanup"
```

---

### Task 9: Platform-guard `arp -an` and move `dns-sd` discovery

**Why:** `arp -an` is BSD-specific (GNU/Windows uses different flags). `dns-sd` is macOS-only. Both are in `lib/relay.sh` which is supposed to be platform-independent.

**Files:**
- Modify: `scripts/onboarding/lib/relay.sh` (lines 187-193)

- [ ] **Step 1: Add platform guard to `arp` call**

Replace lines 187-193:
```bash
  if command -v arp >/dev/null 2>&1; then
    arp_candidates="$(
      arp -an 2>/dev/null \
        | sed -nE 's/.*\(([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)\).*/\1/p' \
        | head -n 4
    )"
  fi
```
with:
```bash
  if command -v arp >/dev/null 2>&1; then
    case "$(uname -s)" in
      Darwin)
        arp_candidates="$(
          arp -an 2>/dev/null \
            | sed -nE 's/.*\(([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)\).*/\1/p' \
            | head -n 4
        )" ;;
      *)
        arp_candidates="$(
          arp -a 2>/dev/null \
            | sed -nE 's/.*[[:space:]]([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)[[:space:]].*/\1/p' \
            | head -n 4
        )" ;;
    esac
  fi
```

- [ ] **Step 2: Fix "from this Mac" string in `lib/relay.sh`**

Line 259: `"reachable from this Mac"` → `"reachable from this computer"`

- [ ] **Step 3: Commit**

```bash
git add scripts/onboarding/lib/relay.sh
git commit -m "fix: platform-guard arp flags and fix hardcoded macOS strings"
```

---

### Task 10: Rename onboarding entry points (drop `macos-` prefix)

**Why:** The setup wizard is now platform-agnostic. The `macos-` prefix is misleading and blocks Windows users conceptually. We keep the old names as aliases for backwards compat.

**Files:**
- Create: `scripts/onboarding/setup.sh` (copy of `macos-setup.sh` with updated source)
- Create: `scripts/onboarding/doctor.sh` (copy of `macos-doctor.sh` with updated source)
- Modify: `scripts/onboarding/bin/ha-nova` (route to new names)
- Modify: `package.json` (add generic npm scripts)

- [ ] **Step 1: Create `setup.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/macos-lib.sh"

run_setup "$@"
```

Note: Still sources `macos-lib.sh` (which now uses the platform dispatcher). We rename `macos-lib.sh` → `onboarding-lib.sh` in a later task to keep this commit minimal.

- [ ] **Step 2: Create `doctor.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/macos-lib.sh"

run_doctor "$@"
```

- [ ] **Step 3: Update `bin/ha-nova` to use new entry points**

Change line 28:
```bash
    exec bash "${SCRIPT_DIR}/macos-setup.sh" "${setup_args[@]+"${setup_args[@]}"}"
```
to:
```bash
    exec bash "${SCRIPT_DIR}/setup.sh" "${setup_args[@]+"${setup_args[@]}"}"
```

Change line 32:
```bash
    exec bash "${SCRIPT_DIR}/macos-doctor.sh" "$@"
```
to:
```bash
    exec bash "${SCRIPT_DIR}/doctor.sh" "$@"
```

- [ ] **Step 4: Add generic npm scripts to `package.json`**

Add alongside existing `onboarding:macos` entries (keep old ones as aliases):
```json
"onboarding": "bash scripts/onboarding/setup.sh",
"onboarding:doctor": "bash scripts/onboarding/doctor.sh",
```

The existing `onboarding:macos` scripts stay — backwards compat for cached skills.

- [ ] **Step 5: Test**

Run: `npm test -- tests/onboarding/`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add scripts/onboarding/setup.sh scripts/onboarding/doctor.sh scripts/onboarding/bin/ha-nova package.json
git commit -m "feat: add platform-agnostic onboarding entry points"
```

---

### Task 11: Update `install.sh` — remove macOS gate

**Why:** The installer currently hard-rejects non-macOS on line 115. Replace with platform detection that accepts macOS and Windows (Git Bash).

**Files:**
- Modify: `install.sh` (lines 114-118, line 168-173)

- [ ] **Step 1: Replace the platform gate**

Replace lines 114-118:
```bash
  # macOS only (for now)
  if [[ "$(uname -s)" != "Darwin" ]]; then
    fail "HA NOVA currently supports macOS only."
  fi
  info "macOS detected"
```
with:
```bash
  case "$(uname -s)" in
    Darwin)
      info "macOS detected"
      ;;
    MINGW*|MSYS*|CYGWIN*)
      info "Windows (Git Bash) detected"
      # Verify jq is available (not bundled with Git Bash)
      if ! require_cmd jq; then
        echo ""
        echo "  [!!] jq not found."
        echo ""
        echo "      HA NOVA needs jq for JSON processing."
        echo "      Install it:"
        echo "        winget install jqlang.jq"
        echo "      or: scoop install jq"
        echo "      or: choco install jq"
        echo ""
        echo "      After installing, close this terminal, open a new one,"
        echo "      and run this command again."
        echo ""
        exit 1
      fi
      info "jq available"
      ;;
    *)
      fail "HA NOVA supports macOS and Windows (Git Bash). Detected: $(uname -s)"
      ;;
  esac
```

- [ ] **Step 2: Fix git install hint (line 168-173)**

Replace the macOS-specific Xcode hint:
```bash
    echo "      Install Xcode Command Line Tools:"
    echo "        xcode-select --install"
```
with platform-aware text:
```bash
    case "$(uname -s)" in
      Darwin)
        echo "      Install Xcode Command Line Tools:"
        echo "        xcode-select --install"
        ;;
      *)
        echo "      Install git from: https://git-scm.com/downloads"
        ;;
    esac
```

- [ ] **Step 3: Fix `detect_shell_rc` for Windows**

In `detect_shell_rc()`, the `zsh` case defaults — but Windows/Git Bash has no zsh. The `bash` case already handles `.bash_profile` → `.profile` fallback, which is correct for Git Bash. No change needed (the function is already robust).

- [ ] **Step 4: Fix symlink creation for Windows**

In `link_cli()` (line 264), `ln -sfn` may fail on Windows without Developer Mode. Add a fallback:

Replace:
```bash
  ln -sfn "${INSTALL_DIR}/scripts/onboarding/bin/ha-nova" "$BIN_LINK"
```
with:
```bash
  if ! ln -sfn "${INSTALL_DIR}/scripts/onboarding/bin/ha-nova" "$BIN_LINK" 2>/dev/null; then
    # Fallback for Windows without Developer Mode — copy instead of symlink
    cp "${INSTALL_DIR}/scripts/onboarding/bin/ha-nova" "$BIN_LINK"
    chmod 755 "$BIN_LINK"
  fi
```

- [ ] **Step 5: Commit**

```bash
git add install.sh
git commit -m "feat: open installer to Windows (Git Bash) with jq prereq check"
```

---

### Task 12: Fix `install-local-skills.sh` symlink fallback

**Why:** `ln -sfn` fails on Windows without Developer Mode. The skill installer needs a fallback.

**Files:**
- Modify: `scripts/onboarding/install-local-skills.sh` (line 163)

- [ ] **Step 1: Add symlink fallback**

Replace `install_symlink()` function:
```bash
install_symlink() {
  local target="$1"
  local user_skills_dir="$2"

  mkdir -p "${user_skills_dir}"
  cleanup_legacy "${user_skills_dir}" "${target}"

  # Remove existing symlink if present
  if [[ -L "${user_skills_dir}/ha-nova" ]]; then
    rm -f "${user_skills_dir}/ha-nova"
  fi

  if ln -sfn "${SOURCE_SKILLS_DIR}" "${user_skills_dir}/ha-nova" 2>/dev/null; then
    log "[${target}] Symlinked: ${user_skills_dir}/ha-nova -> ${SOURCE_SKILLS_DIR}"
  else
    # Fallback for Windows without Developer Mode — copy instead
    rm -rf "${user_skills_dir}/ha-nova"
    cp -r "${SOURCE_SKILLS_DIR}" "${user_skills_dir}/ha-nova"
    log "[${target}] Copied (symlinks unavailable): ${user_skills_dir}/ha-nova"
  fi
}
```

- [ ] **Step 2: Commit**

```bash
git add scripts/onboarding/install-local-skills.sh
git commit -m "fix: add copy fallback when symlinks unavailable (Windows)"
```

---

### Task 13: Fix `hooks/session-start` relay warning message

**Why:** Line 77 references `npm run onboarding:macos` — stale on Windows.

**Files:**
- Modify: `hooks/session-start` (line 77)

- [ ] **Step 1: Update the warning message**

Change line 77 from:
```bash
      version_warning="[ha-nova] WARNING: Relay unreachable. Check relay status or run: npm run onboarding:macos\\n"
```
to:
```bash
      version_warning="[ha-nova] WARNING: Relay unreachable. Check relay status or run: ha-nova doctor\\n"
```

- [ ] **Step 2: Commit**

```bash
git add hooks/session-start
git commit -m "fix: use platform-agnostic command in relay warning message"
```

---

## Chunk 2: Test Infrastructure (no Windows machine needed)

### Task 14: Extend test helpers with `createPlatformMock`

**Why:** Enables testing Windows code paths on macOS by mocking `powershell.exe` and `cmd.exe` as bash scripts.

**Files:**
- Modify: `tests/onboarding/_helpers.ts`

- [ ] **Step 1: Add `Platform` type and `createWindowsMocks` to `_helpers.ts`**

Add after existing exports:

```typescript
export type Platform = "macos" | "windows";

/**
 * Creates additional mock binaries for Windows platform testing on macOS.
 * Simulates powershell.exe (DPAPI via plaintext) and cmd.exe (path resolution).
 */
export function addWindowsMocks(binDir: string, home: string): void {
  // powershell.exe mock — stores/reads plaintext instead of DPAPI
  const psScript = `#!/usr/bin/env bash
set -euo pipefail
cmd=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    -NoProfile|-NonInteractive) shift ;;
    -Command) shift; cmd="$*"; break ;;
    *) shift ;;
  esac
done

# Store: ConvertFrom-SecureString / FromBase64String writes to file
if echo "$cmd" | grep -q "ConvertFrom-SecureString"; then
  # Extract file path and base64 value using sed (POSIX-compatible, no grep -oP)
  file_path=$(echo "$cmd" | sed -n "s/.*FilePath '\\([^']*\\)'.*/\\1/p")
  b64=$(echo "$cmd" | sed -n "s/.*FromBase64String('\\([^']*\\)').*/\\1/p")
  if [[ -n "$file_path" && -n "$b64" ]]; then
    mkdir -p "$(dirname "$file_path")"
    printf '%s' "$b64" | base64 -d > "$file_path"
  fi
  exit 0
fi

# Read: SecureStringToBSTR reads from file
if echo "$cmd" | grep -q "SecureStringToBSTR"; then
  file_path=$(echo "$cmd" | sed -n "s/.*Path '\\([^']*\\)'.*/\\1/p")
  if [[ -n "$file_path" && -f "$file_path" ]]; then
    cat "$file_path"
  fi
  exit 0
fi

exit 0
`;
  writeFileSync(join(binDir, "powershell.exe"), psScript, { mode: 0o755 });

  // cmd.exe mock — browser open (no-op) and path resolution
  writeFileSync(
    join(binDir, "cmd.exe"),
    "#!/usr/bin/env bash\nexit 0\n",
    { mode: 0o755 },
  );

  // clip.exe mock — clipboard (no-op, captures to file for assertions)
  writeFileSync(
    join(binDir, "clip.exe"),
    `#!/usr/bin/env bash\ncat > "${home}/.config/ha-nova/.mock-clipboard" 2>/dev/null || true\nexit 0\n`,
    { mode: 0o755 },
  );

  // cygpath mock — returns input unchanged (Unix paths work in mock)
  writeFileSync(
    join(binDir, "cygpath"),
    "#!/usr/bin/env bash\nshift; echo \"$1\"\n",
    { mode: 0o755 },
  );
}

/**
 * Build env with platform override for cross-platform testing.
 */
export function mockEnvForPlatform(
  platform: Platform,
  home: string,
  binDir: string,
  extra: Record<string, string> = {},
): Record<string, string> {
  const env = mockEnv(home, binDir, extra);
  if (platform === "windows") {
    env.HA_NOVA_PLATFORM_OVERRIDE = "windows";
  }
  return env;
}
```

- [ ] **Step 2: Commit**

```bash
git add tests/onboarding/_helpers.ts
git commit -m "test: add Windows platform mock helpers (powershell.exe, cmd.exe, clip.exe)"
```

---

### Task 15: Write platform dispatcher tests

**Files:**
- Create: `tests/onboarding/platform-dispatch.test.ts`

- [ ] **Step 1: Write the test file**

```typescript
import { spawnSync } from "node:child_process";
import { describe, expect, it } from "vitest";
import {
  addWindowsMocks,
  createMockBinaries,
  createMockHome,
  mockEnvForPlatform,
  REPO_ROOT,
} from "./_helpers.js";

describe("platform dispatch", () => {
  it("macOS: sources platform/macos.sh and require_platform passes", () => {
    const home = createMockHome({ keychainToken: "test-token" });
    const binDir = createMockBinaries();
    const env = mockEnvForPlatform("macos", home, binDir);

    const result = spawnSync(
      "bash",
      [
        "-c",
        `source "${REPO_ROOT}/scripts/onboarding/lib/platform.sh" && require_platform && echo "OK"`,
      ],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: 10000, env },
    );
    expect(result.stdout.trim()).toBe("OK");
    expect(result.status).toBe(0);
  });

  it("windows override: sources platform/windows.sh and require_platform passes", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();
    addWindowsMocks(binDir, home);
    const env = mockEnvForPlatform("windows", home, binDir);

    const result = spawnSync(
      "bash",
      [
        "-c",
        `source "${REPO_ROOT}/scripts/onboarding/lib/ui.sh" && source "${REPO_ROOT}/scripts/onboarding/lib/platform.sh" && require_platform && echo "OK"`,
      ],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: 10000, env },
    );
    expect(result.stdout.trim()).toBe("OK");
    expect(result.status).toBe(0);
  });

  it("windows credential round-trip via mock powershell.exe", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();
    addWindowsMocks(binDir, home);
    const env = mockEnvForPlatform("windows", home, binDir);

    const result = spawnSync(
      "bash",
      [
        "-c",
        `source "${REPO_ROOT}/scripts/onboarding/lib/ui.sh"
         source "${REPO_ROOT}/scripts/onboarding/lib/platform.sh"
         store_keychain_secret "ha-nova.relay-auth-token" "my-secret-token-123"
         read_keychain_secret "ha-nova.relay-auth-token"`,
      ],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: 10000, env },
    );
    expect(result.stdout.trim()).toBe("my-secret-token-123");
    expect(result.status).toBe(0);
  });

  it("unsupported platform: exits with error", () => {
    const home = createMockHome();
    const binDir = createMockBinaries();
    const env = mockEnvForPlatform("macos", home, binDir, {
      HA_NOVA_PLATFORM_OVERRIDE: "unsupported",
    });

    const result = spawnSync(
      "bash",
      [
        "-c",
        `source "${REPO_ROOT}/scripts/onboarding/lib/platform.sh" 2>&1`,
      ],
      { cwd: REPO_ROOT, encoding: "utf8", timeout: 10000, env },
    );
    expect(result.status).not.toBe(0);
  });
});
```

- [ ] **Step 2: Run tests**

Run: `npm test -- tests/onboarding/platform-dispatch.test.ts`
Expected: All 4 tests pass

- [ ] **Step 3: Commit**

```bash
git add tests/onboarding/platform-dispatch.test.ts
git commit -m "test: add platform dispatcher and credential round-trip tests"
```

---

### Task 16: Write platform isolation contract test

**Why:** Catches accidental platform-specific code in generic modules — prevents future regressions.

**Files:**
- Create: `tests/onboarding/platform-isolation-contract.test.ts`

- [ ] **Step 1: Write the contract test**

```typescript
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const GENERIC_FILES = [
  "scripts/onboarding/lib/ui.sh",
  "scripts/onboarding/lib/relay.sh",
];

const PLATFORM_COMMANDS = [
  /\bsecurity\s+(add|find|delete)-generic-password/,
  /\bpbcopy\b/,
  /\bpbpaste\b/,
  /\bpowershell\.exe\b/,
  /\bcmd\.exe\b/,
  /\bclip\.exe\b/,
];

describe("platform isolation", () => {
  for (const file of GENERIC_FILES) {
    it(`${file} contains no platform-specific commands`, () => {
      const content = readFileSync(file, "utf8");
      const codeLines = content
        .split("\n")
        .filter((line) => !line.trimStart().startsWith("#"))
        .join("\n");

      for (const pattern of PLATFORM_COMMANDS) {
        expect(codeLines).not.toMatch(pattern);
      }
    });
  }
});
```

- [ ] **Step 2: Run test**

Run: `npm test -- tests/onboarding/platform-isolation-contract.test.ts`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/onboarding/platform-isolation-contract.test.ts
git commit -m "test: add platform isolation contract test for generic modules"
```

---

### Task 17: Add `windows-latest` matrix to CI

**Why:** Validates that platform-independent tests pass on real Windows. Does NOT run macOS-specific onboarding tests (those are `skipIf(!isMac)` already).

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add Windows matrix to `ci-gate` job**

Replace:
```yaml
  ci-gate:
    name: ci-gate
    runs-on: ubuntu-latest
```

with:
```yaml
  ci-gate:
    name: ci-gate (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest]
```

Add before the checkout step:
```yaml
      - name: Force LF line endings
        if: matrix.os == 'windows-latest'
        run: git config --global core.autocrlf false

      - name: Install jq (Windows)
        if: matrix.os == 'windows-latest'
        run: choco install jq -y
        shell: powershell
```

Change the Test step to use bash:
```yaml
      - name: Test
        run: npm test
        shell: bash
```

Skip docs check on Windows (bash script, non-essential):
```yaml
      - name: Docs fact-check
        if: matrix.os != 'windows-latest'
        run: bash scripts/check-docs.sh
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add windows-latest matrix for cross-platform testing"
```

---

## Chunk 3: Skill & Doc Updates (text changes only)

### Task 18: Update skill files — onboarding references

**Why:** 8 skills reference `npm run onboarding:macos`. Update to the generic command while keeping the old npm script as alias.

**Files:**
- Modify: `skills/ha-nova/SKILL.md`
- Modify: `skills/ha-nova-read/SKILL.md`
- Modify: `skills/ha-nova-write/SKILL.md`
- Modify: `skills/ha-nova-helper/SKILL.md`
- Modify: `skills/ha-nova-service-call/SKILL.md`
- Modify: `skills/ha-nova-entity-discovery/SKILL.md`
- Modify: `skills/ha-nova-review/SKILL.md`
- Modify: `skills/ha-nova-fallback/SKILL.md`
- Modify: `skills/ha-nova-onboarding/SKILL.md`

- [ ] **Step 1: Replace `npm run onboarding:macos` with `ha-nova setup`**

In each file, replace all occurrences of:
```
npm run onboarding:macos
```
with:
```
ha-nova setup
```

This is the CLI command that works on all platforms (the `bin/ha-nova` script handles platform dispatch).

- [ ] **Step 2: Update context skill platform section**

In `skills/ha-nova/SKILL.md`, update the "Runtime Prerequisite (macOS)" section:

Replace:
```
## Runtime Prerequisite (macOS)

Before HA operations in this session:

1. Verify relay CLI: `~/.config/ha-nova/relay health`
2. If this fails, ask user to run: `npm run onboarding:macos`
```

with:
```
## Runtime Prerequisite

Before HA operations in this session:

1. Verify relay CLI: `~/.config/ha-nova/relay health`
2. If this fails, ask user to run: `ha-nova setup`
```

- [ ] **Step 3: Update quoting reliability section**

Add after existing quoting rules:
```
- Windows users: Git Bash required. All relay CLI examples use bash syntax.
```

- [ ] **Step 4: Update test assertions that reference `npm run onboarding:macos`**

Search for `onboarding:macos` in `tests/` and update to match:
- `tests/skills/ha-cross-skill-integration.test.ts`
- `tests/skills/ha-nova-contract.test.ts`
- `tests/skills/ha-entities-contract.test.ts`
- Any other test files referencing the old command

Replace `npm run onboarding:macos` with `ha-nova setup` in test assertions.

- [ ] **Step 5: Run full test suite**

Run: `npm test`
Expected: All tests pass

- [ ] **Step 6: Commit**

```bash
git add skills/ tests/
git commit -m "docs(skills): update onboarding references to platform-agnostic commands"
```

---

### Task 19: Update README and INSTALL docs

**Files:**
- Modify: `README.md`
- Modify: `INSTALL.md` (if exists, else add Windows section to README)

- [ ] **Step 1: Add Windows to prerequisites in README**

Find the prerequisites section. Add Windows alongside macOS:

```markdown
### Prerequisites

- **macOS** or **Windows** (with [Git for Windows](https://gitforwindows.org/) / Git Bash)
- [Node.js](https://nodejs.org/) 20 or newer
- A running Home Assistant instance on your network
- Windows only: [jq](https://jqlang.github.io/jq/) (`winget install jqlang.jq`)
```

- [ ] **Step 2: Add Windows install note**

The install one-liner `curl -fsSL ... | bash` works in Git Bash. Add a note:

```markdown
**Windows:** Open Git Bash and run the same command. The installer auto-detects your platform.
```

- [ ] **Step 3: Commit**

```bash
git add README.md INSTALL.md
git commit -m "docs: add Windows platform support to README"
```

---

### Task 20: Push branch and run CI

- [ ] **Step 1: Push the feature branch**

```bash
git push -u origin feat/windows-support
```

- [ ] **Step 2: Watch CI**

```bash
gh pr create --title "feat: Windows platform support" --body "$(cat <<'EOF'
## Summary
- Platform abstraction layer (6-function API) for macOS + Windows
- DPAPI-encrypted credential storage on Windows (via PowerShell)
- Platform dispatcher with auto-detection and test override
- Inline platform dispatch in standalone relay.sh
- All onboarding entry points renamed to platform-agnostic names
- Full mock-based test infrastructure for cross-platform testing
- CI matrix extended with windows-latest

## Test plan
- [ ] All existing macOS tests pass (regression safety)
- [ ] New platform dispatch tests pass
- [ ] Platform isolation contract passes
- [ ] CI passes on ubuntu-latest AND windows-latest
- [ ] Manual smoke test on macOS: `ha-nova setup`
- [ ] Manual smoke test on Windows (Git Bash) when available

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"

gh pr checks <nr> --watch
```

- [ ] **Step 3: Review CI results, fix any failures**

Focus on:
- Windows runner: do bash scripts execute via `shell: bash`?
- Line ending issues: did `.gitattributes` prevent CRLF?
- Mock tests: do `powershell.exe` / `cygpath` mocks work?

---

## Summary: What's NOT in this plan (deferred)

| Item | Reason |
|------|--------|
| **Linux support** | Credential fragmentation (GNOME Keyring vs KWallet vs headless). Separate issue. |
| **Rename `macos-lib.sh` → `onboarding-lib.sh`** | Low-value git history churn. The file works on all platforms now via dispatcher. Can rename later. |
| **Native PowerShell scripts** | Git Bash is sufficient. Native PS only if user demand warrants dual codebase. |
| **Windows CI for onboarding integration tests** | Requires real DPAPI on Windows runner. Can add once basic CI matrix is green. |
| **UTM/VM manual testing** | Only needed for final release validation, not for development. |
