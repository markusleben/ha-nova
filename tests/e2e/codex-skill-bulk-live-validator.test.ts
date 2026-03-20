import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

type PythonRuntimeCandidate = [command: string, prefixArgs: string[]];

function runPythonValidator(script: string) {
  const candidates: PythonRuntimeCandidate[] = process.platform === "win32"
    ? [["py", ["-3"]], ["python", []], ["python3", []]]
    : [["python3", []], ["python", []], ["py", ["-3"]]];

  for (const [command, prefixArgs] of candidates) {
    const result = spawnSync(command, [...prefixArgs, "-c", script], {
      cwd: process.cwd(),
      encoding: "utf8",
    });
    if (result.error && "code" in result.error && result.error.code === "ENOENT") {
      continue;
    }
    if (result.error) {
      throw result.error;
    }
    if (result.status !== 0) {
      throw new Error(result.stderr || result.stdout || `Python exited with ${result.status}`);
    }
    return JSON.parse(result.stdout) as { status: string; errors: string[] };
  }

  throw new Error("Python 3 runtime not found");
}

describe("codex bulk live validator", () => {
  it("anchors bulk prompts to repo-local skill files instead of installed copies", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

review_prompt = module.build_review_prompt("review_area", "Review all automations in area Arbeitszimmer.")
inventory_prompt = module.build_inventory_prompt("inventory_area", "Show all automations in area Arbeitszimmer.")

checks = {
    "review_repo_skill": str(module.REVIEW_SKILL_FILE) in review_prompt,
    "read_repo_skill": str(module.READ_SKILL_FILE) in inventory_prompt,
    "bulk_patterns": str(module.BULK_PATTERNS_FILE) in review_prompt and str(module.BULK_PATTERNS_FILE) in inventory_prompt,
    "installed_copy_ban": "~/.local/share/ha-nova" in review_prompt and "~/.agents/skills" in review_prompt,
}

errors = [name for name, ok in checks.items() if not ok]
print(json.dumps({"status": "pass" if not errors else "fail", "errors": errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("keeps the review harness prompt shell-neutral for cross-OS runs", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

prompt = module.build_review_prompt("review_area", "Review all automations in area Arbeitszimmer.")
checks = {
    "native_file_writing": "native file-writing flow for the current shell" in prompt,
    "powershell_equivalent": "PowerShell or Windows shells must use their native equivalent" in prompt,
    "shell_native_inspection": "shell-native inspection command" in prompt,
    "same_block_entity_visibility": "same command block that executes that lookup" in prompt,
    "config_read_marker": "Every config-body read must emit a parseable target marker" in prompt,
}

errors = [name for name, ok in checks.items() if not ok]
print(json.dumps({"status": "pass" if not errors else "fail", "errors": errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("keeps draining queued stdout after the child process exits", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
import time
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

class FakeStdout:
    def __init__(self, owner):
        self.owner = owner

    def __iter__(self):
        marker_line = json.dumps({
            "type": "item.completed",
            "item": {"type": "agent_message", "text": "Scope\\nNOVA_MARKER"}
        }) + "\\n"
        trailing_line = json.dumps({
            "type": "item.completed",
            "item": {"type": "agent_message", "text": "late_line"}
        }) + "\\n"
        yield marker_line
        self.owner.exited = True
        time.sleep(0.3)
        yield trailing_line

class FakeProcess:
    def __init__(self):
        self.exited = False
        self.stdout = FakeStdout(self)
        self.returncode = 0
        self.pid = 12345

    def poll(self):
        return 0 if self.exited else None

    def wait(self, timeout=None):
        self.returncode = 0
        return 0

    def terminate(self):
        self.exited = True

    def kill(self):
        self.exited = True

fake_process = FakeProcess()
module.subprocess.Popen = lambda *args, **kwargs: fake_process
module.stop_process_group = lambda *args, **kwargs: None
module.SCENARIO_TIMEOUT_SEC = 5

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "run.jsonl"
    rc = module.run_codex("prompt", raw_log, "NOVA_MARKER")
    raw_text = raw_log.read_text(encoding="utf-8")
    checks = {
        "exit_code": rc == 0,
        "late_line_drained": "late_line" in raw_text,
    }
    errors = [name for name, ok in checks.items() if not ok]
    print(json.dumps({"status": "pass" if not errors else "fail", "errors": errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts audited config coverage from aggregated read output", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
        "automation.f",
    ],
    "audited": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
    ],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts audited config coverage from heading-delimited config dumps", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
        "automation.f",
    ],
    "audited": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
    ],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n\\n".join([
                "=== automation.a ===\\nunique_id=111\\n{\\"id\\": \\"111\\"}",
                "=== automation.b ===\\nunique_id=222\\n{\\"id\\": \\"222\\"}",
                "=== automation.c ===\\nunique_id=333\\n{\\"id\\": \\"333\\"}",
                "=== automation.d ===\\nunique_id=444\\n{\\"id\\": \\"444\\"}",
                "=== automation.e ===\\nunique_id=555\\n{\\"id\\": \\"555\\"}",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts numbered heading-delimited config dumps from the live workset loop", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
        "automation.f",
    ],
    "audited": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
    ],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n\\n".join([
                "=== 01 automation.a ===\\nunique_id=111\\n{\\"id\\": \\"111\\"}",
                "=== 02 automation.b ===\\nunique_id=222\\n{\\"id\\": \\"222\\"}",
                "=== 03 automation.c ===\\nunique_id=333\\n{\\"id\\": \\"333\\"}",
                "=== 04 automation.d ===\\nunique_id=444\\n{\\"id\\": \\"444\\"}",
                "=== 05 automation.e ===\\nunique_id=555\\n{\\"id\\": \\"555\\"}",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("fails when the transcript swaps the required bulk section headings for localized titles", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
        "automation.f",
    ],
    "audited": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
    ],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Bereich\\nZusammenfassung\\nHochrisiko-Befunde\\nWiederholte Muster\\nGepruefte Elemente\\nKollisionscluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Bereich\\",\\"Zusammenfassung\\",\\"Hochrisiko-Befunde\\",\\"Wiederholte Muster\\",\\"Gepruefte Elemente\\",\\"Kollisionscluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("review_sections_status_line_mismatch");
    expect(result.errors).toContain("review_sections_mismatch");
  });

  it("fails when the status line and body agree on arbitrary headings instead of the required contract", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
        "automation.f",
    ],
    "audited": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
    ],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Alpha\\nBeta\\nGamma\\nDelta\\nEpsilon\\nZeta\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Alpha\\",\\"Beta\\",\\"Gamma\\",\\"Delta\\",\\"Epsilon\\",\\"Zeta\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("review_sections_status_line_mismatch");
    expect(result.errors).toContain("review_sections_mismatch");
  });

  it("fails when the transcript contains relay invalid-json responses", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b"],
    "audited": ["automation.a"],
    "remaining": 1,
    "non_audited": ["automation.b"],
}

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "{\\"ok\\":false,\\"error\\":{\\"code\\":\\"INVALID_JSON\\",\\"message\\":\\"Request body is not valid JSON\\"}}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\nNOVA_BULK_REVIEW_RESULT id=review_area matched=2 audited=1 remaining=1 item_ids=[\\"automation.a\\"] quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]",
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("relay_invalid_json_response");
  });

  it("fails when the transcript uses placeholder payload rewriting", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
        "automation.f",
    ],
    "audited": [
        "automation.a",
        "automation.b",
        "automation.c",
        "automation.d",
        "automation.e",
    ],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "cat <<'EOF' > payload.json\\n{\\"type\\":\\"config/entity_registry/get\\",\\"entity_id\\":\\"REPLACE_ENTITY_ID\\"}\\nEOF\\nENTITY_ID=\\"automation.a\\" perl -0pi -e 's/REPLACE_ENTITY_ID/$ENV{ENTITY_ID}/g' payload.json",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("placeholder_payload_template_detected");
    expect(result.errors).toContain("in_place_tempfile_rewrite_detected");
  });

  it("allows normal heredoc markers without treating them as unresolved placeholders", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "cat <<EOF > payload.json\\n{\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}\\nEOF\\nha-nova relay ws --data-file payload.json --out result.json",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/111 --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts duplicated audit coverage when the unique audited target set still matches", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

coverage_lines = "\\n".join([
    "1 | automation.a | 111 | /tmp/audit-config-1.json",
    "2 | automation.b | 222 | /tmp/audit-config-2.json",
    "3 | automation.c | 333 | /tmp/audit-config-3.json",
    "4 | automation.d | 444 | /tmp/audit-config-4.json",
    "5 | automation.e | 555 | /tmp/audit-config-5.json",
])

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs_1",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/111 --jq-file config_filter.jq --out config.json",
            "aggregated_output": coverage_lines,
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs_2",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/111 --jq-file config_filter.jq --out config.json",
            "aggregated_output": coverage_lines,
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts tab-separated audit index output from the live workset loop", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1\\tautomation.a\\t111\\t/tmp/entity1_config.json",
                "2\\tautomation.b\\t222\\t/tmp/entity2_config.json",
                "3\\tautomation.c\\t333\\t/tmp/entity3_config.json",
                "4\\tautomation.d\\t444\\t/tmp/entity4_config.json",
                "5\\tautomation.e\\t555\\t/tmp/entity5_config.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts plain tab-separated audit index output without numeric prefixes", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "automation.a\\t111\\t/tmp/entity1_config.json",
                "automation.b\\t222\\t/tmp/entity2_config.json",
                "automation.c\\t333\\t/tmp/entity3_config.json",
                "automation.d\\t444\\t/tmp/entity4_config.json",
                "automation.e\\t555\\t/tmp/entity5_config.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("allows explicit negation when the final bulk review says no Quick-Fix was offered", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "## Scope\\n## Summary\\n- Bulk review stayed read-only; no Quick-Fix was offered.\\n"
    "## High-Risk Findings\\n## Repeated Patterns\\n## Items Checked\\n## Collisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1\\tautomation.a\\t111\\t/tmp/entity1_config.json",
                "2\\tautomation.b\\t222\\t/tmp/entity2_config.json",
                "3\\tautomation.c\\t333\\t/tmp/entity3_config.json",
                "4\\tautomation.d\\t444\\t/tmp/entity4_config.json",
                "5\\tautomation.e\\t555\\t/tmp/entity5_config.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts live workset loop output with IDX, ENTITY, and UNIQUE headers", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "IDX=1",
                "ENTITY=automation.a",
                "UNIQUE=111",
                "{\\"alias\\":\\"A\\"}",
                "---",
                "IDX=2",
                "ENTITY=automation.b",
                "UNIQUE=222",
                "{\\"alias\\":\\"B\\"}",
                "---",
                "IDX=3",
                "ENTITY=automation.c",
                "UNIQUE=333",
                "{\\"alias\\":\\"C\\"}",
                "---",
                "IDX=4",
                "ENTITY=automation.d",
                "UNIQUE=444",
                "{\\"alias\\":\\"D\\"}",
                "---",
                "IDX=5",
                "ENTITY=automation.e",
                "UNIQUE=555",
                "{\\"alias\\":\\"E\\"}",
                "---",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts live workset loop output with ITEM[index], UNIQUE[index], and CONFIG[index] headers", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "ITEM[0]=automation.a",
                "UNIQUE[0]=111",
                "CONFIG[0]=/tmp/config-0.json",
                "ITEM[1]=automation.b",
                "UNIQUE[1]=222",
                "CONFIG[1]=/tmp/config-1.json",
                "ITEM[2]=automation.c",
                "UNIQUE[2]=333",
                "CONFIG[2]=/tmp/config-2.json",
                "ITEM[3]=automation.d",
                "UNIQUE[3]=444",
                "CONFIG[3]=/tmp/config-3.json",
                "ITEM[4]=automation.e",
                "UNIQUE[4]=555",
                "CONFIG[4]=/tmp/config-4.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts pipe-separated audit index output with unique_id but without config paths", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1 | automation.a | 111",
                "2 | automation.b | 222",
                "3 | automation.c | 333",
                "4 | automation.d | 444",
                "5 | automation.e | 555",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("fails when a command mixes relay jq with external jq", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay jq --file result.json '.data' | jq '.values'",
            "aggregated_output": "\\n".join([
                "1 | automation.a | 111 | /tmp/audit-config-1.json",
                "2 | automation.b | 222 | /tmp/audit-config-2.json",
                "3 | automation.c | 333 | /tmp/audit-config-3.json",
                "4 | automation.d | 444 | /tmp/audit-config-4.json",
                "5 | automation.e | 555 | /tmp/audit-config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("external_jq_usage_detected");
  });

  it("fails when a transcript includes a mutating websocket message type", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "cat <<'EOF' > payload.json\\n{\\"type\\":\\"automation/reload\\"}\\nEOF\\nha-nova relay ws --data-file payload.json",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("mutation_ws_type_detected");
  });

  it("fails when codex exits non-zero even if the transcript looks complete", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/111 --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1 | automation.a | 111 | /tmp/audit-config-1.json",
                "2 | automation.b | 222 | /tmp/audit-config-2.json",
                "3 | automation.c | 333 | /tmp/audit-config-3.json",
                "4 | automation.d | 444 | /tmp/audit-config-4.json",
                "5 | automation.e | 555 | /tmp/audit-config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 23)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("codex_exit_nonzero:23");
  });

  it("allows one retryable relay read failure when a later relay read succeeds", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "inventory_label",
    "matches": ["automation.a", "automation.b", "automation.c"],
    "displayed": ["automation.a", "automation.b", "automation.c"],
}

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_selector_fail",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --jq-file filter.jq --out result.json {\\"type\\":\\"config/entity_registry/list\\"}",
            "aggregated_output": "[ha-nova] ERROR: context deadline exceeded (Client.Timeout or context cancellation while reading body)",
            "exit_code": 1,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_selector_retry",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out raw.json {\\"type\\":\\"config/entity_registry/list\\"}",
            "aggregated_output": "{\\"ok\\":true,\\"data\\":[]}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_wrapper",
            "type": "command_execution",
            "command": "ha-nova relay jq --file raw.json --jq-file wrapper.jq > result.json",
            "aggregated_output": "{\\"matched\\":3,\\"displayed\\":3,\\"remaining\\":0,\\"values\\":[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\"],\\"rows\\":[]}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": "NOVA_BULK_INVENTORY_RESULT id=inventory_label matched=3 displayed=3 remaining=0 values=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\"]",
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "inventory.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("inventory", "label_inventory", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("accepts markdown heading levels for bulk-review sections", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "## Scope\\n## Summary\\n## High-Risk Findings\\n## Repeated Patterns\\n## Items Checked\\n## Collisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/111 --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1 | automation.a | 111 | /tmp/audit-config-1.json",
                "2 | automation.b | 222 | /tmp/audit-config-2.json",
                "3 | automation.c | 333 | /tmp/audit-config-3.json",
                "4 | automation.d | 444 | /tmp/audit-config-4.json",
                "5 | automation.e | 555 | /tmp/audit-config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("fails when the final message only fakes sections through the status line", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]",
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("missing_section:Scope");
  });

  it("fails when bulk review offers quick-fix text", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\nQuick-Fix available for automation.a\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/111 --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("bulk_quick_fix_text_detected");
  });

  it("fails when bulk review issues a config write", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "**Scope**\\n**Summary**\\n**High-Risk Findings**\\n**Repeated Patterns**\\n**Items Checked**\\n**Collisions by Cluster**\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method POST --path /api/config/automation/config/111 --body-file payload.json",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("mutation_core_method_detected");
  });

  it("accepts stable inventory smoke with exact matched, displayed, and remaining counts", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "inventory_prefix",
    "matches": ["automation.a", "automation.b", "automation.c"],
    "displayed": ["automation.a", "automation.b", "automation.c"],
}

events = [
    {
        "type": "item.started",
        "item": {
            "id": "cmd_inventory",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --jq-file filter.jq --out result.json {\\"type\\":\\"config/entity_registry/list_for_display\\"}",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_inventory",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --jq-file filter.jq --out result.json {\\"type\\":\\"config/entity_registry/list_for_display\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": "NOVA_BULK_INVENTORY_RESULT id=inventory_prefix matched=3 displayed=3 remaining=0 values=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\"]",
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "inventory.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("inventory", "prefix_inventory", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("fails inventory smoke when config reads leak into the shortlist lane", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "inventory_prefix",
    "matches": ["automation.a", "automation.b", "automation.c"],
    "displayed": ["automation.a", "automation.b", "automation.c"],
}

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_inventory",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --jq-file filter.jq --out result.json {\\"type\\":\\"config/entity_registry/list_for_display\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_config",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/111 --jq-file config_filter.jq --out config.json",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": "NOVA_BULK_INVENTORY_RESULT id=inventory_prefix matched=3 displayed=3 remaining=0 values=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\"]",
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "inventory.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("inventory", "prefix_inventory", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("forbidden_command:/api/config/automation/config/");
  });

  it("fails inventory smoke when selector resolution is missing or live state reads appear", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "inventory_prefix",
    "matches": ["automation.a", "automation.b"],
    "displayed": ["automation.a", "automation.b"],
}

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_wrong_selector",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}\\nha-nova relay core --method GET --path /api/states/automation.a",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": "NOVA_BULK_INVENTORY_RESULT id=inventory_prefix matched=2 displayed=2 remaining=0 values=[\\"automation.a\\",\\"automation.b\\"]",
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "inventory.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("inventory", "prefix_inventory", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("selector_resolution_missing");
    expect(result.errors).toContain("forbidden_command:/api/states/");
  });

  it("fails on incomplete transcripts and CLI help probing", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

events = [
    {
        "type": "item.started",
        "item": {
            "id": "cmd_inventory",
            "type": "command_execution",
            "command": "ha-nova relay ws --help",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": "NOVA_BULK_INVENTORY_RESULT id=inventory_prefix matched=0 displayed=0 remaining=0 values=[]",
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "inventory.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("inventory", "prefix_inventory", {"id": "inventory_prefix", "matches": [], "displayed": []}, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("incomplete_transcript");
    expect(result.errors).toContain("cli_help_probe_detected");
  });

  it("fails when review prefetches a non-audited target or reads live state", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/111 --jq-file config_filter.jq --out config.json\\nha-nova relay core --method GET --path /api/states/automation.a",
            "aggregated_output": "\\n".join([
                "1|automation.a|111|/tmp/config-1.json",
                "2|automation.b|222|/tmp/config-2.json",
                "3|automation.c|333|/tmp/config-3.json",
                "4|automation.d|444|/tmp/config-4.json",
                "5|automation.e|555|/tmp/config-5.json",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_prefetch",
            "type": "command_execution",
            "command": "echo automation.f",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("forbidden_command:/api/states/");
    expect(result.errors).toContain("review_prefetch_outside_workset:automation.f");
  });

  it("allows one extra related-config read outside the audited workset for collision classification", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n\\n".join([
                "=== automation.a ===\\nunique_id=111\\n{\\"id\\": \\"111\\"}",
                "=== automation.b ===\\nunique_id=222\\n{\\"id\\": \\"222\\"}",
                "=== automation.c ===\\nunique_id=333\\n{\\"id\\": \\"333\\"}",
                "=== automation.d ===\\nunique_id=444\\n{\\"id\\": \\"444\\"}",
                "=== automation.e ===\\nunique_id=555\\n{\\"id\\": \\"555\\"}",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_related_config",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$related_unique_id --jq-file config_filter.jq --out related-config.json",
            "aggregated_output": "ENTITY=automation.related_overlap\\nUNIQUE_ID=999\\n{\\"id\\": \\"999\\"}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("allows one extra config read when the transcript body marks it as collision evidence", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n\\n".join([
                "=== automation.a ===\\nunique_id=111\\n{\\"id\\": \\"111\\"}",
                "=== automation.b ===\\nunique_id=222\\n{\\"id\\": \\"222\\"}",
                "=== automation.c ===\\nunique_id=333\\n{\\"id\\": \\"333\\"}",
                "=== automation.d ===\\nunique_id=444\\n{\\"id\\": \\"444\\"}",
                "=== automation.e ===\\nunique_id=555\\n{\\"id\\": \\"555\\"}",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_extra_config",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$extra_unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "Collision evidence for shared helper overlap\\nENTITY=automation.extra_overlap\\nUNIQUE_ID=999\\n{\\"id\\": \\"999\\"}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("pass");
    expect(result.errors).toEqual([]);
  });

  it("fails when an extra off-workset config read is not marked as related collision evidence", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n\\n".join([
                "=== automation.a ===\\nunique_id=111\\n{\\"id\\": \\"111\\"}",
                "=== automation.b ===\\nunique_id=222\\n{\\"id\\": \\"222\\"}",
                "=== automation.c ===\\nunique_id=333\\n{\\"id\\": \\"333\\"}",
                "=== automation.d ===\\nunique_id=444\\n{\\"id\\": \\"444\\"}",
                "=== automation.e ===\\nunique_id=555\\n{\\"id\\": \\"555\\"}",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_extra_config",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$extra_unique_id --jq-file config_filter.jq --out extra-config.json",
            "aggregated_output": "ENTITY=automation.unexpected_overlap\\nUNIQUE_ID=999\\n{\\"id\\": \\"999\\"}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("review_unapproved_extra_config_read:automation.unexpected_overlap");
  });

  it("fails when more than one extra related target is read outside the audited workset", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n\\n".join([
                "=== automation.a ===\\nunique_id=111\\n{\\"id\\": \\"111\\"}",
                "=== automation.b ===\\nunique_id=222\\n{\\"id\\": \\"222\\"}",
                "=== automation.c ===\\nunique_id=333\\n{\\"id\\": \\"333\\"}",
                "=== automation.d ===\\nunique_id=444\\n{\\"id\\": \\"444\\"}",
                "=== automation.e ===\\nunique_id=555\\n{\\"id\\": \\"555\\"}",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_related_config_a",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$related_unique_id_a --jq-file config_filter.jq --out related-config-a.json",
            "aggregated_output": "ENTITY=automation.related_overlap_a\\nUNIQUE_ID=999\\n{\\"id\\": \\"999\\"}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_related_config_b",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$related_unique_id_b --jq-file config_filter.jq --out related-config-b.json",
            "aggregated_output": "ENTITY=automation.related_overlap_b\\nUNIQUE_ID=1000\\n{\\"id\\": \\"1000\\"}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("review_multiple_extra_related_config_reads");
  });

  it("fails when a config-body read does not emit a parseable target marker", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "{\\"id\\":\\"hidden\\"}",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("review_unidentified_config_read");
  });

  it("fails when a non-audited config read only appears in aggregated output", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "review_area",
    "matches": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e", "automation.f"],
    "audited": ["automation.a", "automation.b", "automation.c", "automation.d", "automation.e"],
    "remaining": 1,
    "non_audited": ["automation.f"],
}

status_line = (
    "Scope\\nSummary\\nHigh-Risk Findings\\nRepeated Patterns\\nItems Checked\\nCollisions by Cluster\\n"
    "NOVA_BULK_REVIEW_RESULT id=review_area matched=6 audited=5 remaining=1 "
    "item_ids=[\\"automation.a\\",\\"automation.b\\",\\"automation.c\\",\\"automation.d\\",\\"automation.e\\"] "
    "quick_fix_offered=false sections=[\\"Scope\\",\\"Summary\\",\\"High-Risk Findings\\",\\"Repeated Patterns\\",\\"Items Checked\\",\\"Collisions by Cluster\\"]"
)

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_area",
            "type": "command_execution",
            "command": "ha-nova relay ws --data-file payload.json --out result.json {\\"type\\":\\"search/related\\",\\"item_type\\":\\"area\\",\\"item_id\\":\\"arbeitszimmer\\"}",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_configs",
            "type": "command_execution",
            "command": "ha-nova relay core --method GET --path /api/config/automation/config/$hidden_unique_id --jq-file config_filter.jq --out config.json",
            "aggregated_output": "\\n\\n".join([
                "=== automation.a ===\\nunique_id=111\\n{\\"id\\": \\"111\\"}",
                "=== automation.b ===\\nunique_id=222\\n{\\"id\\": \\"222\\"}",
                "=== automation.c ===\\nunique_id=333\\n{\\"id\\": \\"333\\"}",
                "=== automation.d ===\\nunique_id=444\\n{\\"id\\": \\"444\\"}",
                "=== automation.e ===\\nunique_id=555\\n{\\"id\\": \\"555\\"}",
                "=== automation.f ===\\nunique_id=666\\n{\\"id\\": \\"666\\"}",
            ]),
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": status_line,
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "review.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("review", "area_review", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("review_prefetch_outside_workset:automation.f");
  });

  it("does not mistake shell -lc for a forbidden relay jq -c flag", () => {
    const result = runPythonValidator(`
import importlib.util
import json
import sys
import tempfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("bulk_live_validator", "scripts/e2e/codex-ha-nova-bulk-live-e2e.py")
module = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = module
spec.loader.exec_module(module)

fixture = {
    "id": "inventory_prefix",
    "matches": ["automation.a"],
    "displayed": ["automation.a"],
}

events = [
    {
        "type": "item.completed",
        "item": {
            "id": "cmd_inventory",
            "type": "command_execution",
            "command": "/bin/zsh -lc \\"ha-nova relay ws --data-file payload.json --out result.json && ha-nova relay jq --file result.json '.values'\\"",
            "aggregated_output": "",
            "exit_code": 0,
            "status": "completed",
        },
    },
    {
        "type": "item.completed",
        "item": {
            "id": "msg_final",
            "type": "agent_message",
            "text": "NOVA_BULK_INVENTORY_RESULT id=inventory_prefix matched=1 displayed=1 remaining=0 values=[\\"automation.a\\"]",
        },
    },
]

with tempfile.TemporaryDirectory() as tmpdir:
    raw_log = Path(tmpdir) / "inventory.jsonl"
    raw_log.write_text("\\n".join(json.dumps(event) for event in events), encoding="utf-8")
    result = module.validate_case("inventory", "prefix_inventory", fixture, raw_log, 0)
    print(json.dumps({"status": result.status, "errors": result.errors}))
`);

    expect(result.status).toBe("fail");
    expect(result.errors).toContain("selector_resolution_missing");
    expect(result.errors).not.toContain("invalid_relay_jq_flag");
  });
});
