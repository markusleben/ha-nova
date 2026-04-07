#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import signal
import shutil
import subprocess
import sys
import tempfile
from datetime import datetime
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[2]
SCENARIO_SCRIPT = ROOT / "scripts" / "e2e" / "codex-ha-nova-promoted-live-e2e.py"
OUTPUT_ROOT_ENV = os.environ.get("OUTPUT_DIR")
OUTPUT_ROOT = Path(OUTPUT_ROOT_ENV) if OUTPUT_ROOT_ENV else Path(tempfile.mkdtemp(prefix="ha-nova-promoted-suite."))
CREATED_OUTPUT_ROOT = OUTPUT_ROOT_ENV is None
RUN_ID = datetime.now().strftime("%Y%m%d-%H%M%S")
SUMMARY_FILE = OUTPUT_ROOT / f"summary-{RUN_ID}.json"
RESULTS_FILE = OUTPUT_ROOT / f"results-{RUN_ID}.ndjson"
KEEP_OUTPUT = os.environ.get("PROMOTED_SUITE_KEEP_OUTPUT") == "1"
SUITE_DISCOVERY_TIMEOUT_SEC = int(os.environ.get("PROMOTED_SUITE_DISCOVERY_TIMEOUT_SEC", "30"))
SUITE_SCENARIO_TIMEOUT_SEC = int(os.environ.get("PROMOTED_SUITE_SCENARIO_TIMEOUT_SEC", "540"))
SUITE_CLEANUP_TIMEOUT_SEC = int(os.environ.get("PROMOTED_SUITE_CLEANUP_TIMEOUT_SEC", "120"))
SMOKE_SCENARIOS = (
    "dashboard_storage_lifecycle",
    "organize_label_entity_flow",
    "history_statistics",
)
PROMOTED_SCOPE_RE = re.compile(r"nova_codex_scope(?:_delete)?_\d+")
PROMOTED_DASHBOARD_PATH_RE = re.compile(r"^nova-codex-")
PROMOTED_DASHBOARD_ID_RE = re.compile(r"^nova_codex_")
PROMOTED_RESOURCE_URL_RE = re.compile(r"^/local/nova-codex-[a-z0-9-]+\.(?:js|css)$")
PROMOTED_AREA_RE = re.compile(r"^nova_codex_area_\d+$")
PROMOTED_FLOOR_RE = re.compile(r"^nova_codex_floor_\d+$")
PROMOTED_LABEL_RE = re.compile(r"^nova_codex_label_\d+$")


def log(message: str) -> None:
    print(f"[codex-promoted-live-suite] {message}", flush=True)


def die(message: str) -> None:
    raise SystemExit(f"[codex-promoted-live-suite] {message}")


def require_cmd(command: str) -> None:
    if shutil.which(command) is None:
        die(f"Required command not found: {command}")


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


def run_python(args: list[str], env: dict[str, str] | None = None, timeout_sec: int | None = None) -> subprocess.CompletedProcess[str]:
    popen_kwargs: dict[str, object] = {
        "cwd": ROOT,
        "text": True,
        "stdout": subprocess.PIPE,
        "stderr": subprocess.PIPE,
        "env": env,
    }
    if os.name == "nt":
        popen_kwargs["creationflags"] = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
    else:
        popen_kwargs["start_new_session"] = True

    process = subprocess.Popen([sys.executable, str(SCENARIO_SCRIPT), *args], **popen_kwargs)
    try:
        stdout, stderr = process.communicate(timeout=timeout_sec)
        return subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr)
    except subprocess.TimeoutExpired as exc:
        stop_process_group(process, signal.SIGTERM)
        try:
            stdout, stderr = process.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            stop_process_group(process, signal.SIGKILL)
            stdout, stderr = process.communicate()
        return subprocess.CompletedProcess(
            process.args,
            124,
            exc.stdout or stdout or "",
            exc.stderr or stderr or "",
        )


def discover_all_scenarios() -> list[str]:
    result = run_python(["--list-scenarios"], timeout_sec=SUITE_DISCOVERY_TIMEOUT_SEC)
    if result.returncode != 0:
        raise RuntimeError(result.stderr or result.stdout or "failed to list scenarios")
    return [line.strip() for line in result.stdout.splitlines() if line.strip()]


def parse_requested(argv: list[str]) -> list[str]:
    all_scenarios = discover_all_scenarios()
    if not argv or argv == ["--full"]:
        return all_scenarios
    if argv == ["--smoke"]:
        return list(SMOKE_SCENARIOS)
    invalid = [arg for arg in argv if arg not in all_scenarios]
    if invalid:
        die("Unknown scenario(s): " + ", ".join(invalid))
    return argv


def relay_ws(payload: dict[str, Any]) -> dict[str, Any]:
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as handle:
        json.dump(payload, handle)
        payload_path = Path(handle.name)
    try:
        raw = subprocess.check_output(
            ["ha-nova", "relay", "ws", "--data-file", str(payload_path)],
            cwd=ROOT,
            text=True,
        )
        parsed = json.loads(raw)
        if not parsed.get("ok"):
            raise subprocess.CalledProcessError(1, "ha-nova relay ws", output=raw)
        return parsed
    finally:
        payload_path.unlink(missing_ok=True)


def collect_residue() -> dict[str, Any]:
    dashboards = relay_ws({"type": "lovelace/dashboards/list"}).get("data", [])
    resources = relay_ws({"type": "lovelace/resources"}).get("data", [])
    areas = relay_ws({"type": "config/area_registry/list"}).get("data", [])
    floors = relay_ws({"type": "config/floor_registry/list"}).get("data", [])
    labels = relay_ws({"type": "config/label_registry/list"}).get("data", [])
    entities = relay_ws({"type": "config/entity_registry/list"}).get("data", [])
    scopes: set[str] = set()
    entity_residue: list[dict[str, Any]] = []
    for entity in entities:
        categories = entity.get("categories") or {}
        matched_scopes = [scope for scope in categories.keys() if PROMOTED_SCOPE_RE.search(scope)]
        scopes.update(matched_scopes)
        labels_value = entity.get("labels") or []
        aliases_value = entity.get("aliases") or []
        if matched_scopes or any(PROMOTED_LABEL_RE.match(str(label)) for label in labels_value) or any(
            "nova codex" in str(alias).lower() for alias in aliases_value
        ):
            entity_residue.append(
                {
                    "entity_id": entity.get("entity_id"),
                    "labels": labels_value,
                    "aliases": aliases_value,
                    "categories": categories,
                }
            )
    return {
        "dashboards": [
            item
            for item in dashboards
            if PROMOTED_DASHBOARD_ID_RE.match(str(item.get("id", "")))
            or PROMOTED_DASHBOARD_PATH_RE.match(str(item.get("url_path", "")))
        ],
        "resources": [item for item in resources if PROMOTED_RESOURCE_URL_RE.match(str(item.get("url", "")))],
        "areas": [item for item in areas if PROMOTED_AREA_RE.match(str(item.get("area_id", "")))],
        "floors": [item for item in floors if PROMOTED_FLOOR_RE.match(str(item.get("floor_id", "")))],
        "labels": [item for item in labels if PROMOTED_LABEL_RE.match(str(item.get("label_id", "")))],
        "entity_category_scopes": sorted(scopes),
        "entity_residue": entity_residue,
    }


def residue_empty(residue: dict[str, Any]) -> bool:
    return all(not residue[key] for key in residue)


def main(argv: list[str]) -> int:
    require_cmd("ha-nova")
    require_cmd("trash")
    scenarios = parse_requested(argv)
    OUTPUT_ROOT.mkdir(parents=True, exist_ok=True)

    cleanup = run_python(["--cleanup-only"], timeout_sec=SUITE_CLEANUP_TIMEOUT_SEC)
    if cleanup.returncode != 0:
        die("initial promoted cleanup failed")

    results: list[dict[str, Any]] = []
    try:
        for index, scenario in enumerate(scenarios, start=1):
            log(f"running {scenario}")
            scenario_dir = OUTPUT_ROOT / f"{index:02d}-{scenario}"
            env = os.environ.copy()
            env["OUTPUT_DIR"] = str(scenario_dir)
            result = run_python([scenario], env=env, timeout_sec=SUITE_SCENARIO_TIMEOUT_SEC)
            if result.stdout:
                print(result.stdout, end="")
            if result.stderr:
                print(result.stderr, end="", file=sys.stderr)
            summary_files = sorted(scenario_dir.glob("summary-*.json"))
            if not summary_files:
                results.append(
                    {
                        "scenario_id": scenario,
                        "status": "fail",
                        "errors": ["summary_missing"],
                        "exit_code": result.returncode,
                        "summary_file": None,
                    }
                )
                continue
            parsed = json.loads(summary_files[-1].read_text())
            scenario_result = (parsed.get("results") or [{}])[0]
            results.append(
                {
                    "scenario_id": scenario,
                    "status": scenario_result.get("status", "fail"),
                    "errors": scenario_result.get("errors", []),
                    "exit_code": result.returncode,
                    "summary_file": str(summary_files[-1]),
                }
            )

        cleanup = run_python(["--cleanup-only"], timeout_sec=SUITE_CLEANUP_TIMEOUT_SEC)
        residue = collect_residue()
        summary = {
            "requested": scenarios,
            "results": results,
            "passed": sum(1 for item in results if item["status"] == "pass"),
            "failed": sum(1 for item in results if item["status"] != "pass"),
            "cleanup_exit_code": cleanup.returncode,
            "residue": residue,
        }
        with RESULTS_FILE.open("w", encoding="utf-8") as handle:
            for item in results:
                handle.write(json.dumps(item) + "\n")
        SUMMARY_FILE.write_text(json.dumps(summary, indent=2), encoding="utf-8")
        log(f"Summary: {SUMMARY_FILE}")
        exit_code = 0 if summary["failed"] == 0 and cleanup.returncode == 0 and residue_empty(residue) else 1
        if exit_code == 0 and not KEEP_OUTPUT and CREATED_OUTPUT_ROOT:
            subprocess.run(["trash", str(OUTPUT_ROOT)], cwd=ROOT, check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return exit_code
    finally:
        run_python(["--cleanup-only"], timeout_sec=SUITE_CLEANUP_TIMEOUT_SEC)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
