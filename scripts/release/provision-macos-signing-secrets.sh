#!/usr/bin/env bash
set -euo pipefail

REPOSITORY="markusleben/ha-nova"
ENVIRONMENT="production"
EXPECTED_GITHUB_USER="markusleben"
EXPECTED_IDENTITY="Developer ID Application: Markus Leben (CTF9J94274)"

fail() {
  echo "[provision-macos-signing-secrets] ERROR: $*" >&2
  exit 1
}

cleanup() {
  certificate_password=""
  unset certificate_password
}
trap cleanup EXIT

if [[ "$-" == *x* ]]; then
  fail "shell tracing must be disabled before entering a secret"
fi
if [[ "$#" -ne 1 ]]; then
  fail "usage: bash scripts/release/provision-macos-signing-secrets.sh <developer-id.p12>"
fi

certificate_path="$1"
[[ -f "${certificate_path}" && ! -L "${certificate_path}" ]] \
  || fail "certificate must be a regular non-symlink .p12 file"
[[ "${certificate_path}" == *.p12 ]] \
  || fail "certificate path must end in .p12"

for command in base64 gh openssl tr; do
  command -v "${command}" >/dev/null 2>&1 \
    || fail "required command is unavailable: ${command}"
done
openssl_version="$(openssl version 2>/dev/null)" \
  || fail "could not resolve the OpenSSL version"
openssl_pkcs12_args=()
if [[ "${openssl_version}" == "OpenSSL 3."* ]]; then
  openssl_pkcs12_args=(-legacy)
fi

run_openssl_pkcs12() {
  if (( ${#openssl_pkcs12_args[@]} > 0 )); then
    openssl pkcs12 "${openssl_pkcs12_args[@]}" "$@"
  else
    openssl pkcs12 "$@"
  fi
}

gh auth status --hostname github.com >/dev/null \
  || fail "GitHub CLI authentication is unavailable"
active_user="$(gh api user --jq .login)" \
  || fail "cannot resolve the active GitHub user"
[[ "${active_user}" == "${EXPECTED_GITHUB_USER}" ]] \
  || fail "active GitHub user must be ${EXPECTED_GITHUB_USER}, got ${active_user}"
repository="$(gh repo view "${REPOSITORY}" --json nameWithOwner --jq .nameWithOwner)" \
  || fail "cannot access ${REPOSITORY}"
[[ "${repository}" == "${REPOSITORY}" ]] \
  || fail "GitHub repository identity mismatch"
gh api "repos/${REPOSITORY}/environments/${ENVIRONMENT}" --silent \
  || fail "protected ${ENVIRONMENT} environment is unavailable"

printf 'Developer ID .p12 password (input hidden; requested once): ' >&2
IFS= read -r -s certificate_password \
  || fail "could not read the certificate password"
printf '\n' >&2
[[ -n "${certificate_password}" ]] \
  || fail "certificate password must not be empty"

if ! run_openssl_pkcs12 \
    -in "${certificate_path}" \
    -passin fd:3 \
    -nocerts \
    -nodes \
    3<<<"${certificate_password}" \
    2>/dev/null \
  | openssl pkey -check -noout >/dev/null 2>&1; then
  fail "the .p12 password is incorrect or its private key is invalid"
fi

certificate_subject="$(
  run_openssl_pkcs12 \
      -in "${certificate_path}" \
      -passin fd:3 \
      -clcerts \
      -nokeys \
      3<<<"${certificate_password}" \
      2>/dev/null \
    | openssl x509 -noout -subject 2>/dev/null
)" || fail "the .p12 certificate could not be inspected"
[[ "${certificate_subject}" == *"${EXPECTED_IDENTITY}"* ]] \
  || fail "the .p12 does not contain the expected Developer ID identity"

base64 <"${certificate_path}" \
  | tr -d '\r\n' \
  | gh secret set HA_NOVA_MACOS_CERTIFICATE_P12_BASE64 \
      --repo "${REPOSITORY}" \
      --env "${ENVIRONMENT}" \
  || fail "failed to upload the encrypted .p12 secret"

printf '%s' "${certificate_password}" \
  | gh secret set HA_NOVA_MACOS_CERTIFICATE_PASSWORD \
      --repo "${REPOSITORY}" \
      --env "${ENVIRONMENT}" \
  || fail "failed to upload the .p12 password secret; rerun the command"

echo "[provision-macos-signing-secrets] OK: protected macOS signing secrets updated"
