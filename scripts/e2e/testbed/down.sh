#!/usr/bin/env bash
# Tear the test bed down completely: containers, volumes, network, and the
# runtime state in .state/ (HA writes its config as root, so it is wiped from
# inside a container). Safe to run repeatedly.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE="$HERE/.state"

# The wipe runs as root on whatever .state points at: never follow a link.
[[ -L "$STATE" ]] && { echo "testbed: $STATE is a symlink; refusing to wipe it" >&2; exit 1; }

COMPOSE=(docker compose -f "$HERE/docker-compose.yml")
# Hosted demo: the same overlay up.sh merged.
if [[ -n "${TESTBED_OVERLAY:-}" ]]; then COMPOSE+=(-f "$TESTBED_OVERLAY/docker-compose.yml"); fi
RELAY_AUTH_TOKEN="" HA_LLAT="" "${COMPOSE[@]}" down -v --remove-orphans >&2
if [[ -d "$STATE" ]]; then
  # Hosted demo: TESTBED_WIPE_IMAGE names an image that is always present.
  # Go run under cli.sh --env keeps its module cache in .state/home/go with
  # read-only folders, which even root can't empty on a macOS bind mount:
  # make every folder writable first (find never follows a link).
  docker run --rm -v "$STATE:/target" "${TESTBED_WIPE_IMAGE:-alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6}" \
    sh -c 'find /target -type d -exec chmod u+w {} + && rm -rf /target/* /target/.[!.]* /target/..?*' >&2
  rmdir "$STATE"
fi
echo "testbed: down (containers, volumes, and .state removed)" >&2
