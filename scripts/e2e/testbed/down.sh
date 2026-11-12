#!/usr/bin/env bash
# Tear the test bed down completely: containers, volumes, network, and the
# runtime state in .state/ (HA writes its config as root, so it is wiped from
# inside a container). Safe to run repeatedly.
#
#   bash scripts/e2e/testbed/down.sh [--force]    (or: npm run testbed:down)
#
# Only the bed's owner tears it down: the bed must have been started from this
# checkout, and .state/owner (written by up.sh) must match TESTBED_OWNER (empty
# when unset). --force skips that check; it is for emergencies only.
set -euo pipefail

HERE="$(cd -P "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE="$HERE/.state"

FORCE=0 bed_dirs=""
case "${1:-}" in
  "") ;;
  --force) FORCE=1 ;;
  *) echo "usage: down.sh [--force]" >&2; exit 2 ;;
esac

# The wipe runs as root on whatever .state points at: never follow a link.
[[ -L "$STATE" ]] && { echo "testbed: $STATE is a symlink; refusing to wipe it" >&2; exit 1; }

refuse() {
  echo "testbed: $1. Not torn down: the bed is someone else's (only the same checkout and TESTBED_OWNER may tear it down). Wait for its owner; never --force it without the maintainer's OK" >&2
  exit 1
}
if ((!FORCE)); then
  bed_dirs="$(docker ps -a --filter label=com.docker.compose.project=ha-nova-testbed \
    --format '{{.Label "com.docker.compose.project.working_dir"}}' | sort -u)" ||
    { echo "testbed: cannot list Docker containers; nothing torn down" >&2; exit 1; }
  [[ -z "$bed_dirs" || "$bed_dirs" == "$HERE" ]] ||
    refuse "the running test bed was started from another checkout (${bed_dirs//$'\n'/ })"
  if [[ -n "$bed_dirs" || -e "$STATE" ]]; then
    [[ -f "$STATE/owner" ]] || refuse "this bed has no owner marker ($STATE/owner)"
    owner="$(cat "$STATE/owner")"
    [[ "$owner" == "${TESTBED_OWNER:-}" ]] ||
      refuse "this bed belongs to '${owner:-no TESTBED_OWNER}', you are '${TESTBED_OWNER:-no TESTBED_OWNER}'"
  fi
fi

COMPOSE=(docker compose -f "$HERE/docker-compose.yml")
# Hosted demo: the same overlay up.sh merged.
if [[ -n "${TESTBED_OVERLAY:-}" ]]; then COMPOSE+=(-f "$TESTBED_OVERLAY/docker-compose.yml"); fi
# Without --force, compose down only for this checkout's own bed: one that
# another checkout starts meanwhile stays up.
if ((FORCE)) || [[ -n "$bed_dirs" ]]; then
  RELAY_AUTH_TOKEN="" HA_LLAT="" "${COMPOSE[@]}" down -v --remove-orphans >&2
fi
if [[ -d "$STATE" ]]; then
  # Hosted demo: TESTBED_WIPE_IMAGE names an image that is always present.
  # Go run under cli.sh --env keeps its module cache in .state/home/go with
  # read-only folders, which even root can't empty on a macOS bind mount:
  # make every folder writable first (find never follows a link).
  docker run --rm -v "$STATE:/target" "${TESTBED_WIPE_IMAGE:-alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6}" \
    sh -c 'find /target -type d -exec chmod u+w {} +; rm -rf /target/* /target/.[!.]* /target/..?*' >&2
  rmdir "$STATE"
fi
echo "testbed: down (containers, volumes, and .state removed)" >&2
