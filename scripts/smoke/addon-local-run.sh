#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-ha-nova-addon:local}"
CONTAINER_NAME="${CONTAINER_NAME:-ha-nova-addon-local}"
HOST_PORT="${HOST_PORT:-8791}"
DATA_DIR="${DATA_DIR:-/tmp/ha-nova-addon-data}"

mkdir -p "$DATA_DIR"

if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
  docker rm -f "$CONTAINER_NAME" >/dev/null
fi

docker run -d \
  --name "$CONTAINER_NAME" \
  -p "$HOST_PORT:8791" \
  -v "$DATA_DIR:/data" \
  -e BRIDGE_AUTH_TOKEN="${BRIDGE_AUTH_TOKEN:-dev-bridge-token}" \
  "$IMAGE_NAME"

echo "Container started: $CONTAINER_NAME on port $HOST_PORT"
