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
# The relay must stay small enough that a person can read it. The ceiling moved
# from 2000 to 2800 across relay 0.4.0 (opt-in file transport: containment,
# deny rules, code-execution boundary) and to 3200 across relay 0.5.0 (the
# generic config-snapshot store, ~330 lines of validation, caps and prune;
# and to 3300 for the diagnosability wave: disconnect reasons, request
# logging, server timeouts).
# Growth here is security surface, and it is reviewed as such. The README's own
# number is updated in the release-prep PR — README describes the STABLE
# release, not main.
echo "[1] Relay LOC (must stay readable in one sitting)"
ACTUAL_LOC=$(find "$REPO_ROOT/nova/src" -name '*.ts' -exec cat {} + | wc -l | tr -d ' ')
if (( ACTUAL_LOC >= 1000 && ACTUAL_LOC <= 3300 )); then
  pass "src/ = ${ACTUAL_LOC} LOC (within 1000–3300 range)"
else
  fail "src/ = ${ACTUAL_LOC} LOC — outside the 1000–3300 range. If this is real growth, justify it and update the README claim in the release-prep PR."
fi

# ── 2. Skill count ──
# Active skill inventory expects 29 top-level skill directories
# (context skill + 28 sub-skills; yaml-config/assist/admin/external-sources added 2026-07-11)
echo "[2] Skill directory count (current inventory expects 29)"
SKILL_COUNT=$(find "$REPO_ROOT/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
if (( SKILL_COUNT == 29 )); then
  pass "skills/ has ${SKILL_COUNT} directories"
else
  fail "skills/ has ${SKILL_COUNT} directories — active docs/contracts expect 29. Update README and architecture docs."
fi

# ── 2b. One relay codebase, two distributions ──
# The standalone container must build from the SAME src/ as the App. A second
# implementation (e.g. a Python custom component) is exactly the false-parity
# bug class HA NOVA criticizes in MCP servers.
echo "[2b] Relay container builds from the shared source"
if [[ -f "$REPO_ROOT/nova/Dockerfile.standalone" ]]; then
  if grep -q "COPY src ./src" "$REPO_ROOT/nova/Dockerfile.standalone"; then
    pass "Standalone relay image builds from nova/src (one codebase)"
  else
    fail "nova/Dockerfile.standalone does not build from nova/src — a second relay implementation is not allowed"
  fi
else
  fail "nova/Dockerfile.standalone is missing (the Container/Core distribution)"
fi

# ── 3. No MCP protocol in relay ──
# README claims "not an MCP server", "No tool definitions"
echo "[3] No MCP/tool-definition patterns in src/"
MCP_HITS=$(count_matches "fastmcp\|@mcp\.tool\|mcp\.tool\|McpServer\|MCP_TOOL" "$REPO_ROOT/nova/src")
if (( MCP_HITS == 0 )); then
  pass "No MCP/tool-definition patterns found in src/"
else
  fail "Found ${MCP_HITS} MCP-related patterns in src/ — README claims no MCP server and no tool definitions."
fi

# ── 4. No domain handler patterns in relay ──
# README claims "Zero business logic"
echo "[4] No domain-handler patterns in src/"
DOMAIN_HITS=$(count_matches "domain_handler\|DOMAIN_HANDLERS\|valid_actions\|fuzzy_search\|FuzzySearch" "$REPO_ROOT/nova/src")
if (( DOMAIN_HITS == 0 )); then
  pass "No domain-handler/fuzzy-search patterns found in src/"
else
  fail "Found ${DOMAIN_HITS} domain-logic patterns in src/ — README claims zero business logic."
fi

# ── 5. No unplanned feature creep ──
# The relay must stay minimal: no streaming, and the config-snapshot store
# (relay 0.5.0, POST /backups) must stay a GENERIC blob store — the moment it
# imports the HA clients it has become a domain feature, which is the creep
# this check exists to catch. File access EXISTS (relay 0.4.0) but it is a
# capability, not a default — the check fails if the opt-in gate disappears.
echo "[5] No streaming endpoints; snapshot store stays generic; file access stays opt-in"
CREEP_HITS=$(count_matches "/stream\|EventSource\|SSE" "$REPO_ROOT/nova/src/http/handlers/")
if (( CREEP_HITS != 0 )); then
  fail "Found ${CREEP_HITS} hits for streaming endpoints in handlers/ — the relay must stay minimal."
else
  pass "No streaming endpoints found"
fi
if [[ -f "$REPO_ROOT/nova/src/http/handlers/backups.ts" ]]; then
  DOMAIN_HITS=$(count_matches "ha/rest-client\|ha/ws-client\|coreClient\|wsClient" "$REPO_ROOT/nova/src/http/handlers/backups.ts")
  if (( DOMAIN_HITS != 0 )); then
    fail "backups.ts touches the HA clients — the snapshot store must stay a generic blob store."
  else
    pass "Snapshot store is a generic blob store (no HA client imports)"
  fi
fi

if [[ -f "$REPO_ROOT/nova/src/http/handlers/files.ts" ]]; then
  if grep -q 'FILE_ACCESS_DISABLED' "$REPO_ROOT/nova/src/http/handlers/files.ts" \
     && grep -q 'return "off"' "$REPO_ROOT/nova/src/config/file-access.ts" \
     && grep -q 'file_access: "off"' "$REPO_ROOT/nova/config.yaml"; then
    pass "File access is opt-in and defaults to off"
  else
    fail "File access must default to OFF and refuse with FILE_ACCESS_DISABLED — the gate is the guarantee."
  fi
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
for client in claude codex opencode antigravity hermes; do
  SCRIPT_EXISTS=$(grep -c "install.*${client}" "$REPO_ROOT/package.json" 2>/dev/null || true)
  if (( SCRIPT_EXISTS > 0 )); then
    pass "Install script for ${client} found"
  else
    fail "README lists ${client} as supported but no install script found in package.json"
  fi
done

# ── 9. Route count — verify relay stays minimal ──
echo "[9] Relay route count"
ROUTE_COUNT=$(grep -c "router.register" "$REPO_ROOT/nova/src/index.ts" 2>/dev/null || true)
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
# "segment" alone is an English word (path segments!) — match the vendors, not
# the vocabulary, or the check cries wolf and gets ignored.
TELEMETRY_HITS=$(count_matches "telemetry\|analytics\|mixpanel\|segment\.io\|@segment/\|posthog\|sentry" "$REPO_ROOT/nova/src")
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
