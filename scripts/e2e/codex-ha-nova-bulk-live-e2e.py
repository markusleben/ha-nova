#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import platform
import queue
import re
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import traceback
import time
from collections import defaultdict
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
READ_SKILL_FILE = ROOT / "skills" / "read" / "SKILL.md"
REVIEW_SKILL_FILE = ROOT / "skills" / "review" / "SKILL.md"
BULK_PATTERNS_FILE = ROOT / "skills" / "ha-nova" / "bulk-patterns.md"
OUTPUT_DIR = Path(os.environ.get("OUTPUT_DIR", tempfile.mkdtemp(prefix="ha-nova-codex-bulk-live.")))
RUN_ID = datetime.now().strftime("%Y%m%d-%H%M%S")
LOG_DIR = OUTPUT_DIR / f"logs-{RUN_ID}"
FIXTURES_FILE = OUTPUT_DIR / f"fixtures-{RUN_ID}.json"
RESULTS_FILE = OUTPUT_DIR / f"results-{RUN_ID}.ndjson"
SUMMARY_FILE = OUTPUT_DIR / f"summary-{RUN_ID}.json"
SCENARIO_TIMEOUT_SEC = int(os.environ.get("BULK_E2E_SCENARIO_TIMEOUT_SEC", "360"))
CODEX_BINARY = "codex"
REVIEW_SECTIONS = [
    "Scope",
    "Summary",
    "High-Risk Findings",
    "Repeated Patterns",
    "Items Checked",
    "Collisions by Cluster",
]
SCENARIO_ORDER = (
    "prefix_inventory",
    "area_inventory",
    "label_inventory",
    "area_review",
)


@dataclass
class ScenarioResult:
    fixture_key: str
    mode: str
    status: str
    errors: list[str]
    codex_exit: int
    raw_log: str


def log(message: str) -> None:
    print(f"[codex-bulk-live-e2e] {message}", flush=True)


def die(message: str) -> None:
    raise SystemExit(f"[codex-bulk-live-e2e] {message}")


def normalize_heading(text: str) -> str:
    return re.sub(r"\s+", " ", text.strip().casefold())


def relay_jq_invocations(command: str) -> list[str]:
    return re.findall(r"ha-nova relay jq[^\n]*", command)


def require_cmd(command: str) -> None:
    if shutil.which(command) is None:
        die(f"Required command not found: {command}")


def resolve_codex_binary() -> str:
    wrapper = shutil.which("codex")
    if wrapper is None:
        die("Required command not found: codex")

    wrapper_path = Path(os.path.realpath(wrapper))
    candidate_patterns = []
    system = platform.system().lower()
    machine = platform.machine().lower()
    binary_name = "codex.exe" if system == "windows" else "codex"

    if system == "darwin" and machine in {"arm64", "aarch64"}:
        candidate_patterns.append("node_modules/@openai/codex-darwin-arm64/vendor/*/codex/codex")
    elif system == "darwin" and machine in {"x86_64", "amd64"}:
        candidate_patterns.append("node_modules/@openai/codex-darwin-x64/vendor/*/codex/codex")
    elif system == "linux" and machine in {"arm64", "aarch64"}:
        candidate_patterns.append("node_modules/@openai/codex-linux-arm64/vendor/*/codex/codex")
    elif system == "linux" and machine in {"x86_64", "amd64"}:
        candidate_patterns.append("node_modules/@openai/codex-linux-x64/vendor/*/codex/codex")
    elif system == "windows" and machine in {"arm64", "aarch64"}:
        candidate_patterns.append("node_modules/@openai/codex-win32-arm64/vendor/*/codex/codex.exe")
    elif system == "windows" and machine in {"x86_64", "amd64"}:
        candidate_patterns.append("node_modules/@openai/codex-win32-x64/vendor/*/codex/codex.exe")

    search_roots = [wrapper_path.parent.parent]
    if wrapper_path.suffix == ".js":
        search_roots.append(wrapper_path.parent.parent.parent)

    for root in search_roots:
        for pattern in candidate_patterns:
            matches = sorted(root.glob(pattern))
            if matches:
                return str(matches[0])
        matches = sorted(root.glob(f"node_modules/@openai/codex-*/vendor/*/codex/{binary_name}"))
        if matches:
            return str(matches[0])

    return wrapper


def relay_ws(payload: dict) -> dict:
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
        json.dump(payload, handle)
        payload_path = handle.name
    try:
        raw = subprocess.check_output(
            ["ha-nova", "relay", "ws", "--data-file", payload_path],
            text=True,
            cwd=ROOT,
        )
        return json.loads(raw)
    finally:
        Path(payload_path).unlink(missing_ok=True)


def parse_requested_scenarios(argv: list[str]) -> list[str]:
    if not argv:
        return list(SCENARIO_ORDER)

    requested: list[str] = []
    seen: set[str] = set()
    invalid = [scenario for scenario in argv if scenario not in SCENARIO_ORDER]
    if invalid:
        die(
            "Unknown scenario(s): "
            + ", ".join(invalid)
            + ". Valid scenarios: "
            + ", ".join(SCENARIO_ORDER)
        )

    for scenario in argv:
        if scenario in seen:
            continue
        seen.add(scenario)
        requested.append(scenario)
    return requested


def discover_prefix_fixture(compact: list[dict]) -> dict:
    prefix_value = "growbox"
    prefix_matches = sorted(entity["ei"] for entity in compact if entity["ei"].startswith("automation.growbox"))
    if len(prefix_matches) < 6:
        prefix_candidates: dict[str, list[str]] = defaultdict(list)
        for entity in compact:
            entity_id = entity.get("ei")
            if not isinstance(entity_id, str) or not entity_id.startswith("automation."):
                continue
            suffix = entity_id.split(".", 1)[1].lower()
            prefix = suffix.split("_", 1)[0]
            if not re.match(r"^[a-z][a-z0-9-]{3,}$", prefix):
                continue
            if prefix == "automation":
                continue
            prefix_candidates[prefix].append(entity_id)
        ranked_prefixes = [
            {"selector": selector, "matches": sorted(set(matches))}
            for selector, matches in prefix_candidates.items()
            if len(matches) >= 6
        ]
        ranked_prefixes.sort(key=lambda item: (-len(item["matches"]), item["selector"]))
        if not ranked_prefixes:
            die("Need at least one automation prefix fixture with 6+ matches")
        prefix_value = ranked_prefixes[0]["selector"]
        prefix_matches = ranked_prefixes[0]["matches"]

    return {
        "id": "inventory_prefix",
        "selector": prefix_value,
        "matches": prefix_matches,
        "displayed": prefix_matches[:20],
    }


def discover_label_fixture(registry: list[dict], labels: list[dict]) -> dict:
    label_names = {item["label_id"]: item["name"] for item in labels}
    automation_labels: dict[str, list[str]] = defaultdict(list)
    for item in registry:
        entity_id = item.get("entity_id")
        if isinstance(entity_id, str) and entity_id.startswith("automation."):
            for label_id in item.get("labels") or []:
                automation_labels[label_id].append(entity_id)
    label_candidates = [
        {
            "label_id": label_id,
            "label_name": label_names.get(label_id, label_id),
            "matches": sorted(set(matches)),
        }
        for label_id, matches in automation_labels.items()
        if len(matches) >= 3
    ]
    if not label_candidates:
        die("Need at least one label fixture with 3+ automation matches")
    label_candidates.sort(key=lambda item: (-len(item["matches"]), item["label_id"]))
    label_fixture = label_candidates[0]

    return {
        "id": "inventory_label",
        "label_id": label_fixture["label_id"],
        "label_name": label_fixture["label_name"],
        "matches": label_fixture["matches"],
        "displayed": label_fixture["matches"][:20],
    }


def discover_area_fixture(areas: list[dict]) -> dict:
    area_candidates = []
    for area in areas:
        related = relay_ws({"type": "search/related", "item_type": "area", "item_id": area["area_id"]})["data"]
        matches = sorted(related.get("automation") or [])
        if len(matches) >= 6:
            area_candidates.append(
                {
                    "area_id": area["area_id"],
                    "area_name": area["name"],
                    "matches": matches,
                    "audited": matches[:5],
                    "remaining": len(matches) - 5,
                }
            )
    if not area_candidates:
        die("Need at least one area fixture with 6+ automation matches")
    area_candidates.sort(key=lambda item: (len(item["matches"]), item["area_id"]))
    return area_candidates[0]


def discover_fixtures(requested_scenarios: list[str]) -> dict:
    requested = set(requested_scenarios)
    fixtures: dict[str, dict] = {}

    if "prefix_inventory" in requested:
        compact = relay_ws({"type": "config/entity_registry/list_for_display"})["data"]["entities"]
        fixtures["prefix_inventory"] = discover_prefix_fixture(compact)

    if "label_inventory" in requested:
        registry = relay_ws({"type": "config/entity_registry/list"})["data"]
        labels = relay_ws({"type": "config/label_registry/list"})["data"]
        fixtures["label_inventory"] = discover_label_fixture(registry, labels)

    if {"area_inventory", "area_review"} & requested:
        areas = relay_ws({"type": "config/area_registry/list"})["data"]
        area_fixture = discover_area_fixture(areas)
        if "area_inventory" in requested:
            fixtures["area_inventory"] = {
                "id": "inventory_area",
                "area_id": area_fixture["area_id"],
                "area_name": area_fixture["area_name"],
                "matches": area_fixture["matches"],
                "displayed": area_fixture["matches"][:20],
            }
        if "area_review" in requested:
            fixtures["area_review"] = {
                "id": "review_area",
                "area_id": area_fixture["area_id"],
                "area_name": area_fixture["area_name"],
                "matches": area_fixture["matches"],
                "audited": area_fixture["audited"],
                "remaining": area_fixture["remaining"],
                "non_audited": area_fixture["matches"][5:],
            }

    return fixtures


def build_inventory_prompt(scenario_id: str, request: str) -> str:
    return f"""Use the repo-local read skill for this task.

User request:
{request}

Hard requirements:
1. Work in English only.
2. Use App + Relay terminology.
3. Keep the normal user-facing answer concise.
4. Read only these repo-local files for skill guidance:
   - `{READ_SKILL_FILE}`
   - `{BULK_PATTERNS_FILE}` if needed
   Do not scan broad repo docs or `PROJECT.md`.
5. Do not open installed skill copies under `~/.local/share/ha-nova`, `~/.agents/skills`, or other client cache roots when the repo-local path above exists.
6. Use the current local HA NOVA setup directly. Do not run health, doctor, or readiness preflights unless the first Relay action fails.
7. This is inventory-only. Do not read full config/YAML for any target. Do not read target states. Do not review.
8. Do not modify repository files.
9. If you use `ha-nova relay jq`, pass the jq filter as the final positional argument or use `--jq-file`. Never invent a `--jq` flag for `ha-nova relay jq`.
10. On zsh, do not assign to a variable named `status`; use `rc` instead.
11. Never call external `jq`; use only relay-native `--jq` / `--jq-file` or `ha-nova relay jq`.
12. Any explanatory example or snippet you show must stay cross-OS. Do not rely on macOS-only, Linux-only, or Windows-only path assumptions in user-facing text.
13. Create transient selector files inside exactly one temp directory and reuse fixed file names such as `payload.json`, `filter.jq`, and `result.json`.
14. Write transient JSON and jq files with the native file-writing flow for the current shell. POSIX shells may use literal heredocs like `cat <<'EOF' > "$payload_file"`; PowerShell or Windows shells must use their native equivalent. Do not assemble payloads or filters with `printf` fragments or shell-escaped string concatenation.
15. Do not create payload or jq template files with placeholder tokens such as `REPLACE_ENTITY_ID`, `REPLACE_AREA_ID`, or `REPLACE_LABEL_ID`. Write the final JSON or jq contents directly in one step.
16. Do not mutate payload or jq files afterward with `perl -0pi`, `sed -i`, or similar replacement commands. If a value is dynamic, generate the final file contents directly for that request.
17. Treat every `<...>` token from skill docs as documentation only. Replace it manually with a concrete quoted path before execution. Never execute a command that still contains angle-bracket placeholders, and never materialize those placeholders with `sed`, `perl`, `envsubst`, `eval`, `source`, or command-substitution pipelines.
18. If the first Relay call fails, do not probe the CLI. Do not run `--help`, `-h`, `help`, `version`, `man`, alternate-flag experiments, or trial commands. Re-read the already-loaded repo-local skill text, inspect the exact failed command and files, then make at most one corrected retry using documented syntax only.
19. Once a temp file has a role, keep that role immutable for the rest of the run. Do not overwrite selector results, wrapper files, payload files, or jq files with unrelated content later; create a new dedicated filename instead.
20. Prefer making the first Relay selector call emit the final wrapped inventory object directly. Do not add extra `ha-nova relay jq` passes just to recompute `matched`, `displayed`, `remaining`, or `values` when the initial jq filter can emit that object.
21. For JSON extraction from Relay result files, use `ha-nova relay jq --file` only. Do not use Node, Python, or ad-hoc parsers.
22. Do not use `-c` or `--arg` with `ha-nova relay jq`. Keep JSON arrays and objects in the default output form; use `-r` only when you need an unquoted scalar, and embed dynamic literals by writing the jq program itself with a literal heredoc.
23. If the user request includes a live-resolved selector hint such as `area_id=<id>` or `label_id=<id>`, treat that hint as authoritative for the Relay call instead of reparsing the same scalar through extra shell logic.
24. Do not run `--help`, bare `ha-nova relay jq`, or bare `ha-nova relay ws` to probe CLI usage. The repo-local skill contract is authoritative.
25. After the first Relay selector call saves the wrapped inventory object, inspect only wrapper fields such as `.matched`, `.displayed`, `.remaining`, `.values`, and `.rows`. Do not reapply the original shortlist jq file to the wrapped result file.
26. For wrapper-field extraction, prefer separate simple field selectors or read the whole wrapper object once. If you need a follow-up jq file, use a dedicated wrapper-inspection file such as `rows.jq`, not the original `filter.jq`. Do not build multi-field count strings with precedence-sensitive chained jq expressions.
27. For area inventory, the canonical `search/related` automation-id shortlist is enough for the machine-readable status line; name enrichment is optional.
28. Do not glob for temp files, probe alternate temp names, or run extra shell-debug checks after a successful Relay selector call. Reuse the exact file path you created.
29. Reuse the exact jq idioms from `{BULK_PATTERNS_FILE}` for `prefix`, `area`, `label`, and inventory summary wrappers. Do not invent regex-heavy replacements when the shared doc already gives a simpler filter.
30. End the final answer with exactly one machine-readable status line:
   NOVA_BULK_INVENTORY_RESULT id={scenario_id} matched=<int> displayed=<int> remaining=<int> values=<json_array_of_displayed_entity_ids>
"""


def build_review_prompt(scenario_id: str, request: str) -> str:
    return f"""Use the repo-local review skill for this task.

User request:
{request}

Hard requirements:
1. Work in English only.
2. Use App + Relay terminology.
3. Keep the normal user-facing aggregate review answer.
4. Read only these repo-local files for skill guidance:
   - `{REVIEW_SKILL_FILE}`
   - `{BULK_PATTERNS_FILE}` if needed
   Do not scan broad repo docs or `PROJECT.md`.
5. Do not open installed skill copies under `~/.local/share/ha-nova`, `~/.agents/skills`, or other client cache roots when the repo-local path above exists.
6. Use the current local HA NOVA setup directly. Do not run health, doctor, or readiness preflights unless the first Relay action fails.
7. This is standalone bulk review. Audit exactly one workset only. If more than 5 match, review only the first deterministic 5 and leave the rest untouched.
8. Bulk review stays read-only: do not offer Quick-Fix, do not call services, and do not read target states for Quick-Fix detection.
9. Render the normal aggregate bulk-review body with these exact section titles:
   Scope
   Summary
   High-Risk Findings
   Repeated Patterns
   Items Checked
   Collisions by Cluster
10. Do not modify repository files.
11. If you use `ha-nova relay jq`, pass the jq filter as the final positional argument or use `--jq-file`. Never invent a `--jq` flag for `ha-nova relay jq`.
12. On zsh, do not assign to a variable named `status`; use `rc` instead.
13. Never call external `jq`; use only relay-native `--jq` / `--jq-file` or `ha-nova relay jq`.
14. Any explanatory example or snippet you show must stay cross-OS. Do not rely on macOS-only, Linux-only, or Windows-only path assumptions in user-facing text.
15. Create transient selector files inside exactly one temp directory and reuse fixed file names such as `payload.json`, `filter.jq`, and `result.json`.
16. Write transient JSON and jq files with the native file-writing flow for the current shell. POSIX shells may use literal heredocs like `cat <<'EOF' > "$payload_file"`; PowerShell or Windows shells must use their native equivalent. Do not assemble payloads or filters with `printf` fragments or shell-escaped string concatenation.
17. Do not create payload or jq template files with placeholder tokens such as `REPLACE_ENTITY_ID`, `REPLACE_AREA_ID`, or `REPLACE_LABEL_ID`. Write the final JSON or jq contents directly in one step.
18. Do not mutate payload or jq files afterward with `perl -0pi`, `sed -i`, or similar replacement commands. If a value is dynamic, generate the final file contents directly for that request.
19. Treat every `<...>` token from skill docs as documentation only. Replace it manually with a concrete quoted path before execution. Never execute a command that still contains angle-bracket placeholders, and never materialize those placeholders with `sed`, `perl`, `envsubst`, `eval`, `source`, or command-substitution pipelines.
20. If the first Relay call fails, do not probe the CLI. Do not run `--help`, `-h`, `help`, `version`, `man`, alternate-flag experiments, or trial commands. Re-read the already-loaded repo-local skill text, inspect the exact failed command and files, then make at most one corrected retry using documented syntax only.
21. Once a temp file has a role, keep that role immutable for the rest of the run. Do not overwrite shortlists, worksets, payload files, jq files, or saved results with unrelated content later; create a new dedicated filename instead.
22. For JSON extraction from Relay result files, use `ha-nova relay jq --file` only. Do not use Node, Python, or ad-hoc parsers.
23. Do not use `-c` or `--arg` with `ha-nova relay jq`. Keep JSON arrays and objects in the default output form; use `-r` only when you need an unquoted scalar, and embed dynamic literals by writing the jq program itself with a literal heredoc.
24. For config-body reads, prefer copying the canonical jq file `{ROOT / "skills" / "ha-nova" / "config-body-filter.jq"}` into your temp directory and using that copied file directly as `"$config_filter_file"`. Do not recreate the jq program from shell text unless that file copy fails.
25. The canonical jq file body is exactly:
   if .ok then .data.body else error("relay error: \\(.error.message // "unknown")") end
26. If you must recreate the jq file, print its first line once with a shell-native inspection command before the first config read. On POSIX, `sed -n '1p' "$config_filter_file"` is acceptable; on PowerShell or Windows shells use the native equivalent. Do not compare that line against a shell-escaped string, do not store the jq program in a shell variable, and do not wrap it in a shell-specific string-comparison guard.
27. If the printed line is not the exact canonical jq expression, overwrite the same file with the canonical contents before continuing. Do not create alternate filenames such as `config-body-filter.jq`.
28. For automation `unique_id` resolution, use the safe two-step skill contract: `ha-nova relay ws --data-file <payload-file> --out <registry-file>` and then `ha-nova relay jq -r --file <registry-file> '.data.unique_id'`. Do not create a separate jq file for `.data.unique_id`, do not rely on quoted JSON-string output, and do not strip quotes with shell substitutions.
29. After the area-shortlist jq `(.data.automation // []) | sort`, the saved result file is a plain JSON array of automation `entity_id` strings. Keep that array shape for workset trimming and counts unless you intentionally map the strings into row objects first.
30. After you save the shortlist or workset, keep that file immutable. Use dedicated filenames for later `config/entity_registry/get`, config-body, and collision-read outputs instead of reusing `result.json` for unrelated payloads.
31. If the user request includes a live-resolved selector hint such as `area_id=<id>` or `label_id=<id>`, treat that hint as authoritative for the Relay call instead of reparsing the same scalar through extra shell logic.
32. Do not run `--help`, bare `ha-nova relay jq`, or bare `ha-nova relay ws` to probe CLI usage. The repo-local skill contract is authoritative. If you need an unquoted scalar from a Relay result file, use `ha-nova relay jq -r --file <result-file> '<filter>'`.
33. When a Relay call already emits the exact workset or summary shape you need, reuse that output directly instead of adding extra relay-jq reshaping passes with mismatched assumptions about the file shape.
34. Do not store JSON arrays in shell variables for later command generation. Persist candidate arrays or entity-id lines to files, then iterate from those files.
35. When you prepare a per-entity `config/entity_registry/get` payload, write the final payload contents in the same command block that executes that lookup so the concrete `entity_id` stays visible in the transcript. Do not hide different target lookups behind one opaque reused payload file.
36. Every config-body read must emit a parseable target marker in the transcript output, such as `ENTITY=<entity_id>`, `ITEM[n]=<entity_id>`, `1|<entity_id>|...`, or `=== <entity_id> ===`, so each read stays attributable.
37. Keep Relay operations serial whenever the same temp directory is in play. Do not start parallel command executions that rewrite a shared payload or jq file. If you need multiple collision probes, either run them one at a time or give each probe its own dedicated payload filename before launching it.
38. Do not glob for temp files, probe alternate temp names, or run extra shell-debug checks after a successful Relay call. Reuse the exact file path you created.
39. Reuse the exact jq idioms from `{BULK_PATTERNS_FILE}` for `prefix`, `area`, `label`, and inventory summary wrappers. Do not invent regex-heavy replacements when the shared doc already gives a simpler filter.
40. Keep the run read-only even if one item looks acutely wrong.
41. Keep the aggregate explanation concise. Once you have enough evidence for the six required sections and the status line, finish the response instead of expanding the narrative.
42. End the final answer with exactly one machine-readable status line:
   NOVA_BULK_REVIEW_RESULT id={scenario_id} matched=<int> audited=<int> remaining=<int> item_ids=<json_array_of_audited_entity_ids> quick_fix_offered=<true|false> sections=<json_array_of_exact_section_titles>
"""


def final_status_marker(mode: str, scenario_id: str) -> str:
    if mode == "inventory":
        return f"NOVA_BULK_INVENTORY_RESULT id={scenario_id} "
    return f"NOVA_BULK_REVIEW_RESULT id={scenario_id} "


def stop_process_group(process: subprocess.Popen[str], sig: signal.Signals) -> None:
    if process.poll() is not None:
        return
    if os.name == "nt":
        try:
            if sig == signal.SIGTERM and hasattr(signal, "CTRL_BREAK_EVENT"):
                process.send_signal(signal.CTRL_BREAK_EVENT)
            elif sig == signal.SIGKILL:
                process.kill()
            else:
                process.terminate()
        except ProcessLookupError:
            return
        return

    try:
        os.killpg(process.pid, sig)
    except ProcessLookupError:
        return


def run_codex(prompt: str, raw_log: Path, marker: str) -> int:
    with raw_log.open("w", encoding="utf-8") as handle:
        popen_kwargs: dict[str, object] = {
            "cwd": ROOT,
            "text": True,
            "stdout": subprocess.PIPE,
            "stderr": subprocess.STDOUT,
            "bufsize": 1,
        }
        if os.name == "nt":
            popen_kwargs["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
        else:
            popen_kwargs["start_new_session"] = True

        process = subprocess.Popen(
            [CODEX_BINARY, "exec", "--ephemeral", "--json", "--sandbox", "danger-full-access", "-C", str(ROOT), prompt],
            **popen_kwargs,
        )
        assert process.stdout is not None
        lines: queue.Queue[str | None] = queue.Queue()
        reader_done = threading.Event()

        def drain_stdout() -> None:
            assert process.stdout is not None
            for line in process.stdout:
                lines.put(line)
            lines.put(None)
            reader_done.set()

        reader = threading.Thread(target=drain_stdout, daemon=True)
        reader.start()

        try:
            deadline = time.monotonic() + SCENARIO_TIMEOUT_SEC
            saw_eof = False
            while True:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    log(f"Case timed out after {SCENARIO_TIMEOUT_SEC}s; terminating Codex and grading the partial transcript")
                    stop_process_group(process, signal.SIGTERM)
                    try:
                        return process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        stop_process_group(process, signal.SIGKILL)
                        return process.wait(timeout=5)

                try:
                    line = lines.get(timeout=min(0.25, remaining))
                except queue.Empty:
                    if process.poll() is not None:
                        if reader_done.wait(timeout=min(0.25, max(remaining, 0))):
                            continue
                    continue

                if line is None:
                    saw_eof = True
                    if process.poll() is not None:
                        return process.returncode
                    continue

                handle.write(line)
                handle.flush()

                try:
                    payload = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if not isinstance(payload, dict):
                    continue
                if payload.get("type") != "item.completed":
                    continue
                item = payload.get("item", {})
                if item.get("type") != "agent_message":
                    continue
                if marker not in item.get("text", ""):
                    continue

                post_marker_deadline = min(deadline, time.monotonic() + 20)
                while time.monotonic() < post_marker_deadline:
                    try:
                        trailing = lines.get(timeout=0.25)
                    except queue.Empty:
                        if process.poll() is not None and reader_done.is_set():
                            return process.returncode
                        continue
                    if trailing is None:
                        saw_eof = True
                        if process.poll() is not None:
                            return process.returncode
                        continue
                    handle.write(trailing)
                    handle.flush()
                    post_marker_deadline = min(deadline, time.monotonic() + 20)

                if process.poll() is None:
                    stop_process_group(process, signal.SIGTERM)
                    try:
                        return process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        stop_process_group(process, signal.SIGKILL)
                        return process.wait(timeout=5)
                return process.returncode
        finally:
            if process.poll() is not None or saw_eof:
                reader.join(timeout=1)


def load_events(raw_log: Path) -> list[dict]:
    events: list[dict] = []
    for line in raw_log.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            payload = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(payload, dict):
            events.append(payload)
    return events


def started_and_completed_command_items(events: list[dict]) -> tuple[list[dict], list[dict]]:
    started = [
        event["item"]
        for event in events
        if event.get("type") == "item.started" and event.get("item", {}).get("type") == "command_execution"
    ]
    completed = [
        event["item"]
        for event in events
        if event.get("type") == "item.completed" and event.get("item", {}).get("type") == "command_execution"
    ]
    return started, completed


def structural_errors(events: list[dict], raw_text: str) -> list[str]:
    errors: list[str] = []
    started_commands, completed_commands = started_and_completed_command_items(events)
    static_check_items: dict[str, dict] = {}
    for index, item in enumerate(started_commands + completed_commands):
        item_id = str(item.get("id") or f"command-{index}")
        static_check_items[item_id] = item
    completed_ids = {item.get("id") for item in completed_commands}
    incomplete_command_ids = [
        item.get("id")
        for item in started_commands
        if item.get("id") not in completed_ids
    ]
    if incomplete_command_ids:
        errors.append("incomplete_transcript")

    retryable_read_failure_consumed = False
    for index, item in enumerate(completed_commands):
        exit_code = item.get("exit_code")
        if isinstance(exit_code, int) and exit_code != 0:
            command = item.get("command", "")
            output = item.get("aggregated_output", "")
            later_successful_relay_read = any(
                later_item.get("exit_code") == 0
                and re.search(r"\bha-nova relay (?:ws|core|jq)\b", later_item.get("command", "")) is not None
                for later_item in completed_commands[index + 1 :]
            )
            retryable_relay_read_failure = (
                not retryable_read_failure_consumed
                and re.search(r"\bha-nova relay (?:ws|core)\b", command) is not None
                and re.search(r"\b(?:call_service|/create|/update|/delete|/remove|/save|/edit|/set|/reload|/execute|/install|/uninstall|/start|/stop|/restart|/turn_on|/turn_off|/toggle)\b", command) is None
                and re.search(r"(?i)(?:context deadline exceeded|client\.timeout|timed out|timeout while reading body|relay error)", output) is not None
                and later_successful_relay_read
            )
            if retryable_relay_read_failure:
                retryable_read_failure_consumed = True
            else:
                errors.append(f"failed_command_exit:{exit_code}:{command[:120]}")
    for item in static_check_items.values():
        command = item.get("command", "")
        standalone_jq_count = len(re.findall(r"(?<![\w./-])jq(?=\s|$)", command))
        relay_jq_count = len(re.findall(r"\bha-nova relay jq(?=\s|$)", command))
        if standalone_jq_count > relay_jq_count:
            errors.append("external_jq_usage_detected")
        if re.search(r'\bprintf\b[^\n>]*>\s*.*(?:payload_file|filter_file)', command):
            errors.append("fragmented_tempfile_assembly_detected")
        if re.search(r"(?<!<)<[A-Za-z][A-Za-z0-9_./:-]*>", command):
            errors.append("unresolved_command_placeholder_detected")
        if re.search(r"\bREPLACE_[A-Z0-9_]+\b", command):
            errors.append("placeholder_payload_template_detected")
        if re.search(r"\bperl\s+-0pi\b", command) or re.search(r"\bsed\s+-i(?:''|)?\b", command):
            errors.append("in_place_tempfile_rewrite_detected")
        if re.search(r"\b(?:envsubst|eval|source)\b", command):
            errors.append("template_expansion_command_detected")
        if "ha-nova relay ws" in command and re.search(
            r'"type"\s*:\s*"[^"\n]*(?:call_service|/create|/update|/delete|/remove|/save|/edit|/set|/reload|/execute|/install|/uninstall|/start|/stop|/restart|/turn_on|/turn_off|/toggle)\b',
            command,
        ):
            errors.append("mutation_ws_type_detected")
        if re.search(r"ha-nova relay core[^\n]*--method\s+(POST|PUT|PATCH|DELETE)\b", command):
            errors.append("mutation_core_method_detected")
        if re.search(r"ha-nova relay (?:(?:jq|ws|core)(?:\s+(?:--help|-h|help|version))?|(?:--help|-h|help|version))['\"]?$", command.strip()):
            errors.append("cli_help_probe_detected")

    if "unknown flag: --jq" in raw_text or "unknown flag: -c" in raw_text or "unknown flag: --arg" in raw_text:
        errors.append("invalid_relay_jq_flag")
    if '"code":"INVALID_JSON"' in raw_text or "Request body is not valid JSON" in raw_text:
        errors.append("relay_invalid_json_response")
    if any(
        re.search(r"(^|\s)-c(\s|$)", invocation) is not None
        or re.search(r"(^|\s)--arg(\s|$)", invocation) is not None
        for item in completed_commands
        for invocation in relay_jq_invocations(item.get("command", ""))
    ):
        errors.append("invalid_relay_jq_flag")
    if "[ha-nova] ERROR: jq parse error:" in raw_text:
        errors.append("relay_jq_parse_error")

    return errors


def require(condition: bool, error: str, errors: list[str]) -> None:
    if not condition:
        errors.append(error)


def extract_config_read_ids(output: str) -> list[str]:
    config_read_ids: list[str] = []
    config_read_ids.extend(
        match.group(1).strip()
        for match in re.finditer(
            r"(?m)^(?:\s*\d+\s*(?:\||\t)\s*)?((?:automation|script)\.[^|\t\n]+)\s*(?:\||\t)\s*[^\s|\t\n]+(?:\s*(?:\||\t)\s*[^\t\n]*\.json)?$",
            output,
        )
    )
    config_read_ids.extend(
        match.group(1).strip()
        for match in re.finditer(
            r"(?m)^===\s*(?:\d+\s+)?((?:automation|script)\.[^=\n]+?)\s*===\s*$",
            output,
        )
    )
    config_read_ids.extend(
        match.group(1).strip()
        for match in re.finditer(
            r"(?m)^\s*((?:automation|script)\.[^\n]+)\nFILE\s+[^\n]*config-\d+\.json$",
            output,
        )
    )
    config_read_ids.extend(
        match.group(1).strip()
        for match in re.finditer(
            r"(?m)^ENTITY=((?:automation|script)\.[^\n]+)$",
            output,
        )
    )
    config_read_ids.extend(
        match.group(1).strip()
        for match in re.finditer(
            r"(?m)^ITEM\[\d+\]=((?:automation|script)\.[^\n]+)$",
            output,
        )
    )
    return config_read_ids


def is_related_collision_evidence(item: dict) -> bool:
    command = item.get("command", "")
    item_id = item.get("id", "")
    aggregated_output = item.get("aggregated_output", "")

    command_or_id_patterns = (
        r"(?i)(?:^|[-_\s])(?:related|collision)(?:$|[-_\s])",
        r"(?i)\b(?:related|collision)[-_](?:config|target|read|cluster)\b",
        r"(?i)\b(?:config|target|read|cluster)[-_](?:related|collision)\b",
    )
    if any(
        re.search(pattern, text) is not None
        for pattern in command_or_id_patterns
        for text in (command, item_id)
        if isinstance(text, str)
    ):
        return True

    if not isinstance(aggregated_output, str):
        return False
    return re.search(
        r"(?mi)^(?:RELATED_COLLISION_EVIDENCE|COLLISION_EVIDENCE|RELATED_EVIDENCE)=.+$",
        aggregated_output,
    ) is not None


def validate_inventory(events: list[dict], fixture: dict, selector_pattern: str) -> list[str]:
    errors: list[str] = []
    messages = [event["item"].get("text", "") for event in events if event.get("type") == "item.completed" and event.get("item", {}).get("type") == "agent_message"]
    started_commands, completed_commands = started_and_completed_command_items(events)
    commands = [item.get("command", "") for item in started_commands + completed_commands]
    joined_commands = "\n".join(commands)
    last_message = messages[-1] if messages else ""

    require(bool(events), "invalid_jsonl_transcript", errors)
    require(bool(messages), "missing_agent_message", errors)
    require(re.search(selector_pattern, joined_commands) is not None, "selector_resolution_missing", errors)

    match = re.search(
        r"NOVA_BULK_INVENTORY_RESULT id="
        + re.escape(fixture["id"])
        + r" matched=(\d+) displayed=(\d+) remaining=(\d+) values=(\[[^\n]*\])",
        last_message,
    )
    require(match is not None, "missing_inventory_status_line", errors)
    if match:
        matched = int(match.group(1))
        displayed = int(match.group(2))
        remaining = int(match.group(3))
        values = json.loads(match.group(4))
        require(matched == len(fixture["matches"]), "inventory_matched_count_mismatch", errors)
        require(displayed == len(fixture["displayed"]), "inventory_displayed_count_mismatch", errors)
        require(remaining == len(fixture["matches"]) - len(fixture["displayed"]), "inventory_remaining_count_mismatch", errors)
        require(values == fixture["displayed"], "inventory_values_mismatch", errors)
    require(
        re.search(r"ha-nova relay jq[^\n]*--jq-file[^\n]*(?:filter\.jq|\$filter_file\b)", joined_commands) is None,
        "inventory_reapplied_selector_filter",
        errors,
    )

    for pattern in (
        r"/api/config/automation/config/",
        r"/api/config/script/config/",
        r"/api/states/",
        r"/api/services/",
        r"config/entity_registry/get",
        r"\.data\.unique_id\b",
    ):
        require(re.search(pattern, joined_commands) is None, f"forbidden_command:{pattern}", errors)
    return errors


def validate_review(events: list[dict], fixture: dict, raw_text: str) -> list[str]:
    errors: list[str] = []
    messages = [event["item"].get("text", "") for event in events if event.get("type") == "item.completed" and event.get("item", {}).get("type") == "agent_message"]
    started_commands, completed_commands = started_and_completed_command_items(events)
    commands = [item.get("command", "") for item in started_commands + completed_commands]
    joined_commands = "\n".join(commands)
    last_message = messages[-1] if messages else ""

    require(bool(events), "invalid_jsonl_transcript", errors)
    require(bool(messages), "missing_agent_message", errors)
    require(
        re.search(r"search/related", joined_commands) is not None
        and re.search(r"item_type\"\s*:\s*\"area\"|item_type:\s*area", joined_commands) is not None,
        "area_related_lookup_missing",
        errors,
    )

    match = re.search(
        r"NOVA_BULK_REVIEW_RESULT id="
        + re.escape(fixture["id"])
        + r" matched=(\d+) audited=(\d+) remaining=(\d+) item_ids=(\[[^\n]+\]) quick_fix_offered=(true|false) sections=(\[[^\n]+\])",
        last_message,
    )
    body_text = last_message[:match.start()].rstrip() if match else last_message
    require(match is not None, "missing_review_status_line", errors)
    if match:
        matched = int(match.group(1))
        audited = int(match.group(2))
        remaining = int(match.group(3))
        item_ids = json.loads(match.group(4))
        quick_fix_offered = match.group(5) == "true"
        sections = json.loads(match.group(6))
        require(matched == len(fixture["matches"]), "review_matched_count_mismatch", errors)
        require(audited == len(fixture["audited"]), "review_audited_count_mismatch", errors)
        require(remaining == fixture["remaining"], "review_remaining_count_mismatch", errors)
        require(item_ids == fixture["audited"], "review_item_ids_mismatch", errors)
        require(quick_fix_offered is False, "review_quick_fix_flag_mismatch", errors)
        require(len(sections) == len(REVIEW_SECTIONS), "review_sections_length_mismatch", errors)
        require(sections == REVIEW_SECTIONS, "review_sections_status_line_mismatch", errors)

    expected_sections = REVIEW_SECTIONS
    normalized_sections = [normalize_heading(section) for section in REVIEW_SECTIONS]
    found_sections: list[str] = []
    for line in body_text.splitlines():
        heading = re.match(r"^(?:#{1,6}\s+)?(?:\*\*)?(.+?)(?:\*\*)?$", line.strip())
        if heading is None:
            continue
        candidate = normalize_heading(heading.group(1))
        if candidate in normalized_sections:
            found_sections.append(candidate)
    require(found_sections == normalized_sections, "review_sections_mismatch", errors)

    for section in expected_sections:
        require(
            re.search(rf"(?m)^(?:#{{1,6}}\s+)?(?:\*\*)?{re.escape(section)}(?:\*\*)?$", body_text) is not None,
            f"missing_section:{section}",
            errors,
        )
    require(
        re.search(r"\b(?:continuing|continued)\s+with\s+the\s+remaining\b", last_message.lower()) is None,
        "second_workset_offer_detected",
        errors,
    )
    quick_fix_mentions = re.finditer(r"(?i).{0,40}quick-fix.{0,40}", body_text)
    for mention in quick_fix_mentions:
        snippet = mention.group(0).lower()
        if re.search(r"\b(no|not|without|never|none|disabled|skipped)\b", snippet) is None:
            errors.append("bulk_quick_fix_text_detected")
            break

    for pattern in (r"/api/states/", r"/api/services/"):
        require(re.search(pattern, joined_commands) is None, f"forbidden_command:{pattern}", errors)

    config_read_records: list[tuple[dict, list[str]]] = []
    for item in completed_commands:
        command = item.get("command", "")
        if re.search(r"/api/config/(?:automation|script)/config/", command) is None:
            continue
        entity_ids = extract_config_read_ids(item.get("aggregated_output", ""))
        require(bool(entity_ids), "review_unidentified_config_read", errors)
        config_read_records.append((item, entity_ids))
    audited_config_reads = [entity_id for _, entity_ids in config_read_records for entity_id in entity_ids]
    unique_config_reads: list[str] = []
    seen_config_reads: set[str] = set()
    for entity_id in audited_config_reads:
        if entity_id in seen_config_reads:
            continue
        seen_config_reads.add(entity_id)
        unique_config_reads.append(entity_id)
    workset_config_reads = [entity_id for entity_id in unique_config_reads if entity_id in fixture["audited"]]
    require(len(workset_config_reads) == len(fixture["audited"]), "review_config_read_count_mismatch", errors)
    require(workset_config_reads == fixture["audited"], "review_unique_id_targets_mismatch", errors)

    explicit_related_reads: set[str] = set()
    for item, entity_ids in config_read_records:
        if not is_related_collision_evidence(item):
            continue
        for entity_id in entity_ids:
            if entity_id not in fixture["audited"] and entity_id not in fixture["non_audited"]:
                explicit_related_reads.add(entity_id)
    extra_config_reads = [
        entity_id
        for entity_id in unique_config_reads
        if entity_id not in fixture["audited"] and entity_id not in fixture["non_audited"]
    ]
    require(len(explicit_related_reads) <= 1, "review_multiple_extra_related_config_reads", errors)
    for entity_id in extra_config_reads:
        require(entity_id in explicit_related_reads, f"review_unapproved_extra_config_read:{entity_id}", errors)

    for entity_id in fixture["non_audited"]:
        require(entity_id not in unique_config_reads, f"review_prefetch_outside_workset:{entity_id}", errors)
        require(entity_id not in joined_commands, f"review_prefetch_outside_workset:{entity_id}", errors)
    return errors


def validate_case(mode: str, fixture_key: str, fixture: dict, raw_log: Path, codex_exit: int) -> ScenarioResult:
    events = load_events(raw_log)
    raw_text = raw_log.read_text(encoding="utf-8")
    started_commands, completed_commands = started_and_completed_command_items(events)
    joined_commands = "\n".join(
        item.get("command", "")
        for item in started_commands + completed_commands
    )
    structural = structural_errors(events, raw_text)
    errors = list(structural)
    if codex_exit != 0:
        errors.append(f"codex_exit_nonzero:{codex_exit}")
    if re.search(r"scripts/(smoke|dev|e2e)/", joined_commands):
        errors.append("helper_script_usage_detected")
    if errors:
        status = "pass" if not errors else "fail"
        return ScenarioResult(fixture_key, mode, status, errors, codex_exit, str(raw_log))

    if mode == "inventory":
        selector_patterns = {
            "prefix_inventory": r"config/entity_registry/list_for_display",
            "area_inventory": r'config/area_registry/list|search/related[\s\S]*item_type[\s\S]*area',
            "label_inventory": r"config/entity_registry/list",
        }
        errors.extend(validate_inventory(events, fixture, selector_patterns[fixture_key]))
    else:
        errors.extend(validate_review(events, fixture, raw_text))

    status = "pass" if not errors else "fail"
    return ScenarioResult(fixture_key, mode, status, errors, codex_exit, str(raw_log))


def main() -> None:
    global CODEX_BINARY

    for command in ("codex", "ha-nova"):
        require_cmd(command)
    CODEX_BINARY = resolve_codex_binary()
    requested_scenarios = parse_requested_scenarios(sys.argv[1:])
    LOG_DIR.mkdir(parents=True, exist_ok=True)
    subprocess.run(["ha-nova", "relay", "health"], cwd=ROOT, check=True, stdout=subprocess.DEVNULL)

    fixtures = discover_fixtures(requested_scenarios)
    FIXTURES_FILE.write_text(json.dumps(fixtures, indent=2), encoding="utf-8")
    log(f"Output directory: {OUTPUT_DIR}")
    log(f"Fixtures: {FIXTURES_FILE}")

    prompts = {}
    if "prefix_inventory" in fixtures:
        prompts["prefix_inventory"] = build_inventory_prompt(
            fixtures["prefix_inventory"]["id"],
            f"Show all automations with prefix {fixtures['prefix_inventory']['selector']}.",
        )
    if "area_inventory" in fixtures:
        prompts["area_inventory"] = build_inventory_prompt(
            fixtures["area_inventory"]["id"],
            f"Show all automations in area {fixtures['area_inventory']['area_name']}. Live selector hint: area_id={fixtures['area_inventory']['area_id']}.",
        )
    if "label_inventory" in fixtures:
        prompts["label_inventory"] = build_inventory_prompt(
            fixtures["label_inventory"]["id"],
            f"Show all automations with label {fixtures['label_inventory']['label_name']}. Live selector hint: label_id={fixtures['label_inventory']['label_id']}.",
        )
    if "area_review" in fixtures:
        prompts["area_review"] = build_review_prompt(
            fixtures["area_review"]["id"],
            f"Review all automations in area {fixtures['area_review']['area_name']}. Live selector hint: area_id={fixtures['area_review']['area_id']}.",
        )
    modes = {
        "prefix_inventory": "inventory",
        "area_inventory": "inventory",
        "label_inventory": "inventory",
        "area_review": "review",
    }

    results: list[dict] = []
    RESULTS_FILE.write_text("", encoding="utf-8")
    for fixture_key in requested_scenarios:
        log(f"Running {fixture_key}")
        raw_log = LOG_DIR / f"{fixture_key}.jsonl"
        try:
            codex_exit = run_codex(
                prompts[fixture_key],
                raw_log,
                final_status_marker(modes[fixture_key], fixtures[fixture_key]["id"]),
            )
            payload = validate_case(modes[fixture_key], fixture_key, fixtures[fixture_key], raw_log, codex_exit).__dict__
            log(f"Validated {fixture_key}: {payload['status']}")
        except Exception as exc:  # noqa: BLE001
            payload = ScenarioResult(
                fixture_key=fixture_key,
                mode=modes[fixture_key],
                status="fail",
                errors=[f"scenario_exception:{type(exc).__name__}:{exc}"],
                codex_exit=1,
                raw_log=str(raw_log),
            ).__dict__
            results.append(payload)
            with RESULTS_FILE.open("a", encoding="utf-8") as handle:
                handle.write(json.dumps(payload) + "\n")
            summary = {"status": "fail", "results": results}
            SUMMARY_FILE.write_text(json.dumps(summary, indent=2), encoding="utf-8")
            die(f"Suite failed. See {SUMMARY_FILE}")

        results.append(payload)
        with RESULTS_FILE.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(payload) + "\n")
        log(f"Recorded {fixture_key}")

    summary = {"status": "pass" if all(result["status"] == "pass" for result in results) else "fail", "results": results}
    SUMMARY_FILE.write_text(json.dumps(summary, indent=2), encoding="utf-8")
    if summary["status"] != "pass":
        die(f"Suite failed. See {SUMMARY_FILE}")
    log("Suite passed")
    print(f"NOVA_BULK_E2E_RESULT ok reason=bulk_live_validation_clean summary={SUMMARY_FILE}")


if __name__ == "__main__":
    try:
        main()
    except Exception:  # noqa: BLE001
        OUTPUT_DIR.mkdir(parents=True, exist_ok=True)
        fatal_log = OUTPUT_DIR / f"fatal-{RUN_ID}.log"
        fatal_log.write_text(traceback.format_exc(), encoding="utf-8")
        raise
