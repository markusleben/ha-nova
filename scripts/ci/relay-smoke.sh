#!/usr/bin/env bash
set -euo pipefail

# Smoke test for a NOVA Relay container image, shared by the verify-only and
# publish paths of .github/workflows/relay-image.yml.
#
#   relay-smoke.sh <image-ref> <expected-version>
#
# The image ref must be runnable by the local docker daemon (a locally loaded
# tag or a pullable registry ref). The relay must (1) come up, (2) reject an
# unauthenticated /health with 401 — proving auth is enforced — and (3) answer
# 200 with the bearer token and report exactly the version we built.

image="${1:?usage: relay-smoke.sh <image-ref> <expected-version>}"
expected_version="${2:?usage: relay-smoke.sh <image-ref> <expected-version>}"

container="relay-smoke-$$"

cleanup() {
  docker rm -f "$container" > /dev/null 2>&1 || true
}
trap cleanup EXIT

# HA_LLAT stays required here: the runtime refuses to start without an
# upstream credential (SUPERVISOR_TOKEN or HA_LLAT), and a plain docker run
# has no Supervisor. No RELAY_VERSION on purpose: the image must report its
# baked-in version, exactly like a user running the documented command.
docker run -d --name "$container" \
  -e RELAY_AUTH_TOKEN=smoke-token \
  -e HA_LLAT=smoke-llat \
  -e HA_URL=http://127.0.0.1:8123 \
  -p 8791:8791 "$image" > /dev/null

# The relay must serve /health and reject an unauthenticated request:
# a 401 proves both that it is up and that auth is enforced.
for i in $(seq 1 20); do
  code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8791/health || true)"
  if [[ "$code" == "401" ]]; then
    echo "relay is up and enforcing auth"
    break
  fi
  if [[ "$i" == "20" ]]; then
    echo "relay did not come up (last code: ${code:-none})" >&2
    docker logs "$container" >&2 || true
    exit 1
  fi
  sleep 1
done

# With the token it must answer 200 and report the version we built.
body="$(curl -sf -H 'Authorization: Bearer smoke-token' http://127.0.0.1:8791/health)"
echo "$body"
echo "$body" | grep -qF "\"version\":\"${expected_version}\""
