# Publish-Readiness: Onboarding Wizard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the current multi-step onboarding with a single `npx ha-nova setup` wizard that guides HA beginners through the complete setup — including App installation, token configuration, and skill installation — with deep-links and automatic verification.

**Architecture:** Refactor `scripts/onboarding/` into platform-abstracted structure (platform-specific in `platform/macos.sh`, shared logic in `lib/ui.sh` + `lib/relay.sh`, flow in `setup-flow.sh` + `doctor-flow.sh`). Add `bin/ha-nova` CLI entry point with `"bin"` field in `package.json` for `npx` support. Rewrite README to minimal landing page. Simplify INSTALL docs to one-liners.

**Tech Stack:** Bash (onboarding scripts), Node.js `"bin"` field (npx entry), Vitest (contract tests)

**Design doc:** `docs/plans/2026-03-02-publish-readiness-onboarding-design.md`

---

### Task 1: Platform Abstraction — Extract `platform/macos.sh`

Extract macOS-specific functions from `macos-lib.sh` into a dedicated platform file.

**Files:**
- Create: `scripts/onboarding/platform/macos.sh`
- Modify: `scripts/onboarding/macos-lib.sh`
- Test: `tests/onboarding/macos-onboarding-script-contract.test.ts`

**Step 1: Write the failing test**

Add to `tests/onboarding/macos-onboarding-script-contract.test.ts`:

```typescript
it("provides platform-specific macOS module", () => {
  const file = "scripts/onboarding/platform/macos.sh";
  const stats = statSync(file);
  const content = readFileSync(file, "utf8");

  expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
  expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);

  // Must contain macOS-specific functions
  expect(content).toContain("store_keychain_secret()");
  expect(content).toContain("read_keychain_secret()");
  expect(content).toContain("delete_keychain_secret_if_exists()");
  expect(content).toContain("open_browser()");
  expect(content).toContain("security add-generic-password");
  expect(content).toContain("security find-generic-password");
});
```

**Step 2: Run test to verify it fails**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: FAIL — file `scripts/onboarding/platform/macos.sh` does not exist

**Step 3: Create `scripts/onboarding/platform/macos.sh`**

Extract these functions from `macos-lib.sh`:

```bash
#!/usr/bin/env bash
# Platform-specific functions for macOS.
# Sourced by setup-flow.sh / doctor-flow.sh after platform detection.
set -euo pipefail

require_platform() {
  if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "[ha-nova] This setup requires macOS. Linux/Windows support coming soon." >&2
    exit 1
  fi
}

open_browser() {
  local url="$1"
  open "$url" 2>/dev/null || echo "[ha-nova] Could not open browser. Please visit: $url" >&2
}

store_keychain_secret() {
  local service="$1"
  local value="$2"
  security add-generic-password -U \
    -a "$USER" \
    -s "$service" \
    -w "$value" >/dev/null
}

read_keychain_secret() {
  local service="$1"
  security find-generic-password -a "$USER" -s "$service" -w 2>/dev/null || true
}

delete_keychain_secret_if_exists() {
  local service="$1"
  security delete-generic-password -a "$USER" -s "$service" >/dev/null 2>&1 || true
}
```

**Step 4: Update `macos-lib.sh`**

At the top of `macos-lib.sh`, after `REPO_ROOT=...`, add:

```bash
# Source platform-specific functions
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/platform/macos.sh"
```

Remove the duplicated functions from `macos-lib.sh`: `require_macos`, `store_keychain_secret`, `read_keychain_secret`, `delete_keychain_secret_if_exists`. Replace `require_macos` calls with `require_platform`.

**Step 5: Run tests to verify they pass**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: PASS

**Step 6: Commit**

```bash
git add scripts/onboarding/platform/macos.sh scripts/onboarding/macos-lib.sh tests/onboarding/
git commit -m "refactor(onboarding): extract macOS platform module for cross-platform readiness"
```

---

### Task 2: Shared Libraries — Extract `lib/ui.sh` and `lib/relay.sh`

Extract platform-independent helpers into shared libraries.

**Files:**
- Create: `scripts/onboarding/lib/ui.sh`
- Create: `scripts/onboarding/lib/relay.sh`
- Modify: `scripts/onboarding/macos-lib.sh`
- Test: `tests/onboarding/macos-onboarding-script-contract.test.ts`

**Step 1: Write the failing test**

```typescript
it("provides platform-independent UI and relay libraries", () => {
  const uiFile = "scripts/onboarding/lib/ui.sh";
  const relayFile = "scripts/onboarding/lib/relay.sh";

  const uiStats = statSync(uiFile);
  const relayStats = statSync(relayFile);
  const uiContent = readFileSync(uiFile, "utf8");
  const relayContent = readFileSync(relayFile, "utf8");

  expect((uiStats.mode & constants.S_IXUSR) !== 0).toBe(true);
  expect((relayStats.mode & constants.S_IXUSR) !== 0).toBe(true);

  // UI helpers — no macOS-specific commands
  expect(uiContent).toContain("prompt_with_default()");
  expect(uiContent).toContain("prompt_yes_no()");
  expect(uiContent).toContain("print_step()");
  expect(uiContent).toContain("print_success()");
  expect(uiContent).toContain("print_fail()");
  expect(uiContent).not.toContain("security ");

  // Relay probes — no macOS-specific commands
  expect(relayContent).toContain("probe_relay_health()");
  expect(relayContent).toContain("probe_relay_ws_ping()");
  expect(relayContent).toContain("probe_home_assistant_url_base()");
  expect(relayContent).toContain("explain_relay_probe_failure()");
  expect(relayContent).not.toContain("security ");
});
```

**Step 2: Run test to verify it fails**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: FAIL — files don't exist

**Step 3: Create `scripts/onboarding/lib/ui.sh`**

Extract from `macos-lib.sh`:

```bash
#!/usr/bin/env bash
# Platform-independent UI helpers for onboarding wizard.
set -euo pipefail

print_header() {
  echo ""
  echo "  HA NOVA Setup"
  echo "  ─────────────"
  echo ""
}

print_step() {
  local step="$1"
  local total="$2"
  local title="$3"
  echo ""
  echo "  Step ${step}/${total} — ${title}"
  echo ""
}

print_success() {
  echo "  [ok] $*"
}

print_fail() {
  echo "  [!!] $*" >&2
}

print_info() {
  echo "  $*"
}

print_waiting() {
  echo ""
  echo -n "  Press [Enter] when done... "
}

wait_for_enter() {
  local label="${1:-Press [Enter] to continue...}"
  echo ""
  if ! read -r -p "  ${label} "; then
    echo "[ha-nova] Interactive input required. Re-run in a terminal." >&2
    exit 1
  fi
}

prompt_with_default() {
  local label="$1"
  local default_value="$2"
  local value

  if ! read -r -p "  ${label} [${default_value}]: " value; then
    echo "[ha-nova] Interactive input required. Re-run in a terminal." >&2
    exit 1
  fi
  if [[ -z "$value" ]]; then
    value="$default_value"
  fi

  printf '%s' "$value"
}

prompt_yes_no() {
  local label="$1"
  local default_answer="${2:-N}"
  local hint="y/N"
  local answer

  if [[ "$default_answer" =~ ^[Yy]$ ]]; then
    hint="Y/n"
  fi

  if ! read -r -p "  ${label} [${hint}]: " answer; then
    echo "[ha-nova] Interactive input required. Re-run in a terminal." >&2
    exit 1
  fi
  if [[ -z "$answer" ]]; then
    answer="$default_answer"
  fi

  [[ "$answer" =~ ^[Yy]$ ]]
}

mask_secret_hint() {
  local value="$1"
  local length="${#value}"
  if (( length <= 8 )); then
    printf '***'
    return
  fi

  local tail="${value: -4}"
  printf '***%s' "$tail"
}

fingerprint_secret() {
  local value="$1"

  if command -v shasum >/dev/null 2>&1; then
    printf '%s' "$value" | shasum -a 256 | awk '{print $1}'
    return
  fi

  if command -v openssl >/dev/null 2>&1; then
    printf '%s' "$value" | openssl dgst -sha256 -r | awk '{print $1}'
    return
  fi

  printf 'len:%s' "${#value}"
}

generate_relay_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi

  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr -d '-'
    return
  fi

  echo "[ha-nova] Cannot generate token (missing openssl and uuidgen)." >&2
  exit 1
}
```

**Step 4: Create `scripts/onboarding/lib/relay.sh`**

Extract from `macos-lib.sh` — all `probe_*`, `explain_*`, `normalize_*`, `resolve_*`, `collect_*`, `detect_*`, `validate_relay_*` functions. These are purely curl-based and platform-independent.

Move these functions (lines 102-432 of current `macos-lib.sh`) into `lib/relay.sh`:
- `normalize_host_input()`
- `normalize_url_base_input()`
- `probe_home_assistant_url_base()`
- `resolve_home_assistant_url_base()`
- `probe_home_assistant_host()`
- `guess_home_assistant_url_base()`
- `collect_candidate_hosts()`
- `detect_default_ha_host()`
- `prompt_valid_ha_host()`
- `validate_relay_base_url_format()`
- `probe_relay_health()`
- `probe_relay_ws_ping()`
- `explain_relay_ws_degraded()`
- `explain_relay_probe_failure()`

Plus the shared state variables:
```bash
LAST_RELAY_STATUS_CODE=""
LAST_RELAY_HA_WS_CONNECTED=""
LAST_RELAY_WS_STATUS_CODE=""
LAST_RELAY_WS_BODY=""
```

**Step 5: Update `macos-lib.sh`**

Replace extracted functions with source lines:

```bash
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/lib/ui.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/lib/relay.sh"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/platform/macos.sh"
```

**Step 6: Run tests to verify all pass**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: ALL PASS (existing + new tests)

Also run the full suite: `npx vitest run`

**Step 7: Commit**

```bash
git add scripts/onboarding/lib/ scripts/onboarding/macos-lib.sh tests/onboarding/
git commit -m "refactor(onboarding): extract platform-independent UI and relay libraries"
```

---

### Task 3: CLI Entry Point — `bin/ha-nova`

Create the `npx ha-nova` CLI entry point.

**Files:**
- Create: `scripts/onboarding/bin/ha-nova`
- Modify: `package.json`
- Test: `tests/onboarding/macos-onboarding-script-contract.test.ts`

**Step 1: Write the failing test**

```typescript
it("provides bin/ha-nova CLI entry point for npx", () => {
  const file = "scripts/onboarding/bin/ha-nova";
  const stats = statSync(file);
  const content = readFileSync(file, "utf8");

  expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
  expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  expect(content).toContain("setup");
  expect(content).toContain("doctor");
  expect(content).toContain("Usage:");
});

it("package.json exposes bin field for npx ha-nova", () => {
  const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
    bin?: Record<string, string>;
  };
  expect(pkg.bin?.["ha-nova"]).toBe("scripts/onboarding/bin/ha-nova");
});
```

**Step 2: Run test to verify it fails**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: FAIL

**Step 3: Create `scripts/onboarding/bin/ha-nova`**

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

case "${1:-help}" in
  setup)
    shift
    exec bash "${SCRIPT_DIR}/macos-setup.sh" "$@"
    ;;
  doctor)
    shift
    exec bash "${SCRIPT_DIR}/macos-doctor.sh" "$@"
    ;;
  -h|--help|help|*)
    echo "HA NOVA — AI-powered Home Assistant control"
    echo ""
    echo "Usage:"
    echo "  ha-nova setup    Complete guided setup (App + tokens + skills)"
    echo "  ha-nova doctor   Run diagnostics and verify everything works"
    echo "  ha-nova --help   Show this help"
    ;;
esac
```

Make executable: `chmod +x scripts/onboarding/bin/ha-nova`

**Step 4: Add `"bin"` to `package.json`**

Add after `"type": "module"`:

```json
"bin": {
  "ha-nova": "scripts/onboarding/bin/ha-nova"
},
```

**Step 5: Run tests**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: PASS

**Step 6: Verify manually**

Run: `bash scripts/onboarding/bin/ha-nova --help`
Expected: Shows usage text

**Step 7: Commit**

```bash
git add scripts/onboarding/bin/ha-nova package.json tests/onboarding/
git commit -m "feat(onboarding): add bin/ha-nova CLI entry point for npx support"
```

---

### Task 4: Wizard Flow — New Setup Phases

Rewrite `run_setup()` in `macos-lib.sh` with the 4-phase wizard flow including prerequisites check, App installation guide with deep-links, token setup with auto-generation, and automatic skill installation.

**Files:**
- Modify: `scripts/onboarding/macos-lib.sh` (the `run_setup` function)
- Modify: `scripts/onboarding/lib/ui.sh` (add `check_prerequisites`)
- Test: `tests/onboarding/macos-onboarding-script-contract.test.ts`

**Step 1: Write the failing test for new wizard phases**

```typescript
it("wizard includes prerequisites check, app guide, and skill install", () => {
  const content = readFileSync("scripts/onboarding/macos-lib.sh", "utf8");

  // Phase 1: prerequisites
  expect(content).toContain("check_prerequisites");

  // Phase 2: app installation guide with deep-link
  expect(content).toContain("my.home-assistant.io/redirect/supervisor_add_addon_repository");
  expect(content).toContain("open_browser");

  // Phase 3: token setup with LLAT guide
  expect(content).toContain("my.home-assistant.io/redirect/profile");

  // Phase 4: automatic skill installation
  expect(content).toContain("install-local-skills.sh");
});

it("prerequisites check validates OS and Node version", () => {
  const content = readFileSync("scripts/onboarding/lib/ui.sh", "utf8");

  expect(content).toContain("check_prerequisites()");
  expect(content).toContain("node --version");
  expect(content).toContain("Node.js");
});
```

**Step 2: Run test to verify it fails**

Expected: FAIL — new strings not yet present

**Step 3: Add `check_prerequisites()` to `lib/ui.sh`**

```bash
check_prerequisites() {
  print_info "Checking prerequisites..."

  # OS check (delegated to platform module)
  require_platform
  print_success "macOS detected"

  # Node.js check
  if ! command -v node >/dev/null 2>&1; then
    print_fail "Node.js not found. Install Node.js 20+ from https://nodejs.org"
    exit 1
  fi
  local node_major
  node_major="$(node --version | sed 's/v\([0-9]*\).*/\1/')"
  if (( node_major < 20 )); then
    print_fail "Node.js ${node_major} found, but 20+ required. Update from https://nodejs.org"
    exit 1
  fi
  print_success "Node.js $(node --version)"

  # npm check
  if ! command -v npm >/dev/null 2>&1; then
    print_fail "npm not found. It should come with Node.js."
    exit 1
  fi
  print_success "npm available"

  # curl check
  if ! command -v curl >/dev/null 2>&1; then
    print_fail "curl not found. Install curl."
    exit 1
  fi
}
```

**Step 4: Rewrite `run_setup()` in `macos-lib.sh`**

Replace the current `run_setup()` function entirely. The new version follows the 4-phase wizard flow:

```bash
run_setup() {
  local client="${1:-claude}"

  print_header

  # ── Phase 1: Prerequisites ──
  check_prerequisites
  echo ""

  # ── Phase 2: App Installation Guide ──
  print_step 1 4 "Install NOVA Relay in Home Assistant"

  print_info "First, we need to add the NOVA repository to Home Assistant."
  print_info "I'll open your browser to do this automatically."
  wait_for_enter "Press [Enter] to open your browser..."
  open_browser "https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fmarkusleben%2Fha-nova"

  echo ""
  print_info "In Home Assistant:"
  print_info "  1. Confirm adding the repository"
  print_info "  2. Go to Settings → Add-ons → Add-on Store"
  print_info "  3. Search for \"NOVA Relay\""
  print_info "  4. Click Install → wait → click Start"
  wait_for_enter "Press [Enter] when the add-on is running..."

  # ── Phase 3: Token Setup ──
  print_step 2 4 "Configure Authentication"

  # 3a) Relay token
  local relay_auth_token
  local existing_relay_auth_token
  existing_relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"

  if [[ -n "$existing_relay_auth_token" ]]; then
    print_info "Existing relay token found in Keychain: $(mask_secret_hint "$existing_relay_auth_token")"
    if prompt_yes_no "Keep existing token?" "Y"; then
      relay_auth_token="$existing_relay_auth_token"
    else
      relay_auth_token="$(generate_relay_token)"
      print_info "Generated new token: ${relay_auth_token}"
    fi
  else
    relay_auth_token="$(generate_relay_token)"
    echo ""
    print_info "Generated a secure relay token for you:"
    echo ""
    echo "    ${relay_auth_token}"
    echo ""
    print_info "I'll open the NOVA Relay add-on configuration."
    print_info "Paste this token into the \"relay_auth_token\" field and click Save."
  fi

  if [[ "$relay_auth_token" != "${existing_relay_auth_token:-}" ]]; then
    wait_for_enter "Press [Enter] to open the add-on config..."
    # Try direct link first, fall back to my.home-assistant.io
    load_config
    if [[ -n "${HA_URL:-}" ]]; then
      open_browser "${HA_URL}/hassio/addon/local_ha_nova_relay/config"
    else
      open_browser "https://my.home-assistant.io/redirect/supervisor_addon/?addon=local_ha_nova_relay"
    fi
    wait_for_enter "Press [Enter] when you've saved the relay token..."
  fi

  # 3b) LLAT guide
  echo ""
  print_info "Now we need a Home Assistant access token (Long-Lived Access Token)."
  print_info "This lets the relay communicate with your Home Assistant."
  print_info ""
  print_info "I'll open your HA profile page."
  wait_for_enter "Press [Enter] to open your browser..."
  open_browser "https://my.home-assistant.io/redirect/profile/"

  echo ""
  print_info "In Home Assistant:"
  print_info "  1. Scroll to \"Long-Lived Access Tokens\""
  print_info "  2. Click \"Create Token\" → name it \"NOVA\""
  print_info "  3. Copy the token"
  print_info "  4. Go back to the NOVA Relay add-on → Configuration tab"
  print_info "  5. Paste into the \"ha_llat\" field"
  print_info "  6. Click Save → then Restart the add-on"
  wait_for_enter "Press [Enter] when you've completed these steps..."

  # ── Phase 4: Verify + Save + Install Skills ──
  print_step 3 4 "Verifying connection"

  # Detect HA host
  local default_ha_host
  print_info "Detecting Home Assistant..."
  default_ha_host="$(detect_default_ha_host)"

  prompt_valid_ha_host "$default_ha_host"

  local default_relay_base_url
  default_relay_base_url="${RELAY_BASE_URL:-http://${HA_HOST}:8791}"

  while true; do
    RELAY_BASE_URL="$(prompt_with_default 'Relay URL' "$default_relay_base_url")"
    RELAY_BASE_URL="${RELAY_BASE_URL%/}"
    if validate_relay_base_url_format "$RELAY_BASE_URL"; then
      break
    fi
    print_fail "Invalid URL format. Expected: http://<host>:<port>"
    default_relay_base_url="$RELAY_BASE_URL"
  done

  # Verify relay
  echo ""
  local max_retries=3
  local attempt=0
  while true; do
    if probe_relay_health "$RELAY_BASE_URL" "$relay_auth_token"; then
      print_success "Relay reachable at ${RELAY_BASE_URL}"
      if [[ "$LAST_RELAY_HA_WS_CONNECTED" == "true" ]]; then
        print_success "WebSocket connected to Home Assistant"
      elif [[ "$LAST_RELAY_HA_WS_CONNECTED" == "false" ]]; then
        if probe_relay_ws_ping "$RELAY_BASE_URL" "$relay_auth_token"; then
          print_success "WebSocket connected to Home Assistant"
        else
          print_fail "WebSocket not connected. Is ha_llat set in the add-on config?"
          explain_relay_ws_degraded
          if ! prompt_yes_no "Continue anyway? (you can fix this later with 'ha-nova doctor')" "Y"; then
            exit 1
          fi
        fi
      fi
      break
    else
      attempt=$((attempt + 1))
      print_fail "Can't reach relay at ${RELAY_BASE_URL}"
      explain_relay_probe_failure "$RELAY_BASE_URL"
      if (( attempt >= max_retries )); then
        print_info "Saving config anyway — run 'ha-nova doctor' after fixing the issue."
        break
      fi
      wait_for_enter "Press [Enter] to retry..."
    fi
  done

  # Save config + token
  store_keychain_secret "$RELAY_SERVICE" "$relay_auth_token"
  delete_keychain_secret_if_exists "$LLAT_SERVICE"
  persist_config
  invalidate_doctor_cache
  print_success "Config saved to ~/.config/ha-nova/"
  print_success "Token stored in macOS Keychain"

  # Install skills
  print_step 4 4 "Installing skills"

  local install_target="$client"
  if bash "${REPO_ROOT}/scripts/onboarding/install-local-skills.sh" "$install_target" 2>&1; then
    print_success "Skills installed for ${client}"
  else
    print_fail "Skill installation failed. Run manually: npm run install:${client}-skill"
  fi

  # Success banner
  echo ""
  echo "  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Setup complete!"
  echo ""
  echo "  Try asking: \"List my automations\""
  echo "  Run diagnostics: npx ha-nova doctor"
  echo "  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""
}
```

**Step 5: Update `bin/ha-nova` to pass client target**

```bash
  setup)
    shift
    local client="claude"
    if [[ "${1:-}" == "--codex" ]]; then client="codex"; shift; fi
    if [[ "${1:-}" == "--opencode" ]]; then client="opencode"; shift; fi
    exec bash "${SCRIPT_DIR}/macos-setup.sh" "$client" "$@"
    ;;
```

**Step 6: Run tests**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: PASS

**Step 7: Commit**

```bash
git add scripts/onboarding/ tests/onboarding/
git commit -m "feat(onboarding): implement 4-phase wizard with deep-links and auto skill install"
```

---

### Task 5: README Rewrite

Replace current README with minimal landing page.

**Files:**
- Modify: `README.md`
- Test: `tests/onboarding/macos-onboarding-script-contract.test.ts`

**Step 1: Write the failing test**

```typescript
it("README is a minimal landing page with npx quick start", () => {
  const content = readFileSync("README.md", "utf8");

  expect(content).toContain("npx ha-nova setup");
  expect(content).toContain("npx ha-nova doctor");
  expect(content).toContain("macOS only");
  expect(content).toContain("Home Assistant OS");
  expect(content).toContain("CONTRIBUTING.md");
  // Should NOT contain old multi-step instructions
  expect(content).not.toContain("npm run install:codex-skill");
  expect(content).not.toContain("npm run onboarding:macos");
  expect(content).not.toContain("deploy:app:fast");
  // Should be short
  const lines = content.split("\n").length;
  expect(lines).toBeLessThan(60);
});
```

**Step 2: Run test to verify it fails**

Expected: FAIL — current README contains old instructions

**Step 3: Rewrite `README.md`**

```markdown
# HA NOVA

AI-powered Home Assistant control. One command to set up, then talk to your home.

> macOS only. Requires Home Assistant OS or Supervised.

## Quick Start

```bash
npx ha-nova setup
```

The setup wizard guides you through everything:
installing the relay, configuring tokens, and connecting your AI client.

## What is HA NOVA?

HA NOVA lets AI assistants (Claude, Codex, OpenCode) control your Home Assistant.
It works through a small relay app on your HA instance that bridges
AI clients to your smart home — securely, with no cloud dependency.

## After Setup

Ask your AI assistant things like:
- "List my automations"
- "Turn off the living room lights"
- "Create an automation that turns on the porch light at sunset"

## Troubleshooting

```bash
npx ha-nova doctor
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT
```

**Step 4: Run tests**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
git add README.md tests/onboarding/
git commit -m "docs: rewrite README as minimal landing page with npx quick start"
```

---

### Task 6: Simplify INSTALL Docs

Replace `.claude/INSTALL.md` and `.codex/INSTALL.md` with minimal pointers to `npx ha-nova setup`.

**Files:**
- Modify: `.claude/INSTALL.md`
- Modify: `.codex/INSTALL.md`
- Test: `tests/onboarding/macos-onboarding-script-contract.test.ts`

**Step 1: Write the failing test**

```typescript
it("INSTALL docs point to npx ha-nova setup", () => {
  const claudeInstall = readFileSync(".claude/INSTALL.md", "utf8");
  const codexInstall = readFileSync(".codex/INSTALL.md", "utf8");

  expect(claudeInstall).toContain("npx ha-nova setup");
  expect(codexInstall).toContain("npx ha-nova setup");

  // Should be short
  expect(claudeInstall.split("\n").length).toBeLessThan(30);
  expect(codexInstall.split("\n").length).toBeLessThan(30);
});
```

**Step 2: Rewrite `.claude/INSTALL.md`**

```markdown
# Installing HA NOVA for Claude Code

## Quick Start

```bash
git clone https://github.com/markusleben/ha-nova.git ~/ha-nova
cd ~/ha-nova && npm install
npx ha-nova setup
```

The wizard handles everything: App installation, authentication, and skill setup.

## Already Set Up?

Run diagnostics:

```bash
npx ha-nova doctor
```
```

**Step 3: Rewrite `.codex/INSTALL.md`**

Same content, with "Codex" instead of "Claude Code".

**Step 4: Run tests**

Run: `npx vitest run tests/onboarding/macos-onboarding-script-contract.test.ts`
Expected: PASS

**Step 5: Commit**

```bash
git add .claude/INSTALL.md .codex/INSTALL.md tests/onboarding/
git commit -m "docs: simplify INSTALL files to point to npx ha-nova setup"
```

---

### Task 7: Update Existing Contract Tests

Update the existing contract tests that reference old file structure and strings to match the new architecture.

**Files:**
- Modify: `tests/onboarding/macos-onboarding-script-contract.test.ts`

**Step 1: Audit and update existing tests**

Review each existing test and update assertions that break due to:
- Functions moved from `macos-lib.sh` to `lib/ui.sh`, `lib/relay.sh`, or `platform/macos.sh`
- New strings in README and INSTALL docs
- New `bin/ha-nova` CLI entry point

Key tests to update:

- `"uses Keychain as primary secret storage"` — now checks `platform/macos.sh` instead of `macos-lib.sh` for `security` commands. Keep checking `macos-lib.sh` sources the platform module.
- `"provides executable split onboarding command scripts"` — add `bin/ha-nova` and new lib files to the list.
- `"documents canonical Codex one-link install entrypoint"` — update for new INSTALL.md format.
- `"exposes npm shortcuts"` — keep as-is (npm scripts still work).

**Step 2: Run full test suite**

Run: `npx vitest run`
Expected: ALL PASS

**Step 3: Commit**

```bash
git add tests/onboarding/
git commit -m "test(onboarding): update contract tests for new wizard architecture"
```

---

### Task 8: Legacy Cleanup

Clean up deprecated files and old backup directories.

**Files:**
- Modify: `docs/user-onboarding-macos.md` (add deprecation note or remove)
- Modify: `.codex/ONBOARDING.md` (update reference if exists)

**Step 1: Update `docs/user-onboarding-macos.md`**

Add at the top:

```markdown
> **Deprecated:** This document is superseded by `npx ha-nova setup`. See [README.md](../README.md).
```

**Step 2: Update `.codex/ONBOARDING.md`**

If it exists and references old instructions, update to point to `npx ha-nova setup`.

**Step 3: Run full test suite**

Run: `npx vitest run`
Expected: ALL PASS

**Step 4: Commit**

```bash
git add docs/ .codex/
git commit -m "chore: deprecate old onboarding docs in favor of npx ha-nova setup"
```

---

### Task 9: Final Verification

End-to-end verification of the complete setup.

**Step 1: Run full test suite**

Run: `npx vitest run`
Expected: ALL PASS

**Step 2: Run typecheck**

Run: `npx tsc --noEmit`
Expected: No errors

**Step 3: Verify CLI works**

```bash
bash scripts/onboarding/bin/ha-nova --help
bash scripts/onboarding/bin/ha-nova doctor
```

Expected: Help text displays, doctor runs checks

**Step 4: Verify README is <40 lines**

```bash
wc -l README.md
```

Expected: Under 40 lines

**Step 5: Final commit (if any fixups)**

```bash
git add -A
git commit -m "chore: final verification pass for publish-readiness"
```
