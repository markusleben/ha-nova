#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-ha-nova-app:local}"
CONTAINER_NAME="${CONTAINER_NAME:-ha-nova-app-local}"
HOST_PORT="${HOST_PORT:-8791}"
DATA_DIR="${DATA_DIR:-/tmp/ha-nova-app-data}"
case "$(uname -m)" in
  arm64|aarch64)
    DEFAULT_PLATFORM="linux/arm64"
    ;;
  x86_64|amd64)
    DEFAULT_PLATFORM="linux/amd64"
    ;;
  *)
    DEFAULT_PLATFORM=""
    ;;
esac

PLATFORM="${PLATFORM:-$DEFAULT_PLATFORM}"
: "${HA_LLAT:?HA_LLAT is required for app-local-run.sh}"

# No Supervisor here: the App image runs as the standalone Relay, with its own
# shared token and Home Assistant token from the environment.
mkdir -p "$DATA_DIR"
printf '{"file_access":"off"}\n' > "$DATA_DIR/options.json"

if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
  docker rm -f "$CONTAINER_NAME" >/dev/null
fi

docker run -d \
  ${PLATFORM:+--platform "$PLATFORM"} \
  --name "$CONTAINER_NAME" \
  -p "$HOST_PORT:8791" \
  -v "$DATA_DIR:/data" \
  -e RELAY_AUTH_TOKEN="${RELAY_AUTH_TOKEN:-dev-relay-token}" \
  -e HA_LLAT="${HA_LLAT}" \
  "$IMAGE_NAME"

echo "Container started: $CONTAINER_NAME on port $HOST_PORT"
