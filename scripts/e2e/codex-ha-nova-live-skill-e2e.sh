#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

AUTOMATION_ID="${AUTOMATION_ID:-nova_codex_live_e2e}"
OUTPUT_DIR="${OUTPUT_DIR:-$(mktemp -d "/tmp/ha-nova-codex-live-e2e.XXXXXX")}"
PROMPT_FILE="${OUTPUT_DIR}/prompt.txt"
LOG_FILE="${OUTPUT_DIR}/codex-live-e2e.jsonl"
E2E_SUBAGENT_POLICY="${E2E_SUBAGENT_POLICY:-allow}"

log() {
  echo "[codex-live-e2e] $*"
}

die() {
  echo "[codex-live-e2e] $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    die "Required command not found: ${cmd}"
  fi
}

build_prompt() {
  cat > "$PROMPT_FILE" <<EOF
Use the local skill "ha-nova" for this task.

Objective:
Run a real Home Assistant automation CRUD scenario as a live user-like session.

Hard requirements:
1. Work in English only.
2. Use App + Relay terminology.
3. Do not run onboarding ready/doctor checks before the first HA action.
4. Load onboarding env for this session.
5. This is end-user relay mode. Use Relay POST /core envelope calls for automation CRUD.
   Do not use direct Home Assistant REST in the client session.
   Do not request or store LLAT in client onboarding files/Keychain.
6. For write actions, preview payloads first; explicit confirmation is granted by this prompt for automation id "${AUTOMATION_ID}".
7. Use deterministic automation id: "${AUTOMATION_ID}".
8. Perform create -> read -> update -> read -> delete -> verify absent.
9. Do not run project helper scripts.
10. Subagent delegation is allowed when useful.
11. Do not modify repository files.
12. Final output must contain exactly one status line:
    NOVA_SKILL_E2E_RESULT ok automation_id=${AUTOMATION_ID} reason=<short_reason>
EOF
}

main() {
  local onboarding_env
  local codex_status
  local final_line
  local subagent_count

  require_cmd codex
  require_cmd jq

  log "Running quick onboarding readiness gate"
  bash "${PROJECT_ROOT}/scripts/onboarding/macos-onboarding.sh" quick

  onboarding_env="$(bash "${PROJECT_ROOT}/scripts/onboarding/macos-onboarding.sh" env)"
  # shellcheck disable=SC1091
  # shellcheck disable=SC1090
  source /dev/stdin <<<"$onboarding_env"

  mkdir -p "$OUTPUT_DIR"
  build_prompt

  log "Starting codex live e2e session"
  set +e
  codex exec \
    --ephemeral \
    --json \
    --sandbox danger-full-access \
    -C "$PROJECT_ROOT" \
    "$(cat "$PROMPT_FILE")" >"$LOG_FILE" 2>&1
  codex_status="$?"
  set -e

  if [[ "$codex_status" -ne 0 ]]; then
    log "codex exec exited ${codex_status}; validating transcript outcome before failing."
  fi

  grep -Fq "ha-nova/SKILL.md" "$LOG_FILE" || die "No skill usage evidence found in log. Log: ${LOG_FILE}"
  grep -Fq "macos-onboarding.sh" "$LOG_FILE" || die "Missing onboarding script usage evidence. Log: ${LOG_FILE}"
  if jq -e '
    select(.type == "item.completed" and .item.type == "command_execution")
    | .item.command
    | test("scripts/smoke/")
  ' "$LOG_FILE" >/dev/null; then
    die "Detected helper-smoke-script command execution. This is not a user-like skill path. Log: ${LOG_FILE}"
  fi

  subagent_count="$(
    jq -r '
      select(.type == "item.started" and .item.type == "collab_tool_call" and .item.tool == "spawn_agent")
      | 1
    ' "$LOG_FILE" | wc -l | tr -d '[:space:]'
  )"
  if [[ "$E2E_SUBAGENT_POLICY" == "deny" && "$subagent_count" -gt 0 ]]; then
    die "Detected subagent delegation with E2E_SUBAGENT_POLICY=deny. Log: ${LOG_FILE}"
  fi
  if [[ "$subagent_count" -gt 0 ]]; then
    log "Subagent delegation detected (${subagent_count}); allowed by policy (${E2E_SUBAGENT_POLICY})"
  fi

  final_line="$(
    jq -r '
      select(.type == "item.completed" and .item.type == "agent_message")
      | .item.text
    ' "$LOG_FILE" \
      | awk '/NOVA_SKILL_E2E_RESULT/ { line=$0 } END { print line }'
  )"

  [[ -n "$final_line" ]] || die "No final NOVA_SKILL_E2E_RESULT status line found. Log: ${LOG_FILE}"

  local crud_hits
  local core_hits
  crud_hits="$(
    jq -sr --arg id "$AUTOMATION_ID" '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | ((.item.command // "") + "\n" + (.item.aggregated_output // ""))
        | select(contains("/api/config/automation/config/" + $id))
      ] | length
    ' "$LOG_FILE"
  )"
  core_hits="$(
    jq -sr '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | ((.item.command // "") + "\n" + (.item.aggregated_output // ""))
        | select(test("/core($|[[:space:]\"]|\\?)"))
      ] | length
    ' "$LOG_FILE"
  )"
  [[ "${crud_hits}" -ge 4 ]] || die "Insufficient automation config CRUD evidence (${crud_hits} hits). Log: ${LOG_FILE}"
  [[ "${core_hits}" -ge 4 ]] || die "Insufficient relay /core evidence (${core_hits} hits). Log: ${LOG_FILE}"
  [[ "$final_line" == NOVA_SKILL_E2E_RESULT\ ok\ automation_id=${AUTOMATION_ID}\ reason=* || "$final_line" == NOVA_SKILL_E2E_RESULT\ \<ok\>\ automation_id=${AUTOMATION_ID}\ reason=* ]] \
    || die "Unexpected final status: ${final_line}. Log: ${LOG_FILE}"

  log "Live skill e2e passed"
  log "Output directory: ${OUTPUT_DIR}"
}

main "$@"
