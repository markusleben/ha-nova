#!/usr/bin/env bash
# Platform-independent Relay and Home Assistant probe helpers.
# Sourced by macos-lib.sh — do not execute directly.
# Depends on lib/ui.sh being sourced first (for prompt_with_default, prompt_yes_no, die, log).
set -euo pipefail

# ---------------------------------------------------------------------------
# Shared probe state (set by probe_relay_health / probe_relay_ws_ping)
# ---------------------------------------------------------------------------

LAST_RELAY_STATUS_CODE=""
LAST_RELAY_HA_WS_CONNECTED=""
LAST_RELAY_WS_STATUS_CODE=""
LAST_RELAY_WS_BODY=""

# ---------------------------------------------------------------------------
# Host / URL normalization
# ---------------------------------------------------------------------------

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

# ---------------------------------------------------------------------------
# Home Assistant probing
# ---------------------------------------------------------------------------

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
      echo "[ha-nova] Host input is empty." >&2
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
      echo "[ha-nova] Could not validate Home Assistant host: ${input}" >&2
      echo "[ha-nova] Expected a reachable Home Assistant instance (supports URL/host/port)." >&2
    fi

    if ! prompt_yes_no "Retry host entry" "Y"; then
      if prompt_yes_no "Continue with unverified host" "N"; then
        echo "[ha-nova] Continuing with unverified host: ${host}" >&2
        HA_HOST="$host"
        HA_URL="$(guess_home_assistant_url_base "$input")"
        return
      fi
      die "Cannot continue without a valid Home Assistant host."
    fi

    default_host="${input:-$default_host}"
  done
}

# ---------------------------------------------------------------------------
# Relay probing
# ---------------------------------------------------------------------------

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

# ---------------------------------------------------------------------------
# Relay diagnostics
# ---------------------------------------------------------------------------

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
      echo "[ha-nova] Relay auth rejected (HTTP ${LAST_RELAY_STATUS_CODE})." >&2
      echo "[ha-nova] Action: use the exact relay_auth_token configured in the NOVA Relay App options." >&2
      ;;
    "404")
      echo "[ha-nova] Relay endpoint not found (HTTP 404)." >&2
      echo "[ha-nova] Action: verify Relay base URL and ensure NOVA Relay App is installed and started." >&2
      ;;
    "000"|"")
      echo "[ha-nova] Relay not reachable at ${relay_base_url}/health." >&2
      echo "[ha-nova] Action: verify host/IP, port, and local network reachability." >&2
      ;;
    *)
      echo "[ha-nova] Relay check failed (HTTP ${LAST_RELAY_STATUS_CODE})." >&2
      echo "[ha-nova] Action: inspect Relay logs and App status in Home Assistant." >&2
      ;;
  esac
}
