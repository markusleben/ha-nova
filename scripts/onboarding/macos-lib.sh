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

  echo "[macos-onboarding] Preflight checks:"

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
  require_platform
  require_cmd security
  require_cmd curl

  echo "[macos-onboarding] Step 1/2: detect and validate Home Assistant host."
  load_config

  local default_ha_host
  echo "[..] Detecting Home Assistant host candidates..."
  default_ha_host="$(detect_default_ha_host)"
  echo "[ok] Host detection finished (default: ${default_ha_host})."

  prompt_valid_ha_host "$default_ha_host"

  local default_relay_base_url
  default_relay_base_url="${RELAY_BASE_URL:-http://${HA_HOST}:8791}"

  while true; do
    RELAY_BASE_URL="$(prompt_with_default 'Relay base URL' "$default_relay_base_url")"
    RELAY_BASE_URL="${RELAY_BASE_URL%/}"
    if validate_relay_base_url_format "$RELAY_BASE_URL"; then
      break
    fi
    echo "[macos-onboarding] Invalid relay URL format: ${RELAY_BASE_URL}" >&2
    echo "[macos-onboarding] Expected format: http://<host>:<port> or https://<host>" >&2
    default_relay_base_url="$RELAY_BASE_URL"
  done

  echo "[macos-onboarding] Step 2/2: configure Relay authentication."
  echo "[macos-onboarding] LLAT is not entered in this script."
  echo "[macos-onboarding] Configure LLAT only in App option 'ha_llat' inside Home Assistant."
  local relay_auth_token
  local existing_relay_auth_token
  local relay_token_source="entered"
  existing_relay_auth_token="$(read_keychain_secret "$RELAY_SERVICE")"

  if [[ -n "$existing_relay_auth_token" ]]; then
    echo "[macos-onboarding] Existing Relay token found in Keychain (${RELAY_SERVICE}): $(mask_secret_hint "$existing_relay_auth_token")"
    echo "[macos-onboarding] Press Enter to keep existing token, or paste a new token to rotate."
  else
    echo "[macos-onboarding] No Relay token found in Keychain (${RELAY_SERVICE})."
    echo "[macos-onboarding] Press Enter to auto-generate, or paste a token."
  fi

  echo "Relay auth token (leave empty to keep existing or auto-generate):"
  if ! read -r -s relay_auth_token; then
    die "Interactive input required. Re-run in a terminal."
  fi
  echo

  if [[ -z "$relay_auth_token" ]]; then
    if [[ -n "$existing_relay_auth_token" ]]; then
      relay_auth_token="$existing_relay_auth_token"
      relay_token_source="existing-keychain"
      log "Using existing relay auth token from Keychain."
    else
      relay_auth_token="$(generate_relay_token)"
      relay_token_source="generated"
      log "Generated relay auth token automatically."
      echo "[macos-onboarding] Generated relay auth token (copy now): ${relay_auth_token}"
      echo "[macos-onboarding] Set this value as App option 'relay_auth_token' in NOVA Relay."
    fi
  fi

  echo "[..] Checking Relay health..."
  if ! probe_relay_health "$RELAY_BASE_URL" "$relay_auth_token"; then
    echo "[fail] Relay health check failed."
    echo "[macos-onboarding] Relay check failed: ${RELAY_BASE_URL}/health" >&2
    explain_relay_probe_failure "$RELAY_BASE_URL"
    echo "[macos-onboarding] Install/start NOVA Relay App in Home Assistant and verify relay_auth_token in App options." >&2
    if ! prompt_yes_no "Continue setup to save credentials only (doctor will still fail until Relay is reachable)" "N"; then
      die "Setup aborted until Relay is reachable."
    fi
  elif [[ "$LAST_RELAY_HA_WS_CONNECTED" == "false" ]]; then
    echo "[ok] Relay health reachable."
    echo "[macos-onboarding] Relay reachable, but upstream WS is NOT healthy (ha_ws_connected=false)." >&2
    echo "[macos-onboarding] HA_LLAT is required in App options; configure app option 'ha_llat' and restart the App." >&2
  else
    echo "[ok] Relay health reachable."
  fi

  echo
  echo "[macos-onboarding] Review configuration:"
  echo "  - Home Assistant URL: ${HA_URL}"
  echo "  - Relay base URL: ${RELAY_BASE_URL}"
  echo "  - Relay token: $(mask_secret_hint "$relay_auth_token") (${relay_token_source})"
  echo "  - LLAT location: App option 'ha_llat' (not stored in client Keychain)"
  if ! prompt_yes_no "Save this configuration" "Y"; then
    die "Setup aborted before saving changes."
  fi

  store_keychain_secret "$RELAY_SERVICE" "$relay_auth_token"
  # Legacy cleanup: remove old local LLAT secret to avoid dual-secret drift.
  delete_keychain_secret_if_exists "$LLAT_SERVICE"

  persist_config
  invalidate_doctor_cache

  log "Setup complete."
  echo
  echo "Next commands:"
  echo "  bash scripts/onboarding/macos-onboarding.sh doctor"
  echo "  eval \"\$(bash scripts/onboarding/macos-onboarding.sh env)\""
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
      echo "[macos-onboarding] Ready check passed (cached doctor result, TTL ${ttl_seconds}s)."
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
    echo "[macos-onboarding] Ready check passed (doctor refreshed)."
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
