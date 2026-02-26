#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-ha-nova-app:local}"

case "$(uname -m)" in
  arm64|aarch64)
    DEFAULT_BUILD_FROM="ghcr.io/home-assistant/aarch64-base:3.21"
    DEFAULT_PLATFORM="linux/arm64"
    ;;
  x86_64|amd64)
    DEFAULT_BUILD_FROM="ghcr.io/home-assistant/amd64-base:3.21"
    DEFAULT_PLATFORM="linux/amd64"
    ;;
  *)
    DEFAULT_BUILD_FROM="ghcr.io/home-assistant/amd64-base:3.21"
    DEFAULT_PLATFORM=""
    ;;
esac

BUILD_FROM="${BUILD_FROM:-$DEFAULT_BUILD_FROM}"
PLATFORM="${PLATFORM:-$DEFAULT_PLATFORM}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$ROOT_DIR"

echo "Building $IMAGE_NAME with BUILD_FROM=$BUILD_FROM"
docker build \
  ${PLATFORM:+--platform "$PLATFORM"} \
  --build-arg BUILD_FROM="$BUILD_FROM" \
  -f app/Dockerfile \
  -t "$IMAGE_NAME" \
  .
