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
      HA_HOST|HA_SSH_KEY|SSH_USER|SSH_PORT|APP_SLUG|SUPERVISOR_SLUG|MVP_AUTOMATION_ID)
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

load_env_file_if_present "${PROJECT_ROOT}/.env.local"
load_env_file_if_present "${PROJECT_ROOT}/.env"

log() {
  echo "[ha-app-mvp-smoke] $*"
}

die() {
  echo "[ha-app-mvp-smoke] $*" >&2
  exit 1
}

extract_host_from_url() {
  local url="$1"
  local host
  host="${url#http://}"
  host="${host#https://}"
  host="${host%%/*}"
  host="${host%%:*}"
  printf '%s' "$host"
}

read_config_json_field() {
  local field="$1"
  local config_file="${HOME}/.config/ha-nova/config.json"

  [[ -f "$config_file" ]] || return 0

  node --input-type=module -e '
    import { readFileSync } from "node:fs";

    const [configPath, key] = process.argv.slice(1);

    try {
      const value = JSON.parse(readFileSync(configPath, "utf8"))?.[key];
      if (typeof value === "string" && value.length > 0) {
        process.stdout.write(value);
      }
    } catch {}
  ' "$config_file" "$field"
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

main() {
  local ha_url

  log "Running HA NOVA diagnostics"
  ha-nova doctor

  HA_HOST="${HA_HOST:-$(read_config_json_field ha_host)}"
  ha_url="${HA_URL:-$(read_config_json_field ha_url)}"
  if [[ -z "$HA_HOST" && -n "$ha_url" ]]; then
    HA_HOST="$(extract_host_from_url "$ha_url")"
  fi

  SSH_USER="${SSH_USER:-root}"
  SSH_PORT="${SSH_PORT:-22}"
  HA_SSH_KEY="${HA_SSH_KEY:-${HOME}/.ssh/ha_mcp}"
  APP_SLUG="${APP_SLUG:-ha_nova_relay}"
  SUPERVISOR_SLUG="${SUPERVISOR_SLUG:-local_${APP_SLUG}}"
  MVP_AUTOMATION_ID="${MVP_AUTOMATION_ID:-nova_mvp_crud_smoke}"

  [[ -n "$HA_HOST" ]] || die "HA_HOST could not be resolved."
  [[ -f "$HA_SSH_KEY" ]] || die "HA_SSH_KEY file not found: $HA_SSH_KEY"

  log "Running automation CRUD smoke in App container context"

  remote "SUPERVISOR_SLUG='${SUPERVISOR_SLUG}' MVP_AUTOMATION_ID='${MVP_AUTOMATION_ID}' bash -s" <<'REMOTE'
set -euo pipefail

log() {
  echo "[ha-app-mvp-smoke:remote] $*"
}

die() {
  echo "[ha-app-mvp-smoke:remote] $*" >&2
  exit 1
}

container_name="addon_${SUPERVISOR_SLUG}"
if ! docker ps --format '{{.Names}}' | grep -Fx "$container_name" >/dev/null 2>&1; then
  die "App container not running: ${container_name}"
fi

docker exec -e MVP_AUTOMATION_ID="$MVP_AUTOMATION_ID" "$container_name" sh -s <<'INNER'
set -euo pipefail

: "${SUPERVISOR_TOKEN:?SUPERVISOR_TOKEN missing inside App container}"

base="http://supervisor/core/api"
automation_id="${MVP_AUTOMATION_ID}"

request() {
  method="$1"
  path="$2"
  body="${3:-}"
  outfile="$4"

  if [ -n "$body" ]; then
    code="$(curl -sS -o "$outfile" -w "%{http_code}" -X "$method" \
      -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$body" \
      "${base}${path}")"
  else
    code="$(curl -sS -o "$outfile" -w "%{http_code}" -X "$method" \
      -H "Authorization: Bearer ${SUPERVISOR_TOKEN}" \
      "${base}${path}")"
  fi

  printf '%s' "$code"
}

assert_status() {
  expected="$1"
  actual="$2"
  label="$3"
  if [ "$actual" != "$expected" ]; then
    echo "${label} failed: expected ${expected}, got ${actual}" >&2
    exit 1
  fi
}

cleanup() {
  request DELETE "/config/automation/config/${automation_id}" "" /tmp/nova_mvp_cleanup_delete.json >/dev/null || true
  request POST "/services/automation/reload" "{}" /tmp/nova_mvp_cleanup_reload.json >/dev/null || true
}
trap cleanup EXIT

create_payload='{"alias":"NOVA MVP CRUD Smoke","trigger":[{"platform":"time_pattern","minutes":"/15"}],"condition":[],"action":[{"service":"logbook.log","data":{"name":"NOVA","message":"mvp create"}}],"mode":"single"}'
update_payload='{"alias":"NOVA MVP CRUD Smoke Updated","trigger":[{"platform":"time_pattern","minutes":"/30"}],"condition":[],"action":[{"service":"logbook.log","data":{"name":"NOVA","message":"mvp update"}}],"mode":"single"}'

s_create="$(request POST "/config/automation/config/${automation_id}" "$create_payload" /tmp/nova_mvp_create.json)"
assert_status 200 "$s_create" CREATE

s_reload1="$(request POST "/services/automation/reload" "{}" /tmp/nova_mvp_reload1.json)"
assert_status 200 "$s_reload1" RELOAD_AFTER_CREATE

s_get1="$(request GET "/config/automation/config/${automation_id}" "" /tmp/nova_mvp_get1.json)"
assert_status 200 "$s_get1" GET_AFTER_CREATE
if ! grep -q 'mvp create' /tmp/nova_mvp_get1.json; then
  echo "GET_AFTER_CREATE validation failed: marker missing" >&2
  exit 1
fi

s_update="$(request POST "/config/automation/config/${automation_id}" "$update_payload" /tmp/nova_mvp_update.json)"
assert_status 200 "$s_update" UPDATE

s_reload2="$(request POST "/services/automation/reload" "{}" /tmp/nova_mvp_reload2.json)"
assert_status 200 "$s_reload2" RELOAD_AFTER_UPDATE

s_get2="$(request GET "/config/automation/config/${automation_id}" "" /tmp/nova_mvp_get2.json)"
assert_status 200 "$s_get2" GET_AFTER_UPDATE
if ! grep -q 'mvp update' /tmp/nova_mvp_get2.json; then
  echo "GET_AFTER_UPDATE validation failed: marker missing" >&2
  exit 1
fi

s_delete="$(request DELETE "/config/automation/config/${automation_id}" "" /tmp/nova_mvp_delete.json)"
assert_status 200 "$s_delete" DELETE

s_reload3="$(request POST "/services/automation/reload" "{}" /tmp/nova_mvp_reload3.json)"
assert_status 200 "$s_reload3" RELOAD_AFTER_DELETE

s_get3="$(request GET "/config/automation/config/${automation_id}" "" /tmp/nova_mvp_get3.json)"
assert_status 404 "$s_get3" GET_AFTER_DELETE

echo "CRUD_SMOKE_OK automation_id=${automation_id}"
INNER
REMOTE

  log "MVP CRUD smoke passed"
}

main "$@"
