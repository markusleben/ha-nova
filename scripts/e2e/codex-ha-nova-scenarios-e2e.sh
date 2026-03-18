#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
SCENARIO_FILE="${SCENARIO_FILE:-${SCRIPT_DIR}/codex-ha-nova-scenarios.json}"
OUTPUT_DIR="${OUTPUT_DIR:-$(mktemp -d "/tmp/ha-nova-codex-scenarios.XXXXXX")}"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
LOG_DIR="${OUTPUT_DIR}/logs-${RUN_ID}"
RESULTS_FILE="${OUTPUT_DIR}/results-${RUN_ID}.ndjson"
SUMMARY_FILE="${OUTPUT_DIR}/summary-${RUN_ID}.json"

log() {
  echo "[codex-scenarios-e2e] $*"
}

die() {
  echo "[codex-scenarios-e2e] $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "Required command not found: ${cmd}"
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
      and (
        (
          .expect.type == "entity_id_prefix_count"
          and (.expect.prefix | type == "string" and length > 0)
          and (.expect.count | type == "number" and . > 0)
          and (((.expect.count_mode // "exact") | (. == "exact" or . == "up_to")))
        )
        or
        (.expect.type == "json_array_values")
      )
      and (((.expected_status // "pass") | (. == "pass" or . == "fail")))
      and ((.expected_error // "") | type == "string")
      and (
        (has("forbid_patterns") | not)
        or
        (.forbid_patterns | type == "array" and length > 0 and all(.[]; type == "string" and length > 0))
      )
      and (
        (has("must_contain_text") | not)
        or
        (.must_contain_text | type == "array" and length > 0 and all(.[]; type == "string" and length > 0))
      )
    )
  ' "$SCENARIO_FILE" >/dev/null || die "Invalid scenario file format: ${SCENARIO_FILE}"
}

build_prompt_file() {
  local scenario_id="$1"
  local scenario_prompt="$2"
  local prompt_file="$3"

  cat > "$prompt_file" <<EOF_PROMPT
Use the local skill "ha-nova" for this task.

User request:
${scenario_prompt}

Hard requirements:
1. Work in English only.
2. Use App + Relay terminology.
3. Do not run onboarding ready/doctor/quick checks before the first Home Assistant action unless the user request explicitly requires it.
4. For a simple read-only request, run the fastest viable one-shot path.
5. Do not modify repository files.
6. Final output must contain exactly one status line:
   NOVA_SCENARIO_RESULT id=${scenario_id} values=<json_array_of_entity_ids>
EOF_PROMPT
}

extract_final_status_line() {
  local scenario_log="$1"

  jq -r '
    select(.type == "item.completed" and .item.type == "agent_message")
    | .item.text
  ' "$scenario_log" 2>/dev/null | awk '/NOVA_SCENARIO_RESULT/ { line=$0 } END { print line }' || true
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
    | .item.command
  ' "$scenario_log" 2>/dev/null | grep -E -c "$pattern" || true
}

first_command_index() {
  local scenario_log="$1"
  local pattern="$2"

  jq -r '
    select(.type == "item.completed" and .item.type == "command_execution")
    | .item.command
  ' "$scenario_log" 2>/dev/null | nl -ba | awk -v pat="$pattern" '$0 ~ pat { print $1; exit }' || true
}

run_scenario() {
  local index="$1"
  local scenario_id="$2"
  local scenario_prompt="$3"
  local expect_type="$4"
  local expected_prefix="$5"
  local expected_count="$6"
  local expected_count_mode="$7"
  local expected_status="$8"
  local expected_error="${9}"
  local forbid_patterns_json="${10}"
  local must_contain_text_json="${11}"
  local max_duration_sec="${12}"

  local prompt_file="${LOG_DIR}/${index}-${scenario_id}.prompt.txt"
  local scenario_log="${LOG_DIR}/${index}-${scenario_id}.jsonl"
  local start_ts
  local end_ts
  local duration_sec
  local codex_status
  local final_line
  local last_agent_message
  local values_json
  local validation_error=""
  local status="pass"
  local observed_status
  local observed_error
  local scenario_status
  local scenario_error
  local command_count
  local doctor_count
  local helper_script_count
  local ws_idx
  local health_idx
  local doctor_idx
  local health_before_ws="false"

  build_prompt_file "$scenario_id" "$scenario_prompt" "$prompt_file"

  start_ts="$(date +%s)"
  set +e
  codex exec \
    --ephemeral \
    --json \
    --sandbox danger-full-access \
    -C "$PROJECT_ROOT" \
    "$(cat "$prompt_file")" >"$scenario_log" 2>&1
  codex_status="$?"
  set -e
  end_ts="$(date +%s)"
  duration_sec="$((end_ts - start_ts))"

  if [[ "$status" == "pass" ]] && ! jq -e '.' "$scenario_log" >/dev/null 2>&1; then
    status="fail"
    validation_error="invalid_jsonl_transcript"
  fi

  if [[ "$status" == "pass" ]]; then
    final_line="$(extract_final_status_line "$scenario_log")"
    if [[ -z "$final_line" ]]; then
      status="fail"
      validation_error="missing_final_status_line"
    fi
  fi

  if [[ "$status" == "pass" ]]; then
    if [[ "$final_line" != NOVA_SCENARIO_RESULT\ id=${scenario_id}\ values=* ]]; then
      status="fail"
      validation_error="unexpected_final_status_format"
    fi
  fi

  if [[ "$status" == "pass" ]]; then
    values_json="${final_line#NOVA_SCENARIO_RESULT id="${scenario_id}" values=}"
    if ! echo "$values_json" | jq -e 'type == "array" and all(.[]; type == "string")' >/dev/null; then
      status="fail"
      validation_error="invalid_values_json"
    elif [[ "$expect_type" == "json_array_values" ]]; then
      :
    elif [[ "$expect_type" == "entity_id_prefix_count" ]]; then
      if [[ "$expected_count_mode" == "up_to" ]] && ! echo "$values_json" | jq -e --arg prefix "$expected_prefix" --argjson expected_count "$expected_count" '
        (length > 0 and length <= $expected_count)
        and all(.[]; startswith($prefix))
      ' >/dev/null; then
        status="fail"
        validation_error="values_mismatch"
      elif [[ "$expected_count_mode" == "exact" ]] && ! echo "$values_json" | jq -e --arg prefix "$expected_prefix" --argjson expected_count "$expected_count" '
        (length == $expected_count)
        and all(.[]; startswith($prefix))
      ' >/dev/null; then
        status="fail"
        validation_error="values_mismatch"
      elif [[ "$expected_count_mode" != "exact" && "$expected_count_mode" != "up_to" ]]; then
        status="fail"
        validation_error="unsupported_count_mode"
      fi
    else
      status="fail"
      validation_error="unsupported_expect_type"
    fi
  else
    values_json='[]'
  fi

  if [[ "$status" == "pass" && "$duration_sec" -gt "$max_duration_sec" ]]; then
    status="fail"
    validation_error="duration_exceeded"
  fi

  command_count="$(count_command_hits "$scenario_log" '.*')"
  doctor_count="$(count_command_hits "$scenario_log" '(^|[[:space:]])ha-nova[[:space:]]+doctor([[:space:]]|$)')"
  helper_script_count="$(count_command_hits "$scenario_log" 'scripts/(smoke|e2e)/')"

  ws_idx="$(first_command_index "$scenario_log" '/ws')"
  health_idx="$(first_command_index "$scenario_log" '/health')"
  doctor_idx="$(first_command_index "$scenario_log" '(^|[[:space:]])ha-nova[[:space:]]+doctor([[:space:]]|$)')"
  if [[ -n "$ws_idx" && -n "$health_idx" && "$health_idx" -lt "$ws_idx" ]]; then
    health_before_ws="true"
  fi

  if [[ "$status" == "pass" && "$expect_type" == "entity_id_prefix_count" && -z "$ws_idx" ]]; then
    status="fail"
    validation_error="missing_ws_request"
  fi

  if [[ "$status" == "pass" && "$expect_type" == "entity_id_prefix_count" && "$health_before_ws" == "true" ]]; then
    status="fail"
    validation_error="health_preflight_before_ws_detected"
  fi

  if [[ "$status" == "pass" && "$expect_type" == "entity_id_prefix_count" && -n "$doctor_idx" && -n "$ws_idx" && "$doctor_idx" -lt "$ws_idx" ]]; then
    status="fail"
    validation_error="proactive_doctor_or_ready_detected"
  fi

  if [[ "$status" == "pass" && "$helper_script_count" -gt 0 ]]; then
    status="fail"
    validation_error="helper_script_usage_detected"
  fi

  if [[ "$status" == "pass" ]]; then
    while IFS= read -r forbidden_pattern; do
      if [[ -z "$forbidden_pattern" ]]; then
        continue
      fi
      if [[ "$(count_command_hits "$scenario_log" "$forbidden_pattern")" -gt 0 ]]; then
        status="fail"
        validation_error="forbidden_pattern_detected"
        break
      fi
    done < <(echo "$forbid_patterns_json" | jq -r '.[]')
  fi

  if [[ "$status" == "pass" ]]; then
    last_agent_message="$(extract_last_agent_message "$scenario_log")"
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
  else
    last_agent_message=""
  fi

  observed_status="$status"
  observed_error="$validation_error"
  scenario_status="$status"
  scenario_error="$validation_error"

  if [[ "$observed_status" != "$expected_status" ]]; then
    scenario_status="fail"
    scenario_error="expected_status_mismatch"
  elif [[ -n "$expected_error" && "$observed_error" != "$expected_error" ]]; then
    scenario_status="fail"
    scenario_error="expected_error_mismatch"
  else
    scenario_status="pass"
    scenario_error=""
  fi

  jq -n \
    --arg id "$scenario_id" \
    --arg prompt "$scenario_prompt" \
    --arg status "$scenario_status" \
    --arg error "$scenario_error" \
    --arg observed_status "$observed_status" \
    --arg observed_error "$observed_error" \
    --arg expected_status "$expected_status" \
    --arg expected_error "$expected_error" \
    --arg expect_type "$expect_type" \
    --arg final_line "$final_line" \
    --argjson duration_sec "$duration_sec" \
    --argjson codex_exit "$codex_status" \
    --argjson command_count "$command_count" \
    --argjson doctor_count "$doctor_count" \
    --argjson helper_script_count "$helper_script_count" \
    --argjson health_before_ws "$health_before_ws" \
    --arg expected_count_mode "$expected_count_mode" \
    --arg log_file "$scenario_log" \
    --argjson values "$values_json" \
    '{
      id: $id,
      prompt: $prompt,
      status: $status,
      error: ($error | if . == "" then null else . end),
      observed_status: $observed_status,
      observed_error: ($observed_error | if . == "" then null else . end),
      expected_status: $expected_status,
      expected_error: ($expected_error | if . == "" then null else . end),
      expect_type: $expect_type,
      duration_sec: $duration_sec,
      codex_exit: $codex_exit,
      command_count: $command_count,
      proactive_doctor_count: $doctor_count,
      helper_script_count: $helper_script_count,
      health_before_ws: $health_before_ws,
      expected_count_mode: $expected_count_mode,
      final_line: $final_line,
      values: $values,
      log_file: $log_file
    }' >> "$RESULTS_FILE"

  if [[ "$scenario_status" == "pass" ]]; then
    log "PASS ${scenario_id} (${duration_sec}s, commands=${command_count})"
  else
    log "FAIL ${scenario_id} (${duration_sec}s, error=${scenario_error}, observed=${observed_status}/${observed_error}, log=${scenario_log})"
  fi
}

main() {
  local scenario_count
  local idx
  local failed_count

  require_cmd codex
  require_cmd jq
  require_cmd ha-nova
  validate_scenario_file

  mkdir -p "$LOG_DIR"
  : > "$RESULTS_FILE"

  log "Running doctor readiness gate once"
  ha-nova doctor

  scenario_count="$(jq 'length' "$SCENARIO_FILE")"
  log "Loaded ${scenario_count} scenarios from ${SCENARIO_FILE}"

  for ((idx = 0; idx < scenario_count; idx += 1)); do
    run_scenario \
      "$idx" \
      "$(jq -r ".[$idx].id" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].prompt" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].expect.type" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].expect.prefix // \"\"" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].expect.count // 0" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].expect.count_mode // \"exact\"" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].expected_status // \"pass\"" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].expected_error // \"\"" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].forbid_patterns // []" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].must_contain_text // []" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].max_duration_sec // 60" "$SCENARIO_FILE")"
  done

  jq -s --arg run_id "$RUN_ID" --arg scenario_file "$SCENARIO_FILE" '
    {
      run_id: $run_id,
      scenario_file: $scenario_file,
      passed: ([.[] | select(.status == "pass")] | length),
      failed: ([.[] | select(.status == "fail")] | length),
      scenarios: .
    }
  ' "$RESULTS_FILE" > "$SUMMARY_FILE"

  log "Summary file: ${SUMMARY_FILE}"
  jq -r '
    "run=" + .run_id,
    "passed=" + (.passed|tostring) + " failed=" + (.failed|tostring),
    (.scenarios[] | "- " + .id + " -> " + .status + " (" + (.duration_sec|tostring) + "s)")
  ' "$SUMMARY_FILE"

  failed_count="$(jq -r '.failed' "$SUMMARY_FILE")"
  [[ "$failed_count" -eq 0 ]] || die "Scenario suite failed (${failed_count} failed)."

  log "Scenario suite passed"
}

main "$@"
