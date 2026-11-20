#!/usr/bin/env bash
#
# check-docs.sh — Verify documentation facts and lifecycle rules against the
# actual codebase. Run in CI to catch stale docs.
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
# The relay must stay small enough that a person can read it in one sitting.
# Growth here is security surface: raise the ceiling only with the reason in
# the PR body (git log keeps every raise). The README's own number is updated
# in the release-prep PR — README describes the STABLE release, not main.
echo "[1] Relay LOC (must stay readable in one sitting)"
ACTUAL_LOC=$(find "$REPO_ROOT/nova/src" -name '*.ts' -exec cat {} + | wc -l | tr -d ' ')
if (( ACTUAL_LOC >= 1000 && ACTUAL_LOC <= 14530 )); then
  pass "src/ = ${ACTUAL_LOC} LOC (within 1000–14530 range)"
else
  fail "src/ = ${ACTUAL_LOC} LOC — outside the 1000–14530 range. Justify real Relay growth in active architecture docs; update README only in a release-prep PR."
fi

# ── 2. Skill count ──
# Active skill inventory expects 35 top-level skill directories
# (context skill + 34 sub-skills; hacs added 2026-08-04, device and home-map 2026-10-16, migrate and esphome 2026-10-17)
echo "[2] Skill directory count (current inventory expects 35)"
SKILL_COUNT=$(find "$REPO_ROOT/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
if (( SKILL_COUNT == 35 )); then
  pass "skills/ has ${SKILL_COUNT} directories"
else
  fail "skills/ has ${SKILL_COUNT} directories — active docs/contracts expect 35. Update PROJECT and architecture docs; update README only in release prep."
fi

# ── 2b. One relay codebase, two distributions ──
# The standalone container must build from the SAME src/ as the App. A second
# implementation (e.g. a Python custom component) would drift from the first.
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
# The Relay is no MCP server and holds no tool definitions (`ha-nova mcp` lives in the CLI)
echo "[3] No MCP/tool-definition patterns in src/"
MCP_HITS=$(count_matches "fastmcp\|@mcp\.tool\|mcp\.tool\|McpServer\|MCP_TOOL" "$REPO_ROOT/nova/src")
if (( MCP_HITS == 0 )); then
  pass "No MCP/tool-definition patterns found in src/"
else
  fail "Found ${MCP_HITS} MCP-related patterns in src/ — the Relay must hold no MCP server or tool definitions."
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

# ── 5b. The ha_nova integration stays a pure pass-through ──
# Spec §11.1: discovery, the proxy, and the user hand-off only. Its code
# changes need a Home Assistant restart, so tools, schemas, and checks live in
# the bridge (`ha-nova serve`), never here.
echo "[5b] ha_nova integration: discovery + authenticated pass-through only"
INTEG="$REPO_ROOT/nova/integration/ha_nova"
# No __pycache__ either: the image and the App copy this folder as it is.
INTEG_FILES=$(cd "$INTEG" && find . -mindepth 1 | LC_ALL=C sort | tr '\n' ' ')
if [[ "$INTEG_FILES" != "./__init__.py ./config_flow.py ./manifest.json " ]]; then
  fail "nova/integration/ha_nova holds unexpected files ($INTEG_FILES) — only __init__.py, config_flow.py, manifest.json."
fi
INTEG_LOC=$(cat "$INTEG"/*.py | wc -l | tr -d ' ')
INTEG_LOGIC=$(count_matches -E "hass\.services|hass\.states|helpers\.llm|import llm|async_register_api|async_forward_entry_setups|entity_registry|device_registry|async_track_|json\.loads|request\.(json|read|text|post)\(|\.json\(\)" "$INTEG" --include='*.py')
if (( INTEG_LOC > 230 || INTEG_LOGIC != 0 )) \
   || ! grep -q '"requirements": \[\]' "$INTEG/manifest.json" \
   || ! grep -q '"dependencies": \["http"\]' "$INTEG/manifest.json" \
   || [[ "$(grep -c 'requires_auth' "$INTEG/__init__.py")" != 1 ]] \
   || ! grep -q '^    requires_auth = True$' "$INTEG/__init__.py" \
   || [[ "$(grep -c 'register_view(' "$INTEG/__init__.py")" != 1 ]] \
   || [[ "$(grep -c 'request.get(KEY_HASS_USER)' "$INTEG/__init__.py")" != 1 ]] \
   || ! grep -q '("X-HA-User-Id", user.id)' "$INTEG/__init__.py"; then
  fail "ha_nova integration grew logic (${INTEG_LOC} Python lines, ${INTEG_LOGIC} logic hits) or lost requires_auth / the authenticated user header — it must stay a pure pass-through."
else
  pass "ha_nova integration is a pure pass-through (${INTEG_LOC} Python lines)"
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
# The standalone listener's routes. 7 until layer 4e, then 9: the two ask routes
# (POST /gate/v1/ask, POST /gate/v1/phone) without which a standalone container
# could never approve a typed request; the App's listeners have them already.
ROUTE_COUNT=$(grep -c "router.register" "$REPO_ROOT/nova/src/index.ts" 2>/dev/null || true)
if (( ROUTE_COUNT <= 9 )); then
  pass "Relay has ${ROUTE_COUNT} routes (≤9 — still minimal)"
else
  fail "Relay has ${ROUTE_COUNT} routes — growing beyond 'minimal'. Review architecture claims."
fi

echo "[9c] The App's owner page and Cloud routes stay Supervisor-ingress-only"
if grep -q 'panel_admin: true' "$REPO_ROOT/nova/config.yaml" \
  && grep -q 'ingress_entry: /home' "$REPO_ROOT/nova/config.yaml" \
  && grep -q 'SUPERVISOR_INGRESS_PEERS' "$REPO_ROOT/nova/src/security/ingress-identity.ts" \
  && grep -q '"GET", "/home", { auth: "identity"' "$REPO_ROOT/nova/src/runtime/ingress-routes.ts" \
  && grep -q 'resolveIngressIdentity' "$REPO_ROOT/nova/src/http/handlers/cloud.ts" \
  && grep -q 'resolveIngressIdentity' "$REPO_ROOT/nova/src/runtime/ingress-listener.ts"; then
  pass "Owner page and Cloud routes share the strict Supervisor ingress gate behind the admin-only panel"
else
  fail "Owner page and Cloud routes must share the strict Supervisor ingress gate behind the admin-only panel."
fi

# Pairing is a generic credential exchange, never a second HA API surface.
echo "[9b] Pairing stays transport-only and narrowly bearer-exempt"
PAIR_HITS=$(count_matches "ha/rest-client\|ha/ws-client\|coreClient\|wsClient\|entity_id\|service_data" \
  "$REPO_ROOT/nova/src/http/handlers/pair.ts" "$REPO_ROOT/nova/src/security/pairing.ts")
if (( PAIR_HITS == 0 )); then
  pass "Pairing has no HA client or domain imports"
else
  fail "Pairing contains ${PAIR_HITS} HA client/domain hits — it must remain a generic credential exchange."
fi
if grep -q 'const PAIR_PATH = "/pair"' "$REPO_ROOT/nova/src/index.ts" \
  && grep -q 'bearerExemptRoutes: new Set(\[PAIR_ROUTE\])' "$REPO_ROOT/nova/src/index.ts"; then
  pass "Only POST /pair bypasses relay bearer auth"
else
  fail "Bearer exemptions must stay pinned to POST /pair."
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
# Scope: relay and CLI. -I skips locally built binaries.
# Home Assistant's own analytics settings ("analytics", "analytics/preferences",
# admin skill) are WS types HA NOVA sends to Home Assistant, not a send path.
TELEMETRY_HITS=$( (grep -rnI "telemetry\|analytics\|mixpanel\|segment\.io\|@segment/\|posthog\|sentry" "$REPO_ROOT/nova/src" "$REPO_ROOT/cli" 2>/dev/null || true) | grep -cvE '"analytics(/preferences)?"' || true)
if (( TELEMETRY_HITS == 0 )); then
  pass "No telemetry/analytics patterns in relay or CLI"
else
  fail "Found ${TELEMETRY_HITS} telemetry-related patterns — HA NOVA promises no usage data collection."
fi

# ── 13. Work docs declare whether they are in progress or shipped ──
echo "[13] Work-document lifecycle"
while IFS= read -r work_doc_path; do
  declared_status_line=$(grep -m1 '^Status:' "$work_doc_path" || true)
  if [[ "$declared_status_line" =~ ^Status:\ (active|shipped)($|[[:space:]]) ]]; then
    pass "${work_doc_path#"$REPO_ROOT/"} declares ${BASH_REMATCH[1]} status"
  else
    fail "${work_doc_path#"$REPO_ROOT/"} must declare Status: active or shipped — move superseded or abandoned work to docs/archive/work"
  fi
done < <(
  find "$REPO_ROOT/docs/work" -maxdepth 1 -type f -name '*.md' \
    ! -name 'README.md' -print | sort
)

current_client_version=$(node -p "require('$REPO_ROOT/package.json').version")
while IFS= read -r release_body_path; do
  release_body_name=$(basename "$release_body_path")
  release_body_version=${release_body_name%-release-body.md}
  if node -e '
    const [candidate, current] = process.argv.slice(1).map((value) =>
      value.split(".").map((part) => Number.parseInt(part, 10)),
    );
    for (let index = 0; index < 3; index += 1) {
      if (candidate[index] > current[index]) process.exit(0);
      if (candidate[index] < current[index]) process.exit(1);
    }
    process.exit(1);
  ' "$release_body_version" "$current_client_version"; then
    pass "$release_body_name targets a future version"
  else
    fail "$release_body_name is not newer than v$current_client_version — archive it during release prep"
  fi
done < <(
  find "$REPO_ROOT/docs/work" -maxdepth 1 -type f \
    -name '*-release-body.md' ! -name 'next-release-body.md' -print | sort
)

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
