#!/usr/bin/env bash
set -euo pipefail

CONFIG_DIR="${HOME}/.config/ha-nova"
CONFIG_FILE="${CONFIG_DIR}/onboarding.env"
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
source "${SCRIPT_DIR}/platform/macos.sh"
DOCTOR_CACHE_FILE="${CONFIG_DIR}/doctor-cache.env"

RELAY_SERVICE="ha-nova.relay-auth-token"
# Legacy keychain service name kept only for cleanup during setup migration.
LLAT_SERVICE="ha-nova.ha-llat"

LAST_RELAY_STATUS_CODE=""
LAST_RELAY_HA_WS_CONNECTED=""
LAST_RELAY_WS_STATUS_CODE=""
LAST_RELAY_WS_BODY=""

log() {
  echo "[macos-onboarding] $*"
}

die() {
  echo "[macos-onboarding] $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    die "Required command not found: ${cmd}"
  fi
}

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

prompt_with_default() {
  local label="$1"
  local default_value="$2"
  local value

  if ! read -r -p "${label} [${default_value}]: " value; then
    die "Interactive input required. Re-run in a terminal."
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

  if ! read -r -p "${label} [${hint}]: " answer; then
    die "Interactive input required. Re-run in a terminal."
  fi
  if [[ -z "$answer" ]]; then
    answer="$default_answer"
  fi

  [[ "$answer" =~ ^[Yy]$ ]]
}

normalize_host_input() {
  local value="$1"
  local hostport

  value="${value#http://}"
  value="${value#https://}"
  value="${value%%/*}"
  value="${value##*@}"

  hostport="$value"
  if [[ "$hostport" == \[*\]* ]]; then
    hostport="${hostport#\[}"
    hostport="${hostport%%\]*}"
  elif [[ "$hostport" == *:* ]]; then
    hostport="${hostport%%:*}"
  fi

  printf '%s' "$hostport"
}

normalize_url_base_input() {
  local value="$1"
  value="${value%%/}"
  printf '%s' "$value"
}

probe_home_assistant_url_base() {
  local base_url="$1"
  local discovery
  local manifest
  local root_html

  discovery="$(
    curl -sS --connect-timeout 1 --max-time 2 \
      "${base_url}/api/discovery_info" 2>/dev/null || true
  )"
  if [[ "$discovery" == *"location_name"* || "$discovery" == *"base_url"* ]]; then
    return 0
  fi

  manifest="$(
    curl -sS --connect-timeout 1 --max-time 2 \
      "${base_url}/manifest.json" 2>/dev/null || true
  )"
  if [[ "$manifest" == *"Home Assistant"* || "$manifest" == *"start_url"* ]]; then
    return 0
  fi

  root_html="$(
    curl -sS --connect-timeout 1 --max-time 2 \
      "${base_url}/" 2>/dev/null || true
  )"
  if [[ "$root_html" == *"Home Assistant"* || "$root_html" == *"frontend_latest"* ]]; then
    return 0
  fi

  return 1
}

resolve_home_assistant_url_base() {
  local input="$1"
  local normalized_input
  local host
  local candidate
  local candidate_urls=""

  normalized_input="$(normalize_url_base_input "$input")"
  host="$(normalize_host_input "$normalized_input")"

  if [[ "$normalized_input" == http://* || "$normalized_input" == https://* ]]; then
    candidate_urls="${normalized_input}"
  elif [[ "$normalized_input" == *:* ]]; then
    candidate_urls="http://${normalized_input}|https://${normalized_input}"
  else
    candidate_urls="http://${host}:8123|http://${host}|https://${host}"
  fi

  local IFS='|'
  for candidate in $candidate_urls; do
    if probe_home_assistant_url_base "$candidate"; then
      printf '%s' "$candidate"
      return 0
    fi
  done

  return 1
}

probe_home_assistant_host() {
  local input="$1"
  resolve_home_assistant_url_base "$input" >/dev/null
}

guess_home_assistant_url_base() {
  local input="$1"
  local normalized_input
  local host

  normalized_input="$(normalize_url_base_input "$input")"
  host="$(normalize_host_input "$normalized_input")"

  if [[ "$normalized_input" == http://* || "$normalized_input" == https://* ]]; then
    printf '%s' "$normalized_input"
  elif [[ "$normalized_input" == *:* ]]; then
    printf 'http://%s' "$normalized_input"
  else
    printf 'http://%s:8123' "$host"
  fi
}

collect_candidate_hosts() {
  local list=""
  local candidate
  local relay_candidate=""
  local arp_candidates=""

  if [[ -n "${RELAY_BASE_URL:-}" ]]; then
    relay_candidate="$(normalize_host_input "$RELAY_BASE_URL")"
  fi

  if command -v arp >/dev/null 2>&1; then
    arp_candidates="$(
      arp -an 2>/dev/null \
        | sed -nE 's/.*\(([0-9]+\.[0-9]+\.[0-9]+\.[0-9]+)\).*/\1/p' \
        | head -n 4
    )"
  fi

  for candidate in \
    "${HA_HOST:-}" \
    "${relay_candidate}" \
    homeassistant.local \
    home-assistant.local \
    hass.local \
    $arp_candidates
  do
    candidate="$(normalize_host_input "$candidate")"
    [[ -z "$candidate" ]] && continue

    case "|$list|" in
      *"|$candidate|"*) ;;
      *) list="${list}${list:+|}${candidate}" ;;
    esac
  done

  printf '%s' "$list"
}

detect_default_ha_host() {
  local candidate
  local candidates
  local IFS='|'

  candidates="$(collect_candidate_hosts)"
  for candidate in $candidates; do
    if probe_home_assistant_host "$candidate"; then
      printf '%s' "$candidate"
      return
    fi
  done

  printf '%s' "homeassistant.local"
}

prompt_valid_ha_host() {
  local default_host="$1"
  local input
  local host
  local resolved_ha_url

  while true; do
    input="$(prompt_with_default 'Home Assistant URL or host' "$default_host")"
    host="$(normalize_host_input "$input")"

    if [[ -z "$host" ]]; then
      echo "[macos-onboarding] Host input is empty." >&2
      if ! prompt_yes_no "Retry host entry" "Y"; then
        die "Cannot continue without a valid Home Assistant host."
      fi
      default_host="${input:-$default_host}"
      continue
    fi

    echo "[..] Validating Home Assistant endpoint..."
    if resolved_ha_url="$(resolve_home_assistant_url_base "$input")"; then
      echo "[ok] Home Assistant endpoint validated."
      HA_HOST="$host"
      HA_URL="$resolved_ha_url"
      return
    else
      echo "[fail] Home Assistant endpoint validation failed."
      echo "[macos-onboarding] Could not validate Home Assistant host: ${input}" >&2
      echo "[macos-onboarding] Expected a reachable Home Assistant instance (supports URL/host/port)." >&2
    fi

    if ! prompt_yes_no "Retry host entry" "Y"; then
      if prompt_yes_no "Continue with unverified host" "N"; then
        echo "[macos-onboarding] Continuing with unverified host: ${host}" >&2
        HA_HOST="$host"
        HA_URL="$(guess_home_assistant_url_base "$input")"
        return
      fi
      die "Cannot continue without a valid Home Assistant host."
    fi

    default_host="${input:-$default_host}"
  done
}

validate_relay_base_url_format() {
  local url="$1"
  [[ "$url" =~ ^https?://[^[:space:]]+$ ]]
}

probe_relay_health() {
  local base_url="$1"
  local relay_auth_token="$2"
  local response_file
  local headers_file
  local status_code
  local body

  response_file="$(mktemp)"
  headers_file="$(mktemp)"
  status_code="$(
    curl -sS --connect-timeout 2 --max-time 4 \
      -H "Authorization: Bearer ${relay_auth_token}" \
      -D "$headers_file" \
      -o "$response_file" \
      -w "%{http_code}" \
      "${base_url%/}/health" \
      2>/dev/null || true
  )"
  body="$(cat "$response_file" 2>/dev/null || true)"
  rm -f "$response_file"
  local has_json_content_type="0"
  if grep -Eiq '^content-type:[[:space:]]*application/json' "$headers_file"; then
    has_json_content_type="1"
  fi
  rm -f "$headers_file"

  LAST_RELAY_STATUS_CODE="$status_code"
  LAST_RELAY_HA_WS_CONNECTED=""
  if [[ "$body" =~ \"ha_ws_connected\"[[:space:]]*:[[:space:]]*true ]]; then
    LAST_RELAY_HA_WS_CONNECTED="true"
  elif [[ "$body" =~ \"ha_ws_connected\"[[:space:]]*:[[:space:]]*false ]]; then
    LAST_RELAY_HA_WS_CONNECTED="false"
  fi

  if [[ "$status_code" == "200" && "$has_json_content_type" == "1" && "$body" == *'"status"'* ]]; then
    return 0
  fi

  return 1
}

probe_relay_ws_ping() {
  local base_url="$1"
  local relay_auth_token="$2"
  local response_file
  local status_code
  local body

  response_file="$(mktemp)"
  status_code="$(
    curl -sS --connect-timeout 2 --max-time 4 \
      -H "Authorization: Bearer ${relay_auth_token}" \
      -H "Content-Type: application/json" \
      -o "$response_file" \
      -w "%{http_code}" \
      -d '{"type":"ping"}' \
      "${base_url%/}/ws" \
      2>/dev/null || true
  )"
  body="$(cat "$response_file" 2>/dev/null || true)"
  rm -f "$response_file"

  LAST_RELAY_WS_STATUS_CODE="$status_code"
  LAST_RELAY_WS_BODY="$body"

  if [[ "$status_code" == "200" ]]; then
    return 0
  fi

  return 1
}

explain_relay_ws_degraded() {
  case "${LAST_RELAY_WS_STATUS_CODE:-}" in
    "401"|"403")
      echo "         Cause: Relay auth token rejected on /ws (token mismatch)." >&2
      echo "         Action: verify the exact relay_auth_token configured in App options." >&2
      ;;
    "502")
      if [[ "${LAST_RELAY_WS_BODY:-}" == *"LLAT is required"* ]]; then
        echo "         Cause: Relay is reachable, but HA WS authentication failed because HA_LLAT is missing or mismatched." >&2
        echo "         Action: HA_LLAT is required in App options. Set app option 'ha_llat' to a valid LLAT and restart the App." >&2
      else
        echo "         Cause: Relay reached, but upstream HA WS connection failed." >&2
        echo "         Action: inspect App logs and HA core WS availability." >&2
      fi
      ;;
    *)
      echo "         Cause: WS degraded (HTTP ${LAST_RELAY_WS_STATUS_CODE:-unknown})." >&2
      echo "         Action: verify App runtime health and upstream HA connectivity." >&2
      ;;
  esac
}

explain_relay_probe_failure() {
  local relay_base_url="$1"

  case "${LAST_RELAY_STATUS_CODE:-}" in
    "401"|"403")
      echo "[macos-onboarding] Relay auth rejected (HTTP ${LAST_RELAY_STATUS_CODE})." >&2
      echo "[macos-onboarding] Action: use the exact relay_auth_token configured in the NOVA Relay App options." >&2
      ;;
    "404")
      echo "[macos-onboarding] Relay endpoint not found (HTTP 404)." >&2
      echo "[macos-onboarding] Action: verify Relay base URL and ensure NOVA Relay App is installed and started." >&2
      ;;
    "000"|"")
      echo "[macos-onboarding] Relay not reachable at ${relay_base_url}/health." >&2
      echo "[macos-onboarding] Action: verify host/IP, port, and local network reachability." >&2
      ;;
    *)
      echo "[macos-onboarding] Relay check failed (HTTP ${LAST_RELAY_STATUS_CODE})." >&2
      echo "[macos-onboarding] Action: inspect Relay logs and App status in Home Assistant." >&2
      ;;
  esac
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

  die "Cannot generate token automatically (missing openssl and uuidgen)."
}

emit_export() {
  local key="$1"
  local value="$2"
  printf 'export %s=%q\n' "$key" "$value"
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
