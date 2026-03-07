#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

load_env_file_if_present() {
  local file="$1"
  [[ -f "$file" ]] || return 0

  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    [[ "$line" =~ ^[[:space:]]*$ ]] && continue
    [[ "$line" != *=* ]] && continue

    local key="${line%%=*}"
    local raw="${line#*=}"
    key="${key#"${key%%[![:space:]]*}"}"
    key="${key%"${key##*[![:space:]]}"}"

    case "$key" in
      HA_HOST|HA_SSH_KEY|SSH_USER|SSH_PORT|APP_SLUG|SUPERVISOR_SLUG)
        ;;
      *)
        continue
        ;;
    esac

    if [[ -n "${!key:-}" ]]; then
      continue
    fi

    raw="${raw#"${raw%%[![:space:]]*}"}"
    raw="${raw%"${raw##*[![:space:]]}"}"

    if [[ "$raw" == \"*\" && "$raw" == *\" ]]; then
      raw="${raw:1:${#raw}-2}"
    elif [[ "$raw" == \'*\' && "$raw" == *\' ]]; then
      raw="${raw:1:${#raw}-2}"
    fi

    export "$key=$raw"
  done < "$file"
}

# Contributor convenience only; explicit env vars always win.
load_env_file_if_present "${PROJECT_ROOT}/.env.local"
load_env_file_if_present "${PROJECT_ROOT}/.env"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      # Legacy flag — ignored (always clean deploy now)
      shift; [[ $# -gt 0 ]] && shift
      ;;
    --force)
      # Legacy flag — ignored (always reinstalls on drift)
      shift
      ;;
    -h|--help)
      cat <<'USAGE'
Usage:
  bash scripts/deploy/ha-app-deploy.sh

Required environment:
  HA_HOST       Home Assistant host/IP for SSH
  HA_SSH_KEY    SSH private key path

Optional environment:
  SSH_USER        default: root
  SSH_PORT        default: 22
  APP_SLUG        default: ha_nova_relay
  SUPERVISOR_SLUG default: local_${APP_SLUG}
  Also loaded (if present): .env.local, .env

Always performs a clean deploy:
  1. Sync files (config.yaml, translations/)
  2. Reinstall if metadata drift detected (options saved + restored)
  3. Stop app + clear Docker image cache
  4. Rebuild + start
USAGE
      exit 0
      ;;
    *)
      echo "[ha-app-deploy] Unknown argument: $1" >&2
      exit 1
      ;;
  esac
done

HA_HOST="${HA_HOST:-}"
HA_SSH_KEY="${HA_SSH_KEY:-}"
SSH_USER="${SSH_USER:-root}"
SSH_PORT="${SSH_PORT:-22}"
APP_SLUG="${APP_SLUG:-ha_nova_relay}"
SUPERVISOR_SLUG="${SUPERVISOR_SLUG:-local_${APP_SLUG}}"
APP_CONFIG_PATH="${PROJECT_ROOT}/nova/config.yaml"
EXPECTED_INGRESS="$(sed -n 's/^ingress:[[:space:]]*//p' "${APP_CONFIG_PATH}" | head -n1 || true)"
EXPECTED_PORT_MAPPINGS="$(sed -nE 's/^  ([0-9]+\/tcp:[[:space:]]*[0-9]+)$/\1/p' "${APP_CONFIG_PATH}" || true)"
EXPECTED_OPTION_KEYS="$(
  awk '
    /^options:/ {section="options"; next}
    /^schema:/ {section="schema"; next}
    /^[^[:space:]]/ {section=""}
    section=="options" && /^  [A-Za-z0-9_]+:/ {
      key=$1
      sub(":", "", key)
      print key
    }
  ' "${APP_CONFIG_PATH}" || true
)"
EXPECTED_SCHEMA_KEYS="$(
  awk '
    /^schema:/ {section="schema"; next}
    /^[^[:space:]]/ {section=""}
    section=="schema" && /^  [A-Za-z0-9_]+:/ {
      key=$1
      sub(":", "", key)
      print key
    }
  ' "${APP_CONFIG_PATH}" || true
)"
EXPECTED_SCHEMA_ENTRIES="$(
  awk '
    /^schema:/ {section="schema"; next}
    /^[^[:space:]]/ {section=""}
    section=="schema" && /^  [A-Za-z0-9_]+:/ {
      key=$1; sub(":", "", key)
      val=$2
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", val)
      print key "=" val
    }
  ' "${APP_CONFIG_PATH}" || true
)"

if [[ -z "$HA_HOST" ]]; then
  echo "[ha-app-deploy] HA_HOST is required" >&2
  exit 1
fi

if [[ -z "$HA_SSH_KEY" ]]; then
  echo "[ha-app-deploy] HA_SSH_KEY is required" >&2
  exit 1
fi

if [[ ! -f "$HA_SSH_KEY" ]]; then
  echo "[ha-app-deploy] HA_SSH_KEY file does not exist: $HA_SSH_KEY" >&2
  exit 1
fi

remote() {
  local cmd="$1"
  ssh -i "$HA_SSH_KEY" \
    -o StrictHostKeyChecking=accept-new \
    -o BatchMode=yes \
    -p "$SSH_PORT" \
    "$SSH_USER@$HA_HOST" \
    "$cmd"
}

log() {
  echo "[ha-app-deploy] $*"
}

get_app_info_json() {
  local output

  for _ in 1 2 3; do
    output="$(remote "ha apps info ${SUPERVISOR_SLUG} --raw-json" 2>/dev/null || true)"
    if [[ "$output" == \{\"result\"* ]]; then
      echo "$output"
      return 0
    fi
    sleep 2
  done

  return 1
}

app_installed() {
  if remote "ha apps info ${SUPERVISOR_SLUG} >/dev/null 2>&1"; then
    return 0
  fi

  return 1
}

ensure_installed() {
  if app_installed; then
    return 0
  fi

  log "App '${SUPERVISOR_SLUG}' not installed. Trying install..."
  remote "ha apps install ${SUPERVISOR_SLUG}" || {
    echo "[ha-app-deploy] Install failed for ${SUPERVISOR_SLUG}. Add repository/install app first." >&2
    exit 1
  }
}

metadata_needs_reinstall() {
  local info_json

  if ! info_json="$(get_app_info_json)"; then
    log "Could not read app metadata reliably; skipping auto-reinstall safety path."
    return 1
  fi

  local has_translations="0"
  [[ -d "${PROJECT_ROOT}/translations" ]] && has_translations="1"

  if EXPECTED_INGRESS="$EXPECTED_INGRESS" \
    EXPECTED_PORT_MAPPINGS="$EXPECTED_PORT_MAPPINGS" \
    EXPECTED_OPTION_KEYS="$EXPECTED_OPTION_KEYS" \
    EXPECTED_SCHEMA_KEYS="$EXPECTED_SCHEMA_KEYS" \
    EXPECTED_SCHEMA_ENTRIES="$EXPECTED_SCHEMA_ENTRIES" \
    HAS_TRANSLATIONS="$has_translations" \
    INFO_JSON="$info_json" \
    python3 - <<'PY'
import json
import os
import sys

expected_ingress = os.environ.get("EXPECTED_INGRESS", "").strip().lower()
expected_ports = [line.strip() for line in os.environ.get("EXPECTED_PORT_MAPPINGS", "").splitlines() if line.strip()]
expected_option_keys = sorted(
    line.strip() for line in os.environ.get("EXPECTED_OPTION_KEYS", "").splitlines() if line.strip()
)
expected_schema_keys = sorted(
    line.strip() for line in os.environ.get("EXPECTED_SCHEMA_KEYS", "").splitlines() if line.strip()
)

payload = json.loads(os.environ.get("INFO_JSON", "{}") or "{}")
data = payload.get("data", {})

if expected_ingress:
    actual_ingress = str(data.get("ingress")).lower()
    if actual_ingress != expected_ingress:
        sys.exit(0)  # metadata drift => needs reinstall

network = data.get("network") or {}
for mapping in expected_ports:
    if ":" not in mapping:
        continue
    key, value = mapping.split(":", 1)
    key = key.strip()
    value = value.strip()
    actual_value = network.get(key)
    if actual_value is None:
        sys.exit(0)
    if str(actual_value) != value:
        sys.exit(0)

actual_options = data.get("options") or {}
actual_option_keys = sorted(str(key) for key in actual_options.keys())
if expected_option_keys and actual_option_keys != expected_option_keys:
    sys.exit(0)

actual_schema = data.get("schema") or []
actual_schema_keys = sorted(
    str(entry.get("name"))
    for entry in actual_schema
    if isinstance(entry, dict) and entry.get("name")
)
if expected_schema_keys and actual_schema_keys != expected_schema_keys:
    sys.exit(0)

# Compare schema types (e.g. "password" vs "password?")
# NOTE: Only handles password type — extend if non-password schema fields are added.
expected_schema_entries = {}
for line in os.environ.get("EXPECTED_SCHEMA_ENTRIES", "").splitlines():
    line = line.strip()
    if "=" in line:
        k, v = line.split("=", 1)
        expected_schema_entries[k.strip()] = v.strip()

if expected_schema_entries:
    actual_schema_map = {
        str(entry.get("name")): "password?" if entry.get("optional") else "password"
        for entry in actual_schema
        if isinstance(entry, dict) and entry.get("name") and entry.get("type") == "password"
    }
    for key, expected_type in expected_schema_entries.items():
        actual_type = actual_schema_map.get(key, "")
        if actual_type and actual_type != expected_type:
            sys.exit(0)  # schema type drift

# Translations drift: detects initial load only (local files exist, Supervisor has none).
# Content changes within translations are NOT detected — use a manual reinstall for those.
has_translations = os.environ.get("HAS_TRANSLATIONS", "0") == "1"
actual_translations = data.get("translations") or {}
if has_translations and not actual_translations:
    sys.exit(0)  # translations not loaded yet => needs reinstall

sys.exit(1)  # metadata matches
PY
  then
    return 0
  fi

  return 1
}

# Save current app options via Supervisor API (for restore after reinstall).
# Returns base64-encoded JSON to avoid quoting issues with special chars in values.
save_app_options() {
  remote "SUPERVISOR_SLUG='${SUPERVISOR_SLUG}' bash -s" <<'REMOTE_SAVE' 2>/dev/null || echo ""
set -euo pipefail
[[ -z "${SUPERVISOR_TOKEN:-}" ]] && exit 1
info="$(curl -fsS \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  "http://supervisor/addons/${SUPERVISOR_SLUG}/info")"
echo "$info" | python3 -c "
import json, sys, base64
payload = json.loads(sys.stdin.read())
opts = payload.get('data', {}).get('options', {})
# Keep all non-None values (preserve intentional false/0/empty-string)
opts = {k: v for k, v in opts.items() if v is not None}
print(base64.b64encode(json.dumps(opts).encode()).decode())
"
REMOTE_SAVE
}

# Restore app options via Supervisor API.
# Expects base64-encoded JSON from save_app_options.
restore_app_options() {
  local saved_b64="$1"
  if [[ -z "$saved_b64" ]]; then
    return 0
  fi
  # Decode locally to check for empty object
  local saved_opts
  saved_opts="$(printf '%s' "$saved_b64" | base64 -d 2>/dev/null || true)"
  if [[ -z "$saved_opts" || "$saved_opts" == "{}" ]]; then
    return 0
  fi
  # Validate base64 characters to prevent injection into SSH command
  if [[ ! "$saved_b64" =~ ^[A-Za-z0-9+/=]+$ ]]; then
    log "Warning: Saved options data is not valid base64; skipping restore."
    return 0
  fi
  log "Restoring app options after reinstall"
  # Pass JSON via base64 env var to avoid shell quoting issues with special chars
  remote "SUPERVISOR_SLUG='${SUPERVISOR_SLUG}' OPTIONS_B64='${saved_b64}' bash -s" <<'REMOTE_RESTORE' || log "Warning: Could not restore options. Set them manually in the UI."
set -euo pipefail
[[ -z "${SUPERVISOR_TOKEN:-}" ]] && exit 1
OPTIONS_JSON="$(echo "$OPTIONS_B64" | base64 -d)"
if ! curl -fsS \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  -H "Content-Type: application/json" \
  "http://supervisor/addons/${SUPERVISOR_SLUG}/options/validate" \
  -d "$OPTIONS_JSON" >/dev/null; then
  echo "[ha-app-deploy] Options validation failed (schema may have changed); skipping restore." >&2
  exit 1
fi
curl -fsS \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  -H "Content-Type: application/json" \
  "http://supervisor/addons/${SUPERVISOR_SLUG}/options" \
  -d "{\"options\":${OPTIONS_JSON}}" >/dev/null
REMOTE_RESTORE
}

reinstall_app_for_metadata_sync() {
  log "App metadata drift detected. Reinstalling to refresh Supervisor cache."

  # Save options before uninstall
  local saved_opts
  saved_opts="$(save_app_options)"
  if [[ -z "$saved_opts" ]]; then
    log "Warning: Could not save existing options. They may be lost after reinstall."
  fi

  remote "ha apps uninstall ${SUPERVISOR_SLUG}" || true
  if ! remote "ha apps install ${SUPERVISOR_SLUG}"; then
    log "First reinstall attempt failed; retrying after store reload."
    remote "ha store reload" || true
    remote "ha apps install ${SUPERVISOR_SLUG}"
  fi

  # Restore options
  restore_app_options "$saved_opts"
}

rebuild_or_update() {
  local output
  output="$(remote "ha apps rebuild ${SUPERVISOR_SLUG}" 2>&1 || true)"

  if echo "$output" | grep -qi "use update instead rebuild"; then
    log "Supervisor requested update instead of rebuild"
    remote "ha apps update ${SUPERVISOR_SLUG}"
    return 0
  fi

  if echo "$output" | grep -qi "Another job is running"; then
    log "Another Supervisor job is running; waiting 10s then continuing"
    sleep 10
    return 0
  fi

  if echo "$output" | grep -qiE "error|failed"; then
    echo "[ha-app-deploy] Rebuild failed: $output" >&2
    exit 1
  fi

  if [[ -n "$output" ]]; then
    log "$output"
  fi
}

clear_image_cache() {
  local escaped_app_slug
  escaped_app_slug="$(printf '%s' "$APP_SLUG" | sed 's/[^^A-Za-z0-9_.-]/\\&/g')"

  remote "
    set -e
    ids=\$(docker images --format '{{.Repository}}:{{.Tag}} {{.ID}}' \
      | awk '/addon-${escaped_app_slug}:/ {print \$2}' \
      | sort -u)
    if [ -n \"\$ids\" ]; then
      echo \"\$ids\" | xargs -r docker rmi -f >/dev/null 2>&1 || true
      echo removed
    else
      echo none
    fi
  "
}

# ── Sync files ──

REMOTE_ADDON_DIR="/addons/local/${APP_SLUG}"

log "Syncing app files to ${REMOTE_ADDON_DIR}/"

# Addon metadata files (Supervisor reads these from addon root)
# NOTE: Do NOT copy config.yaml into app/ — the Supervisor uses **/config.*
# glob and a duplicate causes translations/metadata to be lost.
ADDON_FILES=(config.yaml DOCS.md CHANGELOG.md icon.png "icon@2x.png" logo.png "logo@2x.png")
for f in "${ADDON_FILES[@]}"; do
  if [[ -f "${PROJECT_ROOT}/nova/${f}" ]]; then
    scp -i "$HA_SSH_KEY" \
      -o StrictHostKeyChecking=accept-new \
      -o BatchMode=yes \
      -P "$SSH_PORT" \
      "${PROJECT_ROOT}/nova/${f}" \
      "${SSH_USER}@${HA_HOST}:${REMOTE_ADDON_DIR}/${f}"
  fi
done

TRANSLATIONS_DIR="${PROJECT_ROOT}/nova/translations"
if [[ -d "$TRANSLATIONS_DIR" ]]; then
  remote "mkdir -p ${REMOTE_ADDON_DIR}/translations"
  scp -i "$HA_SSH_KEY" \
    -o StrictHostKeyChecking=accept-new \
    -o BatchMode=yes \
    -P "$SSH_PORT" \
    "${TRANSLATIONS_DIR}"/*.yaml \
    "${SSH_USER}@${HA_HOST}:${REMOTE_ADDON_DIR}/translations/"
fi

# ── Reload + reinstall if needed ──

log "Reload app store metadata"
remote "ha store reload"

ensure_installed

if metadata_needs_reinstall; then
  reinstall_app_for_metadata_sync
fi

# ── Clean build ──

log "Stopping app"
remote "ha apps stop ${SUPERVISOR_SLUG}" || true

log "Clearing Docker image cache for '${APP_SLUG}'"
cache_result="$(clear_image_cache)"
log "Image cache result: ${cache_result}"

log "Rebuilding app '${SUPERVISOR_SLUG}'"
rebuild_or_update

log "Starting app '${SUPERVISOR_SLUG}'"
remote "ha apps start ${SUPERVISOR_SLUG}"

# ── Status ──

log "Collecting quick status"
remote "ha apps info ${SUPERVISOR_SLUG}" || true

log "Recent app logs"
remote "ha apps logs ${SUPERVISOR_SLUG} --lines 40" || true

log "Deploy finished"
