#!/usr/bin/env bash
# Platform-specific functions for macOS.
# Sourced by macos-lib.sh — do not execute directly.
set -euo pipefail

PLATFORM_ID="macos"
PLATFORM_LABEL="macOS"
PLATFORM_SECRET_STORE_NAME="Keychain"
PLATFORM_SECRET_STORE_LABEL="macOS Keychain"
PLATFORM_RELAY_BINARY_NAME="relay"

require_platform() {
  if [[ "$(uname -s)" != "Darwin" ]]; then
    die "This script supports macOS only."
  fi
}

require_platform_dependencies() {
  require_cmd security
}

platform_release_os() {
  printf 'macos'
}

platform_relay_binary_name() {
  printf '%s' "${PLATFORM_RELAY_BINARY_NAME}"
}

store_platform_secret() {
  local service="$1"
  local value="$2"

  security add-generic-password -U \
    -a "$USER" \
    -s "$service" \
    -w "$value" >/dev/null
}

read_platform_secret() {
  local service="$1"
  security find-generic-password -a "$USER" -s "$service" -w 2>/dev/null || true
}

delete_platform_secret_if_exists() {
  local service="$1"
  security delete-generic-password -a "$USER" -s "$service" >/dev/null 2>&1 || true
}

copy_to_clipboard() {
  if ! command -v pbcopy >/dev/null 2>&1; then
    return 1
  fi
  printf '%s' "$1" | pbcopy
}

open_browser() {
  local url="$1"
  # Skip browser launch when stdin is piped (non-interactive / test mode).
  if [[ ! -t 0 ]]; then return 0; fi
  open "$url"
}

store_keychain_secret() {
  store_platform_secret "$@"
}

read_keychain_secret() {
  read_platform_secret "$@"
}

delete_keychain_secret_if_exists() {
  delete_platform_secret_if_exists "$@"
}
