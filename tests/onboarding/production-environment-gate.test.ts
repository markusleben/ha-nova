import { spawnSync } from "node:child_process";
import {
  chmodSync,
  mkdtempSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

function runProductionEnvironmentGate(mode: string) {
  const fakeBin = mkdtempSync(join(tmpdir(), "ha-nova-production-env-"));
  const gh = join(fakeBin, "gh");
  writeFileSync(
    gh,
    `#!/usr/bin/env bash
set -euo pipefail
[[ " $* " == *" X-GitHub-Api-Version: 2026-03-10 "* ]]
if [[ "$*" == *"deployment-branch-policies"* ]]; then
  case "\${TEST_MODE}" in
    exact)
      printf '%s\\n' '[{"total_count":2,"branch_policies":[{"name":"main","type":"branch"},{"name":"v*","type":"tag"}]}]'
      ;;
    extra)
      printf '%s\\n' '[{"total_count":3,"branch_policies":[{"name":"main","type":"branch"},{"name":"v*","type":"tag"},{"name":"release","type":"branch"}]}]'
      ;;
    wrong_type)
      printf '%s\\n' '[{"total_count":2,"branch_policies":[{"name":"main","type":"tag"},{"name":"v*","type":"branch"}]}]'
      ;;
  esac
else
  if [[ "\${TEST_MODE}" == "unprotected" ]]; then
    printf '%s\\n' '{"deployment_branch_policy":null,"protection_rules":[]}'
  else
    printf '%s\\n' '{"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true},"protection_rules":[{"type":"branch_policy"}]}'
  fi
fi
`,
    "utf8",
  );
  chmodSync(gh, 0o755);
  return spawnSync(
    "bash",
    ["scripts/release/verify-github-production-environment.sh"],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        PATH: `${fakeBin}:${process.env.PATH ?? ""}`,
        TEST_MODE: mode,
      },
    },
  );
}

describe("production environment release gate", () => {
  it("accepts exactly branch main and tag v*", () => {
    const result = runProductionEnvironmentGate("exact");
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it.each(["unprotected", "extra", "wrong_type"])(
    "rejects %s deployment policy",
    (mode) => {
      const result = runProductionEnvironmentGate(mode);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    },
  );
});
