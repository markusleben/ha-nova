import { spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

type Mode = "exact" | "unprovisioned" | "wrong_app" | "wrong_strict";

function runMainProtectionGate(mode: Mode) {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-main-protection-"));
  const scriptDirectory = join(root, "scripts", "release");
  const policyDirectory = join(root, ".github", "policy");
  const fakeBin = join(root, "bin");
  mkdirSync(scriptDirectory, { recursive: true });
  mkdirSync(policyDirectory, { recursive: true });
  mkdirSync(fakeBin);
  const verifier = join(scriptDirectory, "verify-github-main-protection.sh");
  copyFileSync("scripts/release/verify-github-main-protection.sh", verifier);
  const expectedAppId = mode === "unprovisioned" ? 0 : 42;
  writeFileSync(
    join(policyDirectory, "repo-policy.json"),
    JSON.stringify({
      main_branch_protection: {
        advisory_checks: ["codex-review-gate"],
        dismiss_stale_reviews: true,
        require_code_owner_reviews: true,
        required_approving_review_count: 1,
        required_conversation_resolution: true,
        required_status_check_apps: { "cloud-source-gate": expectedAppId },
        required_status_checks: ["ci-gate", "cloud-source-gate"],
        strict_required_status_checks: true,
      },
    }),
    "utf8",
  );
  const gh = join(fakeBin, "gh");
  writeFileSync(
    gh,
    `#!/usr/bin/env bash
set -euo pipefail
[[ "\${GH_TOKEN:-}" == "dedicated-read-token" ]]
app_id=42
strict=true
[[ "\${TEST_MODE}" == "wrong_app" ]] && app_id=43
[[ "\${TEST_MODE}" == "wrong_strict" ]] && strict=false
printf '%s\\n' '{"required_status_checks":{"strict":'"\${strict}"',"contexts":["ci-gate","cloud-source-gate"],"checks":[{"context":"cloud-source-gate","app_id":'"\${app_id}"'}]},"required_pull_request_reviews":{"required_approving_review_count":1,"require_code_owner_reviews":true,"dismiss_stale_reviews":true},"required_conversation_resolution":{"enabled":true}}'
`,
    "utf8",
  );
  chmodSync(gh, 0o755);
  return spawnSync("bash", [verifier, "owner/repo", "main"], {
    encoding: "utf8",
    env: {
      ...process.env,
      GH_TOKEN: "dedicated-read-token",
      PATH: `${fakeBin}:${process.env.PATH ?? ""}`,
      TEST_MODE: mode,
    },
  });
}

describe("main protection source gate", () => {
  it("accepts strict checks bound to the exact provisioned App", () => {
    const result = runMainProtectionGate("exact");
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it.each<Mode>(["unprovisioned", "wrong_app", "wrong_strict"])(
    "rejects %s protection",
    (mode) => {
      const result = runMainProtectionGate(mode);
      expect(result.status, `${result.stdout}\n${result.stderr}`).not.toBe(0);
    },
  );
});
