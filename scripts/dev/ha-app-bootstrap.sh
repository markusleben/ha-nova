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

usage() {
  cat <<'USAGE'
Usage:
  bash scripts/dev/ha-app-bootstrap.sh

Required environment:
  HA_HOST
  HA_SSH_KEY

The App takes no shared token: pair a computer with ha-nova setup afterwards.

Optional environment:
  SSH_USER          default: root
  SSH_PORT          default: 22
  APP_SLUG          default: ha_nova_relay
  SUPERVISOR_SLUG   default: local_${APP_SLUG}
  Also loaded (if present): .env.local, .env
USAGE
}

log() {
  echo "[ha-app-bootstrap] $*"
}

remote() {
  local cmd="$1"
  ssh -i "$HA_SSH_KEY" \
    -o StrictHostKeyChecking=accept-new \
    -o BatchMode=yes \
    -p "$SSH_PORT" \
    "$SSH_USER@$HA_HOST" \
    "$cmd"
}

load_env_file_if_present "${PROJECT_ROOT}/.env.local"
load_env_file_if_present "${PROJECT_ROOT}/.env"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

HA_HOST="${HA_HOST:-}"
HA_SSH_KEY="${HA_SSH_KEY:-}"
SSH_USER="${SSH_USER:-root}"
SSH_PORT="${SSH_PORT:-22}"
APP_SLUG="${APP_SLUG:-ha_nova_relay}"
SUPERVISOR_SLUG="${SUPERVISOR_SLUG:-local_${APP_SLUG}}"
REMOTE_APP_DIR="/addons/local/${APP_SLUG}"

if [[ -z "$HA_HOST" ]]; then
  echo "[ha-app-bootstrap] HA_HOST is required" >&2
  exit 1
fi

if [[ -z "$HA_SSH_KEY" ]]; then
  echo "[ha-app-bootstrap] HA_SSH_KEY is required" >&2
  exit 1
fi

if [[ ! -f "$HA_SSH_KEY" ]]; then
  echo "[ha-app-bootstrap] HA_SSH_KEY file does not exist: $HA_SSH_KEY" >&2
  exit 1
fi

log "Ensuring remote local app directory exists"
remote "mkdir -p '${REMOTE_APP_DIR}'"

log "Syncing nova/ (Add-on build context) to HA host (${REMOTE_APP_DIR})"
rsync -az --delete \
  --exclude 'node_modules' \
  --exclude 'dist' \
  -e "ssh -i ${HA_SSH_KEY} -p ${SSH_PORT} -o StrictHostKeyChecking=accept-new" \
  "${PROJECT_ROOT}/nova/" \
  "${SSH_USER}@${HA_HOST}:${REMOTE_APP_DIR}/"

log "Preparing Supervisor build context + config defaults"
remote "APP_DIR='${REMOTE_APP_DIR}' bash -s" <<'REMOTE_PREP'
set -euo pipefail

cd "$APP_DIR"

# Ensure run script is executable.
chmod +x ./run

# A dev install must BUILD the synced code: with `image:` Supervisor would pull
# the published image instead.
sed -i '/^image:/d' config.yaml
REMOTE_PREP

log "Reloading app store"
remote "ha store reload"

log "Ensuring app is installed (${SUPERVISOR_SLUG})"
if ! remote "ha apps info '${SUPERVISOR_SLUG}' >/dev/null 2>&1"; then
  remote "ha apps install '${SUPERVISOR_SLUG}'"
fi

log "Rebuilding app image to pick up synced sources"
remote "ha apps rebuild '${SUPERVISOR_SLUG}' || ha apps update '${SUPERVISOR_SLUG}'"

log "Validating + writing app options via Supervisor API"
remote "SUPERVISOR_SLUG='${SUPERVISOR_SLUG}' bash -s" <<'REMOTE_OPTIONS'
set -euo pipefail

if [[ -z "${SUPERVISOR_TOKEN:-}" ]]; then
  echo "[ha-app-bootstrap] SUPERVISOR_TOKEN missing on remote shell; cannot apply options" >&2
  exit 1
fi

current_info_json="$(
curl -fsS \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  "http://supervisor/addons/${SUPERVISOR_SLUG}/info"
)"
export CURRENT_INFO_JSON="${current_info_json}"

options_json="$(
python3 - <<'PY'
import json
import os

current_info = json.loads(os.environ["CURRENT_INFO_JSON"])
current_options = current_info.get("data", {}).get("options", {})

if not isinstance(current_options, dict):
    current_options = {}

# The App's only option; the shared token and LLAT options are gone.
options = {
    "file_access": current_options.get("file_access", "off")
}

print(json.dumps(options))
PY
)"

curl -fsS \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  -H "Content-Type: application/json" \
  "http://supervisor/addons/${SUPERVISOR_SLUG}/options/validate" \
  -d "${options_json}" >/dev/null

curl -fsS \
  -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
  -H "Content-Type: application/json" \
  "http://supervisor/addons/${SUPERVISOR_SLUG}/options" \
  -d "{\"options\":${options_json}}" >/dev/null
REMOTE_OPTIONS

log "Restarting app"
remote "ha apps restart '${SUPERVISOR_SLUG}' || ha apps start '${SUPERVISOR_SLUG}'"

log "Current app status"
remote "ha apps info '${SUPERVISOR_SLUG}'"

log "Recent app logs"
remote "ha apps logs '${SUPERVISOR_SLUG}' --lines 60" || true

log "Bootstrap deploy finished"
