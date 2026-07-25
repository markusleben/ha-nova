import { spawnSync } from "node:child_process";
import { generateKeyPairSync } from "node:crypto";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
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

function runPublicationProtectionGate(
  enabled: boolean,
  provisioned: boolean,
  appId = "42",
) {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-publication-protection-"));
  const releaseDirectory = join(root, "scripts", "release");
  const policyDirectory = join(root, ".github", "policy");
  mkdirSync(releaseDirectory, { recursive: true });
  mkdirSync(policyDirectory, { recursive: true });
  copyFileSync(
    "scripts/release/verify-cloud-publication-main-protection.sh",
    join(releaseDirectory, "verify-cloud-publication-main-protection.sh"),
  );
  writeFileSync(
    join(root, "version.json"),
    JSON.stringify({
      cloud_remote_enabled: enabled,
      cloud_remote_platforms: enabled ? ["linux"] : [],
    }),
    "utf8",
  );
  writeFileSync(
    join(policyDirectory, "repo-policy.json"),
    JSON.stringify({
      cloud_source_gate: { reporter_app_id: enabled ? 42 : 0 },
    }),
    "utf8",
  );
  writeFileSync(
    join(releaseDirectory, "create-cloud-source-check-token.mjs"),
    `import { appendFileSync } from "node:fs";
if (process.env.HA_NOVA_CLOUD_SOURCE_CHECK_TOKEN_MODE !== "administration-read") {
  process.exit(2);
}
appendFileSync(process.env.GITHUB_OUTPUT, "token=dedicated-administration-read-token\\n");
`,
    "utf8",
  );
  writeFileSync(
    join(releaseDirectory, "verify-github-main-protection.sh"),
    `#!/usr/bin/env bash
set -euo pipefail
[[ "\${GH_TOKEN:-}" == "dedicated-administration-read-token" ]]
`,
    "utf8",
  );
  chmodSync(
    join(releaseDirectory, "verify-github-main-protection.sh"),
    0o755,
  );
  return spawnSync(
    "bash",
    [join(releaseDirectory, "verify-cloud-publication-main-protection.sh")],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        GITHUB_REPOSITORY: "owner/repo",
        HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID: provisioned ? appId : "",
        HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY: provisioned
          ? "-----BEGIN PRIVATE KEY-----"
          : "",
      },
    },
  );
}

describe("Cloud publication main protection gate", () => {
  it("preserves disabled release publication without App credentials", () => {
    const result = runPublicationProtectionGate(false, false);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
    expect(result.stdout).toContain("Cloud Remote disabled");
  });

  it("mints an administration-read token before enabled publication", () => {
    const result = runPublicationProtectionGate(true, true);
    expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  });

  it("fails enabled publication when the read App is unprovisioned", () => {
    const result = runPublicationProtectionGate(true, false);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(
      "enabled Cloud publication requires the source-check App ID",
    );
  });

  it("rejects a read credential from an App outside the exact policy", () => {
    const result = runPublicationProtectionGate(true, true, "43");
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain(
      "source-check App secret does not match the exact policy App ID",
    );
  });
});

it("requests an App installation token scoped only to administration read", () => {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-admin-read-token-"));
  const output = join(root, "output");
  const preload = join(root, "preload.mjs");
  const trace = join(root, "trace.json");
  const { privateKey } = generateKeyPairSync("rsa", {
    modulusLength: 2048,
    privateKeyEncoding: { format: "pem", type: "pkcs8" },
    publicKeyEncoding: { format: "pem", type: "spki" },
  });
  writeFileSync(
    preload,
    `import { writeFileSync } from "node:fs";
globalThis.fetch = async (url, init = {}) => {
  const path = new URL(url).pathname;
  if (path.endsWith("/repos/owner/repo/installation")) {
    return { ok: true, status: 200, json: async () => ({ id: 7 }) };
  }
  if (path.endsWith("/app/installations/7/access_tokens")) {
    const body = JSON.parse(init.body);
    writeFileSync(process.env.MOCK_TRACE, JSON.stringify(body));
    return {
      ok: true,
      status: 201,
      json: async () => ({
        permissions: { administration: "read" },
        token: "dedicated-administration-read-token"
      })
    };
  }
  return { ok: false, status: 500, json: async () => ({}) };
};
`,
    "utf8",
  );
  const result = spawnSync(
    process.execPath,
    [
      "--import",
      preload,
      "scripts/release/create-cloud-source-check-token.mjs",
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        GITHUB_OUTPUT: output,
        GITHUB_REPOSITORY: "owner/repo",
        HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID: "42",
        HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY: privateKey,
        HA_NOVA_CLOUD_SOURCE_CHECK_TOKEN_MODE: "administration-read",
        MOCK_TRACE: trace,
      },
    },
  );
  expect(result.status, `${result.stdout}\n${result.stderr}`).toBe(0);
  expect(JSON.parse(readFileSync(trace, "utf8"))).toEqual({
    permissions: { administration: "read" },
    repositories: ["repo"],
  });
  expect(readFileSync(output, "utf8")).toContain(
    "token=dedicated-administration-read-token",
  );
});
