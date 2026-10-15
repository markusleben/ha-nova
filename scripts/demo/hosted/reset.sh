#!/usr/bin/env bash
# Reset the hosted demo house to its pristine seed. Runs as root on the demo
# host only: nightly via ha-nova-demo-reset.timer, and once from bootstrap.sh.
#
# checkout (restore the clone to the pinned ref) -> down (containers, volumes,
# .state) -> clean (untracked files) -> up with the demo overlay.
set -euo pipefail

# Everything runs inside main: the checkout may rewrite this very file, and
# bash reads scripts lazily. A function is parsed in full before it runs.
main() {
  [[ "$(id -u)" == 0 ]] || { echo "reset.sh: run as root" >&2; exit 1; }
  # DEMO_DOMAIN, DEMO_REF (written by bootstrap.sh)
  # shellcheck source=/dev/null
  . /etc/ha-nova-demo.env
  local repo=/opt/ha-nova
  local tb="$repo/scripts/e2e/testbed"
  export TESTBED_OVERLAY="$repo/scripts/demo/hosted" DEMO_DOMAIN

  git -C "$repo" checkout -q --force --detach "$DEMO_REF"
  bash "$tb/down.sh"
  git -C "$repo" clean -qffdx

  # A fresh owner password per reset. The owner is local-only (seed.mjs
  # demo-user), so it only works through an SSH tunnel to 127.0.0.1:18123.
  local owner_password
  owner_password="$(openssl rand -hex 24)"
  (umask 077 && printf '%s\n' "$owner_password" > /root/ha-nova-demo-owner-password)

  # up.sh with the overlay also runs seed.mjs demo-user.
  TESTBED_OWNER_PASSWORD="$owner_password" bash "$tb/up.sh"
  echo "reset.sh: demo reset to $DEMO_REF ($(git -C "$repo" rev-parse --short HEAD))" >&2
}

main "$@"
