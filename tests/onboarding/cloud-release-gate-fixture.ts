import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const CLOUD_CHECK_NAMES = [
  "domains_mfa",
  "lifecycle",
  "parity",
  "redirects_non_disclosure",
  "installed_relay_app",
  "roles",
  "routing",
  "signing_and_update_matrix",
  "stress_10000",
] as const;

export type CloudPlatform = "darwin" | "linux" | "windows";

export type CloudGateFixture = {
  root: string;
  sha: string;
  tree: string;
  script: string;
};

export function cloudGateFixture(
  version: unknown,
  appVersion: unknown = version,
  relayVersionOverride?: string,
): CloudGateFixture {
  const root = mkdtempSync(join(tmpdir(), "ha-nova-cloud-gate-"));
  const releaseDir = join(root, "scripts", "release");
  const appDir = join(root, "nova");
  mkdirSync(releaseDir, { recursive: true });
  mkdirSync(appDir, { recursive: true });
  const script = join(releaseDir, "verify-cloud-release-gate.sh");
  copyFileSync("scripts/release/verify-cloud-release-gate.sh", script);
  chmodSync(script, 0o755);
  copyFileSync(
    "scripts/release/verify-cloud-workflow-uses-only.mjs",
    join(releaseDir, "verify-cloud-workflow-uses-only.mjs"),
  );
  copyFileSync(
    "scripts/release/verify-cloud-nonsensitive-source.mjs",
    join(releaseDir, "verify-cloud-nonsensitive-source.mjs"),
  );
  const normalizeVersion = (value: unknown): unknown => {
    if (
      typeof value === "object" &&
      value !== null &&
      !Array.isArray(value) &&
      !Object.hasOwn(value, "min_relay_version")
    ) {
      return { ...value, min_relay_version: "0.8.0" };
    }
    return value;
  };
  const normalizedVersion = normalizeVersion(version);
  const normalizedAppVersion = normalizeVersion(appVersion);
  writeFileSync(
    join(root, "version.json"),
    `${JSON.stringify(normalizedVersion, null, 2)}\n`,
    "utf8",
  );
  writeFileSync(
    join(appDir, "version.json"),
    `${JSON.stringify(normalizedAppVersion, null, 2)}\n`,
    "utf8",
  );
  const relayVersion =
    relayVersionOverride ??
    (typeof normalizedVersion === "object" &&
    normalizedVersion !== null &&
    !Array.isArray(normalizedVersion) &&
    typeof (normalizedVersion as Record<string, unknown>).min_relay_version ===
      "string"
      ? (normalizedVersion as Record<string, string>).min_relay_version
      : "0.8.0");
  writeFileSync(
    join(appDir, "config.yaml"),
    `name: NOVA Relay\nversion: "${relayVersion}"\n`,
    "utf8",
  );
  execFileSync("git", ["init", "-q"], { cwd: root });
  execFileSync("git", ["config", "user.name", "Cloud Gate Test"], {
    cwd: root,
  });
  execFileSync("git", ["config", "user.email", "cloud-gate@example.invalid"], {
    cwd: root,
  });
  execFileSync("git", ["add", "."], { cwd: root });
  execFileSync("git", ["commit", "-qm", "fixture"], { cwd: root });
  const sha = currentFixtureHead({ root });
  const tree = currentFixtureTree({ root });
  return { root, sha, tree, script };
}

export function validCloudEvidence(
  source: string | Pick<CloudGateFixture, "sha" | "tree">,
  platforms: CloudPlatform[],
  relayAppVersion = "0.8.0",
): Record<string, unknown> {
  const sha = typeof source === "string" ? source : source.sha;
  const tree = typeof source === "string" ? source : source.tree;
  const checks = Object.fromEntries(
    CLOUD_CHECK_NAMES.map((name) => [name, true]),
  );
  return {
    schema: 2,
    commit_sha: sha,
    tree_sha: tree,
    relay_app: {
      version: relayAppVersion,
      source_commit: sha,
      source_tree_sha: tree,
    },
    checks: {
      ...checks,
      keyrings: Object.fromEntries(
        platforms.map((platform) => [platform, true]),
      ),
    },
  };
}

export function currentFixtureTree(
  fixture: Pick<CloudGateFixture, "root">,
): string {
  return execFileSync("git", ["rev-parse", "HEAD^{tree}"], {
    cwd: fixture.root,
    encoding: "utf8",
  }).trim();
}

export function currentFixtureHead(
  fixture: Pick<CloudGateFixture, "root">,
): string {
  return execFileSync("git", ["rev-parse", "HEAD"], {
    cwd: fixture.root,
    encoding: "utf8",
  }).trim();
}

export function runCloudGate(
  fixture: CloudGateFixture,
  evidence: unknown = "",
  githubSha = currentFixtureHead(fixture),
  env: NodeJS.ProcessEnv = {},
): ReturnType<typeof spawnSync> {
  const encoded =
    typeof evidence === "string" ? evidence : JSON.stringify(evidence);
  return spawnSync("bash", [fixture.script], {
    cwd: fixture.root,
    encoding: "utf8",
    env: {
      ...process.env,
      GITHUB_SHA: githubSha,
      HA_NOVA_CLOUD_GATE_EVIDENCE_JSON: encoded,
      ...env,
    },
  });
}
