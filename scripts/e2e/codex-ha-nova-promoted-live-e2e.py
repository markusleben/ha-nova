#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import queue
import re
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[2]
ACTIVE_OUTPUT_PREFIX = "ha-nova-codex-promoted-live-active."
STALE_OUTPUT_PATTERNS = ("ha-nova-codex-promoted-live.*", "ha-nova-promoted-suite.*")
OUTPUT_DIR = Path(os.environ.get("OUTPUT_DIR", tempfile.mkdtemp(prefix=ACTIVE_OUTPUT_PREFIX)))
ACTIVE_OUTPUT_DIR = OUTPUT_DIR.resolve()
ACTIVE_OUTPUT_PROTECTED_DIRS = {ACTIVE_OUTPUT_DIR, *ACTIVE_OUTPUT_DIR.parents}
RUN_ID = datetime.now().strftime("%Y%m%d-%H%M%S")
LOG_DIR = OUTPUT_DIR / f"logs-{RUN_ID}"
RESULTS_FILE = OUTPUT_DIR / f"results-{RUN_ID}.ndjson"
SUMMARY_FILE = OUTPUT_DIR / f"summary-{RUN_ID}.json"
SCENARIO_TIMEOUT_SEC = int(os.environ.get("PROMOTED_E2E_SCENARIO_TIMEOUT_SEC", "420"))
CODEX_BINARY = "codex"
SCENARIO_ORDER = (
    "dashboard_storage_lifecycle",
    "dashboard_card_flow",
    "dashboard_resource_flow",
    "dashboard_delete_token",
    "dashboard_delete_reject_natural",
    "organize_category_flow",
    "organize_floor_area_flow",
    "organize_label_entity_flow",
    "organize_category_delete_token",
    "history_timeline",
    "history_statistics",
)
KNOWN_JSONL_NOISE = ("Reading additional input from stdin...",)
# Codex CLI writes timestamped logger lines (ERROR/WARN, changing logger
# names across releases — e.g. codex_core::tools::router appeared in 0.142)
# to the merged stream. Raw non-JSON lines are never scenario output: agent
# messages and commands only arrive inside JSONL events, so timestamped log
# lines are always transport noise.
KNOWN_JSONL_NOISE_RE = (
    re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z\s+(?:ERROR|WARN)\s+\S+: .*$"),
)
HELPER_SCRIPT_RE = re.compile(r"^(?:\./)?scripts/(?:smoke|e2e|dev)/\S+$")
DOCTOR_RE = re.compile(r"(^|[^\w./-])(?:ha-nova|[.]/cli/cli|cli/cli|scripts/onboarding/bin/ha-nova)(?:\s+[A-Za-z0-9_./-]+){0,2}\s+(?:doctor|ready|quick)(?=$|[^\w-])")
RELAY_HEALTH_RE = re.compile(r"(^|[^\w./-])(?:(?:ha-nova|[.]/cli/cli|cli/cli|go\s+run(?:\s+[A-Za-z0-9_./-]+){1,4})\s+relay\s+health|/health)(?=$|[^\w-])")
HOME_SKILL_RE = re.compile(r"~/.local/share/ha-nova/skills|/\.local/share/ha-nova/skills|~/.agents/skills|/\.agents/skills")
WS_TYPE_RE = re.compile(r"lovelace/(?:dashboards/(?:list|create|update|delete)|config(?:/save|/delete)?|resources(?:/(?:create|update|delete))?)|config/(?:area_registry|floor_registry|label_registry|category_registry|entity_registry)/(?:list|get|create|update|delete)|recorder/statistics_during_period")
PROMOTED_DASHBOARD_ID_RE = re.compile(r"^nova_codex_")
PROMOTED_DASHBOARD_PATH_RE = re.compile(r"^nova-codex-")
PROMOTED_SCOPE_RE = re.compile(r"nova_codex_scope(?:_delete)?_\d+")
PROMOTED_RESOURCE_URL_RE = re.compile(r"^/local/nova-codex-[a-z0-9-]+\.(?:js|css)$")
PROMOTED_AREA_RE = re.compile(r"^nova_codex_area_\d+$")
PROMOTED_FLOOR_RE = re.compile(r"^nova_codex_floor_\d+$")
PROMOTED_LABEL_RE = re.compile(r"^nova_codex_label_\d+$")
NEUTRAL_HISTORY_ENTITY_RE = re.compile(
    r"(?:temperature|humidity|power|energy|voltage|current|battery|signal|"
    r"pressure|illuminance|load|usage|speed|frequency|status|import|export|solar|co2)",
    re.IGNORECASE,
)


@dataclass
class ScenarioResult:
    scenario_id: str
    status: str
    errors: list[str]
    codex_exit: int
    raw_log: str


def log(message: str) -> None:
    print(f"[codex-promoted-live-e2e] {message}", flush=True)


def die(message: str) -> None:
    raise SystemExit(f"[codex-promoted-live-e2e] {message}")


def require_cmd(command: str) -> None:
    if shutil.which(command) is None:
        die(f"Required command not found: {command}")


def trash_path(path: Path) -> None:
    if not path.exists():
        return
    subprocess.run(["trash", str(path)], cwd=ROOT, check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


def relay_ws(payload: dict[str, Any]) -> dict[str, Any]:
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
        json.dump(payload, handle)
        payload_path = handle.name
    try:
        raw = subprocess.check_output(
            ["ha-nova", "relay", "ws", "--data-file", payload_path],
            cwd=ROOT,
            text=True,
        )
        parsed = json.loads(raw)
        if not parsed.get("ok"):
            raise subprocess.CalledProcessError(1, "ha-nova relay ws", output=raw)
        return parsed
    finally:
        Path(payload_path).unlink(missing_ok=True)


def relay_core_get(path: str) -> dict[str, Any]:
    raw = subprocess.check_output(
        ["ha-nova", "relay", "core", "--method", "GET", "--path", path],
        cwd=ROOT,
        text=True,
    )
    parsed = json.loads(raw)
    if not parsed.get("ok"):
        raise subprocess.CalledProcessError(1, "ha-nova relay core", output=raw)
    return parsed


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


def parse_requested_scenarios(argv: list[str]) -> list[str]:
    if "--list-scenarios" in argv or "--cleanup-only" in argv:
        return []
    if not argv:
        return list(SCENARIO_ORDER)

    invalid = [scenario for scenario in argv if scenario not in SCENARIO_ORDER]
    if invalid:
        die(
            "Unknown scenario(s): "
            + ", ".join(invalid)
            + ". Valid scenarios: "
            + ", ".join(SCENARIO_ORDER)
        )
    requested: list[str] = []
    seen: set[str] = set()
    for scenario in argv:
        if scenario in seen:
            continue
        seen.add(scenario)
        requested.append(scenario)
    return requested


def cleanup_promoted_residue() -> None:
    cleanup_stale_promoted_dashboards()
    cleanup_stale_promoted_resources()
    cleanup_stale_promoted_categories()
    cleanup_stale_promoted_organize_metadata()
    cleanup_promoted_output_dirs()


def run_codex(prompt: str, raw_log: Path, marker: str) -> int:
    with raw_log.open("w", encoding="utf-8") as handle:
        popen_kwargs: dict[str, object] = {
            "cwd": ROOT,
            "text": True,
            "stdout": subprocess.PIPE,
            "stderr": subprocess.STDOUT,
            "bufsize": 1,
            "stdin": subprocess.DEVNULL,
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

        saw_eof = False
        try:
            deadline = time.monotonic() + SCENARIO_TIMEOUT_SEC
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
                    if process.poll() is not None and reader_done.wait(timeout=min(0.25, max(remaining, 0))):
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


def load_events(raw_log: Path) -> tuple[list[dict[str, Any]], list[str]]:
    events: list[dict[str, Any]] = []
    invalid_lines: list[str] = []
    for raw_line in raw_log.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line:
            continue
        if line in KNOWN_JSONL_NOISE or any(pattern.match(line) for pattern in KNOWN_JSONL_NOISE_RE):
            continue
        try:
            payload = json.loads(line)
        except json.JSONDecodeError:
            invalid_lines.append(line)
            continue
        if isinstance(payload, dict):
            events.append(payload)
        else:
            invalid_lines.append(line)
    return events, invalid_lines


def extract_completed_commands(events: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return [
        event["item"]
        for event in events
        if event.get("type") == "item.completed" and event.get("item", {}).get("type") == "command_execution"
    ]


def extract_agent_messages(events: list[dict[str, Any]]) -> list[str]:
    return [
        event.get("item", {}).get("text", "")
        for event in events
        if event.get("type") == "item.completed" and event.get("item", {}).get("type") == "agent_message"
    ]


def command_output(item: dict[str, Any]) -> str:
    return f'{item.get("aggregated_output", "")}\n{item.get("raw_output", "")}'


def transcript_text(commands: list[dict[str, Any]]) -> str:
    chunks: list[str] = []
    for item in commands:
        chunks.append(item.get("command", ""))
        chunks.append(command_output(item))
    return "\n".join(chunks)


def contains_helper_script(commands: list[str]) -> bool:
    for command in commands:
        for segment in re.split(r"(?:&&|\|\||;|\n)", command):
            segment = segment.strip()
            if not segment:
                continue
            try:
                tokens = shlex.split(segment)
            except ValueError:
                tokens = segment.split()
            if not tokens:
                continue

            index = 0
            if tokens[index] == "env":
                index += 1
                while index < len(tokens) and re.match(r"^[A-Za-z_][A-Za-z0-9_]*=.*$", tokens[index]):
                    index += 1
            if index < len(tokens) and tokens[index] == "timeout":
                index += 1
                if index < len(tokens):
                    index += 1
            if index >= len(tokens):
                continue

            candidate = tokens[index]
            if HELPER_SCRIPT_RE.match(candidate):
                return True

            runner = Path(candidate).name
            if runner in {"bash", "sh", "zsh", "python", "python3", "node", "bun", "bunx", "tsx"}:
                index += 1
                while index < len(tokens) and tokens[index].startswith("-"):
                    index += 1
                if index < len(tokens) and HELPER_SCRIPT_RE.match(tokens[index]):
                    return True
            if candidate.endswith("/env") and index + 1 < len(tokens):
                runner = Path(tokens[index + 1]).name
                if runner in {"bash", "sh", "zsh", "python", "python3", "node", "bun", "bunx", "tsx"}:
                    script_index = index + 2
                    while script_index < len(tokens) and tokens[script_index].startswith("-"):
                        script_index += 1
                    if script_index < len(tokens) and HELPER_SCRIPT_RE.match(tokens[script_index]):
                        return True
            if "scripts/" in segment and candidate in {"rg", "grep", "sed", "cat", "ls", "find", "awk"}:
                continue
            if "scripts/" in segment and Path(candidate).name in {"rg", "grep", "sed", "cat", "ls", "find", "awk"}:
                continue
            if "scripts/" in segment and HELPER_SCRIPT_RE.search(segment):
                return True
    return False


def is_benign_failed_command(command: dict[str, Any]) -> bool:
    exit_code = command.get("exit_code")
    if not isinstance(exit_code, int) or exit_code == 0:
        return False

    text = command.get("command", "")
    if exit_code == 1 and re.search(r"\b(?:rg|grep)\b", text):
        return True

    return False


def count_ws_mentions(text: str, ws_type: str) -> int:
    return len(re.findall(rf"\b{re.escape(ws_type)}\b", text))


def latest_agent_message(messages: list[str]) -> str:
    return messages[-1] if messages else ""


def common_errors(events: list[dict[str, Any]], invalid_lines: list[str], expected_status_line: str) -> tuple[list[str], list[dict[str, Any]], list[str], str]:
    errors: list[str] = []
    commands = extract_completed_commands(events)
    command_texts = [item.get("command", "") for item in commands]
    messages = extract_agent_messages(events)
    final_message = latest_agent_message(messages)

    if invalid_lines:
        errors.append("invalid_jsonl_transcript")
    if not any(event.get("type") == "turn.completed" for event in events):
        errors.append("incomplete_transcript")
    if any(
        isinstance(item.get("exit_code"), int)
        and item.get("exit_code") != 0
        and not is_benign_failed_command(item)
        for item in commands
    ):
        errors.append("failed_command_exit")
    if contains_helper_script(command_texts):
        errors.append("helper_script_usage_detected")
    if any(DOCTOR_RE.search(command) for command in command_texts):
        errors.append("proactive_doctor_or_ready_detected")
    if any(RELAY_HEALTH_RE.search(command) for command in command_texts):
        errors.append("health_preflight_before_action")
    if any(HOME_SKILL_RE.search(command) for command in command_texts):
        errors.append("installed_skill_copy_accessed")

    all_status_lines = re.findall(r"^NOVA_PROMOTED_SKILL_RESULT.*$", "\n\n".join(messages), flags=re.MULTILINE)
    if len(all_status_lines) != 1:
        errors.append("missing_final_status_line")
    else:
        final_line = all_status_lines[-1]
        stripped_lines = [line.strip() for line in final_message.splitlines() if line.strip()]
        if not stripped_lines or stripped_lines[-1] != final_line:
            errors.append("status_line_not_final")
        if final_line != expected_status_line:
            errors.append("unexpected_final_status_line")

    return errors, commands, messages, final_message


def dashboard_seed(url_path: str, title: str, with_config: bool) -> dict[str, Any]:
    created = relay_ws(
        {
            "type": "lovelace/dashboards/create",
            "url_path": url_path,
            "title": title,
            "icon": "mdi:test-tube",
            "show_in_sidebar": False,
            "require_admin": True,
        }
    )
    if with_config:
        relay_ws(
            {
                "type": "lovelace/config/save",
                "url_path": url_path,
                "config": {
                    "views": [
                        {
                            "title": "Seed View",
                            "path": "default_view",
                            "cards": [{"type": "markdown", "content": "seed"}],
                        }
                    ]
                },
            }
        )
    return {
        "dashboard_id": created["data"]["id"],
        "url_path": url_path,
        "title": title,
    }


def dashboard_card_seed(url_path: str, title: str, view_title: str) -> dict[str, Any]:
    seeded = dashboard_seed(url_path, title, with_config=False)
    relay_ws(
        {
            "type": "lovelace/config/save",
            "url_path": url_path,
            "config": {
                "views": [
                    {
                        "title": view_title,
                        "path": "cards_view",
                        "cards": [
                            {"type": "markdown", "title": "Alpha Card", "content": "alpha"},
                            {"type": "markdown", "title": "Beta Card", "content": "beta"},
                        ],
                    }
                ]
            },
        }
    )
    seeded["view_title"] = view_title
    return seeded


def cleanup_dashboard(fixture: dict[str, Any] | None) -> None:
    if not fixture:
        return
    try:
        relay_ws({"type": "lovelace/dashboards/delete", "dashboard_id": fixture["dashboard_id"]})
    except subprocess.CalledProcessError:
        pass


def cleanup_resource(resource_id: str | None) -> None:
    if not resource_id:
        return
    try:
        relay_ws({"type": "lovelace/resources/delete", "resource_id": resource_id})
    except subprocess.CalledProcessError:
        pass


def create_resource_seed(url: str, res_type: str = "module") -> dict[str, Any]:
    created = relay_ws({"type": "lovelace/resources/create", "url": url, "res_type": res_type})
    return created["data"]


def cleanup_area(area_id: str | None) -> None:
    if not area_id:
        return
    try:
        relay_ws({"type": "config/area_registry/delete", "area_id": area_id})
    except subprocess.CalledProcessError:
        pass


def cleanup_floor(floor_id: str | None) -> None:
    if not floor_id:
        return
    try:
        relay_ws({"type": "config/floor_registry/delete", "floor_id": floor_id})
    except subprocess.CalledProcessError:
        pass


def cleanup_label(label_id: str | None) -> None:
    if not label_id:
        return
    try:
        relay_ws({"type": "config/label_registry/delete", "label_id": label_id})
    except subprocess.CalledProcessError:
        pass


def discover_entity_fixture() -> dict[str, Any]:
    entities = relay_ws({"type": "config/entity_registry/list"}).get("data", [])
    preferred_domains = ("input_boolean", "input_number", "counter", "timer", "sensor", "binary_sensor", "person")
    for domain in preferred_domains:
        for item in entities:
            entity_id = item.get("entity_id")
            if not entity_id or not entity_id.startswith(f"{domain}."):
                continue
            if item.get("disabled_by") or item.get("hidden_by"):
                continue
            labels = item.get("labels", [])
            aliases = item.get("aliases", [])
            categories = item.get("categories", {})
            if labels or aliases or categories:
                continue
            return {"entity_id": entity_id, "categories": categories, "labels": labels, "aliases": aliases}
    for domain in preferred_domains:
        for item in entities:
            entity_id = item.get("entity_id")
            if not entity_id or not entity_id.startswith(f"{domain}."):
                continue
            if item.get("disabled_by") or item.get("hidden_by"):
                continue
            return {
                "entity_id": entity_id,
                "categories": item.get("categories", {}),
                "labels": item.get("labels", []),
                "aliases": item.get("aliases", []),
            }
    die("Unable to discover a safe entity-registry fixture for promoted organize tests")


def discover_clean_entity_fixture() -> dict[str, Any]:
    entities = relay_ws({"type": "config/entity_registry/list"}).get("data", [])
    preferred_domains = ("input_boolean", "input_number", "counter", "timer", "sensor", "binary_sensor", "person")
    for domain in preferred_domains:
        for item in entities:
            entity_id = item.get("entity_id")
            if not entity_id or not entity_id.startswith(f"{domain}."):
                continue
            if item.get("disabled_by") or item.get("hidden_by"):
                continue
            if item.get("labels") or item.get("aliases") or item.get("categories"):
                continue
            return {
                "entity_id": entity_id,
                "categories": {},
                "labels": [],
                "aliases": [],
            }
    die("Unable to discover a clean entity-registry fixture for organize metadata tests")


def core_states() -> list[dict[str, Any]]:
    response = relay_core_get("/api/states")
    body = response.get("data", {}).get("body", [])
    if not isinstance(body, list):
        return []
    return [item for item in body if isinstance(item, dict)]


def category_seed(scope: str, name: str, entity_id: str | None = None) -> dict[str, Any]:
    created = relay_ws({"type": "config/category_registry/create", "scope": scope, "name": name, "icon": "mdi:tag"})
    category = created["data"]
    if entity_id:
        relay_ws(
            {
                "type": "config/entity_registry/update",
                "entity_id": entity_id,
                "categories": {scope: category["category_id"]},
            }
        )
    return category


def cleanup_category(scope: str, category_id: str | None) -> None:
    if not category_id:
        try:
            categories = relay_ws({"type": "config/category_registry/list", "scope": scope}).get("data", [])
        except subprocess.CalledProcessError:
            return
        for category in categories:
            cleanup_category(scope, category.get("category_id"))
        return
    try:
        relay_ws({"type": "config/category_registry/delete", "scope": scope, "category_id": category_id})
    except subprocess.CalledProcessError:
        pass


def cleanup_entity_category_scope(scope: str) -> None:
    try:
        entities = relay_ws({"type": "config/entity_registry/list"}).get("data", [])
    except subprocess.CalledProcessError:
        return

    for item in entities:
        entity_id = item.get("entity_id")
        categories = item.get("categories", {})
        if not entity_id or categories.get(scope) is None:
            continue
        try:
            relay_ws(
                {
                    "type": "config/entity_registry/update",
                    "entity_id": entity_id,
                    "categories": {scope: None},
                }
            )
        except subprocess.CalledProcessError:
            continue


def cleanup_entity_metadata(entity_id: str, labels: list[str], aliases: list[str]) -> None:
    payload: dict[str, Any] = {"type": "config/entity_registry/update", "entity_id": entity_id}
    payload["labels"] = labels
    payload["aliases"] = aliases
    try:
        relay_ws(payload)
    except subprocess.CalledProcessError:
        pass


def artifact_output_dirs() -> list[Path]:
    temp_root = Path(tempfile.gettempdir())
    return sorted(
        output_dir
        for pattern in STALE_OUTPUT_PATTERNS
        for output_dir in temp_root.glob(pattern)
        if not is_protected_output_dir(output_dir)
    )


def is_protected_output_dir(path: Path) -> bool:
    resolved = path.resolve()
    return (
        resolved in ACTIVE_OUTPUT_PROTECTED_DIRS
        or ACTIVE_OUTPUT_DIR.is_relative_to(resolved)
        or resolved.is_relative_to(ACTIVE_OUTPUT_DIR)
    )


def stale_scopes_from_artifacts() -> set[str]:
    scopes: set[str] = set()
    for output_dir in artifact_output_dirs():
        for path in output_dir.rglob("*"):
            if not path.is_file() or path.suffix not in {".json", ".jsonl"}:
                continue
            try:
                text = path.read_text(encoding="utf-8", errors="ignore")
            except OSError:
                continue
            scopes.update(PROMOTED_SCOPE_RE.findall(text))
    return scopes


def cleanup_stale_promoted_dashboards() -> None:
    try:
        dashboards = relay_ws({"type": "lovelace/dashboards/list"}).get("data", [])
    except subprocess.CalledProcessError:
        return

    for dashboard in dashboards:
        dashboard_id = dashboard.get("id")
        url_path = dashboard.get("url_path")
        if not isinstance(dashboard_id, str) or not isinstance(url_path, str):
            continue
        if not PROMOTED_DASHBOARD_ID_RE.search(dashboard_id) and not PROMOTED_DASHBOARD_PATH_RE.search(url_path):
            continue
        cleanup_dashboard({"dashboard_id": dashboard_id})


def cleanup_stale_promoted_categories() -> None:
    for scope in sorted(stale_scopes_from_artifacts()):
        cleanup_entity_category_scope(scope)
        try:
            categories = relay_ws({"type": "config/category_registry/list", "scope": scope}).get("data", [])
        except subprocess.CalledProcessError:
            continue
        for category in categories:
            cleanup_category(scope, category.get("category_id"))


def cleanup_stale_promoted_resources() -> None:
    try:
        resources = relay_ws({"type": "lovelace/resources"}).get("data", [])
    except subprocess.CalledProcessError:
        return
    for resource in resources:
        resource_id = resource.get("id")
        url = resource.get("url")
        if not isinstance(resource_id, str) or not isinstance(url, str):
            continue
        if not PROMOTED_RESOURCE_URL_RE.search(url):
            continue
        cleanup_resource(resource_id)


def cleanup_stale_promoted_organize_metadata() -> None:
    try:
        areas = relay_ws({"type": "config/area_registry/list"}).get("data", [])
        floors = relay_ws({"type": "config/floor_registry/list"}).get("data", [])
        labels = relay_ws({"type": "config/label_registry/list"}).get("data", [])
    except subprocess.CalledProcessError:
        return

    for area in areas:
        area_id = area.get("area_id")
        if isinstance(area_id, str) and PROMOTED_AREA_RE.search(area_id):
            cleanup_area(area_id)

    for floor in floors:
        floor_id = floor.get("floor_id")
        if isinstance(floor_id, str) and PROMOTED_FLOOR_RE.search(floor_id):
            cleanup_floor(floor_id)

    for label in labels:
        label_id = label.get("label_id")
        if isinstance(label_id, str) and PROMOTED_LABEL_RE.search(label_id):
            cleanup_label(label_id)


def cleanup_promoted_output_dirs() -> None:
    for output_dir in artifact_output_dirs():
        if is_protected_output_dir(output_dir):
            continue
        trash_path(output_dir)


def history_fixture() -> dict[str, Any]:
    start = (datetime.now(timezone.utc) - timedelta(hours=24)).strftime("%Y-%m-%dT%H:%M:%SZ")
    end = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    preferred_domains = ("sensor", "binary_sensor", "input_boolean", "input_number")
    candidates = [
        item.get("entity_id")
        for item in core_states()
        if isinstance(item.get("entity_id"), str)
        and item["entity_id"].split(".", 1)[0] in preferred_domains
        and item.get("state") not in {"unknown", "unavailable"}
    ]
    neutral_candidates = [entity_id for entity_id in candidates if entity_id and NEUTRAL_HISTORY_ENTITY_RE.search(entity_id)]
    if not neutral_candidates:
        die("Unable to discover a neutral history fixture with a bounded successful history query")
    for entity_id in neutral_candidates:
        if not entity_id:
            continue
        path = f"/api/history/period/{start}?filter_entity_id={entity_id}&end_time={end}&minimal_response=1&no_attributes=1"
        response = relay_core_get(path)
        if response.get("data", {}).get("status") == 200 and len(response.get("data", {}).get("body", [])) >= 1:
            logbook = relay_core_get(f"/api/logbook/{start}?entity={entity_id}&end_time={end}")
            return {
                "entity_id": entity_id,
                "start": start,
                "end": end,
                "history_rows": len(response["data"]["body"]),
                "logbook_rows": len(logbook.get("data", {}).get("body", [])),
            }
    die("Unable to discover a history fixture with a bounded successful history query")


def statistics_fixture() -> dict[str, Any]:
    end = datetime.now(timezone.utc)
    start = end - timedelta(days=7)
    states = core_states()
    candidates = [
        item["entity_id"]
        for item in states
        if isinstance(item.get("entity_id"), str)
        and item["entity_id"].startswith("sensor.")
        and isinstance(item.get("attributes"), dict)
        and item["attributes"].get("state_class")
        and item.get("state") not in {"unknown", "unavailable"}
    ]
    neutral_candidates = [entity_id for entity_id in candidates if NEUTRAL_HISTORY_ENTITY_RE.search(entity_id)]
    if not neutral_candidates:
        die("Unable to discover a neutral statistics-capable entity fixture")
    for entity_id in neutral_candidates:
        try:
            response = relay_ws(
                {
                    "type": "recorder/statistics_during_period",
                    "start_time": start.isoformat(),
                    "end_time": end.isoformat(),
                    "statistic_ids": [entity_id],
                    "period": "day",
                    "types": ["mean", "min", "max", "sum", "state", "change"],
                }
            )
        except subprocess.CalledProcessError:
            continue
        result = response.get("data", {}).get(entity_id, [])
        if isinstance(result, list) and result:
            return {
                "entity_id": entity_id,
                "start": start.isoformat(),
                "end": end.isoformat(),
                "period": "day",
            }
    die("Unable to discover a statistics-capable entity fixture")


def base_prompt(skill_path: Path, extra_skill: str, task: str, status_line: str) -> str:
    return f"""Use the repo-local {extra_skill} skill for this task.

User request:
{task}

Hard requirements:
1. Work in English only.
2. Use App + Relay terminology.
3. Read only these repo-local files for skill guidance:
   - `{ROOT / "skills" / "ha-nova" / "SKILL.md"}`
   - directly referenced `{ROOT / "skills" / "ha-nova" / "*.md"}` reference files if needed
   - `{skill_path}`
   - `{ROOT / "skills" / "ha-nova" / "relay-api.md"}` if needed
4. Do not open installed skill copies under `~/.local/share/ha-nova/skills`, `~/.agents/skills`, or other home-directory mirrors.
5. Use the current local HA NOVA setup directly. Do not run health, doctor, or readiness preflights unless the first relay call fails.
6. Do not browse the web and do not use external docs or research tools.
7. Do not run project helper scripts.
8. Do not search the repo for implementation hints beyond those allowed files.
9. Do not modify repository files.
10. Do not probe CLI syntax with `--help` or other trial commands.
11. Do not wrap relay commands in ad-hoc debug shells, loops, or extra shell variables. Use minimal one-shot commands only.
12. Do not emit progress updates, meta narration, or tool transcript fragments. Return only the final user-facing answer and the final status line.
13. Final output must contain exactly one status line:
    {status_line}
"""


def build_dashboard_lifecycle_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    status_line = f'NOVA_PROMOTED_SKILL_RESULT id=dashboard_storage_lifecycle ok url_path={fixture["url_path"]} title="{fixture["final_title"]}"'
    task = f"""Create a new storage dashboard shell titled `{fixture["initial_title"]}` at url path `{fixture["url_path"]}` with icon `mdi:test-tube`, hidden from the sidebar, and admin-only.
Immediately save a full config with one view titled `{fixture["view_title"]}`.
Read the dashboard config after that save to verify the new view exists.
Then update the dashboard metadata so the title becomes `{fixture["final_title"]}`, the icon becomes `mdi:view-dashboard-edit`, `show_in_sidebar` becomes true, and `require_admin` becomes false.
Read back through dashboard list and dashboard config again to verify everything stuck.
For the metadata update payload, use `dashboard_id` plus only `title`, `icon`, `show_in_sidebar`, and `require_admin`; do not resend `url_path` or `mode`.
Do not probe any other dashboard's config to infer behavior for this target.

This is a storage-dashboard create + metadata-update + config-save proof. Do not delete the dashboard in the assistant session.
Use Relay WebSocket calls only for this dashboard flow.
"""
    return base_prompt(ROOT / "skills" / "dashboard" / "SKILL.md", "dashboard", task, status_line), status_line


def build_dashboard_card_flow_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    status_line = f'NOVA_PROMOTED_SKILL_RESULT id=dashboard_card_flow ok url_path={fixture["url_path"]} view="{fixture["view_title"]}"'
    task = f"""On the existing storage dashboard at url path `{fixture["url_path"]}`, inspect the dashboard structure first.
Inside view `{fixture["view_title"]}`:
- add a new markdown card titled `{fixture["transient_title"]}` with content `{fixture["transient_content"]}`
- move the existing card titled `{fixture["move_title"]}` to the first position in that same view
- delete the transient markdown card again

Keep the final view with exactly these two cards in this order:
1. `{fixture["move_title"]}`
2. `{fixture["stay_title"]}`

Use targeted dashboard-card behavior only:
- resolve the exact view and card targets before each change
- use full config read -> in-memory patch -> full save -> readback verification
- do not touch other views
- do not create or guess any custom-card schema
- use Relay WebSocket calls only for this dashboard flow
"""
    return base_prompt(ROOT / "skills" / "dashboard" / "SKILL.md", "dashboard", task, status_line), status_line


def build_dashboard_resource_flow_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    status_line = f'NOVA_PROMOTED_SKILL_RESULT id=dashboard_resource_flow ok url={fixture["updated_url"]} type={fixture["updated_type"]}'
    task = f"""List the current Lovelace resources first.
Then create a new Lovelace resource with:
- type `{fixture["initial_type"]}`
- url `{fixture["initial_url"]}`

After that, update the same resource so it becomes:
- type `{fixture["updated_type"]}`
- url `{fixture["updated_url"]}`

Verify through the resource list that the updated resource exists exactly once.
Do not delete the resource in the assistant session.
Use Relay WebSocket calls only for this dashboard flow.
"""
    return base_prompt(ROOT / "skills" / "dashboard" / "SKILL.md", "dashboard", task, status_line), status_line


def build_dashboard_delete_token_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    token = fixture["token"]
    status_line = f'NOVA_PROMOTED_SKILL_RESULT id=dashboard_delete_token ok url_path={fixture["url_path"]} deleted=true'
    task = f"""You are handling the delete step for the existing storage dashboard at url path `{fixture["url_path"]}`.
In this test fixture, the previous turn showed the concrete delete preview for this exact dashboard and the exact confirmation token was `{token}`.
The user's current reply is exactly `{token}`.
Resolve the dashboard, execute only the delete path, verify the dashboard is gone from the dashboard list, and finish.
Use Relay WebSocket calls only for this dashboard flow.
"""
    return base_prompt(ROOT / "skills" / "dashboard" / "SKILL.md", "dashboard", task, status_line), status_line


def build_dashboard_delete_reject_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    token = fixture["token"]
    status_line = f'NOVA_PROMOTED_SKILL_RESULT id=dashboard_delete_reject_natural ok url_path={fixture["url_path"]} refused=true'
    task = f"""You are handling the delete step for the existing storage dashboard at url path `{fixture["url_path"]}`.
In this test fixture, the previous turn showed the concrete delete preview for this exact dashboard and the exact confirmation token was `{token}`.
The user's current reply is only `yes`.
Do not delete anything. Explain briefly that the exact token is still required, include the token literally, and stop.
Use Relay WebSocket calls only for this dashboard flow.
"""
    return base_prompt(ROOT / "skills" / "dashboard" / "SKILL.md", "dashboard", task, status_line), status_line


def build_organize_category_flow_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    status_line = (
        f'NOVA_PROMOTED_SKILL_RESULT id=organize_category_flow ok scope={fixture["scope"]} '
        f'entity_id={fixture["entity_id"]} cleared=true'
    )
    task = f"""Create a category in scope `{fixture["scope"]}` named `{fixture["initial_name"]}`.
Rename it to `{fixture["renamed_name"]}`.
Then assign entity `{fixture["entity_id"]}` to that category for the same scope, verify the assignment, and finally remove that category assignment from the entity again and verify it is cleared.

Use only organize-skill behavior:
- list/resolve before every mutation
- every category registry call in this scenario must include the provided scope
- preview exact field changes
- verify after each change
- keep this one resource/scope at a time
- when clearing the scoped category from the entity, use `categories` with that exact scope set to `null`

    Do not delete the category in the assistant session."""
    return base_prompt(ROOT / "skills" / "organize" / "SKILL.md", "organize", task, status_line), status_line


def build_organize_floor_area_flow_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    status_line = (
        f'NOVA_PROMOTED_SKILL_RESULT id=organize_floor_area_flow ok '
        f'area_id={fixture["area_id_hint"]} floor_id={fixture["floor_id_hint"]}'
    )
    task = f"""Create and verify richer floor and area metadata in one careful flow.

1. Create a floor named `{fixture["floor_name"]}` with:
- level `{fixture["floor_level"]}`
- icon `{fixture["floor_icon"]}`
- aliases `{fixture["floor_aliases"]}`

2. Update that floor so the final metadata is:
- name `{fixture["floor_final_name"]}`
- level `{fixture["floor_final_level"]}`
- icon `{fixture["floor_final_icon"]}`
- aliases `{fixture["floor_final_aliases"]}`

3. Create an area named `{fixture["area_name"]}` assigned to that floor with:
- icon `{fixture["area_icon"]}`
- picture `{fixture["area_picture"]}`
- aliases `{fixture["area_aliases"]}`

4. Update that area so the final metadata is:
- name `{fixture["area_final_name"]}`
- icon `{fixture["area_final_icon"]}`
- picture `{fixture["area_final_picture"]}`
- aliases `{fixture["area_final_aliases"]}`

Use organize-skill rules only:
- exact target resolution first
- field-level preview before each mutation
- verify after each mutation
- one target at a time
- do not delete the area or floor in the assistant session
"""
    return base_prompt(ROOT / "skills" / "organize" / "SKILL.md", "organize", task, status_line), status_line


def build_organize_label_entity_flow_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    status_line = (
        f'NOVA_PROMOTED_SKILL_RESULT id=organize_label_entity_flow ok '
        f'label_id={fixture["label_id_hint"]} entity_id={fixture["entity_id"]}'
    )
    task = f"""Create and verify richer label and entity metadata in one careful flow.

1. Create a label named `{fixture["label_name"]}` with:
- color `{fixture["label_color"]}`
- icon `{fixture["label_icon"]}`
- description `{fixture["label_description"]}`

2. Update that label so the final metadata is:
- name `{fixture["label_final_name"]}`
- color `{fixture["label_final_color"]}`
- icon `{fixture["label_final_icon"]}`
- description `{fixture["label_final_description"]}`

3. On entity `{fixture["entity_id"]}`:
- add the created label
- verify it
- remove that same label again
- verify it is gone
- set aliases to `{fixture["entity_aliases"]}`
- verify them
- clear aliases back to an empty list
- verify they are cleared

Use organize-skill rules only:
- exact target resolution first
- field-level preview before each mutation
- verify after each mutation
- one target at a time
- when you inspect relay write results with `ha-nova relay jq`, target `.data` only; do not emit mixed filters like `.ok, .data | ...`
- do not delete the label in the assistant session
"""
    return base_prompt(ROOT / "skills" / "organize" / "SKILL.md", "organize", task, status_line), status_line


def build_organize_category_delete_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    token = fixture["token"]
    status_line = (
        f'NOVA_PROMOTED_SKILL_RESULT id=organize_category_delete_token ok scope={fixture["scope"]} '
        f'category_id={fixture["category_id"]} deleted=true'
    )
    task = f"""You are handling the delete step for category `{fixture["category_id"]}` in scope `{fixture["scope"]}`.
In this test fixture, the previous turn showed the concrete delete preview for this exact category and scope, and the exact confirmation token was `{token}`.
The user's current reply is exactly `{token}`.
Delete the category, verify it is absent from the category registry for that scope, and verify entity `{fixture["entity_id"]}` no longer keeps that category in the same scope.
Every category registry call in this scenario must include the provided scope."""
    return base_prompt(ROOT / "skills" / "organize" / "SKILL.md", "organize", task, status_line), status_line


def build_history_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    status_line = f'NOVA_PROMOTED_SKILL_RESULT id=history_timeline ok entity_id={fixture["entity_id"]} bounded_window=24h'
    task = f"""Summarize what happened to `{fixture["entity_id"]}` in the last 24 hours.
Use the exact bounded window `{fixture["start"]}` to `{fixture["end"]}` for both the history query and the logbook query.
Keep this flow read-only and bounded. Do not omit `end_time`.
Use Relay core GET only; do not use Relay WebSocket in this history flow.
For `ha-nova relay jq`, do not invent a `--jq` flag. The filter is positional unless `--jq-file` is used.
Keep the analysis simple: first/last, min/max, broad trend, and whether logbook entries exist. Do not add fragile timestamp-gap parsing.
If the history series contains `unknown` or `unavailable`, do not run raw `tonumber` reductions across the whole series; keep the summary simple instead of forcing numeric min/max.
Do not build complex jq expressions just to recover min/max event timestamps; the simple summary is enough for this proof.
The relay-core envelope stays under `.data.body`; use `.data.body[0]` for the history series and `.data.body` for logbook entries. Do not probe `.[0]` or `.[0][0]`.
    Return the normal history-skill shape with `Target`, `Window`, `Summary`, `Key events` or `Key transitions`, and `Next step`."""
    return base_prompt(ROOT / "skills" / "history" / "SKILL.md", "history", task, status_line), status_line


def build_history_statistics_prompt(fixture: dict[str, Any]) -> tuple[str, str]:
    status_line = f'NOVA_PROMOTED_SKILL_RESULT id=history_statistics ok entity_id={fixture["entity_id"]} period={fixture["period"]}'
    task = f"""Summarize the longer-term trend for `{fixture["entity_id"]}` using statistics, not a wide raw history scan.
Use the exact bounded window `{fixture["start"]}` to `{fixture["end"]}` and aggregation period `{fixture["period"]}`.
Use the recorder statistics WebSocket command only for the statistics read.
Keep this flow read-only and bounded.
Return the normal history-skill shape with `Target`, `Window`, `Summary`, `Key periods`, and `Next step`.
"""
    return base_prompt(ROOT / "skills" / "history" / "SKILL.md", "history", task, status_line), status_line


def validate_dashboard_lifecycle(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, _final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    if count_ws_mentions(text, "lovelace/dashboards/list") < 1:
        errors.append("dashboard_list_readback_missing")
    for token, error in (
        ("lovelace/dashboards/create", "dashboard_create_missing"),
        ("lovelace/dashboards/update", "dashboard_metadata_update_missing"),
        ("lovelace/config/save", "dashboard_save_missing"),
    ):
        if token not in text:
            errors.append(error)
    if count_ws_mentions(text, "lovelace/config") < 2:
        errors.append("dashboard_config_readback_missing")
    if "lovelace/config/delete" in text:
        errors.append("forbidden_dashboard_config_delete")
    if "ha-nova relay core" in text:
        errors.append("forbidden_dashboard_core_usage")

    dashboards = relay_ws({"type": "lovelace/dashboards/list"}).get("data", [])
    matched = next((item for item in dashboards if item.get("url_path") == fixture["url_path"]), None)
    if not matched:
        errors.append("dashboard_missing_after_lifecycle")
        return errors
    if matched.get("mode") != "storage":
        errors.append("dashboard_mode_not_storage")
    if matched.get("title") != fixture["final_title"]:
        errors.append("dashboard_title_mismatch")
    if matched.get("icon") != "mdi:view-dashboard-edit":
        errors.append("dashboard_icon_mismatch")
    if matched.get("show_in_sidebar") is not True:
        errors.append("dashboard_sidebar_flag_mismatch")
    if matched.get("require_admin") is not False:
        errors.append("dashboard_admin_flag_mismatch")

    config = relay_ws({"type": "lovelace/config", "url_path": fixture["url_path"]}).get("data", {})
    views = config.get("views", [])
    if not views or views[0].get("title") != fixture["view_title"]:
        errors.append("dashboard_view_title_mismatch")
    return errors


def validate_dashboard_card_flow(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, _final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    if count_ws_mentions(text, "lovelace/config") < 2:
        errors.append("dashboard_card_inventory_missing")
    if count_ws_mentions(text, "lovelace/config/save") < 3:
        errors.append("dashboard_card_save_sequence_missing")
    if "lovelace/dashboards/delete" in text or "lovelace/config/delete" in text:
        errors.append("dashboard_card_flow_should_not_delete_dashboard")

    config = relay_ws({"type": "lovelace/config", "url_path": fixture["url_path"]}).get("data", {})
    views = config.get("views", [])
    if not views:
        errors.append("dashboard_card_view_missing")
        return errors
    cards = views[0].get("cards", [])
    titles = [card.get("title") for card in cards]
    if titles != [fixture["move_title"], fixture["stay_title"]]:
        errors.append("dashboard_card_order_mismatch")
    if any(title == fixture["transient_title"] for title in titles):
        errors.append("dashboard_transient_card_still_present")
    return errors


def validate_dashboard_resource_flow(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, _final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    for token, error in (
        ("lovelace/resources", "dashboard_resource_list_missing"),
        ("lovelace/resources/create", "dashboard_resource_create_missing"),
        ("lovelace/resources/update", "dashboard_resource_update_missing"),
    ):
        if token not in text:
            errors.append(error)
    if "lovelace/resources/delete" in text:
        errors.append("unexpected_dashboard_resource_delete")

    resources = relay_ws({"type": "lovelace/resources"}).get("data", [])
    matches = [item for item in resources if item.get("url") == fixture["updated_url"]]
    if len(matches) != 1:
        errors.append("dashboard_updated_resource_missing")
        return errors
    if matches[0].get("type") != fixture["updated_type"]:
        errors.append("dashboard_resource_type_mismatch")
    fixture["resource_id"] = matches[0]["id"]
    return errors


def validate_dashboard_delete_token(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, _final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    if "lovelace/dashboards/delete" not in text:
        errors.append("dashboard_delete_missing")
    if "lovelace/config/delete" in text:
        errors.append("forbidden_dashboard_config_delete")
    if "lovelace/config/save" in text:
        errors.append("unexpected_dashboard_save")
    dashboards = relay_ws({"type": "lovelace/dashboards/list"}).get("data", [])
    if any(item.get("url_path") == fixture["url_path"] for item in dashboards):
        errors.append("dashboard_still_present_after_delete")
    return errors


def validate_dashboard_delete_reject(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    if "lovelace/dashboards/delete" in text or "lovelace/config/delete" in text:
        errors.append("dashboard_delete_should_not_run")
    dashboards = relay_ws({"type": "lovelace/dashboards/list"}).get("data", [])
    if not any(item.get("url_path") == fixture["url_path"] for item in dashboards):
        errors.append("dashboard_missing_after_refusal")
    if fixture["token"] not in final_message:
        errors.append("delete_token_not_repeated_in_refusal")
    return errors


def validate_organize_category_flow(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, _final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    for token, error in (
        ("config/category_registry/list", "category_list_missing"),
        ("config/category_registry/create", "category_create_missing"),
        ("config/category_registry/update", "category_update_missing"),
        ("config/entity_registry/update", "entity_category_update_missing"),
    ):
        if token not in text:
            errors.append(error)
    if count_ws_mentions(text, "config/entity_registry/get") < 1:
        errors.append("entity_readback_missing")
    if "config/category_registry/delete" in text:
        errors.append("unexpected_category_delete")
    if "ha-nova relay core" in text:
        errors.append("forbidden_organize_core_usage")

    categories = relay_ws({"type": "config/category_registry/list", "scope": fixture["scope"]}).get("data", [])
    matched = next((item for item in categories if item.get("name") == fixture["renamed_name"]), None)
    if not matched:
        errors.append("category_missing_after_flow")
        return errors
    entity = relay_ws({"type": "config/entity_registry/get", "entity_id": fixture["entity_id"]}).get("data", {})
    if entity.get("categories", {}).get(fixture["scope"]) is not None:
        errors.append("entity_category_not_cleared")
    fixture["category_id"] = matched["category_id"]
    return errors


def validate_organize_floor_area_flow(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, _final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    for token, error in (
        ("config/floor_registry/create", "floor_create_missing"),
        ("config/floor_registry/update", "floor_update_missing"),
        ("config/area_registry/create", "area_create_missing"),
        ("config/area_registry/update", "area_update_missing"),
    ):
        if token not in text:
            errors.append(error)

    floors = relay_ws({"type": "config/floor_registry/list"}).get("data", [])
    floor = next((item for item in floors if item.get("name") == fixture["floor_final_name"]), None)
    if not floor:
        errors.append("final_floor_missing")
    else:
        fixture["floor_id"] = floor["floor_id"]
        if floor.get("level") != fixture["floor_final_level"]:
            errors.append("final_floor_level_mismatch")
        if floor.get("icon") != fixture["floor_final_icon"]:
            errors.append("final_floor_icon_mismatch")

    areas = relay_ws({"type": "config/area_registry/list"}).get("data", [])
    area = next((item for item in areas if item.get("name") == fixture["area_final_name"]), None)
    if not area:
        errors.append("final_area_missing")
    else:
        fixture["area_id"] = area["area_id"]
        if floor and area.get("floor_id") != floor.get("floor_id"):
            errors.append("final_area_floor_mismatch")
        if area.get("icon") != fixture["area_final_icon"]:
            errors.append("final_area_icon_mismatch")
        if area.get("picture") != fixture["area_final_picture"]:
            errors.append("final_area_picture_mismatch")
    return errors


def validate_organize_label_entity_flow(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, _final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    for token, error in (
        ("config/label_registry/create", "label_create_missing"),
        ("config/label_registry/update", "label_update_missing"),
        ("config/entity_registry/update", "entity_metadata_update_missing"),
    ):
        if token not in text:
            errors.append(error)
    entity_get_reads = count_ws_mentions(text, "config/entity_registry/get") + len(
        re.findall(r"(?:^|[\s\"'])\S*entity[-_]get\S*\.json", text)
    ) + len(
        re.findall(r"\{entity_id,labels,aliases\}", text)
    )
    if entity_get_reads < 2:
        errors.append("entity_metadata_readback_missing")

    labels = relay_ws({"type": "config/label_registry/list"}).get("data", [])
    label = next((item for item in labels if item.get("name") == fixture["label_final_name"]), None)
    if not label:
        errors.append("final_label_missing")
    else:
        fixture["label_id"] = label["label_id"]
        if label.get("color") != fixture["label_final_color"]:
            errors.append("final_label_color_mismatch")
        if label.get("icon") != fixture["label_final_icon"]:
            errors.append("final_label_icon_mismatch")
        if label.get("description") != fixture["label_final_description"]:
            errors.append("final_label_description_mismatch")

    entity = relay_ws({"type": "config/entity_registry/get", "entity_id": fixture["entity_id"]}).get("data", {})
    if entity.get("labels", []):
        errors.append("entity_labels_not_cleared_after_flow")
    if entity.get("aliases", []):
        errors.append("entity_aliases_not_cleared_after_flow")
    return errors


def validate_organize_category_delete(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, _final_message = common_errors(events, invalid_lines, status_line)
    text = "\n".join(item.get("command", "") for item in commands)
    if "config/category_registry/delete" not in text:
        errors.append("category_delete_missing")
    if count_ws_mentions(text, "config/category_registry/list") < 1:
        errors.append("category_delete_readback_missing")
    if count_ws_mentions(text, "config/entity_registry/get") < 1:
        errors.append("entity_cleanup_readback_missing")
    categories = relay_ws({"type": "config/category_registry/list", "scope": fixture["scope"]}).get("data", [])
    if any(item.get("category_id") == fixture["category_id"] for item in categories):
        errors.append("category_still_present_after_delete")
    entity = relay_ws({"type": "config/entity_registry/get", "entity_id": fixture["entity_id"]}).get("data", {})
    if entity.get("categories", {}).get(fixture["scope"]) is not None:
        errors.append("entity_category_scope_not_cleared_after_delete")
    return errors


def validate_history(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, final_message = common_errors(events, invalid_lines, status_line)
    transcript = transcript_text(commands)
    command_text = "\n".join(item.get("command", "") for item in commands)
    history_re = re.compile(
        rf"/api/history/period/[^\"'\s?]+\?(?=[^\"'\s]*filter_entity_id={re.escape(fixture['entity_id'])})(?=[^\"'\s]*end_time={re.escape(fixture['end'])})"
    )
    logbook_re = re.compile(
        rf"/api/logbook/[^\"'\s?]+\?(?=[^\"'\s]*entity={re.escape(fixture['entity_id'])})(?=[^\"'\s]*end_time={re.escape(fixture['end'])})"
    )
    if not history_re.search(transcript):
        errors.append("bounded_history_call_missing")
    if not logbook_re.search(transcript):
        errors.append("bounded_logbook_call_missing")
    if re.search(r"\bha-nova relay ws\b", command_text):
        errors.append("unexpected_history_ws_usage")
    if re.search(r"\bha-nova relay core\b[^\n]*--method\s+(?:POST|PUT|PATCH|DELETE)\b", command_text):
        errors.append("unexpected_mutating_core_usage")
    for section in ("Target", "Window", "Summary", "Next step"):
        if section not in final_message:
            errors.append(f"history_output_missing_{section.lower().replace(' ', '_')}")
    if "Key events" not in final_message and "Key transitions" not in final_message:
        errors.append("history_output_missing_key_section")
    return errors


def validate_history_statistics(events: list[dict[str, Any]], invalid_lines: list[str], fixture: dict[str, Any], status_line: str) -> list[str]:
    errors, commands, _messages, final_message = common_errors(events, invalid_lines, status_line)
    command_text = "\n".join(item.get("command", "") for item in commands)
    if "recorder/statistics_during_period" not in command_text:
        errors.append("statistics_ws_call_missing")
    if re.search(r"\bha-nova relay core\b[^\n]*--method\s+(?:POST|PUT|PATCH|DELETE)\b", command_text):
        errors.append("unexpected_mutating_core_usage")
    if "Key periods" not in final_message:
        errors.append("history_statistics_output_missing_key_periods")
    for section in ("Target", "Window", "Summary", "Next step"):
        if section not in final_message:
            errors.append(f"history_statistics_output_missing_{section.lower().replace(' ', '_')}")
    return errors


def run_case(scenario_id: str, prompt: str, status_line: str, validator, fixture: dict[str, Any]) -> ScenarioResult:
    raw_log = LOG_DIR / f"{scenario_id}.jsonl"
    codex_exit = run_codex(prompt, raw_log, f"NOVA_PROMOTED_SKILL_RESULT id={scenario_id} ")
    events, invalid_lines = load_events(raw_log)
    errors = validator(events, invalid_lines, fixture, status_line)
    if codex_exit != 0:
        errors.append(f"codex_exit_{codex_exit}")
    return ScenarioResult(
        scenario_id=scenario_id,
        status="pass" if not errors else "fail",
        errors=errors,
        codex_exit=codex_exit,
        raw_log=str(raw_log),
    )


def main(argv: list[str]) -> int:
    require_cmd("codex")
    require_cmd("ha-nova")
    require_cmd("trash")
    LOG_DIR.mkdir(parents=True, exist_ok=True)

    if "--list-scenarios" in argv:
        for scenario in SCENARIO_ORDER:
            print(scenario)
        return 0
    if "--cleanup-only" in argv:
        cleanup_promoted_residue()
        return 0

    requested = parse_requested_scenarios(argv)
    fixtures: dict[str, dict[str, Any]] = {}
    results: list[ScenarioResult] = []
    cleanup_dashboards: list[dict[str, Any]] = []
    cleanup_resources: list[str] = []
    cleanup_categories: list[dict[str, Any]] = []
    cleanup_areas: list[str] = []
    cleanup_floors: list[str] = []
    cleanup_labels: list[str] = []
    entity_metadata_restores: list[dict[str, Any]] = []
    exit_code = 1

    cleanup_promoted_residue()

    try:
        if "dashboard_storage_lifecycle" in requested:
            suffix = str(int(time.time()))
            fixture = {
                "url_path": f"nova-codex-storage-{suffix}",
                "initial_title": f"NOVA Codex Storage {suffix}",
                "final_title": f"NOVA Codex Storage Updated {suffix}",
                "view_title": "NOVA Codex Created View",
            }
            fixtures["dashboard_storage_lifecycle"] = fixture
            prompt, status_line = build_dashboard_lifecycle_prompt(fixture)
            result = run_case("dashboard_storage_lifecycle", prompt, status_line, validate_dashboard_lifecycle, fixture)
            results.append(result)
            dashboards = relay_ws({"type": "lovelace/dashboards/list"}).get("data", [])
            matched = next((item for item in dashboards if item.get("url_path") == fixture["url_path"]), None)
            if matched:
                cleanup_dashboards.append({"dashboard_id": matched["id"], "url_path": fixture["url_path"]})

        if "dashboard_card_flow" in requested:
            suffix = str(int(time.time()))
            fixture = dashboard_card_seed(f"nova-codex-cards-{suffix}", f"NOVA Codex Cards {suffix}", "NOVA Codex Card View")
            fixture["transient_title"] = "NOVA Codex Inserted Card"
            fixture["transient_content"] = "inserted"
            fixture["move_title"] = "Beta Card"
            fixture["stay_title"] = "Alpha Card"
            fixtures["dashboard_card_flow"] = fixture
            cleanup_dashboards.append(fixture)
            prompt, status_line = build_dashboard_card_flow_prompt(fixture)
            results.append(run_case("dashboard_card_flow", prompt, status_line, validate_dashboard_card_flow, fixture))

        if "dashboard_resource_flow" in requested:
            suffix = str(int(time.time()))
            fixture = {
                "initial_type": "module",
                "initial_url": f"/local/nova-codex-resource-{suffix}.js",
                "updated_type": "css",
                "updated_url": f"/local/nova-codex-resource-{suffix}.css",
            }
            fixtures["dashboard_resource_flow"] = fixture
            prompt, status_line = build_dashboard_resource_flow_prompt(fixture)
            result = run_case("dashboard_resource_flow", prompt, status_line, validate_dashboard_resource_flow, fixture)
            results.append(result)
            if fixture.get("resource_id"):
                cleanup_resources.append(fixture["resource_id"])

        if "dashboard_delete_token" in requested:
            suffix = str(int(time.time()))
            fixture = dashboard_seed(f"nova-codex-delete-{suffix}", f"NOVA Codex Delete {suffix}", with_config=True)
            fixture["token"] = f"confirm:del-dashboard-{suffix}"
            fixtures["dashboard_delete_token"] = fixture
            cleanup_dashboards.append(fixture)
            prompt, status_line = build_dashboard_delete_token_prompt(fixture)
            results.append(run_case("dashboard_delete_token", prompt, status_line, validate_dashboard_delete_token, fixture))

        if "dashboard_delete_reject_natural" in requested:
            suffix = str(int(time.time()))
            fixture = dashboard_seed(f"nova-codex-refuse-{suffix}", f"NOVA Codex Refuse {suffix}", with_config=True)
            fixture["token"] = f"confirm:del-dashboard-refuse-{suffix}"
            fixtures["dashboard_delete_reject_natural"] = fixture
            prompt, status_line = build_dashboard_delete_reject_prompt(fixture)
            results.append(run_case("dashboard_delete_reject_natural", prompt, status_line, validate_dashboard_delete_reject, fixture))
            cleanup_dashboards.append(fixture)

        entity_fixture: dict[str, Any] | None = None
        if "organize_category_flow" in requested or "organize_category_delete_token" in requested:
            entity_fixture = discover_entity_fixture()

        if "organize_category_flow" in requested:
            suffix = str(int(time.time()))
            fixture = {
                "scope": f"nova_codex_scope_{suffix}",
                "initial_name": f"NOVA Codex Category {suffix}",
                "renamed_name": f"NOVA Codex Category Renamed {suffix}",
                "entity_id": entity_fixture["entity_id"],
            }
            fixtures["organize_category_flow"] = fixture
            prompt, status_line = build_organize_category_flow_prompt(fixture)
            result = run_case("organize_category_flow", prompt, status_line, validate_organize_category_flow, fixture)
            results.append(result)
            cleanup_categories.append(fixture)

        if "organize_floor_area_flow" in requested or "organize_label_entity_flow" in requested:
            suffix = str(int(time.time()))
            base_fixture = {
                "floor_name": f"NOVA Codex Floor {suffix}",
                "floor_final_name": f"NOVA Codex Floor Final {suffix}",
                "floor_id_hint": f"nova_codex_floor_{suffix}",
                "floor_level": 1,
                "floor_final_level": 2,
                "floor_icon": "mdi:home-floor-1",
                "floor_final_icon": "mdi:home-floor-2",
                "floor_aliases": ["upper"],
                "floor_final_aliases": ["main level"],
                "area_name": f"NOVA Codex Area {suffix}",
                "area_final_name": f"NOVA Codex Area Final {suffix}",
                "area_id_hint": f"nova_codex_area_{suffix}",
                "area_icon": "mdi:room-service",
                "area_final_icon": "mdi:sofa",
                "area_picture": "https://example.invalid/nova-codex-area-initial.png",
                "area_final_picture": "https://example.invalid/nova-codex-area-final.png",
                "area_aliases": ["zone alpha"],
                "area_final_aliases": ["zone beta"],
                "label_name": f"NOVA Codex Label {suffix}",
                "label_final_name": f"NOVA Codex Label Final {suffix}",
                "label_id_hint": f"nova_codex_label_{suffix}",
                "label_color": "blue",
                "label_final_color": "green",
                "label_icon": "mdi:tag",
                "label_final_icon": "mdi:label",
                "label_description": "initial organize proof label",
                "label_final_description": "final organize proof label",
            }
        if "organize_floor_area_flow" in requested:
            fixture = dict(base_fixture)
            fixtures["organize_floor_area_flow"] = fixture
            prompt, status_line = build_organize_floor_area_flow_prompt(fixture)
            result = run_case("organize_floor_area_flow", prompt, status_line, validate_organize_floor_area_flow, fixture)
            results.append(result)
            if fixture.get("area_id"):
                cleanup_areas.append(fixture["area_id"])
            if fixture.get("floor_id"):
                cleanup_floors.append(fixture["floor_id"])

        if "organize_label_entity_flow" in requested:
            clean_entity_fixture = discover_clean_entity_fixture()
            fixture = dict(base_fixture)
            fixture["entity_id"] = clean_entity_fixture["entity_id"]
            fixture["entity_aliases"] = ["nova codex alias"]
            fixtures["organize_label_entity_flow"] = fixture
            entity_metadata_restores.append(
                {
                    "entity_id": clean_entity_fixture["entity_id"],
                    "labels": clean_entity_fixture.get("labels", []),
                    "aliases": clean_entity_fixture.get("aliases", []),
                }
            )
            prompt, status_line = build_organize_label_entity_flow_prompt(fixture)
            result = run_case("organize_label_entity_flow", prompt, status_line, validate_organize_label_entity_flow, fixture)
            results.append(result)
            if fixture.get("label_id"):
                cleanup_labels.append(fixture["label_id"])

        if "organize_category_delete_token" in requested:
            suffix = str(int(time.time()))
            scope = f"nova_codex_scope_delete_{suffix}"
            category = category_seed(scope, f"NOVA Codex Delete Category {suffix}", entity_id=entity_fixture["entity_id"])
            fixture = {
                "scope": scope,
                "category_id": category["category_id"],
                "entity_id": entity_fixture["entity_id"],
                "token": f"confirm:del-category-{suffix}",
            }
            fixtures["organize_category_delete_token"] = fixture
            cleanup_categories.append(fixture)
            prompt, status_line = build_organize_category_delete_prompt(fixture)
            results.append(run_case("organize_category_delete_token", prompt, status_line, validate_organize_category_delete, fixture))

        if "history_timeline" in requested:
            fixture = history_fixture()
            fixtures["history_timeline"] = fixture
            prompt, status_line = build_history_prompt(fixture)
            results.append(run_case("history_timeline", prompt, status_line, validate_history, fixture))

        if "history_statistics" in requested:
            fixture = statistics_fixture()
            fixtures["history_statistics"] = fixture
            prompt, status_line = build_history_statistics_prompt(fixture)
            results.append(run_case("history_statistics", prompt, status_line, validate_history_statistics, fixture))
        with RESULTS_FILE.open("w", encoding="utf-8") as handle:
            for result in results:
                handle.write(
                    json.dumps(
                        {
                            "scenario_id": result.scenario_id,
                            "status": result.status,
                            "errors": result.errors,
                            "codex_exit": result.codex_exit,
                            "raw_log": result.raw_log,
                        }
                    )
                    + "\n"
                )

        summary = {
            "requested": requested,
            "fixtures": fixtures,
            "results": [
                {
                    "scenario_id": result.scenario_id,
                    "status": result.status,
                    "errors": result.errors,
                    "codex_exit": result.codex_exit,
                    "raw_log": result.raw_log,
                }
                for result in results
            ],
            "passed": sum(1 for result in results if result.status == "pass"),
            "failed": sum(1 for result in results if result.status == "fail"),
            "results_file": str(RESULTS_FILE),
        }
        SUMMARY_FILE.write_text(json.dumps(summary, indent=2), encoding="utf-8")

        for result in results:
            suffix = "" if not result.errors else f" errors={json.dumps(result.errors)}"
            log(f"{result.scenario_id}: {result.status}{suffix}")
        log(f"Summary: {SUMMARY_FILE}")
        exit_code = 0 if all(result.status == "pass" for result in results) else 1
    finally:
        for restore in entity_metadata_restores:
            cleanup_entity_metadata(restore["entity_id"], restore["labels"], restore["aliases"])
        for category_fixture in cleanup_categories:
            scope = category_fixture["scope"]
            cleanup_entity_category_scope(scope)
            cleanup_category(scope, category_fixture.get("category_id"))
        for label_id in cleanup_labels:
            cleanup_label(label_id)
        for area_id in cleanup_areas:
            cleanup_area(area_id)
        for floor_id in cleanup_floors:
            cleanup_floor(floor_id)
        for resource_id in cleanup_resources:
            cleanup_resource(resource_id)
        for dashboard in cleanup_dashboards:
            cleanup_dashboard(dashboard)
        cleanup_promoted_residue()

    return exit_code


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
