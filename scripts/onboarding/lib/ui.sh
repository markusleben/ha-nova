#!/usr/bin/env bash
# Platform-independent UI and prompt helpers.
# Sourced by macos-lib.sh — do not execute directly.
set -euo pipefail

# ---------------------------------------------------------------------------
# Screen control
# ---------------------------------------------------------------------------

clear_screen() {
  # Only clear when running in a real terminal (not piped / test mode).
  [[ -t 1 ]] || return 0
  printf '\033[2J\033[H'
}

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

log() {
  echo "[ha-nova] $*"
}

die() {
  echo "[ha-nova] $*" >&2
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

copy_secret_to_clipboard() {
  local secret="$1"

  if declare -F copy_to_clipboard >/dev/null 2>&1 && copy_to_clipboard "$secret"; then
    return 0
  fi

  return 1
}

# ---------------------------------------------------------------------------
# Spinner — shows animated progress while a background command runs
# Usage: with_spinner "Discovering Home Assistant..." some_command arg1 arg2
# Captures stdout of the command; exits with its exit code.
# Falls back silently in non-interactive (piped) mode.
# ---------------------------------------------------------------------------

SPINNER_RESULT=""

with_spinner() {
  local label="$1"; shift

  # Non-interactive: just run the command silently
  if [[ ! -t 1 ]]; then
    SPINNER_RESULT="$("$@" 2>/dev/null)" || return $?
    return 0
  fi

  local frames=('⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏')
  local tmpfile
  tmpfile="$(mktemp)"

  # Run command in background, capture output to tmpfile
  "$@" > "$tmpfile" 2>/dev/null &
  local pid=$!
  local i=0

  # Animate while command runs
  while kill -0 "$pid" 2>/dev/null; do
    printf '\r  %s %s' "${frames[i % ${#frames[@]}]}" "$label"
    i=$((i + 1))
    sleep 0.1
  done

  # Get exit code
  wait "$pid"
  local rc=$?

  # Clear spinner line
  printf '\r%*s\r' "$((${#label} + 6))" ""

  SPINNER_RESULT="$(cat "$tmpfile")"
  rm -f "$tmpfile"
  return $rc
}

# ---------------------------------------------------------------------------
# Wizard display helpers (used by the upcoming 4-phase wizard)
# ---------------------------------------------------------------------------

print_header() {
  echo ""
  echo "  ╭─────────────────────────────╮"
  echo "  │       HA NOVA Setup         │"
  echo "  ╰─────────────────────────────╯"
  echo ""
}

print_step() {
  local step="$1"
  local total="$2"
  local title="$3"
  local bar=""
  for ((s=1; s<=total; s++)); do
    if (( s <= step )); then bar="${bar}●"; else bar="${bar}○"; fi
    if (( s < total )); then bar="${bar} "; fi
  done
  echo ""
  echo "  ${bar}  Step ${step} of ${total} — ${title}"
  echo "  ──────────────────────────────────"
}

print_success() {
  echo "  ✓ $*"
}

print_fail() {
  echo "  ✗ $*" >&2
}

print_info() {
  echo "  $*"
}

wait_for_enter() {
  local prompt="${1:-Press Enter to continue...}"
  # Skip interactive pauses when stdin is piped (non-interactive / test mode).
  if [[ ! -t 0 ]]; then return 0; fi
  if ! read -r -p "$prompt"; then
    die "Interactive input required. Re-run in a terminal."
  fi
}

# Like wait_for_enter but adds a [c] shortcut to copy a secret to the clipboard.
# Usage: wait_for_enter_or_copy "Press [Enter] to continue..." "$token"
wait_for_enter_or_copy() {
  local prompt="${1:-Press Enter to continue...}"
  local secret="${2:-}"
  # Non-interactive: skip
  if [[ ! -t 0 ]]; then return 0; fi

  while true; do
    local input=""
    if ! read -r -p "${prompt} [c] copy token  " input; then
      die "Interactive input required. Re-run in a terminal."
    fi
    if [[ "$input" =~ ^[Cc]$ && -n "$secret" ]]; then
      if copy_secret_to_clipboard "$secret"; then
        print_success "Token copied to clipboard."
      else
        echo ""
        echo "    ${secret}"
        echo ""
      fi
      # Loop back — let user press Enter when ready
      continue
    fi
    break
  done
}

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------

check_prerequisites() {
  print_info "Checking prerequisites..."

  # OS check (delegated to platform module — must be sourced before this call)
  require_platform
  print_success "Platform supported"

  if declare -F require_platform_dependencies >/dev/null 2>&1; then
    require_platform_dependencies
    print_success "Platform dependencies available"
  fi

  if ! command -v curl >/dev/null 2>&1; then
    print_fail "curl not found."
    exit 1
  fi
  print_success "curl available"
}
