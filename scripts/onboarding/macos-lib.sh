#!/usr/bin/env bash
set -euo pipefail

CONFIG_DIR="${HOME}/.config/ha-nova"
CONFIG_FILE="${CONFIG_DIR}/onboarding.env"
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/lib/ui.sh"
source "${SCRIPT_DIR}/lib/relay.sh"
source "${SCRIPT_DIR}/platform/macos.sh"
DOCTOR_CACHE_FILE="${CONFIG_DIR}/doctor-cache.env"

RELAY_SERVICE="ha-nova.relay-auth-token"

ensure_config_dir() {
  mkdir -p "$CONFIG_DIR"
  chmod 700 "$CONFIG_DIR"
}

load_config() {
  if [[ -f "$CONFIG_FILE" ]]; then
    # Parse only known keys (no arbitrary code execution from config file)
    local key value
    while IFS='=' read -r key value; do
      # Strip surrounding quotes produced by printf %q
      value="${value#\'}" ; value="${value%\'}"
      value="${value#\"}" ; value="${value%\"}"
      case "$key" in
        HA_HOST)        HA_HOST="$value" ;;
        HA_URL)         HA_URL="$value" ;;
        RELAY_BASE_URL) RELAY_BASE_URL="$value" ;;
      esac
    done < "$CONFIG_FILE"
  fi
}

persist_config() {
  ensure_config_dir
  umask 077
  {
    printf 'HA_HOST=%q\n' "$HA_HOST"
    printf 'HA_URL=%q\n' "$HA_URL"
    printf 'RELAY_BASE_URL=%q\n' "$RELAY_BASE_URL"
  } > "$CONFIG_FILE"
  chmod 600 "$CONFIG_FILE"
}

invalidate_doctor_cache() {
  rm -f "$DOCTOR_CACHE_FILE"
}

build_env_exports() {
  load_config

  if [[ -z "${HA_HOST:-}" ]]; then
    die "Missing ${CONFIG_FILE}. Run setup first."
  fi

  if [[ -z "${HA_URL:-}" ]]; then
    HA_URL="http://${HA_HOST}:8123"
  fi

  if [[ -z "${RELAY_BASE_URL:-}" ]]; then
    RELAY_BASE_URL="http://${HA_HOST}:8791"
  fi

  local relay_auth_token
  relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"
  if [[ -z "$relay_auth_token" ]]; then
    die "Missing relay auth token in Keychain (${RELAY_SERVICE}). Run setup first."
  fi

  emit_export "HA_HOST" "$HA_HOST"
  emit_export "HA_URL" "$HA_URL"
  emit_export "RELAY_BASE_URL" "$RELAY_BASE_URL"
  emit_export "RELAY_AUTH_TOKEN" "$relay_auth_token"
}

run_doctor_checks() {
  load_config

  local overall_ok="1"
  local relay_auth_token

  echo "[ha-nova] Preflight checks:"

  if [[ -n "${HA_HOST:-}" ]]; then
    echo "  [ok] Config file found: ${CONFIG_FILE}"
  else
    echo "  [fail] Missing config. Run: npm run onboarding:macos"
    overall_ok="0"
  fi

  if [[ -z "${HA_URL:-}" && -n "${HA_HOST:-}" ]]; then
    HA_URL="http://${HA_HOST}:8123"
  fi

  if [[ -z "${RELAY_BASE_URL:-}" && -n "${HA_HOST:-}" ]]; then
    RELAY_BASE_URL="http://${HA_HOST}:8791"
  fi

  relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"
  if [[ -n "$relay_auth_token" ]]; then
    echo "  [ok] Keychain token found (${RELAY_SERVICE})"
  else
    echo "  [fail] Keychain token missing (${RELAY_SERVICE}). Re-run setup."
    overall_ok="0"
  fi

  if [[ -n "${HA_URL:-}" ]] && probe_home_assistant_url_base "$HA_URL"; then
    echo "  [ok] Home Assistant reachable: ${HA_URL}"
  else
    echo "  [fail] Home Assistant not reachable: ${HA_URL:-<unset>}"
    overall_ok="0"
  fi

  if [[ -n "${RELAY_BASE_URL:-}" && -n "$relay_auth_token" ]] && probe_relay_health "$RELAY_BASE_URL" "$relay_auth_token"; then
    echo "  [ok] Relay health reachable: ${RELAY_BASE_URL}/health"
    # Version check
    if [[ -n "$LAST_RELAY_VERSION" ]]; then
      local min_relay_version=""
      local vf="${REPO_ROOT}/version.json"
      if [[ -f "$vf" ]]; then
        min_relay_version=$(grep -o '"min_relay_version"[[:space:]]*:[[:space:]]*"[^"]*"' "$vf" 2>/dev/null | sed 's/.*"\([^"]*\)"$/\1/' || true)
      fi
      if [[ -n "$min_relay_version" && "$min_relay_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] && semver_lt "$LAST_RELAY_VERSION" "$min_relay_version"; then
        echo "  [warn] Relay version ${LAST_RELAY_VERSION} is below minimum ${min_relay_version}. Update: HA Settings > Apps > NOVA Relay > Update"
        overall_ok="0"
      else
        echo "  [ok] Relay version: ${LAST_RELAY_VERSION}"
      fi
    fi
    if [[ "$LAST_RELAY_HA_WS_CONNECTED" == "false" ]]; then
      # Runtime keeps WS lazy-connected; validate once via ping before failing.
      if probe_relay_ws_ping "$RELAY_BASE_URL" "$relay_auth_token"; then
        echo "  [ok] Relay /ws ping succeeded (upstream WS operational)."
      else
        echo "  [fail] Relay reports degraded upstream WS capability (ha_ws_connected=false)."
        echo "         Action: HA_LLAT is required in App options. Verify app option 'ha_llat' and restart the App."
        explain_relay_ws_degraded
        overall_ok="0"
      fi
    fi
  else
    echo "  [fail] Relay health check failed: ${RELAY_BASE_URL:-<unset>}/health"
    explain_relay_probe_failure "${RELAY_BASE_URL:-<unset>}"
    echo "         Action: install/start NOVA Relay App and verify relay_auth_token."
    overall_ok="0"
  fi

  # Remote update check (synchronous in doctor — user explicitly asked for diagnostics)
  local vf="${REPO_ROOT}/version.json"
  if [[ -f "$vf" ]]; then
    local local_skill_version
    local_skill_version=$(grep -o '"skill_version"[[:space:]]*:[[:space:]]*"[^"]*"' "$vf" 2>/dev/null | sed 's/.*"\([^"]*\)"$/\1/' || true)
    local remote_json
    remote_json=$(curl -sS --connect-timeout 2 --max-time 5 "https://raw.githubusercontent.com/markusleben/ha-nova/main/version.json" 2>/dev/null || true)
    if [[ -n "$remote_json" ]]; then
      local latest_skill_version
      latest_skill_version=$(echo "$remote_json" | grep -o '"skill_version"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"\([^"]*\)"$/\1/')
      if [[ -n "$latest_skill_version" && "$latest_skill_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && -n "$local_skill_version" ]] && semver_lt "$local_skill_version" "$latest_skill_version"; then
        echo "  [warn] Skills update available: v${local_skill_version} -> v${latest_skill_version}. See: https://github.com/markusleben/ha-nova/blob/main/skills/ha-nova/update-guide.md"
      else
        echo "  [ok] Skills version: ${local_skill_version:-unknown} (up to date)"
      fi
      # Update the cache for SessionStart hook
      local update_cache_dir="${HOME}/.cache/ha-nova"
      mkdir -p "$update_cache_dir" 2>/dev/null || true
      # Only cache if response contains valid JSON (prevents caching 404 pages)
      if echo "$remote_json" | grep -q '"skill_version"' 2>/dev/null; then
        echo "$remote_json" > "${update_cache_dir}/latest-version.json" 2>/dev/null || true
      fi
    else
      echo "  [info] Skills version: ${local_skill_version:-unknown} (remote check failed, offline?)"
    fi
  fi

  if [[ "$overall_ok" == "1" ]]; then
    echo "  [ok] Onboarding preflight passed."
    return 0
  fi

  echo "  [fail] Onboarding preflight failed."
  return 1
}

# Sub-skill names needed by detect_setup_state.
HA_NOVA_SUB_SKILLS=(
  "write"
  "read"
  "helper"
  "entity-discovery"
  "onboarding"
  "service-call"
  "review"
)

detect_setup_state() {
  local client="$1"
  local relay_auth_token

  # Config present? Only trust the config file, not inherited env vars.
  SETUP_HAS_CONFIG="0"
  if [[ -f "$CONFIG_FILE" ]]; then
    load_config
    if [[ -n "${HA_HOST:-}" && -n "${HA_URL:-}" && -n "${RELAY_BASE_URL:-}" ]]; then
      SETUP_HAS_CONFIG="1"
    fi
  fi

  # Relay token in Keychain?
  relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"
  if [[ -n "$relay_auth_token" ]]; then
    SETUP_HAS_TOKEN="1"
  else
    SETUP_HAS_TOKEN="0"
  fi

  # Relay reachable + auth OK?
  SETUP_RELAY_OK="0"
  SETUP_WS_OK="0"
  if [[ "$SETUP_HAS_CONFIG" == "1" && "$SETUP_HAS_TOKEN" == "1" ]]; then
    if probe_relay_health "$RELAY_BASE_URL" "$relay_auth_token"; then
      SETUP_RELAY_OK="1"
      if [[ "$LAST_RELAY_HA_WS_CONNECTED" == "true" ]]; then
        SETUP_WS_OK="1"
      elif [[ "$LAST_RELAY_HA_WS_CONNECTED" == "false" ]]; then
        if probe_relay_ws_ping "$RELAY_BASE_URL" "$relay_auth_token"; then
          SETUP_WS_OK="1"
        fi
      fi
    fi
  fi

  # Skills installed?
  SETUP_SKILLS_OK="1"
  case "$client" in
    codex)
      # Symlink check: ha-nova -> repo/skills (also catches broken symlinks)
      if [[ ! -L "${HOME}/.agents/skills/ha-nova" ]]; then
        SETUP_SKILLS_OK="0"
      elif [[ ! -e "${HOME}/.agents/skills/ha-nova" ]]; then
        # Symlink exists but target is gone (broken)
        SETUP_SKILLS_OK="0"
      elif [[ ! -f "${HOME}/.agents/skills/ha-nova/ha-nova/SKILL.md" ]]; then
        SETUP_SKILLS_OK="0"
      fi
      ;;
    gemini)
      # Flat copy check: ha-nova/SKILL.md + ha-nova-{sub}/SKILL.md
      if [[ ! -f "${HOME}/.agents/skills/ha-nova/SKILL.md" ]]; then
        SETUP_SKILLS_OK="0"
      else
        for sub_skill in "${HA_NOVA_SUB_SKILLS[@]}"; do
          if [[ ! -f "${HOME}/.agents/skills/ha-nova-${sub_skill}/SKILL.md" ]]; then
            SETUP_SKILLS_OK="0"
            break
          fi
        done
      fi
      ;;
    claude)
      # Check plugin is installed via claude CLI
      if command -v claude &>/dev/null; then
        if ! claude plugin list 2>/dev/null | grep -q "ha-nova"; then
          SETUP_SKILLS_OK="0"
        fi
      elif [[ ! -f "${REPO_ROOT}/.claude-plugin/plugin.json" ]]; then
        SETUP_SKILLS_OK="0"
      fi
      ;;
    opencode)
      # Symlink check (also catches broken symlinks)
      if [[ ! -L "${HOME}/.config/opencode/skills/ha-nova" ]]; then
        SETUP_SKILLS_OK="0"
      elif [[ ! -e "${HOME}/.config/opencode/skills/ha-nova" ]]; then
        SETUP_SKILLS_OK="0"
      elif [[ ! -f "${HOME}/.config/opencode/skills/ha-nova/ha-nova/SKILL.md" ]]; then
        SETUP_SKILLS_OK="0"
      fi
      ;;
    all)
      # Check each client's skills without recursive detect_setup_state
      # (which would overwrite SETUP_HAS_CONFIG/TOKEN/RELAY_OK/WS_OK)
      for sub_skill in "${HA_NOVA_SUB_SKILLS[@]}"; do
        # Codex symlink (flat layout: skills/{sub}/SKILL.md through symlink)
        if [[ ! -L "${HOME}/.agents/skills/ha-nova" ]] || [[ ! -f "${HOME}/.agents/skills/ha-nova/${sub_skill}/SKILL.md" ]]; then
          SETUP_SKILLS_OK="0"
          break
        fi
        # Gemini flat
        if [[ ! -f "${HOME}/.agents/skills/ha-nova-${sub_skill}/SKILL.md" ]]; then
          SETUP_SKILLS_OK="0"
          break
        fi
      done
      # OpenCode symlink
      if [[ "$SETUP_SKILLS_OK" == "1" ]]; then
        if [[ ! -L "${HOME}/.config/opencode/skills/ha-nova" ]] || [[ ! -f "${HOME}/.config/opencode/skills/ha-nova/ha-nova/SKILL.md" ]]; then
          SETUP_SKILLS_OK="0"
        fi
      fi
      # Claude plugin
      if [[ "$SETUP_SKILLS_OK" == "1" ]]; then
        if [[ ! -f "${REPO_ROOT}/.claude-plugin/plugin.json" ]]; then
          SETUP_SKILLS_OK="0"
        fi
      fi
      ;;
    *)
      SETUP_SKILLS_OK="0"
      ;;
  esac
}

print_setup_status() {
  echo ""
  print_info "Checking current setup..."
  if [[ "$SETUP_RELAY_OK" == "1" ]]; then
    print_success "Relay reachable"
  else
    print_fail "Relay not reachable"
  fi
  if [[ "$SETUP_RELAY_OK" == "1" ]]; then
    print_success "Authentication valid"
  elif [[ "$SETUP_HAS_TOKEN" == "1" ]]; then
    print_fail "Authentication failed"
  else
    print_fail "No auth token found"
  fi
  if [[ "$SETUP_WS_OK" == "1" ]]; then
    print_success "WebSocket connected"
  else
    print_fail "WebSocket not connected"
  fi
  if [[ "$SETUP_SKILLS_OK" == "1" ]]; then
    print_success "Skills installed"
  else
    print_fail "Skills not installed"
  fi
  echo ""
}

pick_client() {
  echo "" >&2
  echo "  Which AI client do you use?" >&2
  echo "" >&2
  echo "    1) Claude Code" >&2
  echo "    2) Codex CLI" >&2
  echo "    3) OpenCode" >&2
  echo "    4) Gemini CLI" >&2
  echo "    5) All of the above" >&2
  echo "" >&2
  printf "  Enter [1-5] (default 1): " >&2
  read -r choice
  case "${choice:-1}" in
    1) echo "claude" ;;
    2) echo "codex" ;;
    3) echo "opencode" ;;
    4) echo "gemini" ;;
    5) echo "all" ;;
    *) echo "claude" ;;
  esac
}

run_setup() {
  local client="${1:-}"

  # CLI flags (set by bin/ha-nova via env vars)
  local flag_host="${HA_NOVA_HOST:-}"
  local flag_token="${HA_NOVA_TOKEN:-}"

  clear_screen
  print_header

  if [[ -z "$client" ]]; then
    client="$(pick_client)"
  fi

  # ── Phase 1: Prerequisites ──
  check_prerequisites
  echo ""

  # ── Smart Resume: detect what's already done ──
  detect_setup_state "$client"

  if [[ "$SETUP_RELAY_OK" == "1" && "$SETUP_WS_OK" == "1" && "$SETUP_SKILLS_OK" == "1" ]]; then
    print_setup_status
    echo ""
    print_success "Everything is already set up!"
    print_info "Run 'npx ha-nova doctor' for full diagnostics."
    echo ""
    return 0
  fi

  # Determine which phases to skip
  local skip_app_install="0"
  local skip_relay_token="0"
  local skip_llat="0"
  local skip_verify="0"
  local skip_skills="0"

  if [[ "$SETUP_HAS_CONFIG" == "1" ]]; then
    skip_app_install="1"
  fi
  if [[ "$SETUP_RELAY_OK" == "1" ]]; then
    skip_relay_token="1"
  fi
  if [[ "$SETUP_WS_OK" == "1" ]]; then
    skip_llat="1"
  fi
  if [[ "$SETUP_RELAY_OK" == "1" ]]; then
    skip_verify="1"
  fi
  if [[ "$SETUP_SKILLS_OK" == "1" ]]; then
    skip_skills="1"
  fi

  # CLI flags override: --host and/or --token skip the corresponding prompts
  if [[ -n "$flag_host" ]]; then
    skip_app_install="1"
  fi
  if [[ -n "$flag_token" ]]; then
    skip_app_install="1"
    skip_relay_token="1"
  fi
  if [[ -n "$flag_host" && -n "$flag_token" ]]; then
    skip_llat="1"
  fi

  # Show status if any phase is being skipped
  if [[ "$skip_app_install" == "1" || "$skip_relay_token" == "1" || "$skip_llat" == "1" || "$skip_skills" == "1" ]]; then
    print_setup_status

    local skip_summary=""
    [[ "$skip_app_install" == "1" ]] && skip_summary="${skip_summary}app installation, "
    [[ "$skip_relay_token" == "1" ]] && skip_summary="${skip_summary}relay token, "
    [[ "$skip_llat" == "1" ]] && skip_summary="${skip_summary}access token, "
    [[ "$skip_verify" == "1" ]] && skip_summary="${skip_summary}connection check, "
    [[ "$skip_skills" == "1" ]] && skip_summary="${skip_summary}skill installation, "
    skip_summary="${skip_summary%, }"
    print_info "Already done: ${skip_summary}"
    echo ""
  fi

  # ── Phase 1b: Resolve HA host early (needed for deeplinks in Phase 2+3) ──
  if [[ -z "${HA_URL:-}" ]]; then
    if [[ -n "$flag_host" ]]; then
      HA_HOST="$(normalize_host_input "$flag_host")"
      local resolved
      if resolved="$(resolve_home_assistant_url_base "$flag_host")"; then
        HA_URL="$resolved"
      else
        HA_URL="$(guess_home_assistant_url_base "$flag_host")"
      fi
    else
      load_config
      if [[ -z "${HA_URL:-}" ]]; then
        echo ""
        local default_ha_host
        if with_spinner "Discovering Home Assistant on your network..." detect_default_ha_host; then
          default_ha_host="$SPINNER_RESULT"
        else
          default_ha_host="homeassistant.local"
        fi
        prompt_valid_ha_host "$default_ha_host"
      fi
    fi
  fi

  # ── Phase 2: App Installation Guide ──
  local relay_auth_token="${flag_token:-}"
  local existing_relay_auth_token

  if [[ "$skip_app_install" == "0" ]]; then
    # Install NOVA Relay App in Home Assistant (do NOT start yet — tokens come first).
    clear_screen
    print_header
    print_step 1 4 "Install NOVA Relay in Home Assistant"
    echo ""
    print_info "I'll open your browser to add the HA NOVA repository."
    print_info "Just click \"Open link\" when prompted."
    echo ""
    wait_for_enter "Press [Enter] to open your browser... "
    open_browser "https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fmarkusleben%2Fha-nova"
    echo ""
    print_info "Once the repository is added:"
    print_info "  1. Go to Settings > Apps > App Store"
    print_info "  2. Search for \"NOVA Relay\""
    print_info "  3. Click Install and wait for it to finish"
    print_info "     (don't start the app yet — we'll set up the tokens first)"
    echo ""
    wait_for_enter "Press [Enter] when the installation is complete... "
  fi

  # ── Phase 3: Token Setup ──
  if [[ "$skip_relay_token" == "0" ]]; then
    clear_screen
    print_header
    print_step 2 4 "Set up secure access"
    echo ""
    print_info "NOVA needs two passwords (\"tokens\") to work securely:"
    print_info "  a) Relay token — keeps the connection between this Mac and Home Assistant private"
    print_info "  b) HA access token — allows the relay to control your devices and automations"
    echo ""

    # 3a) Relay token
    # Relay auth token (leave empty to keep existing or auto-generate):
    existing_relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"

    if [[ -n "$existing_relay_auth_token" ]]; then
      echo ""
      print_info "Existing relay token found: $(mask_secret_hint "$existing_relay_auth_token")"
      if prompt_yes_no "Keep existing token?" "Y"; then
        relay_auth_token="$existing_relay_auth_token"
        if prompt_yes_no "Copy token to clipboard? (to paste into app config)" "N"; then
          if command -v pbcopy >/dev/null 2>&1; then
            printf '%s' "$relay_auth_token" | pbcopy
            print_success "Copied to clipboard."
          else
            echo ""
            echo "    ${relay_auth_token}"
            echo ""
          fi
        fi
        log "Using existing relay auth token from Keychain."
      else
        relay_auth_token="$(generate_relay_token)"
        store_keychain_secret "$RELAY_SERVICE" "$relay_auth_token"
        echo ""
        print_info "New token: ${relay_auth_token}"
        print_success "Saved to Keychain."
        if command -v pbcopy >/dev/null 2>&1; then
          printf '%s' "$relay_auth_token" | pbcopy
          print_success "Copied to clipboard."
        fi
      fi
    else
      relay_auth_token="$(generate_relay_token)"
      store_keychain_secret "$RELAY_SERVICE" "$relay_auth_token"
      echo ""
      print_info "Here is your relay token (generated automatically):"
      echo ""
      echo "    ${relay_auth_token}"
      echo ""
      print_success "Saved to Keychain — safe even if you quit now."
      if command -v pbcopy >/dev/null 2>&1; then
        printf '%s' "$relay_auth_token" | pbcopy
        print_success "Copied to clipboard — just paste it in the next step."
      else
        print_info "Copy the token above — you'll paste it in the next step."
      fi
      print_info "You can press [c] at any prompt to copy it again."
    fi

    if [[ "$relay_auth_token" != "${existing_relay_auth_token:-}" ]]; then
      echo ""
      print_info "I'll open the NOVA Relay settings in your browser."
      print_info "Paste the token into the \"relay_auth_token\" field and click Save."
      echo ""
      wait_for_enter_or_copy "Press [Enter] to open the settings..." "$relay_auth_token"
      open_browser "${HA_URL}/hassio/addon/2368fcfa_ha_nova_relay/config"
      echo ""
      wait_for_enter_or_copy "Press [Enter] when you've saved the token..." "$relay_auth_token"
    fi
  else
    # CLI --token takes precedence; fall back to Keychain
    if [[ -z "$relay_auth_token" ]]; then
      relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"
    fi
  fi

  # 3b) LLAT guide
  if [[ "$skip_llat" == "0" ]]; then
    # LLAT location: App option 'ha_llat' (not stored in client Keychain).
    echo ""
    print_info "Now for the second token: a Home Assistant access token."
    print_info "This lets the relay control your devices and automations."
    echo ""
    print_info "I'll open your Home Assistant profile page."
    print_info "Follow these steps there:"
    echo ""
    print_info "  1. Click the \"Security\" tab"
    print_info "  2. Scroll down to \"Long-Lived Access Tokens\""
    print_info "  3. Click \"Create Token\" and name it \"NOVA\""
    print_info "  4. Copy the token that appears"
    echo ""
    wait_for_enter "Press [Enter] to open your HA profile... "
    open_browser "${HA_URL}/profile/security"
    echo ""
    print_info "Got it? Now I'll open the NOVA Relay settings so you can paste it."
    echo ""
    wait_for_enter "Press [Enter] to open the relay settings... "
    open_browser "${HA_URL}/hassio/addon/2368fcfa_ha_nova_relay/config"
    echo ""
    print_info "  5. Paste the token into the \"ha_llat\" field"
    print_info "  6. Click Save"
    print_info "  7. Click Start (or Restart if already running)"
    echo ""
    wait_for_enter "Press [Enter] when the app is running... "

    # Quick WS re-check if relay was already OK (no full verify needed)
    if [[ "$skip_verify" == "1" ]]; then
      echo ""
      if with_spinner "Checking connection..." probe_relay_health "$RELAY_BASE_URL" "$relay_auth_token"; then
        if [[ "$LAST_RELAY_HA_WS_CONNECTED" == "true" ]]; then
          print_success "Connected to Home Assistant"
        elif probe_relay_ws_ping "$RELAY_BASE_URL" "$relay_auth_token"; then
          print_success "Connected to Home Assistant"
        else
          print_fail "Relay is running but can't reach Home Assistant. Run: npx ha-nova doctor"
        fi
      else
        print_fail "Can't reach the relay. Run: npx ha-nova doctor"
      fi
    fi
  fi

  # ── Phase 4: Verify + Save + Install Skills ──
  if [[ "$skip_verify" == "0" ]]; then
    clear_screen
    print_header
    print_step 3 4 "Verifying connection"

    # HA_HOST/HA_URL already resolved in Phase 1b

    local default_relay_base_url
    default_relay_base_url="${RELAY_BASE_URL:-http://${HA_HOST}:8791}"

    # Auto-derive relay URL from known HA host — only prompt if verification fails
    RELAY_BASE_URL="$default_relay_base_url"

    # Verify relay
    echo ""
    local max_retries=3
    local non_interactive_verify="0"
    [[ -n "$flag_host" && -n "$flag_token" ]] && non_interactive_verify="1"

    local attempt=0
    while true; do
      if with_spinner "Connecting to NOVA Relay..." probe_relay_health "$RELAY_BASE_URL" "$relay_auth_token"; then
        print_success "NOVA Relay is running at ${RELAY_BASE_URL}"
        if [[ "$LAST_RELAY_HA_WS_CONNECTED" == "true" ]]; then
          print_success "Connected to Home Assistant"
        elif [[ "$LAST_RELAY_HA_WS_CONNECTED" == "false" ]]; then
          if probe_relay_ws_ping "$RELAY_BASE_URL" "$relay_auth_token"; then
            print_success "Connected to Home Assistant"
          else
            echo ""
            print_fail "The relay is running but can't reach Home Assistant."
            print_info "This usually means the HA access token (ha_llat) is missing or incorrect."
            explain_relay_ws_degraded
            if [[ "$non_interactive_verify" == "1" ]]; then
              print_info "Continuing — you can fix this later with: npx ha-nova doctor"
            elif ! prompt_yes_no "Continue anyway? (you can fix this later)" "Y"; then
              die "Setup aborted."
            fi
          fi
        fi
        break
      else
        attempt=$((attempt + 1))
        print_fail "Can't reach the relay at ${RELAY_BASE_URL}"
        echo ""
        print_info "Quick checklist — please verify:"
        print_info "  1. Is the NOVA Relay app started in Home Assistant?"
        print_info "     (Settings > Apps > NOVA Relay > Start)"
        print_info "  2. Is the relay_auth_token saved in the app settings?"
        if [[ -n "${relay_auth_token:-}" ]]; then
          print_info "     Your token: $(mask_secret_hint "$relay_auth_token")"
        fi
        print_info "  3. Is the HA access token (ha_llat) saved in the app settings?"
        print_info "  4. Is this Mac on the same network as Home Assistant?"
        echo ""
        if (( attempt >= max_retries )) || [[ "$non_interactive_verify" == "1" ]]; then
          print_info "Saving your settings anyway. You can troubleshoot later with: npx ha-nova doctor"
          break
        fi
        if [[ "$non_interactive_verify" == "0" ]]; then
          # Retry prompt with copy shortcut
          local retry_input=""
          if ! read -r -p "  [Enter] retry · [c] copy token · or enter a different URL: " retry_input; then
            retry_input=""
          fi
          if [[ "$retry_input" =~ ^[Cc]$ ]]; then
            if command -v pbcopy >/dev/null 2>&1 && [[ -n "${relay_auth_token:-}" ]]; then
              printf '%s' "$relay_auth_token" | pbcopy
              print_success "Token copied to clipboard."
            else
              echo ""
              echo "    ${relay_auth_token:-<no token>}"
              echo ""
            fi
            # Don't count copy as a retry attempt — let them try again
            attempt=$((attempt - 1))
          elif [[ -n "$retry_input" ]]; then
            RELAY_BASE_URL="${retry_input%/}"
          fi
        fi
      fi
    done

    # Save
    store_keychain_secret "$RELAY_SERVICE" "$relay_auth_token"
    persist_config
    invalidate_doctor_cache
    print_success "Config saved to ~/.config/ha-nova/"
    print_success "Token stored in macOS Keychain"
  fi

  if [[ "$skip_skills" == "0" ]]; then
    echo ""
    clear_screen
    print_header
    print_step 4 4 "Installing HA NOVA skills"
    if with_spinner "Setting up HA NOVA for ${client}..." bash "${REPO_ROOT}/scripts/onboarding/install-local-skills.sh" "$client"; then
      print_success "HA NOVA skills installed for ${client}"
    else
      print_fail "Skill installation had issues. You can retry with: npm run install:${client}-skill"
    fi
  fi

  # Success banner
  clear_screen
  print_header

  # Friendly client name for the banner
  local client_label
  case "$client" in
    claude)   client_label="Claude Code" ;;
    codex)    client_label="Codex CLI" ;;
    opencode) client_label="OpenCode" ;;
    gemini)   client_label="Gemini CLI" ;;
    all)      client_label="your AI assistant" ;;
    *)        client_label="your AI assistant" ;;
  esac

  echo ""
  print_success "Setup complete!"
  echo ""
  print_info "Open ${client_label} and try asking:"
  echo ""
  echo "    \"Turn off the living room light\""
  echo "    \"List my automations\""
  echo ""
  print_info "Need help? Run: npx ha-nova doctor"
  echo ""
}

run_doctor() {
  require_platform
  require_cmd security
  require_cmd curl
  if ! run_doctor_checks; then
    invalidate_doctor_cache
    return 1
  fi
}

run_env() {
  require_platform
  require_cmd security
  build_env_exports
}

run_ready() {
  local quiet="0"
  if [[ "${1:-}" == "--quiet" ]]; then
    quiet="1"
    shift || true
  fi

  require_platform
  require_cmd security
  require_cmd curl

  load_config
  local ttl_seconds="${READY_TTL_SECONDS:-900}"
  local now
  local relay_auth_token
  local relay_token_fingerprint
  now="$(date +%s)"
  local use_cache="0"

  relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"
  relay_token_fingerprint="$(fingerprint_secret "$relay_auth_token")"

  if [[ -z "$relay_auth_token" ]]; then
    use_cache="0"
  fi

  if [[ -f "$DOCTOR_CACHE_FILE" ]]; then
    local cache_timestamp=""
    local cache_ha_url=""
    local cache_relay_base_url=""
    local cache_relay_token_fingerprint=""
    # Parse only known keys (no arbitrary code execution from cache file)
    local _ck _cv
    while IFS='=' read -r _ck _cv; do
      _cv="${_cv#\'}" ; _cv="${_cv%\'}"
      _cv="${_cv#\"}" ; _cv="${_cv%\"}"
      case "$_ck" in
        DOCTOR_CACHE_TIMESTAMP)              cache_timestamp="$_cv" ;;
        DOCTOR_CACHE_HA_URL)                 cache_ha_url="$_cv" ;;
        DOCTOR_CACHE_RELAY_BASE_URL)         cache_relay_base_url="$_cv" ;;
        DOCTOR_CACHE_RELAY_TOKEN_FINGERPRINT) cache_relay_token_fingerprint="$_cv" ;;
      esac
    done < "$DOCTOR_CACHE_FILE" || { invalidate_doctor_cache; use_cache="0"; }

    if [[ "$cache_timestamp" =~ ^[0-9]+$ ]]; then
      if (( now - cache_timestamp <= ttl_seconds )) \
        && [[ -n "$relay_auth_token" ]] \
        && [[ "${HA_URL:-}" == "$cache_ha_url" ]] \
        && [[ "${RELAY_BASE_URL:-}" == "$cache_relay_base_url" ]] \
        && [[ "$relay_token_fingerprint" == "$cache_relay_token_fingerprint" ]]; then
        use_cache="1"
      fi
    fi
  fi

  if [[ "$use_cache" == "1" ]]; then
    if [[ "$quiet" != "1" ]]; then
      echo "[ha-nova] Ready check passed (cached doctor result, TTL ${ttl_seconds}s)."
    fi
    return 0
  fi

  if [[ "$quiet" == "1" ]]; then
    if ! run_doctor_checks >/dev/null 2>&1; then
      invalidate_doctor_cache
      # Re-run once with visible output to provide actionable errors.
      run_doctor_checks
      return 1
    fi
  else
    run_doctor_checks
  fi

  ensure_config_dir
  umask 077
  {
    printf 'DOCTOR_CACHE_TIMESTAMP=%q\n' "$now"
    printf 'DOCTOR_CACHE_HA_URL=%q\n' "${HA_URL:-}"
    printf 'DOCTOR_CACHE_RELAY_BASE_URL=%q\n' "${RELAY_BASE_URL:-}"
    printf 'DOCTOR_CACHE_RELAY_TOKEN_FINGERPRINT=%q\n' "$relay_token_fingerprint"
  } > "$DOCTOR_CACHE_FILE"
  chmod 600 "$DOCTOR_CACHE_FILE"
  if [[ "$quiet" != "1" ]]; then
    echo "[ha-nova] Ready check passed (doctor refreshed)."
  fi
}

run_quick() {
  require_platform
  require_cmd security
  require_cmd curl

  run_ready --quiet

  local codex_skill_link="${HOME}/.agents/skills/ha-nova"
  if [[ -L "$codex_skill_link" ]]; then
    local link_target
    link_target="$(readlink "$codex_skill_link")"
    if [[ "$link_target" != "${REPO_ROOT}/skills" ]]; then
      die "Codex skill symlink points to wrong repo (${link_target}). Re-run: npm run install:codex-skill"
    fi
    if [[ ! -f "${codex_skill_link}/ha-nova/SKILL.md" ]]; then
      die "Codex skill symlink broken. Re-run: npm run install:codex-skill"
    fi
    echo "  [ok] Codex skill installed (symlink): ${codex_skill_link}"
  elif [[ -d "$codex_skill_link" ]]; then
    # Legacy copy — still functional but recommend re-install
    if [[ ! -f "${codex_skill_link}/ha-nova/SKILL.md" ]]; then
      die "Missing Codex skill file. Re-run: npm run install:codex-skill"
    fi
    echo "  [ok] Codex skill installed (copy, consider re-installing for symlink): ${codex_skill_link}"
  else
    die "Missing Codex skill: ${codex_skill_link}. Run: npm run install:codex-skill"
  fi
  echo "  [ok] Quick readiness passed."
  echo
  echo "Fresh Codex session prompt:"
  echo "  Use ha-nova skill. Run one read-only Home Assistant action first (for example: list first 5 entities)."
  echo
  echo "Optional contributor deep check:"
  echo "  npm run smoke:app:mvp"
}
