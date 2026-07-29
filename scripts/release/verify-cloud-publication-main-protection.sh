#!/usr/bin/env bash
set -euo pipefail

TRUSTED_ROOT="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SOURCE_ROOT="$(cd -- "${HA_NOVA_SOURCE_ROOT:-${TRUSTED_ROOT}}" && pwd)"
TOKEN_OUTPUT=""

fail() {
  echo "[verify-cloud-publication-main-protection] ERROR: $*" >&2
  exit 1
}

cleanup() {
  if [[ -n "${TOKEN_OUTPUT}" && -f "${TOKEN_OUTPUT}" ]]; then
    rm -f "${TOKEN_OUTPUT}"
  fi
}
trap cleanup EXIT

cloud_enabled="$(
  node -e '
    const value = JSON.parse(
      require("node:fs").readFileSync(process.argv[1], "utf8"),
    ).cloud_remote_enabled;
    if (typeof value !== "boolean") process.exit(2);
    process.stdout.write(String(value));
  ' "${SOURCE_ROOT}/version.json"
)" || fail "version.json cloud_remote_enabled must be a boolean"

if [[ "${cloud_enabled}" == "false" ]]; then
  echo "[verify-cloud-publication-main-protection] Cloud Remote disabled; dedicated App protection check not required."
  exit 0
fi
[[ "${cloud_enabled}" == "true" ]] \
  || fail "version.json cloud_remote_enabled must be a boolean"

[[ "${HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID:-}" =~ ^[1-9][0-9]*$ ]] \
  || fail "enabled Cloud publication requires the source-check App ID"
[[ "${HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY:-}" == *"PRIVATE KEY"* ]] \
  || fail "enabled Cloud publication requires the source-check App private key"
[[ "${GITHUB_REPOSITORY:-}" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] \
  || fail "GITHUB_REPOSITORY must identify one repository"

expected_app_id="$(
  node -e '
    const value = JSON.parse(
      require("node:fs").readFileSync(process.argv[1], "utf8"),
    ).cloud_source_gate?.reporter_app_id;
    if (!Number.isSafeInteger(value) || value <= 0) process.exit(2);
    process.stdout.write(String(value));
  ' "${TRUSTED_ROOT}/.github/policy/repo-policy.json"
)" || fail "enabled Cloud publication requires a provisioned source-check App policy"
[[ "${HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID}" == "${expected_app_id}" ]] \
  || fail "source-check App secret does not match the exact policy App ID"

TOKEN_OUTPUT="$(mktemp)"
GITHUB_OUTPUT="${TOKEN_OUTPUT}" \
HA_NOVA_CLOUD_SOURCE_CHECK_TOKEN_MODE="administration-read" \
  node "${TRUSTED_ROOT}/scripts/release/create-cloud-source-check-token.mjs"

token="$(sed -n 's/^token=//p' "${TOKEN_OUTPUT}")"
[[ "${token}" != *$'\n'* && "${#token}" -ge 20 ]] \
  || fail "dedicated administration-read installation token is invalid"

GH_TOKEN="${token}" \
  bash "${TRUSTED_ROOT}/scripts/release/verify-github-main-protection.sh" \
    "${GITHUB_REPOSITORY}" main
