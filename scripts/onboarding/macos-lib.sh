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
# Legacy keychain service name kept only for cleanup during setup migration.
LLAT_SERVICE="ha-nova.ha-llat"

ensure_config_dir() {
  mkdir -p "$CONFIG_DIR"
  chmod 700 "$CONFIG_DIR"
}

load_config() {
  if [[ -f "$CONFIG_FILE" ]]; then
    # shellcheck disable=SC1090
    source "$CONFIG_FILE"
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

  if [[ "$overall_ok" == "1" ]]; then
    echo "  [ok] Onboarding preflight passed."
    return 0
  fi

  echo "  [fail] Onboarding preflight failed."
  return 1
}

run_setup() {
  local client="${1:-claude}"

  print_header

  # ── Phase 1: Prerequisites ──
  check_prerequisites
  echo ""

  # ── Phase 2: App Installation Guide ──
  # Install/start NOVA Relay App in Home Assistant before configuring tokens.
  print_step 1 4 "Install NOVA Relay in Home Assistant"
  echo ""
  print_info "We need to add the NOVA repository to your Home Assistant."
  print_info "I'll open your browser to do this."
  echo ""
  wait_for_enter "Press [Enter] to open your browser... "
  open_browser "https://my.home-assistant.io/redirect/supervisor_add_addon_repository/?repository_url=https%3A%2F%2Fgithub.com%2Fmarkusleben%2Fha-nova"
  echo ""
  print_info "In Home Assistant:"
  print_info "  1. Confirm adding the repository"
  print_info "  2. Go to Settings > Add-ons > Add-on Store"
  print_info "  3. Search for \"NOVA Relay\""
  print_info "  4. Click Install, wait, then click Start"
  echo ""
  wait_for_enter "Press [Enter] when the add-on is running... "

  # ── Phase 3: Token Setup ──
  print_step 2 4 "Configure Authentication"

  # 3a) Relay token
  # Relay auth token (leave empty to keep existing or auto-generate):
  local relay_auth_token
  local existing_relay_auth_token
  existing_relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"

  if [[ -n "$existing_relay_auth_token" ]]; then
    echo ""
    print_info "Existing relay token found: $(mask_secret_hint "$existing_relay_auth_token")"
    if prompt_yes_no "Keep existing token?" "Y"; then
      relay_auth_token="$existing_relay_auth_token"
      # Using existing relay auth token from Keychain.
      log "Using existing relay auth token from Keychain."
    else
      relay_auth_token="$(generate_relay_token)"
      echo ""
      print_info "New token: ${relay_auth_token}"
    fi
  else
    relay_auth_token="$(generate_relay_token)"
    echo ""
    print_info "Generated a secure relay token:"
    echo ""
    echo "    ${relay_auth_token}"
    echo ""
    print_info "Next: paste this into the NOVA Relay add-on configuration."
    print_info "(The \"relay_auth_token\" field)"
  fi

  if [[ "$relay_auth_token" != "${existing_relay_auth_token:-}" ]]; then
    echo ""
    wait_for_enter "Press [Enter] to open the add-on config... "
    open_browser "https://my.home-assistant.io/redirect/supervisor_addon/?addon=2368fcfa_ha_nova_relay"
    echo ""
    wait_for_enter "Press [Enter] when you've saved the relay token... "
  fi

  # 3b) LLAT guide
  # LLAT location: App option 'ha_llat' (not stored in client Keychain).
  echo ""
  print_info "Now we need a Home Assistant access token (LLAT)."
  print_info "This lets the relay talk to your Home Assistant."
  echo ""
  wait_for_enter "Press [Enter] to open your HA profile... "
  open_browser "https://my.home-assistant.io/redirect/profile/"
  echo ""
  print_info "In Home Assistant:"
  print_info "  1. Scroll to \"Long-Lived Access Tokens\""
  print_info "  2. Click \"Create Token\", name it \"NOVA\""
  print_info "  3. Copy the token"
  print_info "  4. Go to the NOVA Relay add-on > Configuration"
  print_info "  5. Paste into the \"ha_llat\" field"
  print_info "  6. Click Save, then Restart the add-on"
  echo ""
  wait_for_enter "Press [Enter] when done... "

  # ── Phase 4: Verify + Save + Install Skills ──
  print_step 3 4 "Verifying connection"

  # Detect HA host
  echo ""
  print_info "Detecting Home Assistant..."
  local default_ha_host
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
    print_fail "Invalid URL. Expected: http://<host>:<port>"
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
          if ! prompt_yes_no "Continue anyway? (fix later with 'ha-nova doctor')" "Y"; then
            die "Setup aborted."
          fi
        fi
      fi
      break
    else
      attempt=$((attempt + 1))
      print_fail "Can't reach relay at ${RELAY_BASE_URL}"
      explain_relay_probe_failure "$RELAY_BASE_URL"
      if (( attempt >= max_retries )); then
        print_info "Saving config anyway. Run 'ha-nova doctor' after fixing."
        break
      fi
      wait_for_enter "Press [Enter] to retry... "
    fi
  done

  # Save
  store_keychain_secret "$RELAY_SERVICE" "$relay_auth_token"
  delete_keychain_secret_if_exists "$LLAT_SERVICE"
  persist_config
  invalidate_doctor_cache
  print_success "Config saved to ~/.config/ha-nova/"
  print_success "Token stored in macOS Keychain"

  # Install skills
  echo ""
  print_step 4 4 "Installing skills"
  echo ""
  if bash "${REPO_ROOT}/scripts/onboarding/install-local-skills.sh" "$client" 2>&1; then
    print_success "Skills installed for ${client}"
  else
    print_fail "Skill installation failed. Run: npm run install:${client}-skill"
  fi

  # Success banner
  echo ""
  echo "  ======================================="
  echo "  Setup complete!"
  echo ""
  echo "  Try asking: \"List my automations\""
  echo "  Diagnostics: npx ha-nova doctor"
  echo "  ======================================="
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
    # shellcheck disable=SC1090
    if ! source "$DOCTOR_CACHE_FILE"; then
      invalidate_doctor_cache
      use_cache="0"
    fi

    cache_timestamp="${DOCTOR_CACHE_TIMESTAMP:-}"
    cache_ha_url="${DOCTOR_CACHE_HA_URL:-}"
    cache_relay_base_url="${DOCTOR_CACHE_RELAY_BASE_URL:-}"
    cache_relay_token_fingerprint="${DOCTOR_CACHE_RELAY_TOKEN_FINGERPRINT:-}"

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

  local codex_skill_file="${HOME}/.agents/skills/ha-nova/SKILL.md"
  if [[ ! -f "$codex_skill_file" ]]; then
    die "Missing Codex skill file: ${codex_skill_file}. Run: npm run install:codex-skill"
  fi

  local marker_line
  local installed_repo_root
  marker_line="$(grep -F -m1 "ha-nova-managed-install repo_root:" "$codex_skill_file" || true)"
  if [[ -z "$marker_line" ]]; then
    die "Invalid Codex skill installation marker. Re-run: npm run install:codex-skill"
  fi
  installed_repo_root="${marker_line#*repo_root: }"
  installed_repo_root="${installed_repo_root%-->}"
  installed_repo_root="${installed_repo_root%"${installed_repo_root##*[![:space:]]}"}"

  if [[ -n "$installed_repo_root" && "$installed_repo_root" != "$REPO_ROOT" ]]; then
    die "Codex skill points to another repo root (${installed_repo_root}). Re-run: npm run install:codex-skill"
  fi

  echo "  [ok] Codex skill installed: ${codex_skill_file}"
  echo "  [ok] Quick readiness passed."
  echo
  echo "Fresh Codex session prompt:"
  echo "  Use ha-nova skill. Run one read-only Home Assistant action first (for example: list first 5 entities)."
  echo
  echo "Optional contributor deep check:"
  echo "  npm run smoke:app:mvp"
}
