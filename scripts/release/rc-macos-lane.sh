#!/usr/bin/env bash
# The macOS part of an rc's checks on a disposable GitHub-hosted runner (never
# a person's Mac): the published installer puts the rc in place, and the
# installed binary must be signed as com.markusleben.ha-nova.cli2, the
# identity its Keychain items are bound to from 0.32 on.
# Not here: pairing into the Keychain, its generations, the pin, purge, and the
# update from v0.31.0. HA NOVA never allows keychain UI there, and a hosted
# runner answers "interaction not allowed" to that (SECRET_UI_FORBIDDEN), so
# those stay with the Go tests, the Linux container lane, and a person's own
# check on a real Mac.
set -euo pipefail

tag="${1:?usage: rc-macos-lane.sh <vX.Y.Z-rcN>}"
if [ "${GITHUB_ACTIONS:-}" != true ] || [ "${RUNNER_ENVIRONMENT:-}" != github-hosted ] || [ "$(uname -s)" != Darwin ]; then
  echo "Runs only on a disposable GitHub-hosted macOS runner." >&2
  exit 2
fi

curl -fsSL "https://raw.githubusercontent.com/markusleben/ha-nova/$tag/install.sh" | HA_NOVA_VERSION="$tag" HA_NOVA_NO_SETUP=1 bash
bin="$HOME/.local/bin/ha-nova"
"$bin" version | grep -qxF "${tag#v}" || { echo "FAIL: the installed binary isn't $tag" >&2; exit 1; }
bash scripts/release/verify-macos-signature.sh "$HOME/.local/share/ha-nova/ha-nova"
echo "PASS: $tag installed, signed as com.markusleben.ha-nova.cli2"
