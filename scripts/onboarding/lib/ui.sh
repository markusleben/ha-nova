#!/usr/bin/env bash
# Platform-independent UI and prompt helpers.
# Sourced by macos-lib.sh — do not execute directly.
set -euo pipefail

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

log() {
  echo "[macos-onboarding] $*"
}

die() {
  echo "[macos-onboarding] $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    die "Required command not found: ${cmd}"
  fi
}

# ---------------------------------------------------------------------------
# Interactive prompts
# ---------------------------------------------------------------------------

prompt_with_default() {
  local label="$1"
  local default_value="$2"
  local value

  if ! read -r -p "${label} [${default_value}]: " value; then
    die "Interactive input required. Re-run in a terminal."
  fi
  if [[ -z "$value" ]]; then
    value="$default_value"
  fi

  printf '%s' "$value"
}

prompt_yes_no() {
  local label="$1"
  local default_answer="${2:-N}"
  local hint="y/N"
  local answer

  if [[ "$default_answer" =~ ^[Yy]$ ]]; then
    hint="Y/n"
  fi

  if ! read -r -p "${label} [${hint}]: " answer; then
    die "Interactive input required. Re-run in a terminal."
  fi
  if [[ -z "$answer" ]]; then
    answer="$default_answer"
  fi

  [[ "$answer" =~ ^[Yy]$ ]]
}

# ---------------------------------------------------------------------------
# Secrets / tokens
# ---------------------------------------------------------------------------

mask_secret_hint() {
  local value="$1"
  local length="${#value}"
  if (( length <= 8 )); then
    printf '***'
    return
  fi

  local tail="${value: -4}"
  printf '***%s' "$tail"
}

fingerprint_secret() {
  local value="$1"

  if command -v shasum >/dev/null 2>&1; then
    printf '%s' "$value" | shasum -a 256 | awk '{print $1}'
    return
  fi

  if command -v openssl >/dev/null 2>&1; then
    printf '%s' "$value" | openssl dgst -sha256 -r | awk '{print $1}'
    return
  fi

  printf 'len:%s' "${#value}"
}

generate_relay_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi

  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr -d '-'
    return
  fi

  die "Cannot generate token automatically (missing openssl and uuidgen)."
}

emit_export() {
  local key="$1"
  local value="$2"
  printf 'export %s=%q\n' "$key" "$value"
}

# ---------------------------------------------------------------------------
# Wizard display helpers (used by the upcoming 4-phase wizard)
# ---------------------------------------------------------------------------

print_header() {
  echo ""
  echo "========================================="
  echo "  HA NOVA Setup"
  echo "========================================="
  echo ""
}

print_step() {
  local step="$1"
  local total="$2"
  local title="$3"
  echo "--- Step ${step}/${total} -- ${title} ---"
}

print_success() {
  echo "  [ok] $*"
}

print_fail() {
  echo "  [!!] $*" >&2
}

print_info() {
  echo "  $*"
}

wait_for_enter() {
  local prompt="${1:-Press Enter to continue...}"
  read -r -p "$prompt"
}
