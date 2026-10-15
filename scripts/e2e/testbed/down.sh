#!/usr/bin/env bash
# Tear the test bed down completely: containers, volumes, network, and the
# runtime state in .state/ (HA writes its config as root, so it is wiped from
# inside a container). Safe to run repeatedly.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE="$HERE/.state"

# The wipe runs as root on whatever .state points at: never follow a link.
[[ -L "$STATE" ]] && { echo "testbed: $STATE is a symlink; refusing to wipe it" >&2; exit 1; }

RELAY_AUTH_TOKEN="" HA_LLAT="" docker compose -f "$HERE/docker-compose.yml" down -v --remove-orphans >&2
if [[ -d "$STATE" ]]; then
  docker run --rm -v "$STATE:/target" alpine:3 \
    sh -c 'rm -rf /target/* /target/.[!.]* /target/..?*' >&2
  rmdir "$STATE"
fi
echo "testbed: down (containers, volumes, and .state removed)" >&2
