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

normalize_jsonl_transcript() {
  local input_file="$1"

  python3 - "$input_file" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    for lineno, raw_line in enumerate(handle, start=1):
        line = raw_line.strip()
        if not line:
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as exc:
            sys.stderr.write(f"invalid jsonl transcript line {lineno}: {exc}\n")
            sys.exit(2)
        if not isinstance(event, dict):
            sys.stderr.write(f"invalid jsonl transcript line {lineno}: expected object event\n")
            sys.exit(3)
        print(json.dumps(event, separators=(",", ":")))
PY
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

wait_for_log_completion() {
  local file="$1"
  local previous_size="-1"
  local stable_reads=0
  local current_size

  for _ in $(seq 1 20); do
    current_size="$(wc -c <"$file" 2>/dev/null || echo 0)"
    if [[ "$current_size" == "$previous_size" && "$current_size" != "0" ]]; then
      stable_reads="$((stable_reads + 1))"
    else
      stable_reads=0
    fi
    previous_size="$current_size"

    if [[ "$stable_reads" -ge 2 ]] && grep -q '"type":"turn.completed"' "$file" 2>/dev/null; then
      return 0
    fi
    sleep 1
  done

  return 0
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
      and (
        (has("must_not_contain_text") | not)
        or
        (.must_not_contain_text | type == "array" and length > 0 and all(.[]; type == "string" and length > 0))
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

extract_status_metadata() {
  local scenario_log="$1"

  python3 - "$scenario_log" <<'PY'
import json
import re
import sys

events = []
with open(sys.argv[1], encoding="utf-8") as handle:
    for raw_line in handle:
        try:
            event = json.loads(raw_line)
        except json.JSONDecodeError:
            sys.stderr.write("invalid jsonl transcript while extracting scenario status metadata\n")
            sys.exit(2)
        events.append(event)

status_line = ""
status_line_count = 0
status_line_event_idx = 0
last_agent_message = ""
last_agent_message_last_line = ""
unexpected_events_after_final_message = 0
status_line_pattern = re.compile(r"^NOVA_SCENARIO_RESULT id=.* values=.*$", re.MULTILINE)

for idx, event in enumerate(events, start=1):
    item = event.get("item") or {}
    if event.get("type") == "item.completed" and item.get("type") == "agent_message":
        text = item.get("text") or ""
        last_agent_message = text
        matches = status_line_pattern.findall(text)
        if matches:
            status_line_count += len(matches)
            status_line = matches[-1]
            status_line_event_idx = idx
        stripped_lines = [line.strip() for line in text.splitlines() if line.strip()]
        if stripped_lines:
            last_agent_message_last_line = stripped_lines[-1]

for idx, event in enumerate(events, start=1):
    if status_line_event_idx == 0 or idx <= status_line_event_idx:
        continue
    item = event.get("item") or {}
    if event.get("type") == "turn.completed":
        continue
    if item.get("type") == "todo_list":
        continue
    unexpected_events_after_final_message += 1

print(json.dumps({
    "status_line": status_line,
    "status_line_count": status_line_count,
    "last_agent_message": last_agent_message,
    "last_agent_message_last_line": last_agent_message_last_line,
    "unexpected_events_after_final_message": unexpected_events_after_final_message,
}))
PY
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

count_helper_script_exec_hits() {
  local scenario_log="$1"

  count_command_hits \
    "$scenario_log" \
    '(^|[^[:alnum:]_./-])(\./)?scripts/(smoke|e2e|dev)/|\\b(bash|sh|zsh|python3?|node|bunx?|bun|tsx)\\b[^[:cntrl:]]*\\b(\./)?scripts/(smoke|e2e|dev)/'
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
  local must_not_contain_text_json="${12}"
  local max_duration_sec="${13}"

  local prompt_file="${LOG_DIR}/${index}-${scenario_id}.prompt.txt"
  local scenario_log="${LOG_DIR}/${index}-${scenario_id}.jsonl"
  local parsed_log="${LOG_DIR}/${index}-${scenario_id}.parsed.jsonl"
  local start_ts
  local end_ts
  local duration_sec
  local codex_status
  local final_line=""
  local last_agent_message=""
  local last_agent_message_last_line=""
  local status_line_count
  local unexpected_events_after_final_message
  local values_json='[]'
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
  run_codex_with_timeout "$max_duration_sec" "$prompt_file" "$scenario_log"
  codex_status="$?"
  set -e
  end_ts="$(date +%s)"
  duration_sec="$((end_ts - start_ts))"

  if [[ "$status" == "pass" && "$codex_status" -ne 0 ]]; then
    status="fail"
    if [[ "$codex_status" -eq 124 ]]; then
      validation_error="duration_exceeded"
    else
      validation_error="codex_exec_failed"
    fi
  fi

  if [[ "$status" == "pass" ]]; then
    wait_for_log_completion "$scenario_log"
    grep -q '"type":"turn.completed"' "$scenario_log" || {
      status="fail"
      validation_error="incomplete_transcript"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    local normalized_tmp="${parsed_log}.tmp"
    rm -f "$normalized_tmp" "$parsed_log"
    if normalize_jsonl_transcript "$scenario_log" >"$normalized_tmp"; then
      mv "$normalized_tmp" "$parsed_log"
    else
      rm -f "$normalized_tmp" "$parsed_log"
      status="fail"
      validation_error="invalid_jsonl_transcript"
    fi
  fi

  if [[ "$status" == "pass" ]]; then
    if [[ ! -s "$parsed_log" ]]; then
      status="fail"
      validation_error="invalid_jsonl_transcript"
    fi
  fi

  if [[ "$status" == "pass" ]]; then
    extract_status_metadata "$parsed_log" >"${parsed_log%.jsonl}.status.json"
    final_line="$(jq -r '.status_line' "${parsed_log%.jsonl}.status.json")"
    status_line_count="$(jq -r '.status_line_count' "${parsed_log%.jsonl}.status.json")"
    last_agent_message="$(jq -r '.last_agent_message' "${parsed_log%.jsonl}.status.json")"
    last_agent_message_last_line="$(jq -r '.last_agent_message_last_line' "${parsed_log%.jsonl}.status.json")"
    unexpected_events_after_final_message="$(jq -r '.unexpected_events_after_final_message' "${parsed_log%.jsonl}.status.json")"
    if [[ "$status_line_count" -ne 1 || -z "$final_line" ]]; then
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
    if [[ "$last_agent_message_last_line" != "$final_line" ]]; then
      status="fail"
      validation_error="status_line_not_final"
    elif [[ "$unexpected_events_after_final_message" -ne 0 ]]; then
      status="fail"
      validation_error="trailing_events_after_final_message"
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

  command_count="$(count_command_hits "$parsed_log" '.*')"
  doctor_count="$(count_command_hits "$parsed_log" '(^|[[:space:]])ha-nova[[:space:]]+doctor([[:space:]]|$)')"
  helper_script_count="$(count_helper_script_exec_hits "$parsed_log")"

  ws_idx="$(first_command_index "$parsed_log" 'relay[[:space:]]+ws([[:space:]]|$)')"
  health_idx="$(first_command_index "$parsed_log" '/health')"
  doctor_idx="$(first_command_index "$parsed_log" '(^|[[:space:]])ha-nova[[:space:]]+doctor([[:space:]]|$)')"
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
      if [[ "$(count_command_hits "$parsed_log" "$forbidden_pattern")" -gt 0 ]]; then
        status="fail"
        validation_error="forbidden_pattern_detected"
        break
      fi
    done < <(echo "$forbid_patterns_json" | jq -r '.[]')
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
      "$(jq -c ".[$idx].must_not_contain_text // []" "$SCENARIO_FILE")" \
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
