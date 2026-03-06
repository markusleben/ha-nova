#!/usr/bin/env bash
#
# check-docs.sh — Verify that factual claims in README.md and CONTRIBUTING.md
# match the actual codebase. Run in CI to catch stale docs.
#
# Exit 0 = all claims verified. Exit 1 = at least one mismatch.
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ERRORS=()

fail() { ERRORS+=("$1"); echo "  FAIL: $1"; }
pass() { echo "  ok: $1"; }

# Count grep matches without triggering pipefail on zero results
count_matches() {
  local result
  result=$(grep -rn "$@" 2>/dev/null || true)
  if [[ -z "$result" ]]; then echo 0; else echo "$result" | wc -l | tr -d ' '; fi
}

echo "=== Documentation Fact-Check ==="
echo ""

# ── 1. LOC count ──
# README claims "~1.5K LOC" — actual must be 1000–2000
echo "[1] Relay LOC (README claims ~1.5K)"
ACTUAL_LOC=$(find "$REPO_ROOT/src" -name '*.ts' -exec cat {} + | wc -l | tr -d ' ')
if (( ACTUAL_LOC >= 1000 && ACTUAL_LOC <= 2000 )); then
  pass "src/ = ${ACTUAL_LOC} LOC (within 1000–2000 range)"
else
  fail "src/ = ${ACTUAL_LOC} LOC — README says ~1.5K but actual is outside 1000–2000. Update README."
fi

# ── 2. Skill count ──
# README claims "7 skill files"
echo "[2] Skill directory count (README claims 7)"
SKILL_COUNT=$(find "$REPO_ROOT/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
if (( SKILL_COUNT == 7 )); then
  pass "skills/ has ${SKILL_COUNT} directories"
else
  fail "skills/ has ${SKILL_COUNT} directories — README says 7. Update README and diagram."
fi

# ── 3. No MCP protocol in relay ──
# README claims "not an MCP server", "No tool definitions"
echo "[3] No MCP/tool-definition patterns in src/"
MCP_HITS=$(count_matches "fastmcp\|@mcp\.tool\|mcp\.tool\|McpServer\|MCP_TOOL" "$REPO_ROOT/src")
if (( MCP_HITS == 0 )); then
  pass "No MCP/tool-definition patterns found in src/"
else
  fail "Found ${MCP_HITS} MCP-related patterns in src/ — README claims no MCP server and no tool definitions."
fi

# ── 4. No domain handler patterns in relay ──
# README claims "Zero business logic"
echo "[4] No domain-handler patterns in src/"
DOMAIN_HITS=$(count_matches "domain_handler\|DOMAIN_HANDLERS\|valid_actions\|fuzzy_search\|FuzzySearch" "$REPO_ROOT/src")
if (( DOMAIN_HITS == 0 )); then
  pass "No domain-handler/fuzzy-search patterns found in src/"
else
  fail "Found ${DOMAIN_HITS} domain-logic patterns in src/ — README claims zero business logic."
fi

# ── 5. Planned features not yet shipped ──
# README marks auto-backup, filesystem access, state streaming as "(planned)"
echo "[5] Planned features still unimplemented"
PLANNED_HITS=$(count_matches "backup\|/backup\|/files\|filesystem\|/stream\|EventSource\|SSE" "$REPO_ROOT/src/http/handlers/")
if (( PLANNED_HITS == 0 )); then
  pass "No backup/filesystem/streaming endpoints found — correctly marked as (planned)"
else
  fail "Found ${PLANNED_HITS} hits for planned features in handlers/ — remove '(planned)' from README if shipped."
fi

# ── 6. Internal links ──
echo "[6] Internal links resolve"
for linked_file in CONTRIBUTING.md LICENSE; do
  if [[ -f "$REPO_ROOT/$linked_file" ]]; then
    pass "$linked_file exists"
  else
    fail "$linked_file referenced in README but does not exist"
  fi
done

# ── 7. install.sh exists ──
echo "[7] install.sh referenced in Quick Start"
if [[ -f "$REPO_ROOT/install.sh" ]]; then
  pass "install.sh exists"
else
  fail "install.sh referenced in README Quick Start but does not exist"
fi

# ── 8. Supported clients match install scripts ──
echo "[8] Supported AI clients have install scripts"
for client in claude codex opencode gemini; do
  SCRIPT_EXISTS=$(grep -c "install.*${client}" "$REPO_ROOT/package.json" 2>/dev/null || true)
  if (( SCRIPT_EXISTS > 0 )); then
    pass "Install script for ${client} found"
  else
    fail "README lists ${client} as supported but no install script found in package.json"
  fi
done

# ── 9. Route count — verify relay stays minimal ──
echo "[9] Relay route count"
ROUTE_COUNT=$(grep -c "router.register" "$REPO_ROOT/src/index.ts" 2>/dev/null || true)
if (( ROUTE_COUNT <= 5 )); then
  pass "Relay has ${ROUTE_COUNT} routes (≤5 — still minimal)"
else
  fail "Relay has ${ROUTE_COUNT} routes — growing beyond 'minimal'. Review architecture claims."
fi

# ── 10. Keychain usage exists ──
echo "[10] macOS Keychain integration"
KEYCHAIN_HITS=$(count_matches "security find-generic-password\|store_keychain_secret\|read_keychain_secret" "$REPO_ROOT/scripts/")
if (( KEYCHAIN_HITS > 0 )); then
  pass "Keychain integration found (${KEYCHAIN_HITS} references)"
else
  fail "README claims 'All auth via macOS Keychain' but no Keychain usage found in scripts/"
fi

# ── 11. No telemetry/analytics ──
echo "[11] No telemetry or analytics code"
TELEMETRY_HITS=$(count_matches "telemetry\|analytics\|mixpanel\|segment\|posthog\|sentry" "$REPO_ROOT/src")
if (( TELEMETRY_HITS == 0 )); then
  pass "No telemetry/analytics patterns in src/"
else
  fail "Found ${TELEMETRY_HITS} telemetry-related patterns in src/ — README claims 'No telemetry'."
fi

# ── Results ──
echo ""
if (( ${#ERRORS[@]} == 0 )); then
  echo "=== All documentation claims verified ==="
  exit 0
else
  echo "=== ${#ERRORS[@]} claim(s) need attention ==="
  for err in "${ERRORS[@]}"; do
    echo "  - $err"
  done
  exit 1
fi
