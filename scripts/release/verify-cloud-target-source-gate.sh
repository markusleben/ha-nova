#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SOURCE_REF="${HA_NOVA_CLOUD_GATE_SOURCE_REF:-}"
EXPECTED_TARGET="${HA_NOVA_CLOUD_GATE_EXPECTED_TARGET_COMMIT:-}"
EXPECTED_HEAD="${HA_NOVA_CLOUD_GATE_EXPECTED_HEAD_COMMIT:-}"
EXPECTED_BASE="${HA_NOVA_CLOUD_GATE_EXPECTED_BASE_COMMIT:-}"
APPROVAL_ID="${HA_NOVA_CLOUD_GATE_APPROVAL_ID:-}"
MODE="${1:-release}"

fail() {
  echo "[verify-cloud-target-source-gate] ERROR: $*" >&2
  exit 1
}

[[ "${SOURCE_REF}" == refs/pull/*/merge \
  || "${SOURCE_REF}" == refs/heads/gh-readonly-queue/main/* ]] \
  || fail "source ref must identify a pull request merge or main merge queue"
[[ "${MODE}" == "release" || "${MODE}" == "candidate" ]] \
  || fail "mode must be release or candidate"
[[ -z "${EXPECTED_TARGET}" || "${EXPECTED_TARGET}" =~ ^[0-9a-f]{40}$ ]] \
  || fail "expected target commit must be a full lowercase SHA-1"
[[ -z "${EXPECTED_HEAD}" || "${EXPECTED_HEAD}" =~ ^[0-9a-f]{40}$ ]] \
  || fail "expected head commit must be a full lowercase SHA-1"
[[ -z "${EXPECTED_BASE}" || "${EXPECTED_BASE}" =~ ^[0-9a-f]{40}$ ]] \
  || fail "expected base commit must be a full lowercase SHA-1"
[[ -z "${EXPECTED_HEAD}${EXPECTED_BASE}" \
  || -n "${EXPECTED_HEAD}" && -n "${EXPECTED_BASE}" ]] \
  || fail "expected pull request head and base must be provided together"

target_ref="refs/remotes/ha-nova-cloud-target/current"
git -C "${ROOT_DIR}" fetch --quiet --no-tags origin \
  "+${SOURCE_REF}:${target_ref}" \
  || fail "target source ref is unavailable"

target_commit="$(git -C "${ROOT_DIR}" rev-parse --verify "${target_ref}^{commit}")"
target_tree="$(git -C "${ROOT_DIR}" rev-parse --verify "${target_commit}^{tree}")"
[[ -z "${EXPECTED_TARGET}" || "${target_commit}" == "${EXPECTED_TARGET}" ]] \
  || fail "target source moved before verification"

if [[ -n "${EXPECTED_HEAD}" ]]; then
  read -r commit parent_base parent_head extra <<<"$(
    git -C "${ROOT_DIR}" rev-list --parents -n 1 "${target_commit}"
  )"
  [[ "${commit}" == "${target_commit}" && -z "${extra:-}" \
    && "${parent_base:-}" == "${EXPECTED_BASE}" \
    && "${parent_head:-}" == "${EXPECTED_HEAD}" ]] \
    || fail "pull request merge commit does not bind the expected base and head"
fi

target_root="$(mktemp -d)"
mkdir -p "${target_root}/nova"
trap 'trash "${target_root}" >/dev/null 2>&1 || true' EXIT

for relative_path in version.json nova/version.json nova/config.yaml; do
  git -C "${ROOT_DIR}" show "${target_commit}:${relative_path}" \
    > "${target_root}/${relative_path}" \
    || fail "target commit is missing ${relative_path}"
done

cloud_enabled="$(
  node -e \
    'process.stdout.write(String(JSON.parse(require("node:fs").readFileSync(process.argv[1], "utf8")).cloud_remote_enabled))' \
    "${target_root}/version.json"
)"
if [[ "${cloud_enabled}" == "false" ]]; then
  [[ "${MODE}" == "release" ]] \
    || fail "candidate source must enable Home Assistant Cloud"
  HA_NOVA_CLOUD_GATE_TARGET_COMMIT="${target_commit}" \
  HA_NOVA_CLOUD_GATE_TARGET_TREE="${target_tree}" \
  HA_NOVA_CLOUD_GATE_TRUSTED_PR_MODE=1 \
    bash "${ROOT_DIR}/scripts/release/verify-cloud-release-gate.sh" \
      "${target_root}"
  exit 0
fi
[[ "${cloud_enabled}" == "true" ]] \
  || fail "target Cloud release switch must be a boolean"

node - "${ROOT_DIR}/.github/policy/repo-policy.json" <<'NODE'
const { readFileSync } = require("node:fs");

const policy = JSON.parse(readFileSync(process.argv[2], "utf8"));
const gate = policy.cloud_source_gate ?? {};
const required =
  policy.main_branch_protection?.required_status_checks ?? [];
const apps =
  policy.main_branch_protection?.required_status_check_apps ?? {};

if (
  policy.main_branch_protection?.strict_required_status_checks !== true
) {
  console.error(
    "[verify-cloud-target-source-gate] ERROR: enabled Cloud source requires strict up-to-date branch protection policy",
  );
  process.exit(1);
}

const name = gate.check_name;
const slug = gate.reporter_app_slug;
const appID = gate.reporter_app_id;
if (
  typeof name !== "string" ||
  name.length === 0 ||
  typeof slug !== "string" ||
  slug.length === 0 ||
  !Number.isSafeInteger(appID) ||
  appID <= 0 ||
  !required.includes(name) ||
  apps[name] !== appID
) {
  console.error(
    "[verify-cloud-target-source-gate] ERROR: enabled Cloud source requires the provisioned, exact App-bound source reporter check",
  );
  process.exit(1);
}
NODE

trusted_workflows_tree="$(
  git -C "${ROOT_DIR}" rev-parse --verify "HEAD:.github/workflows"
)"
target_workflows_tree="$(
  git -C "${ROOT_DIR}" rev-parse --verify \
    "${target_commit}:.github/workflows"
)" || fail "target commit must retain the trusted workflow tree"
exact_evidence=0
if [[ "${MODE}" == "candidate" ]]; then
  [[ "${SOURCE_REF}" =~ ^refs/pull/([1-9][0-9]*)/merge$ ]] \
    || fail "candidate approval requires one exact pull request base, head, and merge"
  approval_pr="${BASH_REMATCH[1]}"
  [[ -n "${EXPECTED_TARGET}" && -n "${EXPECTED_HEAD}" \
    && -n "${EXPECTED_BASE}" ]] \
    || fail "candidate approval requires one exact pull request base, head, and merge"
  expected_approval="pr${approval_pr}-${EXPECTED_BASE}-${EXPECTED_HEAD}-${EXPECTED_TARGET}-${target_workflows_tree}"
  [[ "${APPROVAL_ID}" == "${expected_approval}" ]] \
    || fail "candidate request_id does not match the exact pull request approval"
fi

if [[ "${target_workflows_tree}" != "${trusted_workflows_tree}" ]]; then
  trusted_commit="$(git -C "${ROOT_DIR}" rev-parse HEAD)"
  if node "${ROOT_DIR}/scripts/release/verify-cloud-workflow-uses-only.mjs" \
    "${ROOT_DIR}" "${trusted_commit}" "${target_commit}" \
    workflow-tree-only >/dev/null 2>&1; then
    :
  else
    [[ "${SOURCE_REF}" =~ ^refs/pull/[1-9][0-9]*/merge$ ]] \
      || fail "merge queue cannot approve sensitive workflow changes"
    node "${ROOT_DIR}/scripts/release/verify-cloud-workflow-uses-only.mjs" \
      "${ROOT_DIR}" "${trusted_commit}" "${target_commit}" \
      single-sensitive-workflow
    [[ "${MODE}" == "candidate" ]] || exact_evidence=1
  fi
fi

if [[ "${MODE}" == "candidate" ]]; then
  HA_NOVA_CLOUD_GATE_TARGET_COMMIT="${target_commit}" \
  HA_NOVA_CLOUD_GATE_TARGET_TREE="${target_tree}" \
  HA_NOVA_CLOUD_GATE_TRUSTED_PR_MODE=1 \
    bash "${ROOT_DIR}/scripts/release/verify-cloud-release-gate.sh" \
      "${target_root}" metadata-only
  exit 0
fi

HA_NOVA_CLOUD_GATE_TARGET_COMMIT="${target_commit}" \
HA_NOVA_CLOUD_GATE_TARGET_TREE="${target_tree}" \
HA_NOVA_CLOUD_GATE_TRUSTED_PR_MODE=1 \
HA_NOVA_CLOUD_GATE_REQUIRE_EXACT_EVIDENCE="${exact_evidence}" \
  bash "${ROOT_DIR}/scripts/release/verify-cloud-release-gate.sh" \
    "${target_root}"
