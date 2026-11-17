#!/usr/bin/env bash
# Runs the rc lane (scripts/e2e/rc-lane.mjs) in a Linux container on a macOS
# host, so every row runs the tag's Linux binary and nothing reaches this
# user's login Keychain. The container drives the host's Docker through its
# socket, on the Docker VM's network (where the bed publishes its ports), with
# this checkout at its own path so the bed's bind mounts resolve.
#
#   bash scripts/e2e/rc-lane-container.sh <vX.Y.Z-rcN> [--keep]
#
# Row c (the chat bridge with headless Claude) SKIPs here: the container has
# no Claude login. Run from a checkout at the tag, as rc-lane itself.
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
image=ha-nova-rc-lane
work="$root/scripts/e2e/.rc-lane-container"
rm -rf "$work"
mkdir -p "$work/home" "$work/tmp"
trap 'rm -rf "$work"' EXIT

go_version="$(sed -n 's/^go \([0-9.]*\).*/\1/p' "$root/cli/go.mod")"
docker build -q -t "$image" --build-arg GO_VERSION="$go_version" - >/dev/null <<'DOCKERFILE'
ARG GO_VERSION
FROM golang:${GO_VERSION}-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl gnupg jq python3 bsdextrautils \
 && install -d /etc/apt/keyrings \
 && curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg \
 && echo "deb [signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian bookworm stable" >/etc/apt/sources.list.d/docker.list \
 && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
 && apt-get install -y --no-install-recommends nodejs docker-ce-cli docker-compose-plugin \
 && rm -rf /var/lib/apt/lists/*
DOCKERFILE

docker run --rm --network host \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$root:$root" -w "$root" \
  -e HOME="$work/home" -e TMPDIR="$work/tmp" -e GOCACHE="$work/home/.cache/go" \
  "$image" bash -c 'git config --global --add safe.directory "$PWD" && npm run rc-lane -- "$@"' rc-lane "$@"
