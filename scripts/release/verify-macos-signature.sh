#!/usr/bin/env bash
set -euo pipefail

EXPECTED_IDENTIFIER="com.markusleben.ha-nova.cli"
EXPECTED_TEAM_ID="CTF9J94274"
EXPECTED_AUTHORITY="Developer ID Application: Markus Leben (${EXPECTED_TEAM_ID})"

fail() {
  echo "[verify-macos-signature] ERROR: $*" >&2
  exit 1
}

if [[ "$#" -ne 1 ]]; then
  fail "usage: bash scripts/release/verify-macos-signature.sh <binary>"
fi
if [[ "$(uname -s)" != "Darwin" ]]; then
  fail "macOS is required"
fi

binary="$1"
[[ -f "${binary}" && ! -L "${binary}" ]] \
  || fail "binary must be a regular non-symlink file: ${binary}"

/usr/bin/codesign --verify --strict --verbose=2 "${binary}" \
  || fail "codesign verification failed"

details="$(/usr/bin/codesign -d --verbose=4 "${binary}" 2>&1)" \
  || fail "could not inspect code signature"
grep -Fxq "Identifier=${EXPECTED_IDENTIFIER}" <<<"${details}" \
  || fail "unexpected code-signing identifier"
grep -Fxq "TeamIdentifier=${EXPECTED_TEAM_ID}" <<<"${details}" \
  || fail "unexpected or missing Developer ID team"
grep -Fxq "Authority=${EXPECTED_AUTHORITY}" <<<"${details}" \
  || fail "unexpected signing authority"
if grep -Fxq "Signature=adhoc" <<<"${details}"; then
  fail "ad-hoc signatures are not release evidence"
fi

flags_line="$(grep -E '^CodeDirectory .* flags=' <<<"${details}")" \
  || fail "Code Directory flags are missing"
for required_flag in hard kill library-validation runtime; do
  [[ "${flags_line}" == *"${required_flag}"* ]] \
    || fail "Code Directory flag is missing: ${required_flag}"
done

echo "[verify-macos-signature] OK: ${binary} (${EXPECTED_IDENTIFIER}, ${EXPECTED_TEAM_ID})"
