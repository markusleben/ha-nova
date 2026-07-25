import { fail } from "./dependabot-direct-merge-github.mjs";

const marker = "<!-- ha-nova-dependabot-safe-policy -->";

export function requiredPolicy(policy, requireProvisioned = false) {
  const required = policy.main_branch_protection?.required_status_checks;
  const apps = policy.main_branch_protection?.required_status_check_apps ?? {};
  const safeLabel = policy.dependabot_safe_lane?.label;
  const sourceName = policy.cloud_source_gate?.check_name;
  const invalidatorName =
    policy.cloud_source_gate?.synchronous_invalidator_check_name;
  if (
    !Array.isArray(required) ||
    required.length === 0 ||
    new Set(required).size !== required.length ||
    typeof safeLabel !== "string" ||
    safeLabel.length === 0 ||
    typeof sourceName !== "string" ||
    sourceName.length === 0 ||
    typeof invalidatorName !== "string" ||
    invalidatorName.length === 0
  ) {
    fail("authenticated repository policy is invalid");
  }
  for (const [name, appId] of Object.entries(apps)) {
    if (!required.includes(name) || !Number.isSafeInteger(appId) || appId < 0) {
      fail(`required check ${name} App policy is invalid`);
    }
    if (requireProvisioned && appId === 0) {
      fail(`required check ${name} App id is not provisioned`);
    }
  }
  return { apps, invalidatorName, required, safeLabel, sourceName };
}

export function ownedMarker(comments, safeLabel, policySHA) {
  const expected = [
    marker,
    `safe_label=${safeLabel}`,
    `policy_sha=${policySHA}`,
  ].join("\n");
  return comments
    .filter(
      (comment) =>
        comment.user?.login === "github-actions[bot]" &&
        comment.body?.trimEnd() === expected,
    )
    .sort((left, right) => left.id - right.id)
    .at(-1);
}

function latestCheck(checks, name, appId) {
  return checks
    .filter(
      (check) =>
        check.name === name && (appId === undefined || check.app?.id === appId),
    )
    .sort((left, right) => left.id - right.id)
    .at(-1);
}

export function requireChecks(checks, policy, mergeSHA, prNumber) {
  const { apps, invalidatorName, required, sourceName } =
    requiredPolicy(policy);
  for (const name of required) {
    const check = latestCheck(checks, name, apps[name]);
    if (check?.status !== "completed" || check.conclusion !== "success") {
      fail(`required check ${name} is not green`);
    }
    if (name === sourceName) {
      const match =
        /^workflow-run:[1-9][0-9]*:attempt:[1-9][0-9]*:target:([0-9a-f]{40})$/.exec(
          check.external_id ?? "",
        );
      if (match?.[1] !== mergeSHA) {
        fail("latest dedicated-App source check targets another merge commit");
      }
    }
    if (
      name === invalidatorName &&
      check.external_id !== `pull-request:${prNumber}:target:${mergeSHA}`
    ) {
      fail(
        "latest dedicated-App invalidator check targets another merge commit",
      );
    }
  }
}
