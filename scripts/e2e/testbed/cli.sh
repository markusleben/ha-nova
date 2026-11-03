#!/usr/bin/env bash
# Run the ha-nova CLI, built from THIS checkout, against the test bed — with an
# isolated HOME in .state/home and the file-backed test keyring. Never touches
# the real ~/.config/ha-nova or an installed ha-nova binary.
#
#   bash scripts/e2e/testbed/cli.sh find "Leselampe"     (npm run testbed:cli -- …)
#   eval "$(bash scripts/e2e/testbed/cli.sh --env)"      exports the env + PATH
#
# TESTBED_CLI_BIN=<path to an ha-nova binary> runs that build instead (the RC
# lane: a published tag); TESTBED_CLI_HOME=<dir inside .state> uses another
# isolated home (the phone lane keeps its link out of the default home).
set -euo pipefail

# eval "$(… --env)" evals nothing when this script fails, so a later ha-nova
# would be the installed one with the real HOME and reach a real home. On any
# failure, --env prints exports that leave that CLI with no settings, then
# false, so the eval fails too.
if [[ "${1:-}" == "--env" ]]; then
  trap 'rc=$?; (( rc == 0 )) || printf "export HOME=/var/empty/ha-nova-testbed-env-failed XDG_CONFIG_HOME=/var/empty/ha-nova-testbed-env-failed\nfalse\n"' EXIT
fi

HERE="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
STATE="$HERE/.state"
CLI_HOME="${TESTBED_CLI_HOME:-$STATE/home}"
BIN="${TESTBED_CLI_BIN:-$STATE/bin/ha-nova}"

[[ -f "$STATE/relay-token" ]] || { echo "testbed: not running; npm run testbed:up first" >&2; exit 1; }
# down.sh wipes .state: a home anywhere else would outlive the bed.
[[ "$CLI_HOME" == "$STATE"/* && "$CLI_HOME" != *..* ]] || { echo "testbed: TESTBED_CLI_HOME must be inside $STATE" >&2; exit 1; }

# Always build: go's build cache makes this cheap, and it never runs stale code.
[[ -n "${TESTBED_CLI_BIN:-}" ]] || (cd "$ROOT/cli" && go build -o "$BIN" .) >&2

if [[ ! -f "$CLI_HOME/.config/ha-nova/config.json" ]]; then
  mkdir -p "$CLI_HOME/.config/ha-nova" "$CLI_HOME/.cache/ha-nova"
  printf '{"ha_url":"http://127.0.0.1:18123","relay_base_url":"http://127.0.0.1:18791"}\n' \
    > "$CLI_HOME/.config/ha-nova/config.json"
  (umask 077 && cp "$STATE/relay-token" "$CLI_HOME/.test-relay-token")
fi

ENV_VARS=(
  "HOME=$CLI_HOME"
  "XDG_CONFIG_HOME=$CLI_HOME/.config"
  "XDG_CACHE_HOME=$CLI_HOME/.cache"
  "XDG_DATA_HOME=$CLI_HOME/.local/share"
  "HA_NOVA_ALLOW_INSECURE_TEST_KEYRING=1"
  "HA_NOVA_TEST_KEYRING_FILE=$CLI_HOME/.test-relay-token"
  "HA_NOVA_NO_BROWSER=1"
  "HA_NOVA_DEV_ROOT=$ROOT"
)

if [[ "${1:-}" == "--env" ]]; then
  for kv in "${ENV_VARS[@]}"; do printf 'export %s=%q\n' "${kv%%=*}" "${kv#*=}"; done
  printf 'export PATH=%q:"$PATH"\n' "$(dirname "$BIN")"
  exit 0
fi
exec env "${ENV_VARS[@]}" "$BIN" "$@"
