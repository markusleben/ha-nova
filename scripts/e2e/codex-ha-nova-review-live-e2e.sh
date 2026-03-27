#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
SCENARIO_FILE="${SCENARIO_FILE:-${SCRIPT_DIR}/codex-ha-nova-review-live-scenarios.json}"
OUTPUT_DIR="${OUTPUT_DIR:-$(mktemp -d "/tmp/ha-nova-codex-review-live.XXXXXX")}"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
LOG_DIR="${OUTPUT_DIR}/logs-${RUN_ID}"
RESULTS_FILE="${OUTPUT_DIR}/results-${RUN_ID}.ndjson"
SUMMARY_FILE="${OUTPUT_DIR}/summary-${RUN_ID}.json"

log() {
  echo "[codex-review-live-e2e] $*"
}

die() {
  echo "[codex-review-live-e2e] $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "Required command not found: ${cmd}"
}

run_codex_with_timeout() {
  local timeout_sec="$1"
  local prompt_file="$2"
  local scenario_log="$3"

  python3 - "$timeout_sec" "$PROJECT_ROOT" "$prompt_file" "$scenario_log" <<'PY'
import pathlib
import subprocess
import sys

timeout_sec = int(sys.argv[1])
project_root = sys.argv[2]
prompt_file = pathlib.Path(sys.argv[3])
scenario_log = pathlib.Path(sys.argv[4])
prompt = prompt_file.read_text(encoding="utf-8")

with scenario_log.open("w", encoding="utf-8") as log_file:
    try:
        completed = subprocess.run(
            [
                "codex",
                "exec",
                "--ephemeral",
                "--json",
                "--sandbox",
                "danger-full-access",
                "-C",
                project_root,
                prompt,
            ],
            stdout=log_file,
            stderr=subprocess.STDOUT,
            text=True,
            timeout=timeout_sec,
            check=False,
        )
        sys.exit(completed.returncode)
    except subprocess.TimeoutExpired:
        sys.exit(124)
PY
}

validate_scenario_file() {
  [[ -f "$SCENARIO_FILE" ]] || die "Scenario file not found: ${SCENARIO_FILE}"
  jq -e '
    type == "array"
    and (length > 0)
    and all(
      .[];
      (.id | type == "string" and length > 0)
      and (.prompt | type == "string" and length > 0)
      and (.must_contain_text | type == "array" and all(.[]; type == "string" and length > 0))
      and (
        (has("must_not_contain_text") | not)
        or
        (.must_not_contain_text | type == "array" and all(.[]; type == "string" and length > 0))
      )
      and (
        (has("ordered_text") | not)
        or
        (.ordered_text | type == "array" and length > 1 and all(.[]; type == "string" and length > 0))
      )
      and (
        (has("section_order") | not)
        or
        (.section_order | type == "array" and length > 1 and all(.[]; type == "string" and length > 0))
      )
      and ((.max_duration_sec // 90) | type == "number" and . > 0)
    )
  ' "$SCENARIO_FILE" >/dev/null || die "Invalid review scenario file format: ${SCENARIO_FILE}"
}

build_prompt_file() {
  local scenario_prompt="$1"
  local prompt_file="$2"

  cat > "$prompt_file" <<EOF_PROMPT
Use the local skill "ha-nova" for this task.

User request:
${scenario_prompt}

Hard requirements:
1. Work in English only.
2. Use App + Relay terminology.
3. Treat this as a pasted-YAML review unless the prompt explicitly requires Home Assistant reads.
4. Use only the local repo skills plus the pasted YAML in this prompt.
5. Do not browse the web and do not use external research tools or docs fetches.
6. Do not use Exa, Ref, web search, or official-doc lookup tools.
7. Do not run onboarding ready/doctor/quick checks.
8. Do not modify repository files.
EOF_PROMPT
}

extract_last_agent_message() {
  local scenario_log="$1"

  jq -sr -r '
    map(select(.type == "item.completed" and .item.type == "agent_message") | .item.text)
    | if length == 0 then "" else .[-1] end
  ' "$scenario_log" 2>/dev/null || true
}

count_command_hits() {
  local scenario_log="$1"
  local pattern="$2"

  jq -r '
    select(.type == "item.completed" and .item.type == "command_execution")
    | (.item.command // "")
  ' "$scenario_log" 2>/dev/null | grep -E -c "$pattern" || true
}

count_external_research_hits() {
  local scenario_log="$1"

  jq -sr '
    [
      .[]
      | select(.type == "item.started" or .type == "item.completed")
      | .item
      | select(
          (.type == "mcp_tool_call")
          or
          (.type == "web_search")
        )
      | select(
          (.type == "web_search")
          or
          (
            (.server // "" | test("^(exa|Ref)$"))
            or
            (.tool // "" | test("^(get_code_context_exa|web_search_exa|ref_search_documentation|ref_read_url)$"))
          )
        )
    ] | length
  ' "$scenario_log" 2>/dev/null || true
}

assert_text_sequence() {
  local haystack="$1"
  shift

  local remaining="$haystack"
  local needle
  for needle in "$@"; do
    if [[ "$remaining" != *"$needle"* ]]; then
      return 1
    fi
    remaining="${remaining#*"$needle"}"
  done

  return 0
}

run_scenario() {
  local index="$1"
  local scenario_id="$2"
  local scenario_prompt="$3"
  local must_contain_text_json="$4"
  local must_not_contain_text_json="$5"
  local ordered_text_json="$6"
  local section_order_json="$7"
  local max_duration_sec="$8"

  local prompt_file="${LOG_DIR}/${index}-${scenario_id}.prompt.txt"
  local scenario_log="${LOG_DIR}/${index}-${scenario_id}.jsonl"
  local parsed_log="${LOG_DIR}/${index}-${scenario_id}.parsed.jsonl"
  local start_ts
  local end_ts
  local duration_sec
  local codex_status
  local status="pass"
  local validation_error=""
  local last_agent_message=""
  local helper_script_count
  local external_research_count

  build_prompt_file "$scenario_prompt" "$prompt_file"

  start_ts="$(date +%s)"
  set +e
  run_codex_with_timeout "$max_duration_sec" "$prompt_file" "$scenario_log"
  codex_status="$?"
  set -e
  end_ts="$(date +%s)"
  duration_sec="$((end_ts - start_ts))"

  if [[ "$codex_status" -eq 124 ]]; then
    status="fail"
    validation_error="duration_exceeded"
  fi

  jq -Rrc 'fromjson? | select(type == "object")' "$scenario_log" >"$parsed_log" || true
  if [[ "$status" == "pass" && ! -s "$parsed_log" ]]; then
    status="fail"
    validation_error="invalid_jsonl_transcript"
  fi

  helper_script_count="$(count_command_hits "$parsed_log" 'scripts/(smoke|dev|e2e)/')"
  if [[ "$status" == "pass" && "$helper_script_count" -gt 0 ]]; then
    status="fail"
    validation_error="helper_script_usage_detected"
  fi

  external_research_count="$(count_external_research_hits "$parsed_log")"
  if [[ "$external_research_count" -gt 0 ]]; then
    status="fail"
    validation_error="unexpected_external_research_detected"
  fi

  if [[ "$status" == "pass" ]]; then
    last_agent_message="$(extract_last_agent_message "$parsed_log")"
    if [[ -z "$last_agent_message" ]]; then
      status="fail"
      validation_error="missing_agent_message"
    fi
  fi

  if [[ "$status" == "pass" && "$duration_sec" -gt "$max_duration_sec" ]]; then
    status="fail"
    validation_error="duration_exceeded"
  fi

  if [[ "$status" == "pass" ]]; then
    while IFS= read -r required_text; do
      if [[ -z "$required_text" ]]; then
        continue
      fi
      if [[ "$last_agent_message" != *"$required_text"* ]]; then
        status="fail"
        validation_error="required_text_missing"
        break
      fi
    done < <(echo "$must_contain_text_json" | jq -r '.[]')
  fi

  if [[ "$status" == "pass" ]]; then
    while IFS= read -r forbidden_text; do
      if [[ -z "$forbidden_text" ]]; then
        continue
      fi
      if [[ "$last_agent_message" == *"$forbidden_text"* ]]; then
        status="fail"
        validation_error="forbidden_text_present"
        break
      fi
    done < <(echo "$must_not_contain_text_json" | jq -r '.[]')
  fi

  if [[ "$status" == "pass" && "$ordered_text_json" != "[]" ]]; then
    mapfile -t ordered_text < <(echo "$ordered_text_json" | jq -r '.[]')
    if ! assert_text_sequence "$last_agent_message" "${ordered_text[@]}"; then
      status="fail"
      validation_error="ordered_text_mismatch"
    fi
  fi

  if [[ "$status" == "pass" && "$section_order_json" != "[]" ]]; then
    mapfile -t ordered_sections < <(echo "$section_order_json" | jq -r '.[]')
    if ! assert_text_sequence "$last_agent_message" "${ordered_sections[@]}"; then
      status="fail"
      validation_error="section_order_mismatch"
    fi
  fi

  jq -cn \
    --arg id "$scenario_id" \
    --arg status "$status" \
    --arg error "$validation_error" \
    --argjson duration_sec "$duration_sec" \
    --argjson codex_status "$codex_status" \
    --arg last_agent_message "$last_agent_message" \
    '{
      id: $id,
      status: $status,
      error: $error,
      duration_sec: $duration_sec,
      codex_status: $codex_status,
      last_agent_message: $last_agent_message
    }' >>"$RESULTS_FILE"
}

main() {
  require_cmd codex
  require_cmd jq
  require_cmd python3

  validate_scenario_file
  mkdir -p "$LOG_DIR"
  : >"$RESULTS_FILE"

  local scenario_count
  scenario_count="$(jq 'length' "$SCENARIO_FILE")"

  local idx
  for ((idx = 0; idx < scenario_count; idx += 1)); do
    run_scenario \
      "$((idx + 1))" \
      "$(jq -r ".[$idx].id" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].prompt" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].must_contain_text" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].must_not_contain_text // []" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].ordered_text // []" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].section_order // []" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].max_duration_sec // 90" "$SCENARIO_FILE")"
  done

  jq -s '{
    total: length,
    passed: map(select(.status == "pass")) | length,
    failed: map(select(.status == "fail")) | length,
    scenarios: .
  }' "$RESULTS_FILE" >"$SUMMARY_FILE"

  if jq -e '.failed == 0' "$SUMMARY_FILE" >/dev/null; then
    log "Review scenario suite passed"
    jq -r '.scenarios[] | "PASS \(.id) (\(.duration_sec)s)"' "$SUMMARY_FILE"
    exit 0
  fi

  log "Review scenario suite failed"
  jq -r '.scenarios[] | select(.status == "fail") | "FAIL \(.id): \(.error)"' "$SUMMARY_FILE" >&2
  exit 1
}

main "$@"
