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

mkdir -p "$DATA_DIR"
cat > "$DATA_DIR/options.json" <<EOF
{
  "relay_auth_token": "${RELAY_AUTH_TOKEN:-dev-relay-token}",
  "ha_llat": "${HA_LLAT:-}",
  "ws_allowlist_append": "${WS_ALLOWLIST_APPEND:-}"
}
EOF

if docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER_NAME"; then
  docker rm -f "$CONTAINER_NAME" >/dev/null
fi

docker run -d \
  ${PLATFORM:+--platform "$PLATFORM"} \
  --name "$CONTAINER_NAME" \
  -p "$HOST_PORT:8791" \
  -v "$DATA_DIR:/data" \
  -e RELAY_AUTH_TOKEN="${RELAY_AUTH_TOKEN:-dev-relay-token}" \
  -e HA_LLAT="${HA_LLAT:-}" \
  -e WS_ALLOWLIST_APPEND="${WS_ALLOWLIST_APPEND:-}" \
  "$IMAGE_NAME"

echo "Container started: $CONTAINER_NAME on port $HOST_PORT"
