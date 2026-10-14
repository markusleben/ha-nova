#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  HA_NOVA_LIVE_SSH_HOST=user@host \
  HA_NOVA_LIVE_INSTALL_CMD='curl -fsSL https://your-bundle-host.example/install.sh | bash' \
  ./scripts/smoke/linux-headless-setup-check.sh

Required env:
  HA_NOVA_LIVE_SSH_HOST    SSH target for the real Linux host
  HA_NOVA_LIVE_INSTALL_CMD Install command to run remotely before setup

Important:
  For SSH/headless Linux recovery proof, connect to the same logged-in desktop
  user session that owns the user D-Bus / Secret Service session.

Optional env:
  HA_NOVA_LIVE_SETUP_CMD   Interactive setup command to run remotely
                           default: HA_NOVA_NO_BROWSER=1 ha-nova setup
                           Hermes proof override:
                           HA_NOVA_LIVE_SETUP_CMD='HA_NOVA_NO_BROWSER=1 ha-nova setup hermes'
  HA_NOVA_LIVE_SKIP_INSTALL=1
                           Skip the remote install command and run setup only
                           repair/debug only; not a full release-lane proof

What this helper does:
  1. Verifies SSH connectivity to the Linux host
  2. Captures a small keyring/session preflight snapshot on the remote host
  3. Runs the provided install command unless HA_NOVA_LIVE_SKIP_INSTALL=1
  4. Hands off into an interactive remote `ha-nova setup ...` session

What this helper does not do:
  - store hostnames, tokens, or passwords in the repo
  - bypass the interactive Linux secure-storage prompts
  - mutate your release docs; it is only a live validation helper
EOF
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "[linux-live-check] missing required env: ${name}" >&2
    usage >&2
    exit 1
  fi
}

run_remote() {
  local command="$1"
  local isolated_command="export HA_NOVA_NO_CENSUS=1; ${command}"
  ssh "${HA_NOVA_LIVE_SSH_HOST}" "bash -lc $(printf '%q' "$isolated_command")"
}

run_remote_tty() {
  local command="$1"
  local isolated_command="export HA_NOVA_NO_CENSUS=1; ${command}"
  ssh -tt "${HA_NOVA_LIVE_SSH_HOST}" "bash -lc $(printf '%q' "$isolated_command")"
}

if [[ "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

require_env "HA_NOVA_LIVE_SSH_HOST"
if [[ "${HA_NOVA_LIVE_SKIP_INSTALL:-0}" != "1" ]]; then
  require_env "HA_NOVA_LIVE_INSTALL_CMD"
fi

setup_cmd="${HA_NOVA_LIVE_SETUP_CMD:-HA_NOVA_NO_BROWSER=1 ha-nova setup}"

echo "[linux-live-check] SSH target: ${HA_NOVA_LIVE_SSH_HOST}"
echo "[linux-live-check] checking remote session..."
run_remote 'set -euo pipefail; whoami >/dev/null; uname -srm'

echo "[linux-live-check] capturing remote secure-storage preflight..."
preflight_output="$(run_remote '
set -euo pipefail
echo "[remote] DBUS_SESSION_BUS_ADDRESS=${DBUS_SESSION_BUS_ADDRESS:-<unset>}"
if command -v gdbus >/dev/null 2>&1; then
  echo "[remote] Secret Service default alias:"
  gdbus call --session \
    --dest org.freedesktop.secrets \
    --object-path /org/freedesktop/secrets \
    --method org.freedesktop.Secret.Service.ReadAlias default || true
else
  echo "[remote] gdbus not installed"
fi
if command -v ha-nova >/dev/null 2>&1; then
  echo "[remote] installed ha-nova version:"
  ha-nova version || true
else
  echo "[remote] ha-nova not installed yet"
fi
')"
printf '%s\n' "${preflight_output}"

if [[ "${preflight_output}" == *"DBUS_SESSION_BUS_ADDRESS=<unset>"* ]]; then
  echo "[linux-live-check] warning: remote user D-Bus session looks unset; reconnect inside the same logged-in desktop user session before treating this as a release proof." >&2
fi

if [[ "${preflight_output}" == *"[remote] gdbus not installed"* ]]; then
  echo "[linux-live-check] warning: remote gdbus is missing; Secret Service preflight evidence is incomplete and this usually is not a valid release-proof state." >&2
fi

if [[ "${HA_NOVA_LIVE_SKIP_INSTALL:-0}" != "1" ]]; then
  echo "[linux-live-check] running remote install command..."
  run_remote_tty "${HA_NOVA_LIVE_INSTALL_CMD}"
fi

echo "[linux-live-check] starting interactive remote setup..."
echo "[linux-live-check] complete the Linux secure-storage prompts, then finish the normal setup flow."
run_remote_tty "${setup_cmd}"
