#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

AUTOMATION_ID="${AUTOMATION_ID:-nova_codex_live_e2e}"
OUTPUT_DIR="${OUTPUT_DIR:-$(mktemp -d "/tmp/ha-nova-codex-live-e2e.XXXXXX")}"
PROMPT_FILE="${OUTPUT_DIR}/prompt.txt"
LOG_FILE="${OUTPUT_DIR}/codex-live-e2e.jsonl"
E2E_SUBAGENT_POLICY="${E2E_SUBAGENT_POLICY:-allow}"
E2E_REQUIRE_QUICK_GATE="${E2E_REQUIRE_QUICK_GATE:-0}"

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
   (Harness-level preconditions may run outside assistant actions.)
4. Use the current local HA NOVA setup for this session.
5. This is end-user relay mode. Use Relay POST /core envelope calls for automation CRUD.
   Do not use direct Home Assistant REST in the client session.
   Do not request or store LLAT in client onboarding files/Keychain.
   Keep method/path explicit in /core payloads so they are visible in executed curl commands.
   Do not redirect /core curl responses to files.
6. For write actions, preview payloads first; explicit confirmation is granted by this prompt for automation id "${AUTOMATION_ID}".
7. Use deterministic automation id: "${AUTOMATION_ID}".
8. Perform create -> read -> update -> read -> delete -> verify absent.
9. Do not run project helper scripts.
10. Parallelize independent stages when beneficial; subagent delegation may be used.
11. Do not modify repository files.
12. Final output must contain exactly one status line:
    NOVA_SKILL_E2E_RESULT ok automation_id=${AUTOMATION_ID} reason=<short_reason>
EOF
}

main() {
  local codex_status
  local parsed_log
  local final_line_count
  local final_line
  local subagent_count

  require_cmd codex
  require_cmd jq
  require_cmd ha-nova

  if [[ "$E2E_REQUIRE_QUICK_GATE" == "1" ]]; then
    log "Running doctor readiness gate (harness precondition)"
    ha-nova doctor
  fi

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

  parsed_log="${OUTPUT_DIR}/codex-live-e2e.parsed.jsonl"
  jq -Rrc 'fromjson? | select(type == "object")' "$LOG_FILE" >"$parsed_log" || true
  [[ -s "$parsed_log" ]] || die "No parseable JSONL events in codex log. Log: ${LOG_FILE}"

  if jq -e '
    select(.type == "item.completed" and .item.type == "command_execution")
    | (.item.command // "")
    | test("scripts/smoke/|scripts/dev/|scripts/e2e/")
  ' "$parsed_log" >/dev/null; then
    die "Detected forbidden helper-script command execution in assistant session. Log: ${LOG_FILE}"
  fi

  subagent_count="$(
    jq -r '
      select(.type == "item.started" and .item.type == "collab_tool_call" and .item.tool == "spawn_agent")
      | 1
    ' "$parsed_log" | wc -l | tr -d '[:space:]'
  )"
  if [[ "$E2E_SUBAGENT_POLICY" == "deny" && "$subagent_count" -gt 0 ]]; then
    die "Detected subagent delegation with E2E_SUBAGENT_POLICY=deny. Log: ${LOG_FILE}"
  fi
  if [[ "$E2E_SUBAGENT_POLICY" == "require" && "$subagent_count" -eq 0 ]]; then
    die "Expected subagent delegation with E2E_SUBAGENT_POLICY=require, found none. Log: ${LOG_FILE}"
  fi
  if [[ "$subagent_count" -gt 0 ]]; then
    log "Subagent delegation detected (${subagent_count}); allowed by policy (${E2E_SUBAGENT_POLICY})"
  fi

  final_line_count="$(
    jq -r '
      select(.type == "item.completed" and .item.type == "agent_message")
      | (.item.text // "")
      | select(test("^NOVA_SKILL_E2E_RESULT\\s+ok\\s+automation_id="))
      | 1
    ' "$parsed_log" | wc -l | tr -d "[:space:]"
  )"
  [[ "$final_line_count" == "1" ]] || die "Expected exactly one final status line, got ${final_line_count}. Log: ${LOG_FILE}"

  final_line="$(
    jq -r '
      select(.type == "item.completed" and .item.type == "agent_message")
      | (.item.text // "")
      | select(test("^NOVA_SKILL_E2E_RESULT\\s+ok\\s+automation_id="))
    ' "$parsed_log" \
      | tail -n 1
  )"

  [[ -n "$final_line" ]] || die "No final NOVA_SKILL_E2E_RESULT status line found. Log: ${LOG_FILE}"

  local direct_rest_hits
  local core_redirect_hits
  local post_hits
  local get_hits
  local delete_hits
  local absent_get_hits
  local op_sequence
  local sequence_ok

  direct_rest_hits="$(
    jq -sr '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | .command = (.item.command // "")
        | select(.command | test("(^|[\\n;])[[:space:]]*curl[[:space:]]"))
        | select(.command | test("/api/"))
        | select(.command | test("/core($|[[:space:]\"]|\\?)") | not)
      ] | length
    ' "$parsed_log"
  )"
  [[ "${direct_rest_hits}" -eq 0 ]] || die "Detected direct Home Assistant REST calls (bypassing relay /core). Log: ${LOG_FILE}"

  core_redirect_hits="$(
    jq -sr '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | (.item.command // "")
        | select(test("(^|[\\n;])[[:space:]]*curl[[:space:]]"))
        | select(test("/core($|[[:space:]\"]|\\?)"))
        | select(test("[[:space:]]>>?[[:space:]]|\\|[[:space:]]*tee\\b"))
      ] | length
    ' "$parsed_log"
  )"
  [[ "${core_redirect_hits}" -eq 0 ]] || die "Detected forbidden /core response redirection to files/tee. Log: ${LOG_FILE}"

  post_hits="$(
    jq -sr --arg id "$AUTOMATION_ID" '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | select(((.item.exit_code // 0) == 0))
        | .command = (.item.command // "")
        | .output = ((.item.aggregated_output // "") + "\n" + (.item.raw_output // ""))
        | select(.command | test("(^|[\\n;])[[:space:]]*curl[[:space:]]"))
        | select(.command | test("\\becho\\b|\\bprintf\\b") | not)
        | select(.command | test("/core($|[[:space:]\"]|\\?)"))
        | select(.command | contains("/api/config/automation/config/" + $id))
        | select(.command | test("\"method\"[[:space:]]*:[[:space:]]*\"POST\"|\\b-X[[:space:]]+POST\\b"))
        | select(.output | test("\"ok\"[[:space:]]*:[[:space:]]*true"))
        | select(.output | test("\"status\"[[:space:]]*:[[:space:]]*20(0|1)"))
      ] | length
    ' "$parsed_log"
  )"
  get_hits="$(
    jq -sr --arg id "$AUTOMATION_ID" '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | select(((.item.exit_code // 0) == 0))
        | .command = (.item.command // "")
        | .output = ((.item.aggregated_output // "") + "\n" + (.item.raw_output // ""))
        | select(.command | test("(^|[\\n;])[[:space:]]*curl[[:space:]]"))
        | select(.command | test("\\becho\\b|\\bprintf\\b") | not)
        | select(.command | test("/core($|[[:space:]\"]|\\?)"))
        | select(.command | contains("/api/config/automation/config/" + $id))
        | select(.command | test("\"method\"[[:space:]]*:[[:space:]]*\"GET\"|\\b-X[[:space:]]+GET\\b"))
        | select(.output | test("\"ok\"[[:space:]]*:[[:space:]]*true"))
        | select(.output | test("\"status\"[[:space:]]*:[[:space:]]*200"))
      ] | length
    ' "$parsed_log"
  )"
  delete_hits="$(
    jq -sr --arg id "$AUTOMATION_ID" '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | select(((.item.exit_code // 0) == 0))
        | .command = (.item.command // "")
        | .output = ((.item.aggregated_output // "") + "\n" + (.item.raw_output // ""))
        | select(.command | test("(^|[\\n;])[[:space:]]*curl[[:space:]]"))
        | select(.command | test("\\becho\\b|\\bprintf\\b") | not)
        | select(.command | test("/core($|[[:space:]\"]|\\?)"))
        | select(.command | contains("/api/config/automation/config/" + $id))
        | select(.command | test("\"method\"[[:space:]]*:[[:space:]]*\"DELETE\"|\\b-X[[:space:]]+DELETE\\b"))
        | select(.output | test("\"ok\"[[:space:]]*:[[:space:]]*true"))
        | select(.output | test("\"status\"[[:space:]]*:[[:space:]]*20(0|4)"))
      ] | length
    ' "$parsed_log"
  )"
  absent_get_hits="$(
    jq -sr --arg id "$AUTOMATION_ID" '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | select(((.item.exit_code // 0) == 0))
        | .command = (.item.command // "")
        | .output = ((.item.aggregated_output // "") + "\n" + (.item.raw_output // ""))
        | select(.command | test("(^|[\\n;])[[:space:]]*curl[[:space:]]"))
        | select(.command | test("\\becho\\b|\\bprintf\\b") | not)
        | select(.command | test("/core($|[[:space:]\"]|\\?)"))
        | select(.command | contains("/api/config/automation/config/" + $id))
        | select(.command | test("\"method\"[[:space:]]*:[[:space:]]*\"GET\"|\\b-X[[:space:]]+GET\\b"))
        | select(.output | test("\"ok\"[[:space:]]*:[[:space:]]*true"))
        | select(.output | test("404|Resource not found|\"status\"[[:space:]]*:[[:space:]]*404"; "i"))
      ] | length
    ' "$parsed_log"
  )"
  op_sequence="$(
    jq -sr --arg id "$AUTOMATION_ID" '
      [
        .[]
        | select(.type == "item.completed" and .item.type == "command_execution")
        | select(((.item.exit_code // 0) == 0))
        | .command = (.item.command // "")
        | .output = ((.item.aggregated_output // "") + "\n" + (.item.raw_output // ""))
        | select(.command | test("(^|[\\n;])[[:space:]]*curl[[:space:]]"))
        | select(.command | test("\\becho\\b|\\bprintf\\b") | not)
        | select(.command | test("/core($|[[:space:]\"]|\\?)"))
        | select(.command | contains("/api/config/automation/config/" + $id))
        | if ((.command | test("\"method\"[[:space:]]*:[[:space:]]*\"DELETE\"|\\b-X[[:space:]]+DELETE\\b")) and (.output | test("\"ok\"[[:space:]]*:[[:space:]]*true")) and (.output | test("\"status\"[[:space:]]*:[[:space:]]*20(0|4)"))) then "D"
          elif ((.command | test("\"method\"[[:space:]]*:[[:space:]]*\"POST\"|\\b-X[[:space:]]+POST\\b")) and (.output | test("\"ok\"[[:space:]]*:[[:space:]]*true")) and (.output | test("\"status\"[[:space:]]*:[[:space:]]*20(0|1)"))) then "P"
          elif ((.command | test("\"method\"[[:space:]]*:[[:space:]]*\"GET\"|\\b-X[[:space:]]+GET\\b")) and (.output | test("\"ok\"[[:space:]]*:[[:space:]]*true")) and (.output | test("404|Resource not found|\"status\"[[:space:]]*:[[:space:]]*404"; "i"))) then "V"
          elif ((.command | test("\"method\"[[:space:]]*:[[:space:]]*\"GET\"|\\b-X[[:space:]]+GET\\b")) and (.output | test("\"ok\"[[:space:]]*:[[:space:]]*true")) and (.output | test("\"status\"[[:space:]]*:[[:space:]]*200"))) then "G"
          else empty end
      ] | join("")
    ' "$parsed_log"
  )"
  sequence_ok="$(
    printf '%s' "$op_sequence" | grep -Eq '^[GV]?P+G+P+G+D+V+$' && echo "1" || echo "0"
  )"
  [[ "${post_hits}" -ge 2 ]] || die "Insufficient create/update POST evidence via relay /core (${post_hits} hits). Log: ${LOG_FILE}"
  [[ "${get_hits}" -ge 2 ]] || die "Insufficient read GET(200) evidence via relay /core (${get_hits} hits). Log: ${LOG_FILE}"
  [[ "${delete_hits}" -ge 1 ]] || die "Missing delete evidence via relay /core. Log: ${LOG_FILE}"
  [[ "${absent_get_hits}" -ge 1 ]] || die "Missing verify-absent GET(404) evidence via relay /core. Log: ${LOG_FILE}"
  [[ "$sequence_ok" == "1" ]] || die "CRUD sequence evidence failed. Expected ordered flow [optional precreate GET(200/404)] + PGPGDV, got: ${op_sequence}. Log: ${LOG_FILE}"
  [[ "$final_line" == NOVA_SKILL_E2E_RESULT\ ok\ automation_id=${AUTOMATION_ID}\ reason=* ]] \
    || die "Unexpected final status: ${final_line}. Log: ${LOG_FILE}"

  log "Live skill e2e passed"
  log "Output directory: ${OUTPUT_DIR}"
}

main "$@"
