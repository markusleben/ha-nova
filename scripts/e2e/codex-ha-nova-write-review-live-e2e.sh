#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"
SCENARIO_FILE="${SCENARIO_FILE:-${SCRIPT_DIR}/codex-ha-nova-write-review-live-scenarios.json}"
OUTPUT_DIR="${OUTPUT_DIR:-$(mktemp -d "/tmp/ha-nova-codex-write-review-live.XXXXXX")}"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
LOG_DIR="${OUTPUT_DIR}/logs-${RUN_ID}"
RESULTS_FILE="${OUTPUT_DIR}/results-${RUN_ID}.ndjson"
SUMMARY_FILE="${OUTPUT_DIR}/summary-${RUN_ID}.json"

log() {
  echo "[codex-write-review-live-e2e] $*"
}

die() {
  echo "[codex-write-review-live-e2e] $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "Required command not found: ${cmd}"
}

contains_rule_code_marker() {
  local text="$1"

  printf '%s\n' "$text" | grep -Eiq '(^|[^[:alnum:]_.])([SRPMFH])-[0-9]{2}($|[^[:alnum:]_])|(^|[^[:alnum:]_.])([SRPMFH])[0-9]+($|[^[:alnum:]_])'
}

normalize_jsonl_transcript() {
  local input_file="$1"

  python3 - "$input_file" <<'PY'
import json
import re
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    for lineno, raw_line in enumerate(handle, start=1):
        line = raw_line.strip()
        if not line:
            continue
        if line == "Reading additional input from stdin...":
            continue
        if re.match(
            r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z ERROR codex_core::tools::router: error=write_stdin failed: stdin is closed for this session; rerun exec_command with tty=true to keep stdin open$",
            line,
        ):
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

log_has_transient_capacity_failure() {
  local scenario_log="$1"

  grep -Fq 'Selected model is at capacity. Please try a different model.' "$scenario_log" 2>/dev/null
}

wait_for_log_completion() {
  local file="$1"
  local expect_turn_completed="${2:-yes}"
  local previous_size="-1"
  local stable_reads=0
  local current_size
  local saw_turn_completed

  for _ in $(seq 1 20); do
    current_size="$(wc -c <"$file" 2>/dev/null || echo 0)"
    saw_turn_completed="no"
    if grep -q '"type":"turn.completed"' "$file" 2>/dev/null; then
      saw_turn_completed="yes"
    fi

    if [[ "$current_size" == "$previous_size" && "$current_size" != "0" ]]; then
      stable_reads="$((stable_reads + 1))"
    else
      stable_reads=0
    fi
    previous_size="$current_size"

    if [[ "$stable_reads" -ge 2 ]]; then
      if [[ "$expect_turn_completed" == "no" || "$saw_turn_completed" == "yes" ]]; then
        return 0
      fi
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
        (has("must_contain_prewrite_text") | not)
        or
        (.must_contain_prewrite_text | type == "array" and all(.[]; type == "string" and length > 0))
      )
      and (
        (has("must_not_contain_prewrite_text") | not)
        or
        (.must_not_contain_prewrite_text | type == "array" and length > 0 and all(.[]; type == "string" and length > 0))
      )
      and (
        (has("must_contain_postwrite_text") | not)
        or
        (.must_contain_postwrite_text | type == "array" and all(.[]; type == "string" and length > 0))
      )
      and (
        (has("must_not_contain_postwrite_text") | not)
        or
        (.must_not_contain_postwrite_text | type == "array" and length > 0 and all(.[]; type == "string" and length > 0))
      )
      and (
        (.max_duration_sec // 120)
        | type == "number"
        and . > 0
        and . == floor
      )
    )
  ' "$SCENARIO_FILE" >/dev/null || die "Invalid write-review scenario file format: ${SCENARIO_FILE}"
}

sanitize_id() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9' '_'
}

build_prompt_file() {
  local scenario_id="$1"
  local scenario_prompt="$2"
  local automation_id="$3"
  local prompt_file="$4"

  cat > "$prompt_file" <<EOF_PROMPT
Use the repo-local HA NOVA skill files in this checkout as authoritative for this task.
Do not use installed skill copies from ~/.local/share/ha-nova/skills.

User request:
${scenario_prompt}

Hard requirements:
1. Work in English only.
2. Use App + Relay terminology.
3. Use only the repo-local skills/ and docs/reference/ files in this checkout plus the local HA NOVA setup for this session.
4. Do not browse the web and do not use external research tools or docs fetches.
5. Do not use Exa, Ref, web search, or official-doc lookup tools.
6. Do not run onboarding ready/doctor/quick checks before the first HA action.
7. This is a real write-flow scenario. Create or replace exactly one disposable automation whose final config id is "${automation_id}".
8. For this harness, prefer the deterministic automation id even if the normal write skill would otherwise generate a timestamp for a fresh create.
9. Preview payload first. This harness simulates the user's next post-preview reply only for that exact preview; if the payload changes after preview, repeat preview before applying.
10. Do not run --help, dry-run probes, CLI shape checks, or fallback discovery commands. Use the repo-local skill contract directly.
11. Do not retry the write flow with alternate commands after a failed attempt. Use one clean write path only.
12. Do not delete the automation yourself; the harness will clean it up after the session.
13. Do not modify repository files.
14. After the normal user-facing result, include a ## Post-Write Review section. Report only what has substance: show a "Findings", "Collision check", or "Advisory" label only for a section that actually has content.
15. Omit every empty section. Never print any of these exact strings anywhere in the post-write section: No issues found in this review. / No related items found. / No conflicts found. / No additional advisories.
16. Show the "Collision check" label only when the collision scan returned related items. The scan still runs every time; when it returns nothing, suppress its output instead of printing an empty bucket.
17. When nothing is worth reporting, collapse the Post-Write Review to a single confirmation line (for example "Verified - no issues or conflicts"). Do not print empty headings.
18. Show the "Advisory" label only when there is at least one advisory item; otherwise omit it entirely.
19. Do not repeat a Pre-write check line inside the post-write section. Do not duplicate the same item in both Findings and Advisory.
20. End with exactly one final machine line on its own line:
    NOVA_WRITE_REVIEW_RESULT id=${scenario_id} automation_id=${automation_id} status=ok
21. Use the canonical automation payload keys "triggers", "conditions", and "actions".
22. Keep repo reads minimal. Load only the repo-local ha-nova / write skill material you actually need. Do not inspect unrelated tests, workflows, or release files.
23. Before the apply step, include exactly one Preview Payload slot that shows the payload with the canonical keys. A Markdown `##` prefix is allowed but not required. Do not print a second Preview Payload slot. If you reconsider the draft, revise silently before sending the final answer.
24. Before the apply step, print exactly one explicit prewrite verdict line:
    - safe draft: Pre-write check: no issues worth flagging before save.
    - flagged draft: Pre-write check: this draft may not behave as intended.
25. After the preview, immediately apply the write and then perform exactly these post-write checks in order: config read-back, automation reload, one target entity state read, one collision scan.
26. The one target entity state read must be a single GET to /api/states/input_boolean.mcp_stress_toggle.
27. Keep the collision-scan evidence explicit. The successful command block that runs \`ha-nova relay ws --data-file ...\` must also inline or create the \`search/related\` payload for the target entity in that same command block. Use one dedicated payload file for that collision scan command block, make the \`--data-file\` argument point to that same file, and write that payload file exactly once before the ws call. Do not hide the collision target only inside a previously prepared external file.
28. Do not include internal shorthand like R18 or R19 in preview aliases, descriptions, or any other user-facing names.
29. In the final user-facing result, do not echo raw related automation ids, raw config ids, or other machine-like related-item identifiers from the collision scan. Summarize overlap in natural language or by count instead.
30. Use this exact simple Relay sequence after the preview: one POST write with --body-file, one raw GET read-back for the same config path, one POST reload to /api/services/automation/reload, one raw GET state read to /api/states/input_boolean.mcp_stress_toggle, one ws collision scan with --data-file. Do not use /api/config/automation/reload. Do not add jq/jq-file filters, extra parsing helper steps, or alternate Relay variants.
31. Use simple deterministic local filenames such as draft.json, readback.json, reload.json, state.json, and collision.json. Do not use mktemp.
32. If you use inline Python to create local JSON payload files, use \`python3\` only. Do not use \`python\`.
33. Any shell command block that contains the write flow must begin with \`set -e\` so a local prep error aborts before the write. Do not continue after a prep error and do not retry with a second write path.
34. In user-facing text, never print tool-call syntax, JSON command envelopes, raw exec transcripts, or fragments like \`to=functions.exec_command\`, \`to=multi_tool_use.parallel\`, or \`{\"cmd\":...\` . The only allowed JSON in user-facing output is the fenced preview payload, plus the final machine line.
35. The first agent message after the repo-local file reads must be the preview block plus the explicit prewrite verdict. Do not announce that the write is running before the preview is shown.
36. After those checks, stop and print the final user-facing result, the ## Post-Write Review section, and the machine line. Do not emit extra messages after the machine line.
37. Before the first HA action, read at most these repo-local files unless a write would otherwise fail: skills/ha-nova/SKILL.md, directly referenced skills/ha-nova/*.md reference files, skills/write/SKILL.md, skills/review/checks.md.
38. Do not read docs/reference/, tests/, workflows, or release files for this harness.
39. Do not emit todo lists, meta progress updates, or extra planning summaries. Move directly from the minimal local reads to preview payload, explicit prewrite verdict, apply, ordered checks, final result, and machine line.
EOF_PROMPT
}

count_command_hits() {
  local scenario_log="$1"
  local pattern="$2"

  jq -r '
    select(.type == "item.completed" and .item.type == "command_execution")
    | (.item.command // "")
  ' "$scenario_log" 2>/dev/null | grep -E -c "$pattern" || true
}

count_command_hits_before_index() {
  local scenario_log="$1"
  local max_idx="$2"
  local pattern="$3"

  if [[ "$max_idx" -le 0 ]]; then
    count_command_hits "$scenario_log" "$pattern"
    return
  fi

  jq -sr --arg pattern "$pattern" --argjson max_idx "$max_idx" '
    [
      to_entries[]
      | (.key + 1) as $idx
      | select($idx < $max_idx)
      | .value
      | select(.type == "item.completed" and .item.type == "command_execution")
      | (.item.command // "")
      | select(test($pattern))
    ] | length
  ' "$scenario_log" 2>/dev/null || true
}

count_helper_script_exec_hits() {
  local scenario_log="$1"

  python3 - "$scenario_log" <<'PY'
import json
import re
import sys

direct_re = re.compile(r'^(?:\./)?scripts/(?:smoke|e2e|dev)/\S+')
shell_re = re.compile(
    r'(?:^|[\s\'"`])(?:env(?:\s+[A-Za-z_][A-Za-z0-9_]*=\S+)*\s+)?(?:timeout\s+\S+\s+)?(?:\S*/)?(?:bash|sh|zsh|python3?|node|bunx?|bun|tsx)\b[^\n]*\b(?:\./)?scripts/(?:smoke|e2e|dev)/\S+'
)

count = 0

with open(sys.argv[1], encoding="utf-8") as handle:
    for raw_line in handle:
        try:
            event = json.loads(raw_line)
        except json.JSONDecodeError:
            continue
        if event.get("type") != "item.completed":
            continue
        item = event.get("item") or {}
        if item.get("type") != "command_execution":
            continue
        command = (item.get("command") or "").strip()
        if not command:
            continue
        for segment in re.split(r'(?:&&|\|\||;|\n)', command):
            segment = segment.strip()
            if not segment:
                continue
            if direct_re.search(segment) or shell_re.search(segment):
                count += 1
                break

print(count)
PY
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
          (.type != "web_search")
          or
          ((.query // "") | test("^time:\\s*\\{") | not)
        )
      | select(
          (.type == "web_search")
          or
          (
            (.server // "" | test("^(exa|Ref|web)$"))
            or
            (.tool // "" | test("^(get_code_context_exa|web_search_exa|ref_search_documentation|ref_read_url|search_query|open|click|find|screenshot|image_query|sports|finance|weather|time)$"))
          )
        )
    ] | length
  ' "$scenario_log" 2>/dev/null || true
}

count_shell_network_hits() {
  local scenario_log="$1"

  python3 - "$scenario_log" <<'PY'
import json
import re
import sys

count = 0
with open(sys.argv[1], encoding="utf-8") as handle:
    for raw_line in handle:
        try:
            event = json.loads(raw_line)
        except json.JSONDecodeError:
            continue

        if event.get("type") != "item.completed":
            continue

        item = event.get("item") or {}
        if item.get("type") != "command_execution":
            continue

        command = (item.get("command") or "").lower()
        if not command:
            continue

        if re.search(r"\b(curl|wget|httpie|lynx|links|elinks|xh)\b", command):
            count += 1
            continue

        if re.search(r"\bpython3?\b", command) and re.search(r"\b(requests|urllib|httpx)\b", command):
            count += 1
            continue

        if re.search(r"\bnode\b", command) and (
            re.search(r"\bfetch\b", command) or re.search(r"https?://", command)
        ):
            count += 1
            continue

        if re.search(r"\bruby\b", command) and re.search(r"(net::http|open-uri)", command):
            count += 1

print(count)
PY
}

extract_write_proof() {
  local scenario_log="$1"
  local automation_id="$2"
  local collision_item_id="$3"

  python3 - "$scenario_log" "$automation_id" "$collision_item_id" <<'PY'
import json
import re
import sys

scenario_log = sys.argv[1]
automation_id = sys.argv[2]
collision_item_id = sys.argv[3]

events = []
with open(scenario_log, encoding="utf-8") as handle:
    for raw_line in handle:
        try:
            event = json.loads(raw_line)
        except json.JSONDecodeError:
            sys.stderr.write("invalid jsonl transcript while extracting write-review proof\n")
            sys.exit(2)
        events.append(event)

pre_messages = []
post_messages = []
status_line = ""
status_line_count = 0
final_message_last_line = ""
status_line_event_idx = 0
unexpected_events_after_final_message = 0
first_write_idx = 0
first_write_attempt_idx = 0
first_write_pos = -1
write_attempts = 0
successful_write_attempts = 0
write_hits = 0
readback_after_write_key = None
reload_after_write_key = None
state_after_write_key = None
collision_after_write_key = None
readback_after_write_count = 0
reload_after_write_count = 0
wrong_reload_after_write_count = 0
state_after_write_count = 0
collision_after_write_count = 0

post_pattern = re.compile(r'"method"\s*:\s*"POST"|(?:^|\s)-X\s+POST\b|--method(?:=|\s+)POST\b')
get_pattern = re.compile(r'"method"\s*:\s*"GET"|(?:^|\s)-X\s+GET\b|--method(?:=|\s+)GET\b')
reload_pattern = re.compile(r"/api/services/automation/reload")
wrong_reload_pattern = re.compile(r"/api/config/automation/reload")
state_target_pattern = re.compile(
    rf"/api/states/{re.escape(collision_item_id)}(?=$|[^A-Za-z0-9_])"
) if collision_item_id else re.compile(r"$^")
collision_pattern = re.compile(r'"type":"search/related"|"type"\s*:\s*"search/related"|search/related')
relay_ws_pattern = re.compile(r"\bha-nova\s+relay\s+ws\b")
status_line_pattern = re.compile(
    r"^NOVA_WRITE_REVIEW_RESULT id=.* automation_id=.* status=ok$",
    re.MULTILINE,
)

command_separator_pattern = re.compile(r"(?:&&|\|\||[;\n])")

def first_pattern_pos_after(command: str, pattern: re.Pattern[str], min_pos: int) -> int:
    for match in pattern.finditer(command):
        if match.start() > min_pos:
            return match.start()
    return -1

def first_pattern_pos(command: str, pattern: re.Pattern[str]) -> int:
    match = pattern.search(command)
    return match.start() if match else -1

def count_pattern_after(command: str, pattern: re.Pattern[str], min_pos: int) -> int:
    return sum(1 for match in pattern.finditer(command) if match.start() > min_pos)

def maybe_set_key(current, idx: int, pos: int):
    if pos < 0:
        return current
    candidate = (idx, pos)
    if current is None or candidate < current:
        return candidate
    return current

def collapse_shell_line_continuations(command: str) -> str:
    return re.sub(r'\\[ \t]*\n[ \t]*', ' ', command)

def command_segment_bounds(command: str, pos: int):
    segment_start = 0
    for match in command_separator_pattern.finditer(command):
        if match.end() <= pos:
            segment_start = match.end()
            continue
        return segment_start, match.start()
    return segment_start, len(command)

def path_is_bound_to_arg(command: str, pos: int) -> bool:
    prefix = command[max(0, pos - 48):pos]
    return bool(re.search(r"--path(?:=|\s+)(?:\\?['\"])?$", prefix))

def first_path_arg_operation_pos_after(command: str, positions: list[int], method_re: re.Pattern[str], min_pos: int):
    for pos in positions:
        if pos <= min_pos or not path_is_bound_to_arg(command, pos):
            continue
        segment_start, segment_end = command_segment_bounds(command, pos)
        if method_re.search(command[segment_start:segment_end]):
            return pos
    return -1

def count_path_arg_operations_after(command: str, positions: list[int], method_re: re.Pattern[str], min_pos: int):
    count = 0
    for pos in positions:
        if pos <= min_pos or not path_is_bound_to_arg(command, pos):
            continue
        segment_start, segment_end = command_segment_bounds(command, pos)
        if method_re.search(command[segment_start:segment_end]):
            count += 1
    return count

def extract_target_path_positions(command: str):
    literal_path_pattern = re.compile(
        rf"/api/config/automation/config/{re.escape(automation_id)}(?=$|[^A-Za-z0-9_])"
    )
    positions = [match.start() for match in literal_path_pattern.finditer(command)]

    for match in re.finditer(rf"\b([A-Za-z_][A-Za-z0-9_]*)=(['\"]?){re.escape(automation_id)}\2", command):
        var_name = match.group(1)
        ref_pattern = re.compile(
            rf"/api/config/automation/config/\$\{{?{re.escape(var_name)}\}}?(?=$|[^A-Za-z0-9_])"
        )
        positions.extend(ref_match.start() for ref_match in ref_pattern.finditer(command))
    return sorted(set(positions))

def first_target_operation_pos_after(command: str, method_re: re.Pattern[str], min_pos: int):
    return first_path_arg_operation_pos_after(
        command,
        extract_target_path_positions(command),
        method_re,
        min_pos,
    )

def count_target_operations_after(command: str, method_re: re.Pattern[str], min_pos: int):
    return count_path_arg_operations_after(
        command,
        extract_target_path_positions(command),
        method_re,
        min_pos,
    )

def command_bound_to_state_target(command: str) -> bool:
    return first_state_target_pos_after(command, -1) >= 0

def command_bound_to_collision_item(command: str) -> bool:
    return bool(collision_item_id) and collision_item_id in command

def command_has_visible_collision_payload_context(command: str) -> bool:
    return command_bound_to_collision_item(command) and bool(collision_pattern.search(command))

def normalize_ref(token: str) -> str:
    token = token.strip().strip("'\"")
    if token.startswith("${") and token.endswith("}"):
        return token[2:-1]
    if token.startswith("$"):
        return token[1:]
    return token

def extract_state_target_positions(command: str):
    positions = [match.start() for match in state_target_pattern.finditer(command)]
    if collision_item_id:
        for match in re.finditer(rf"\b([A-Za-z_][A-Za-z0-9_]*)=(['\"]?){re.escape(collision_item_id)}\2", command):
            var_name = match.group(1)
            ref_pattern = re.compile(rf"/api/states/\$\{{?{re.escape(var_name)}\}}?(?=$|[^A-Za-z0-9_])")
            positions.extend(ref_match.start() for ref_match in ref_pattern.finditer(command))
    return sorted(set(positions))

def first_state_target_pos_after(command: str, min_pos: int):
    return first_path_arg_operation_pos_after(
        command,
        extract_state_target_positions(command),
        get_pattern,
        min_pos,
    )

def count_state_target_operations_after(command: str, min_pos: int):
    return count_path_arg_operations_after(
        command,
        extract_state_target_positions(command),
        get_pattern,
        min_pos,
    )

def window_has_collision_payload_context(command: str, start: int, end: int):
    window_start = max(0, start - 400)
    window_end = min(len(command), end + 400)
    window = command[window_start:window_end]
    return bool(collision_item_id) and collision_item_id in window and bool(collision_pattern.search(window))

def extract_collision_payload_ref_counts_before(command: str, max_pos: int):
    ref_counts = {}

    def record(ref: str):
        normalized = normalize_ref(ref)
        if normalized:
            ref_counts[normalized] = ref_counts.get(normalized, 0) + 1

    for match in re.finditer(r'>\s*([^\s;]+)', command):
        if match.end() > max_pos:
            continue
        if window_has_collision_payload_context(command, match.start(), match.end()):
            record(match.group(1))
    for match in re.finditer(r'\btee\s+([^\s|;]+)', command):
        if match.end() > max_pos:
            continue
        if window_has_collision_payload_context(command, match.start(), match.end()):
            record(match.group(1))
    for match in re.finditer(r'Path\(\s*[\'"]([^\'"]+)[\'"]\s*\)\.write_(?:text|bytes)', command):
        if match.end() > max_pos:
            continue
        if window_has_collision_payload_context(command, match.start(), match.end()):
            record(match.group(1))
    for match in re.finditer(r'open\(\s*[\'"]([^\'"]+)[\'"]\s*,\s*[\'"][^\'"]*w', command):
        if match.end() > max_pos:
            continue
        if window_has_collision_payload_context(command, match.start(), match.end()):
            record(match.group(1))
    return ref_counts

def first_ws_data_file_ref_after(command: str, min_pos: int):
    ws_matches = list(relay_ws_pattern.finditer(command))
    for idx, ws_match in enumerate(ws_matches):
        if ws_match.start() <= min_pos:
            continue
        segment_end = ws_matches[idx + 1].start() if idx + 1 < len(ws_matches) else len(command)
        ws_segment = command[ws_match.start():segment_end]
        data_file_match = re.search(r'--data-file(?:=|\s+)([^\s;]+)', ws_segment)
        if data_file_match:
            return ws_match.start(), normalize_ref(data_file_match.group(1)), idx
        return ws_match.start(), "", idx
    return -1, "", -1

def collision_payload_ref_counts_for_ws(command: str, ws_matches, ws_idx: int):
    context_start = ws_matches[ws_idx - 1].end() if ws_idx > 0 else 0
    ws_pos = ws_matches[ws_idx].start()
    return extract_collision_payload_ref_counts_before(command[context_start:], ws_pos - context_start)

def count_data_file_ref_mentions(text: str, data_file_ref: str):
    if not data_file_ref:
        return 0
    if "/" in data_file_ref or "." in data_file_ref:
        return text.count(data_file_ref)

    patterns = [
        rf"\b{re.escape(data_file_ref)}\b",
        rf"\${re.escape(data_file_ref)}\b",
        rf"\$\{{{re.escape(data_file_ref)}\}}",
    ]
    return sum(len(re.findall(pattern, text)) for pattern in patterns)

def command_has_visible_collision_payload_context_for_ref(command: str, ws_matches, ws_idx: int, data_file_ref: str):
    context_start = ws_matches[ws_idx - 1].end() if ws_idx > 0 else 0
    ws_pos = ws_matches[ws_idx].start()
    context_before_ws = command[context_start:ws_pos]

    if not collision_item_id or collision_item_id not in context_before_ws:
        return False
    if not collision_pattern.search(context_before_ws):
        return False
    if not data_file_ref:
        return False

    ref_counts = collision_payload_ref_counts_for_ws(command, ws_matches, ws_idx)
    if ref_counts.get(data_file_ref, 0) > 0:
        return True

    # Shell blocks sometimes echo the exact data-file ref near the payload write; accept
    # that only when the same ref is visibly present in the collision-payload context.
    return count_data_file_ref_mentions(context_before_ws, data_file_ref) > 0

def first_collision_scan_pos_after(command: str, min_pos: int):
    ws_pos, data_file_ref, ws_idx = first_ws_data_file_ref_after(command, min_pos)
    if ws_pos < 0:
        return -1

    # The proof must stay inside one successful command block: the same command that
    # executes `ha-nova relay ws --data-file ...` must also show the visible collision
    # payload context for the target entity.
    ws_matches = list(relay_ws_pattern.finditer(command))
    if command_has_visible_collision_payload_context_for_ref(command, ws_matches, ws_idx, data_file_ref):
        return ws_pos
    return -1

def count_collision_scans_after(command: str, min_pos: int):
    count = 0
    ws_matches = list(relay_ws_pattern.finditer(command))
    for idx, ws_match in enumerate(ws_matches):
        if ws_match.start() <= min_pos:
            continue
        segment_start, segment_end = command_segment_bounds(command, ws_match.start())
        segment = command[segment_start:segment_end]
        ws_offset = ws_match.start() - segment_start
        next_ws_offset = ws_matches[idx + 1].start() - segment_start if idx + 1 < len(ws_matches) and ws_matches[idx + 1].start() < segment_end else len(segment)
        ws_segment = segment[ws_offset:next_ws_offset]
        data_file_match = re.search(r'--data-file(?:=|\s+)([^\s;]+)', ws_segment)
        if not data_file_match:
            continue
        data_file_ref = normalize_ref(data_file_match.group(1))
        if command_has_visible_collision_payload_context_for_ref(command, ws_matches, idx, data_file_ref):
            count += 1
    return count

def normalize_postwrite_item(line: str):
    line = line.strip()
    if not line:
        return ""
    line = re.sub(r"^[-*]\s+", "", line)
    line = re.sub(r"^\d+\.\s+", "", line)
    line = re.sub(r"^[🔴🟠🟡]\s*", "", line)
    return re.sub(r"\s+", " ", line).strip().lower()

FORBIDDEN_EMPTY_BUCKETS = (
    "No issues found in this review.",
    "No related items found.",
    "No conflicts found.",
    "No additional advisories.",
)

POSTWRITE_LABEL_PATTERN = re.compile(
    r"(?mi)^\s*(?:#+\s*)?(?:\*\*)?(?:Findings|Collision check|Advisory)(?:\*\*)?\s*$"
)


def collect_postwrite_items(section_text: str):
    items = []
    for raw_line in section_text.splitlines():
        line = raw_line.strip()
        if not line:
            continue
        if status_line_pattern.match(line):
            continue
        if line in FORBIDDEN_EMPTY_BUCKETS:
            continue
        if line.startswith("Why:") or line.startswith("Fix:"):
            continue
        normalized = normalize_postwrite_item(line)
        if normalized:
            items.append(normalized)
    return items


def extract_postwrite_label_block(section_text: str, label: str) -> str:
    label_pattern = re.compile(
        rf"(?mi)^\s*(?:#+\s*)?(?:\*\*)?{re.escape(label)}(?:\*\*)?\s*$"
    )
    start_match = label_pattern.search(section_text)
    if not start_match:
        return ""
    block_start = start_match.end()
    next_label = POSTWRITE_LABEL_PATTERN.search(section_text, block_start)
    next_heading = re.search(r"(?m)^\s*##\s", section_text[block_start:])
    block_end = len(section_text)
    if next_label:
        block_end = min(block_end, next_label.start())
    if next_heading:
        block_end = min(block_end, block_start + next_heading.start())
    return section_text[block_start:block_end].strip()


POSTWRITE_OPTIONAL_LABELS = ("Findings", "Collision check", "Advisory")


def postwrite_label_present(section_text: str, label: str) -> bool:
    return bool(
        re.search(
            rf"(?mi)^\s*(?:#+\s*)?(?:\*\*)?{re.escape(label)}(?:\*\*)?\s*$",
            section_text,
        )
    )


def postwrite_label_is_empty_heading(section_text: str, label: str) -> bool:
    # A label with substantive items is fine; a label carrying only a forbidden
    # "none" bucket is already caught by postwrite_forbidden_empty_bucket. Flag only
    # a label with literally nothing under it — the dangling empty heading the
    # post-write contract forbids and that no other signal catches.
    if not postwrite_label_present(section_text, label):
        return False
    label_block = extract_postwrite_label_block(section_text, label)
    if collect_postwrite_items(label_block):
        return False
    return not any(bucket in label_block for bucket in FORBIDDEN_EMPTY_BUCKETS)


def postwrite_review_has_content(block: str) -> bool:
    # The post-write review must carry substance: section content or, when clean, a
    # single confirmation line. A bare heading with nothing under it (status lines
    # are already stripped upstream) is the all-empty review the contract forbids —
    # it must collapse to a confirmation line instead.
    for raw_line in block.splitlines()[1:]:
        line = raw_line.strip()
        if line and not status_line_pattern.match(line):
            return True
    return False


def strip_status_lines(text: str) -> str:
    kept_lines = []
    for raw_line in text.splitlines():
        if status_line_pattern.match(raw_line.strip()):
            continue
        kept_lines.append(raw_line)
    return "\n".join(kept_lines).strip()

for idx, event in enumerate(events, start=1):
    item = event.get("item") or {}
    if event.get("type") == "item.completed" and item.get("type") == "agent_message":
        text = item.get("text") or ""
        matches = status_line_pattern.findall(text)
        if matches:
            status_line_count += len(matches)
            status_line = matches[-1]
            status_line_event_idx = idx
        stripped_lines = [line.strip() for line in text.splitlines() if line.strip()]
        if stripped_lines:
            final_message_last_line = stripped_lines[-1]
        if first_write_attempt_idx == 0:
            pre_messages.append(text)
        else:
            stripped_text = strip_status_lines(text)
            if stripped_text:
                post_messages.append(stripped_text)

    if event.get("type") == "item.completed" and item.get("type") == "command_execution":
        command = collapse_shell_line_continuations(item.get("command") or "")
        exit_code = item.get("exit_code")
        write_attempt_pos = first_target_operation_pos_after(command, post_pattern, -1)
        write_attempts += count_target_operations_after(command, post_pattern, -1)
        if write_attempt_pos >= 0 and first_write_attempt_idx == 0:
            first_write_attempt_idx = idx
        write_pos = write_attempt_pos
        if write_pos >= 0 and first_write_idx == 0:
            first_write_idx = idx
            first_write_pos = write_pos
        if write_pos >= 0 and exit_code == 0:
            successful_write_attempts += count_target_operations_after(command, post_pattern, -1)
            write_hits += 1
        if first_write_idx > 0 and exit_code == 0:
            if idx == first_write_idx:
                readback_pos = first_target_operation_pos_after(command, get_pattern, first_write_pos)
                readback_after_write_count += count_target_operations_after(command, get_pattern, first_write_pos)
                readback_after_write_key = maybe_set_key(
                    readback_after_write_key,
                    idx,
                    readback_pos,
                )
                reload_pos = first_pattern_pos_after(command, reload_pattern, first_write_pos)
                reload_after_write_count += count_pattern_after(command, reload_pattern, first_write_pos)
                wrong_reload_after_write_count += count_pattern_after(command, wrong_reload_pattern, first_write_pos)
                reload_after_write_key = maybe_set_key(
                    reload_after_write_key,
                    idx,
                    reload_pos,
                )
                if command_bound_to_state_target(command):
                    state_pos = first_state_target_pos_after(command, first_write_pos)
                    state_after_write_count += count_state_target_operations_after(command, first_write_pos)
                    state_after_write_key = maybe_set_key(
                        state_after_write_key,
                        idx,
                        state_pos,
                    )
                collision_pos = first_collision_scan_pos_after(command, first_write_pos)
                collision_after_write_count += count_collision_scans_after(command, first_write_pos)
                if collision_pos >= 0:
                    collision_after_write_key = maybe_set_key(
                        collision_after_write_key,
                        idx,
                        collision_pos,
                    )
            else:
                readback_pos = first_target_operation_pos_after(command, get_pattern, -1)
                readback_after_write_count += count_target_operations_after(command, get_pattern, -1)
                if readback_pos >= 0:
                    readback_after_write_key = maybe_set_key(
                        readback_after_write_key,
                        idx,
                        readback_pos,
                    )
                reload_pos = first_pattern_pos(command, reload_pattern)
                reload_after_write_count += count_pattern_after(command, reload_pattern, -1)
                wrong_reload_after_write_count += count_pattern_after(command, wrong_reload_pattern, -1)
                reload_after_write_key = maybe_set_key(
                    reload_after_write_key,
                    idx,
                    reload_pos,
                )
                if command_bound_to_state_target(command):
                    state_pos = first_state_target_pos_after(command, -1)
                    state_after_write_count += count_state_target_operations_after(command, -1)
                    state_after_write_key = maybe_set_key(
                        state_after_write_key,
                        idx,
                        state_pos,
                    )
                collision_pos = first_collision_scan_pos_after(command, -1)
                collision_after_write_count += count_collision_scans_after(command, -1)
                if collision_pos >= 0:
                    collision_after_write_key = maybe_set_key(
                        collision_after_write_key,
                        idx,
                        collision_pos,
                    )

for idx, event in enumerate(events, start=1):
    if status_line_event_idx == 0 or idx <= status_line_event_idx:
        continue
    item = event.get("item") or {}
    if event.get("type") == "turn.completed":
        continue
    if item.get("type") == "todo_list":
        continue
    if event.get("type") != "turn.completed":
        unexpected_events_after_final_message += 1

ordered_postwrite_verification = bool(
    readback_after_write_key
    and reload_after_write_key
    and state_after_write_key
    and collision_after_write_key
    and readback_after_write_key < reload_after_write_key < state_after_write_key < collision_after_write_key
)

result = {
    "first_write_attempt_idx": first_write_attempt_idx,
    "first_write_idx": first_write_idx,
    "write_attempts": write_attempts,
    "successful_write_attempts": successful_write_attempts,
    "write_hits": write_hits,
    "prewrite_text": "\n\n".join([part for part in pre_messages if part]),
    "postwrite_text": "\n\n".join([part for part in post_messages if part]),
    "status_line": status_line,
    "status_line_count": status_line_count,
    "final_message_last_line": final_message_last_line,
    "readback_after_write": readback_after_write_count,
    "reload_after_write": reload_after_write_count,
    "wrong_reload_after_write": wrong_reload_after_write_count,
    "state_after_write": state_after_write_count,
    "collision_after_write": collision_after_write_count,
    "ordered_postwrite_verification": ordered_postwrite_verification,
    "unexpected_events_after_final_message": unexpected_events_after_final_message,
}
preview_sections = re.findall(
    r"^(?:##\s*)?Preview Payload\b(.*?)(?=\nPre-write check:|\n(?:##\s*)?[A-Z][^\n]*:|\n## |\Z)",
    result["prewrite_text"],
    re.MULTILINE | re.DOTALL,
)
result["preview_section_count"] = len(preview_sections)
result["preview_has_canonical_keys"] = (
    len(preview_sections) == 1
    and all(key in preview_sections[0] for key in ("triggers", "conditions", "actions"))
)
# Issue #390: a preview whose only explanation for a touched collection is a
# count transition ("5 items | 3 items", "5 items → 3 items", "... and N
# more") or a type-only row ("5 (number) | 5 (string)") must also carry a
# plain-language behavior narrative. Structural check: the narrative must sit
# in the same or an adjacent paragraph as the count/type line (the card shape
# puts it directly above the changes block), so unrelated boilerplate prose
# elsewhere cannot satisfy the gate.
COUNT_ONLY_RE = re.compile(
    r"\|\s*\d+\s+items?\s*\|\s*\d+\s+items?\s*\|"
    r"|\d+\s+items?\s*(?:→|->)\s*\d+\s+items?"
    r"|…\s*and\s+\d+\s+more"
    r"|\|\s*([^|()\n]+?)\s*\(\w+\)\s*\|\s*\1\s*\(\w+\)\s*\|"
    r"|([^|()\n]+?)\s*\(\w+\)\s*(?:→|->)\s*\2\s*\(\w+\)",
)


def is_narrative_line(line: str) -> bool:
    stripped = line.strip()
    if not stripped or len(stripped.split()) < 4:
        return False
    # A count/type transition line is the thing needing explanation, never
    # the explanation itself.
    if COUNT_ONLY_RE.search(stripped):
        return False
    if stripped.startswith(("|", "#", "-", "`", "📝", "⚠", "✅", "🗑", '"', "{", "}", "[", "]")):
        return False
    # Card slot/scaffolding lines are not narrative prose.
    if stripped.startswith(
        ("Options:", "Option:", "Pre-write check:", "Save status", "Status:",
         "Manifest:", "Recovery:", "Impact:", "Used by:", "Checked:",
         "To delete", "Reply ", "NOVA_WRITE_REVIEW_RESULT")
    ):
        return False
    if "Preview Payload" in stripped or " · " in stripped:
        return False
    # Save-status scaffolding without the emoji/label ("Nothing has been
    # saved yet.") is a card slot, not narrative.
    if re.search(r"(?i)^(?:nothing|not)\b.*\b(?:saved|deleted|executed|applied|written|run)\b.*\byet\b", stripped):
        return False
    return True


# Fenced payload blocks are machine data — neither narrative nor diff rows.
prewrite_prose = re.sub(r"```.*?(?:```|\Z)", "", result["prewrite_text"], flags=re.DOTALL)
paragraphs = [p for p in re.split(r"\n\s*\n", prewrite_prose)]
uncovered_count_paragraph = False
for i, paragraph in enumerate(paragraphs):
    if not COUNT_ONLY_RE.search(paragraph):
        continue
    nearby = paragraphs[max(0, i - 1) : i + 2]
    if not any(
        is_narrative_line(line) for block in nearby for line in block.splitlines()
    ):
        uncovered_count_paragraph = True
        break
result["count_only_preview_without_narrative"] = uncovered_count_paragraph
# The post-write contract no longer mandates fixed Findings/Collision check/Advisory
# headings: report only sections with substance, omit empties, and never print a
# "none" bucket. Structure is valid as soon as a Post-Write Review section exists.
postwrite_review_match = re.search(
    r"(?ms)^(?:##\s*)?Post-Write Review\b.*",
    result["postwrite_text"],
)
postwrite_review_block = postwrite_review_match.group(0) if postwrite_review_match else ""
# Findings and Advisory are optional; parse them only when present so we can still
# block the same item appearing in both sections.
findings_text = extract_postwrite_label_block(postwrite_review_block, "Findings")
advisory_text = extract_postwrite_label_block(postwrite_review_block, "Advisory")
findings_items = collect_postwrite_items(findings_text)
advisory_items = collect_postwrite_items(advisory_text)
result["duplicate_findings_advisory_items"] = sorted(set(findings_items) & set(advisory_items))
result["postwrite_repeats_prewrite_verdict"] = "Pre-write check:" in result["postwrite_text"]
result["postwrite_forbidden_empty_bucket"] = any(
    bucket in result["postwrite_text"] for bucket in FORBIDDEN_EMPTY_BUCKETS
)
# A bare optional label with no substantive items is an empty heading, which the
# post-write contract forbids ("show a label only for a section that actually has
# content"). Treat that as an invalid structure so the gate cannot pass an empty
# post-write review; the FORBIDDEN_EMPTY_BUCKETS check only catches the old fixed
# "none" strings, not a label left dangling with nothing under it.
empty_postwrite_labels = [
    label
    for label in POSTWRITE_OPTIONAL_LABELS
    if postwrite_label_is_empty_heading(postwrite_review_block, label)
]
result["empty_postwrite_labels"] = empty_postwrite_labels
result["postwrite_review_has_content"] = postwrite_review_has_content(postwrite_review_block)
result["postwrite_section_structure_valid"] = (
    bool(postwrite_review_match)
    and not empty_postwrite_labels
    and result["postwrite_review_has_content"]
)
print(json.dumps(result))
PY
}

cleanup_automation() {
  local automation_id="$1"
  local cleanup_body="${OUTPUT_DIR}/cleanup-empty.json"

  printf '{}' > "$cleanup_body"
  ha-nova relay core --method DELETE --path "/api/config/automation/config/${automation_id}" --body-file "$cleanup_body" >/dev/null 2>&1 || true
}

run_scenario() {
  local index="$1"
  local scenario_id="$2"
  local scenario_prompt="$3"
  local must_contain_json="$4"
  local must_not_contain_json="$5"
  local must_contain_postwrite_json="$6"
  local must_not_contain_postwrite_json="$7"
  local max_duration_sec="$8"
  local collision_item_id="$9"

  local automation_id="nova_write_review_$(sanitize_id "$scenario_id")"
  local prompt_file="${LOG_DIR}/${index}-${scenario_id}.prompt.txt"
  local scenario_log="${LOG_DIR}/${index}-${scenario_id}.jsonl"
  local parsed_log="${LOG_DIR}/${index}-${scenario_id}.parsed.jsonl"
  local analysis_json="${LOG_DIR}/${index}-${scenario_id}.analysis.json"
  local start_ts
  local end_ts
  local duration_sec
  local codex_status
  local status="pass"
  local validation_error=""
  local status_line
  local status_line_count
  local final_message_last_line
  local prewrite_text
  local postwrite_text
  local first_write_attempt_idx
  local write_hits
  local write_attempts
  local successful_write_attempts
  local first_write_idx
  local readback_after_write
  local reload_after_write
  local wrong_reload_after_write
  local state_after_write
  local collision_after_write
  local ordered_postwrite_verification
  local duplicate_findings_advisory_items_count
  local postwrite_repeats_prewrite_verdict
  local postwrite_forbidden_empty_bucket
  local postwrite_section_structure_valid
  local unexpected_events_after_final_message
  local preview_section_count
  local preview_has_canonical_keys
  local count_only_preview_without_narrative
  local helper_script_count
  local onboarding_count
  local external_research_hits
  local shell_network_hits
  local codex_attempt=1

  build_prompt_file "$scenario_id" "$scenario_prompt" "$automation_id" "$prompt_file"

  start_ts="$(date +%s)"
  while true; do
    set +e
    run_codex_with_timeout "$max_duration_sec" "$prompt_file" "$scenario_log"
    codex_status="$?"
    set -e

    if [[ "$codex_status" -eq 0 ]]; then
      break
    fi

    if [[ "$codex_attempt" -ge 2 ]] || [[ ! -s "$scenario_log" ]] || ! log_has_transient_capacity_failure "$scenario_log"; then
      break
    fi

    log "Retrying ${scenario_id} after transient model-capacity failure"
    codex_attempt="$((codex_attempt + 1))"
    sleep 2
  done
  end_ts="$(date +%s)"
  duration_sec="$((end_ts - start_ts))"

  cleanup_automation "$automation_id"

  if [[ "$status" == "pass" && "$codex_status" -ne 0 ]]; then
    status="fail"
    if [[ "$codex_status" -eq 124 ]]; then
      validation_error="duration_exceeded"
    else
      validation_error="codex_exec_failed"
    fi
  fi

  if [[ "$status" == "pass" ]]; then
    wait_for_log_completion "$scenario_log" "yes"
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
    [[ -s "$parsed_log" ]] || {
      status="fail"
      validation_error="invalid_jsonl_transcript"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    helper_script_count="$(count_helper_script_exec_hits "$parsed_log")"
    [[ "$helper_script_count" -eq 0 ]] || {
      status="fail"
      validation_error="helper_script_usage_detected"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    extract_write_proof "$parsed_log" "$automation_id" "$collision_item_id" >"$analysis_json"
    first_write_attempt_idx="$(jq -r '.first_write_attempt_idx' "$analysis_json")"
    write_hits="$(jq -r '.write_hits' "$analysis_json")"
    write_attempts="$(jq -r '.write_attempts' "$analysis_json")"
    successful_write_attempts="$(jq -r '.successful_write_attempts' "$analysis_json")"
    first_write_idx="$(jq -r '.first_write_idx' "$analysis_json")"
    prewrite_text="$(jq -r '.prewrite_text' "$analysis_json")"
    postwrite_text="$(jq -r '.postwrite_text' "$analysis_json")"
    status_line="$(jq -r '.status_line' "$analysis_json")"
    status_line_count="$(jq -r '.status_line_count' "$analysis_json")"
    final_message_last_line="$(jq -r '.final_message_last_line' "$analysis_json")"
    readback_after_write="$(jq -r '.readback_after_write' "$analysis_json")"
    reload_after_write="$(jq -r '.reload_after_write' "$analysis_json")"
    wrong_reload_after_write="$(jq -r '.wrong_reload_after_write' "$analysis_json")"
    state_after_write="$(jq -r '.state_after_write' "$analysis_json")"
    collision_after_write="$(jq -r '.collision_after_write' "$analysis_json")"
    ordered_postwrite_verification="$(jq -r '.ordered_postwrite_verification' "$analysis_json")"
    duplicate_findings_advisory_items_count="$(jq -r '.duplicate_findings_advisory_items | length' "$analysis_json")"
    postwrite_repeats_prewrite_verdict="$(jq -r '.postwrite_repeats_prewrite_verdict' "$analysis_json")"
    postwrite_forbidden_empty_bucket="$(jq -r '.postwrite_forbidden_empty_bucket' "$analysis_json")"
    postwrite_section_structure_valid="$(jq -r '.postwrite_section_structure_valid' "$analysis_json")"
    unexpected_events_after_final_message="$(jq -r '.unexpected_events_after_final_message' "$analysis_json")"
    preview_section_count="$(jq -r '.preview_section_count' "$analysis_json")"
    preview_has_canonical_keys="$(jq -r '.preview_has_canonical_keys' "$analysis_json")"
    count_only_preview_without_narrative="$(jq -r '.count_only_preview_without_narrative' "$analysis_json")"

    [[ "$write_hits" -ge 1 ]] || {
      status="fail"
      validation_error="missing_first_write"
    }
    [[ "$first_write_idx" -gt 0 ]] || {
      status="fail"
      validation_error="missing_first_write_index"
    }
    [[ "$write_attempts" -eq 1 ]] || {
      status="fail"
      validation_error="multiple_write_attempts_detected"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    onboarding_count="$(count_command_hits_before_index "$parsed_log" "$first_write_attempt_idx" '(^|[[:space:]])ha-nova[[:space:]]+(doctor|ready|quick)([[:space:]]|$)')"
    [[ "$onboarding_count" -eq 0 ]] || {
      status="fail"
      validation_error="forbidden_onboarding_check_detected"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    external_research_hits="$(count_external_research_hits "$parsed_log")"
    shell_network_hits="$(count_shell_network_hits "$parsed_log")"
    [[ "$external_research_hits" -eq 0 && "$shell_network_hits" -eq 0 ]] || {
      status="fail"
      validation_error="unexpected_external_research_detected"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    [[ "$status_line_count" -eq 1 ]] || {
      status="fail"
      validation_error="missing_final_status_line"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    [[ "$final_message_last_line" == "NOVA_WRITE_REVIEW_RESULT id=${scenario_id} automation_id=${automation_id} status=ok" ]] || {
      status="fail"
      validation_error="status_line_not_final"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    [[ "$unexpected_events_after_final_message" -eq 0 ]] || {
      status="fail"
      validation_error="trailing_events_after_final_message"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    [[ "$postwrite_text" == *"Post-Write Review"* ]] || {
      status="fail"
      validation_error="missing_postwrite_review_section"
    }
  fi

  if [[ "$status" == "pass" && "$postwrite_section_structure_valid" != "true" ]]; then
    status="fail"
    validation_error="postwrite_section_structure_invalid"
  fi

  if [[ "$status" == "pass" && "$wrong_reload_after_write" -gt 0 ]]; then
    status="fail"
    validation_error="wrong_reload_path_detected"
  fi

  if [[ "$status" == "pass" ]]; then
    [[ "$readback_after_write" -ge 1 && "$reload_after_write" -ge 1 && "$state_after_write" -ge 1 && "$collision_after_write" -ge 1 ]] || {
      status="fail"
      validation_error="missing_postwrite_verification"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    [[ "$ordered_postwrite_verification" == "true" ]] || {
      status="fail"
      validation_error="postwrite_verification_out_of_order"
    }
  fi

  if [[ "$status" == "pass" ]]; then
    [[ "$preview_section_count" -eq 1 && "$preview_has_canonical_keys" == "true" ]] || {
      status="fail"
      validation_error="missing_prewrite_preview_section"
    }
  fi

  if [[ "$status" == "pass" && "$count_only_preview_without_narrative" == "true" ]]; then
    status="fail"
    validation_error="count_only_preview_without_narrative"
  fi

  if [[ "$status" == "pass" && "$postwrite_repeats_prewrite_verdict" == "true" ]]; then
    status="fail"
    validation_error="prewrite_verdict_repeated_postwrite"
  fi

  if [[ "$status" == "pass" && "$duplicate_findings_advisory_items_count" -gt 0 ]]; then
    status="fail"
    validation_error="duplicate_findings_advisory_item"
  fi

  if [[ "$status" == "pass" && "$postwrite_forbidden_empty_bucket" == "true" ]]; then
    status="fail"
    validation_error="forbidden_empty_bucket_present"
  fi

  if [[ "$status" == "pass" && "$must_contain_json" != "[]" ]]; then
    while IFS= read -r required_text; do
      [[ -z "$required_text" ]] && continue
      if [[ "$prewrite_text" != *"$required_text"* ]]; then
        status="fail"
        validation_error="required_prewrite_text_missing"
        break
      fi
    done < <(jq -r '.[]' <<<"$must_contain_json")
  fi

  if [[ "$status" == "pass" ]]; then
    while IFS= read -r forbidden_text; do
      [[ -z "$forbidden_text" ]] && continue
      if [[ "$prewrite_text" == *"$forbidden_text"* ]]; then
        status="fail"
        validation_error="forbidden_prewrite_text_present"
        break
      fi
    done < <(jq -r '.[]' <<<"$must_not_contain_json")
  fi

  if [[ "$status" == "pass" ]] && contains_rule_code_marker "$prewrite_text"; then
    status="fail"
    validation_error="rule_code_marker_present_prewrite"
  fi

  if [[ "$status" == "pass" && "$must_contain_postwrite_json" != "[]" ]]; then
    while IFS= read -r required_text; do
      [[ -z "$required_text" ]] && continue
      if [[ "$postwrite_text" != *"$required_text"* ]]; then
        status="fail"
        validation_error="required_postwrite_text_missing"
        break
      fi
    done < <(jq -r '.[]' <<<"$must_contain_postwrite_json")
  fi

  if [[ "$status" == "pass" ]]; then
    while IFS= read -r forbidden_text; do
      [[ -z "$forbidden_text" ]] && continue
      if [[ "$postwrite_text" == *"$forbidden_text"* ]]; then
        status="fail"
        validation_error="forbidden_postwrite_text_present"
        break
      fi
    done < <(jq -r '.[]' <<<"$must_not_contain_postwrite_json")
  fi

  if [[ "$status" == "pass" ]] && contains_rule_code_marker "$postwrite_text"; then
    status="fail"
    validation_error="rule_code_marker_present_postwrite"
  fi

  jq -n \
    --arg id "$scenario_id" \
    --arg automation_id "$automation_id" \
    --arg status "$status" \
    --arg error "$validation_error" \
    --argjson duration_sec "$duration_sec" \
    '{
      id: $id,
      automation_id: $automation_id,
      status: $status,
      error: $error,
      duration_sec: $duration_sec
    }' >>"$RESULTS_FILE"

  if [[ "$status" == "pass" ]]; then
    log "PASS ${scenario_id} (${duration_sec}s)"
  else
    log "FAIL ${scenario_id} (${duration_sec}s, error=${validation_error})"
  fi
}

main() {
  require_cmd codex
  require_cmd jq
  require_cmd ha-nova
  validate_scenario_file

  mkdir -p "$LOG_DIR"
  : >"$RESULTS_FILE"

  local scenario_count
  scenario_count="$(jq 'length' "$SCENARIO_FILE")"
  log "Loaded ${scenario_count} write-review scenarios from ${SCENARIO_FILE}"

  for ((idx = 0; idx < scenario_count; idx += 1)); do
    run_scenario \
      "$idx" \
      "$(jq -r ".[$idx].id" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].prompt" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].must_contain_prewrite_text // []" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].must_not_contain_prewrite_text // []" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].must_contain_postwrite_text // []" "$SCENARIO_FILE")" \
      "$(jq -c ".[$idx].must_not_contain_postwrite_text // []" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].max_duration_sec // 120" "$SCENARIO_FILE")" \
      "$(jq -r ".[$idx].collision_item_id // \"\"" "$SCENARIO_FILE")"
  done

  jq -s --arg run_id "$RUN_ID" --arg scenario_file "$SCENARIO_FILE" '
    {
      run_id: $run_id,
      scenario_file: $scenario_file,
      scenarios: .
    }
  ' "$RESULTS_FILE" >"$SUMMARY_FILE"

  if jq -e 'all(.scenarios[]; .status == "pass")' "$SUMMARY_FILE" >/dev/null; then
    log "Write-review scenario suite passed"
    jq -r '.scenarios[] | "PASS \(.id) (\(.duration_sec)s)"' "$SUMMARY_FILE"
    return 0
  fi

  log "Write-review scenario suite failed"
  jq -r '.scenarios[] | select(.status == "fail") | "FAIL \(.id): \(.error)"' "$SUMMARY_FILE" >&2
  return 1
}

main "$@"
