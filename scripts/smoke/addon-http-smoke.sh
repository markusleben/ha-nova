#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8791}"
AUTH_TOKEN="${AUTH_TOKEN:-dev-bridge-token}"

curl -fsS \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  "$BASE_URL/health"

curl -fsS \
  -X POST \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"type":"ping"}' \
  "$BASE_URL/ws"

echo "HTTP smoke checks completed for $BASE_URL"
