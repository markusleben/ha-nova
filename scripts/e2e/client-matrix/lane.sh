#!/usr/bin/env bash
# One platform lane of the client release matrix (run.sh starts it on the macOS
# host or inside a throwaway Linux container). Two fresh scenarios, each in its
# own throwaway HOME:
#   A  SHELL=zsh   install FROM -> setup (codex, opencode, claude) -> MCP -> update TO
#                  -> checks -> uninstall --yes
#   B  SHELL=bash  install FROM -> setup -> update TO -> uninstall --purge
#                  (Linux: real headless `setup --service hermes`)
# In:  LANE FROM TO TO_VERSION BUNDLE_BASE RELAY_URL HA_URL RELAY_TOKEN WORK RESULTS
#      CLAUDE_BIN (optional; macOS lane only)
# Out: "lane|scenario|step|PASS/FAIL/SKIP|detail" lines appended to RESULTS.
set -uo pipefail

INSTALL_URL="https://raw.githubusercontent.com/markusleben/ha-nova/${FROM}/install.sh"

record() { printf '%s|%s|%s|%s|%s\n' "$LANE" "$SCN" "$1" "$2" "${3//|//}" >>"$RESULTS"; }
pass() { record "$1" PASS "${2:-}"; }
fail() { record "$1" FAIL "${2:-}"; }
skip() { record "$1" SKIP "${2:-}"; }

# Every ha-nova call: scrubbed environment, throwaway HOME/XDG, no stdin.
hn() { env -i "${ENV[@]}" ha-nova "$@" </dev/null; }

# run_step <name> <cmd...>: PASS on exit 0 (detail = last "note: " line), else
# FAIL with the log's last line.
run_step() {
  local name="$1"; shift
  "$@" >"$LOGS/$name.log" 2>&1
  local rc=$?
  ((rc == 0)) && { pass "$name" "$(sed -n 's/^note: //p' "$LOGS/$name.log" | tail -n 1)"; return 0; }
  fail "$name" "exit $rc - $(tail -n 1 "$LOGS/$name.log") (log: $LOGS/$name.log)"
  return 1
}

# shows <ERE> <cmd...>: print the output, succeed when it matches (no grep -q
# behind a pipe: an early exit SIGPIPEs the writer and pipefail flips the result).
shows() {
  local re="$1" out; shift
  out="$("$@" 2>&1)" || { printf '%s\n' "$out"; return 1; }
  printf '%s\n' "$out"
  grep -Eq -- "$re" <<<"$out"
}

ver_lt() { # ver_lt a b: a < b for X.Y.Z (pre-release suffix ignored)
  local i x y; IFS=. read -r -a x <<<"${1%%-*}"; IFS=. read -r -a y <<<"${2%%-*}"
  for i in 0 1 2; do
    ((${x[i]:-0} < ${y[i]:-0})) && return 0
    ((${x[i]:-0} > ${y[i]:-0})) && return 1
  done
  return 1
}

bundle_asset() {
  local os arch
  case "$(uname -s)" in Darwin) os=macos ;; *) os=linux ;; esac
  case "$(uname -m)" in x86_64) arch=amd64 ;; *) arch=arm64 ;; esac
  printf 'ha-nova-installer-bundle-%s-%s.tar.gz\n' "$os" "$arch"
}

# new_scenario <id> <shell> <keyring|service>
#   keyring: desktop token path; the token goes to a test keyring file
#            (HA_NOVA_TEST_KEYRING_FILE), standing in for an unlocked keyring
#   service: real headless path, `setup --service hermes`, token file in config
new_scenario() {
  SCN="$1"; MODE="$3"; S="$WORK/$LANE-$1"; H="$S/home"; LOGS="$S/logs"
  mkdir -p "$H" "$LOGS" "$S/tmp" "$S/bin"
  # Client runtimes are detected by PATH lookup; stubs stand in for them.
  local c
  for c in codex opencode hermes; do printf '#!/bin/sh\nexit 0\n' >"$S/bin/$c"; chmod +x "$S/bin/$c"; done
  [[ -n "${CLAUDE_BIN:-}" ]] && ln -s "$CLAUDE_BIN" "$S/bin/claude"
  ENV=(HOME="$H" USER="$(id -un)" LOGNAME="$(id -un)" TMPDIR="$S/tmp" LANG=C.UTF-8 TERM=dumb
    XDG_CONFIG_HOME="$H/.config" XDG_DATA_HOME="$H/.local/share" XDG_CACHE_HOME="$H/.cache"
    XDG_STATE_HOME="$H/.local/state" CLAUDE_CONFIG_DIR="$H/.claude" SHELL="$2"
    PATH="$H/.local/bin:$S/bin:/usr/bin:/bin:/usr/sbin:/sbin" HA_NOVA_NO_BROWSER=1)
  TOKEN_STORE="$H/.config/ha-nova/relay-token"
  if [[ "$MODE" == keyring ]]; then
    TOKEN_STORE="$S/relay-token.test"
    ENV+=(HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1 HA_NOVA_TEST_KEYRING_FILE="$TOKEN_STORE"
      HA_NOVA_KEYRING_SERVICE="ha-nova.client-matrix.$$")
  fi
  if [[ "$LANE" == macos ]]; then
    # Routes this throwaway install's own device credential to a file. NOT
    # keychain isolation: the macOS keychain belongs to the OS user, not to
    # HOME, so purge is skipped below when it holds HA NOVA credentials.
    mkdir -p "$H/.config/ha-nova/secrets" && chmod 700 "$H/.config/ha-nova/secrets"
    (umask 077 && printf 'file\n' >"$H/.config/ha-nova/secrets/.file-backend")
  fi
}

has_verb() { hn "$1" --help >/dev/null 2>&1; } # probes the verb itself: `mcp` is callable but not listed in --help

rc_file() { case "$1" in */zsh) echo "$H/.zshrc" ;; *) [[ -f "$H/.bash_profile" ]] && echo "$H/.bash_profile" || echo "$H/.profile" ;; esac; }

# --- steps --------------------------------------------------------------------

do_install() { curl -fsSL "$INSTALL_URL" | env -i "${ENV[@]}" HA_NOVA_VERSION="$FROM" HA_NOVA_NO_SETUP=1 bash; }

check_install() {
  local v; v="$(hn version)"
  [[ "$v" == "${FROM#v}" ]] || { echo "version $v != $FROM"; return 1; }
  grep -q '^# Added by HA NOVA$' "$RC" || { echo "no PATH block in $RC"; return 1; }
  [[ -L "$H/.local/bin/ha-nova" ]] || { echo "no ~/.local/bin/ha-nova link"; return 1; }
  echo "note: v$v, PATH block in ${RC##*/}"
}

do_setup() {
  local args=(--non-interactive --relay-url "$RELAY_URL" --relay-token "$RELAY_TOKEN" --ha-url "$HA_URL")
  if [[ "$MODE" == service ]]; then
    hn setup --service hermes "${args[@]}" && echo "note: setup --service hermes (token file)"
  else
    hn setup codex "${args[@]}" && hn setup opencode --non-interactive &&
      echo "note: relay token, codex + opencode"
  fi
}

do_read() {
  if has_verb find; then
    shows '"entity_id":"light\.' hn find --domain light && echo "note: find --domain light"
  else
    shows 'API running' hn relay core --method GET --path /api/ && echo "note: relay core GET /api/ (no find verb)"
  fi
}

check_clients() { # skill trees exist, resolve into the installed bundle, match it
  [[ "$MODE" == service ]] && { check_hermes "$@"; return; }
  local root="$H/.local/share/ha-nova" link real want
  want="$(cd -P "$root/skills" && pwd -P)" || return 1
  for link in "$H/.agents/skills/ha-nova" "$H/.config/opencode/skills/ha-nova"; do
    [[ -e "$link/ha-nova/SKILL.md" ]] || { echo "missing $link/ha-nova/SKILL.md"; return 1; }
    cmp -s "$link/ha-nova/SKILL.md" "$root/skills/ha-nova/SKILL.md" || { echo "$link differs from bundle"; return 1; }
    if [[ -L "$link" ]]; then
      real="$(cd -P "$link" && pwd -P)"
      [[ "$real" == "$want" ]] || { echo "$link -> $real, want $want"; return 1; }
    fi
  done
  grep -q '"codex"' "$H/.config/ha-nova/state.json" && grep -q '"opencode"' "$H/.config/ha-nova/state.json" &&
    echo "note: codex + opencode skill trees -> installed bundle"
}

# check_hermes [stamp]: every bundle skill has its namespaced Hermes dir
# (ha-nova, ha-nova-<skill>) with the same .md files and line counts (Hermes
# rewrites refs in place, never adds lines), no stale extra dirs, and - with a
# stamp - every file rewritten after it (the update really resynced).
check_hermes() {
  local src="$H/.local/share/ha-nova/skills" dst="$H/.hermes/skills/ha-nova" dir name f want=0 got
  for dir in "$src"/*/; do
    [[ -f "$dir/SKILL.md" ]] || continue
    name="${dir%/}"; name="${name##*/}"; [[ "$name" == ha-nova ]] || name="ha-nova-$name"
    want=$((want + 1))
    for f in "$dir"*.md; do
      [[ -f "$dst/$name/${f##*/}" ]] || { echo "missing $dst/$name/${f##*/}"; return 1; }
      [[ "$(wc -l <"$f")" == "$(wc -l <"$dst/$name/${f##*/}")" ]] || { echo "stale $dst/$name/${f##*/}"; return 1; }
    done
  done
  got="$(find "$dst" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')"
  ((want > 1 && got == want)) || { echo "hermes dirs: $got, bundle skills: $want"; return 1; }
  if [[ -n "${1:-}" && -n "$(find "$dst" -type f ! -newer "$1")" ]]; then
    echo "not resynced:"; find "$dst" -type f ! -newer "$1"; return 1
  fi
  grep -q '"hermes"' "$H/.config/ha-nova/state.json" && echo "note: hermes: $got namespaced skills match the bundle${1:+, resynced}"
}

claude_plugins() { env -i "${ENV[@]}" claude plugin list </dev/null; }
do_claude() { hn setup claude --non-interactive && check_claude "${FROM#v}"; }
check_claude() { # ha-nova@ha-nova installed, enabled, at the runtime's version
  local out; out="$(claude_plugins 2>&1)"; printf '%s\n' "$out"
  out="$(grep -A4 'ha-nova@ha-nova' <<<"$out")"
  # An RC plugin carries its base version; the release dir names the exact tag.
  grep -q "Version: ${1%%-rc*}$" <<<"$out" && grep -q "releases/v$1/" <<<"$out" &&
    grep -q 'enabled' <<<"$out" &&
    echo "note: ha-nova@ha-nova v$1 enabled (isolated CLAUDE_CONFIG_DIR)"
}

do_mcp() {
  printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"client-matrix","version":"1"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ha_find","arguments":{"domain":"light"}}}' |
    env -i "${ENV[@]}" ha-nova mcp >"$S/mcp.out" || return 1
  cat "$S/mcp.out"
  grep -q '"id":1,.*"protocolVersion"' "$S/mcp.out" &&
    grep -q '"id":2,.*"name":"ha_find"' "$S/mcp.out" &&
    grep -q 'light\.' <<<"$(grep '"id":3,' "$S/mcp.out")" &&
    ! grep -q '"isError":true' <<<"$(grep '"id":3,' "$S/mcp.out")" &&
    echo "note: initialize + tools/list + ha_find(domain light) ok"
}

snapshot() { # key config fields + stored token, compared across the update
  grep -E '"(default_server|ha_url|relay_base_url|profile_id|route_policy|relay_token_file)"' \
    "$H/.config/ha-nova/config.json" | sort
  printf 'token:%s\n' "$(cat "$TOKEN_STORE" 2>/dev/null | tr -d '[:space:]' | cksum)"
}

do_update() {
  if [[ "$TO" == local ]]; then
    env -i "${ENV[@]}" HA_NOVA_BUNDLE_URL="$BUNDLE_BASE/$(bundle_asset)" ha-nova update --version "$TO_VERSION" </dev/null
  else
    hn update --version "$TO_VERSION"
  fi
}

check_survived() {
  local v; v="$(hn version)"
  [[ "$v" == "$TO_VERSION" ]] || { echo "version $v != $TO_VERSION"; return 1; }
  snapshot >"$S/after.txt"
  diff "$S/before.txt" "$S/after.txt" || { echo "config/token changed across update"; return 1; }
  [[ -n "$(tr -d '[:space:]' <"$TOKEN_STORE")" ]] || { echo "token empty"; return 1; }
  grep -q "\"clients_verified_version\": \"$TO_VERSION\"" "$H/.config/ha-nova/state.json" ||
    { echo "clients not verified for $TO_VERSION"; cat "$H/.config/ha-nova/state.json"; return 1; }
  shows '^default ' hn server list && echo "note: v$v; config, token, profile unchanged"
}

check_relay_notice() {
  local min relay
  min="$(sed -n 's/.*"min_relay_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$H/.local/share/ha-nova/version.json")"
  relay="$(curl -fsS -H "Authorization: Bearer $RELAY_TOKEN" "$RELAY_URL/health" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
  [[ -n "$min" && -n "$relay" ]] || { echo "cannot read min ($min) or relay ($relay) version"; return 1; }
  if ver_lt "$relay" "$min"; then
    grep -q "Relay outdated: v$relay is below minimum v$min" "$LOGS/update.log" &&
      echo "note: relay v$relay < min v$min: outdated notice shown (expected)"
  else
    ! grep -q 'Relay outdated' "$LOGS/update.log" && echo "note: relay v$relay >= min v$min: no outdated notice (correct)"
  fi
}

do_uninstall() { hn uninstall --yes "$@"; }

# Leftovers in HOME after uninstall: any file/link whose path or content names
# ha-nova (client files like Claude's own .claude.json backups do not count).
# Kept by contract:
#   --yes:   ~/.config/ha-nova/{config.json,relay-token,secrets/.file-backend} (settings kept for reinstall)
#   both:    ~/.config/.ha-nova-* opaque lifecycle markers (README: retained marker)
check_leftovers() {
  local mode="$1" kept=() keep='^\.config/\.ha-nova-[a-z-]+$' bad=() rel f
  [[ "$mode" == yes ]] && keep="$keep|^\\.config/ha-nova/(config\\.json|relay-token|secrets/\\.file-backend)\$"
  while IFS= read -r f; do
    rel="${f#"$H"/}"
    case "$rel" in .zshrc | .profile | .bash_profile) continue ;; esac
    [[ "$rel" =~ $keep ]] && { kept+=("${rel##*/}"); continue; }
    if [[ "$rel" == *ha-nova* || -L "$f" ]] || grep -qi 'ha-nova' "$f" 2>/dev/null; then bad+=("$rel"); fi
  done < <(find "$H" -mindepth 1 \( -type f -o -type l \))
  while IFS= read -r f; do
    rel="${f#"$H"/}"
    [[ "$mode" == yes && "$rel" =~ ^\.config/ha-nova(/secrets)?$ ]] || bad+=("$rel/")
  done < <(find "$H" -mindepth 1 -type d -iname '*ha-nova*' -o -type d -path '*/ha-nova/*')
  ((${#bad[@]} == 0)) || { printf 'leftover: %s\n' "${bad[@]}"; return 1; }
  if [[ "$mode" == yes ]]; then
    [[ -f "$H/.config/ha-nova/config.json" && -s "$TOKEN_STORE" ]] || { echo "--yes must keep config + token"; return 1; }
  else
    [[ ! -e "$TOKEN_STORE" ]] || { echo "--purge left the token at $TOKEN_STORE"; return 1; }
  fi
  echo "note: kept ${kept[*]:-nothing}"
}

# The rc file is seeded with a sentinel before install: uninstall must keep the
# file and the user's own lines, and drop only the managed block.
SENTINEL='# client-matrix sentinel: user content'
check_rc_clean() {
  [[ -f "$RC" ]] || { echo "$RC was deleted"; return 1; }
  grep -qxF "$SENTINEL" "$RC" || { echo "user line lost from $RC"; cat "$RC"; return 1; }
  ! grep -q 'Added by HA NOVA' "$RC" && ! grep -q 'HOME/.local/bin' "$RC" || { cat "$RC"; return 1; }
  echo "note: PATH block removed from ${RC##*/}, user line kept"
}

check_relay_alive() { shows '"ok":true' curl -fsS -H "Authorization: Bearer $RELAY_TOKEN" "$RELAY_URL/health" && echo "note: test bed relay still answers"; }

# The macOS keychain is per OS user: never purge in a login session whose
# keychain holds real HA NOVA device credentials. Item names only, no secrets.
real_keychain_has_ha_nova() {
  [[ "$LANE" == macos ]] || return 1
  local n
  n="$(security dump-keychain 2>/dev/null | grep -c 'ha-nova.device-credential' || true)"
  [[ "${n:-0}" -gt 0 ]]
}

# --- scenarios ----------------------------------------------------------------

scenario_a() {
  new_scenario A /bin/zsh keyring; RC="$(rc_file /bin/zsh)"; printf '%s\n' "$SENTINEL" >"$RC"
  run_step install do_install && run_step install-check check_install || return
  run_step setup do_setup || return
  run_step doctor hn doctor
  run_step relay-health hn relay health
  run_step read do_read
  run_step clients check_clients
  if [[ -n "${CLAUDE_BIN:-}" ]]; then run_step claude do_claude
  else skip claude "no claude binary on this lane; real Claude config never used"; fi
  if has_verb mcp; then run_step mcp do_mcp
  else skip mcp "ha-nova mcp not in $FROM"; fi
  snapshot >"$S/before.txt"
  run_step update do_update || return
  run_step update-survived check_survived
  run_step update-relay-notice check_relay_notice
  run_step update-clients check_clients
  [[ -n "${CLAUDE_BIN:-}" ]] && run_step update-claude check_claude "$TO_VERSION"
  run_step update-doctor hn doctor
  run_step update-read do_read
  if has_verb mcp; then run_step update-mcp do_mcp
  else skip update-mcp "ha-nova mcp not in $TO"; fi
  run_step uninstall do_uninstall || return
  run_step uninstall-leftovers check_leftovers yes
  run_step uninstall-rc check_rc_clean
  run_step relay-still-up check_relay_alive
}

scenario_b() {
  # Linux B takes the real headless path (no secure storage in the container).
  local mode=keyring; [[ "$LANE" == linux ]] && mode=service
  new_scenario B /bin/bash "$mode"; RC="$(rc_file /bin/bash)"; printf '%s\n' "$SENTINEL" >"$RC"
  run_step install do_install && run_step install-check check_install || return
  run_step setup do_setup || return
  run_step clients check_clients
  snapshot >"$S/before.txt"
  run_step update do_update || return
  run_step update-survived check_survived
  run_step update-clients check_clients "$S/before.txt"
  run_step update-doctor hn doctor
  if real_keychain_has_ha_nova; then
    local why="real keychain holds HA NOVA credentials; run purge on the Linux lane or a separate macOS user"
    skip purge "$why"; skip purge-leftovers "$why"; skip purge-rc "$why"
  else
    run_step purge do_uninstall --purge || return
    run_step purge-leftovers check_leftovers purge
    run_step purge-rc check_rc_clean
  fi
  run_step relay-still-up check_relay_alive
}

scenario_a
scenario_b
exit 0 # results carry the verdict; run.sh flags a lane without results
