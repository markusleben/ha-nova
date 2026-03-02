#!/usr/bin/env bash
# Platform-specific functions for macOS.
# Sourced by macos-lib.sh — do not execute directly.
set -euo pipefail

require_platform() {
  if [[ "$(uname -s)" != "Darwin" ]]; then
    die "This script supports macOS only."
  fi
}

store_keychain_secret() {
  local service="$1"
  local value="$2"

  security add-generic-password -U \
    -a "$USER" \
    -s "$service" \
    -w "$value" >/dev/null
}

read_keychain_secret() {
  local service="$1"
  security find-generic-password -a "$USER" -s "$service" -w 2>/dev/null || true
}

delete_keychain_secret_if_exists() {
  local service="$1"
  security delete-generic-password -a "$USER" -s "$service" >/dev/null 2>&1 || true
}

open_browser() {
  local url="$1"
  # Skip browser launch when stdin is piped (non-interactive / test mode).
  if [[ ! -t 0 ]]; then return 0; fi
  open "$url"
}
