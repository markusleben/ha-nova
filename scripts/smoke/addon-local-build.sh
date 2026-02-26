#!/usr/bin/env bash
set -euo pipefail

IMAGE_NAME="${IMAGE_NAME:-ha-nova-addon:local}"
BUILD_FROM="${BUILD_FROM:-ghcr.io/home-assistant/amd64-base:3.21}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$ROOT_DIR"

echo "Building $IMAGE_NAME with BUILD_FROM=$BUILD_FROM"
docker build \
  --build-arg BUILD_FROM="$BUILD_FROM" \
  -f addon/Dockerfile \
  -t "$IMAGE_NAME" \
  .
