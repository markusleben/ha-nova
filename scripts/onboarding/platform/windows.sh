#!/usr/bin/env bash
# Platform-specific functions for Windows (Git Bash / MSYS2).
# Sourced by macos-lib.sh — do not execute directly.
set -euo pipefail

PLATFORM_ID="windows"
PLATFORM_LABEL="Windows"
PLATFORM_SECRET_STORE_NAME="secure local storage"
PLATFORM_SECRET_STORE_LABEL="Windows protected storage"
PLATFORM_RELAY_BINARY_NAME="relay.exe"

require_platform() {
  case "${HA_NOVA_PLATFORM_OVERRIDE:-$(uname -s)}" in
    windows|MINGW*|MSYS*|CYGWIN*)
      ;;
    *)
      die "This script supports Windows only."
      ;;
  esac
}

require_platform_dependencies() {
  require_cmd powershell.exe
}

platform_release_os() {
  printf 'windows'
}

platform_relay_binary_name() {
  printf '%s' "${PLATFORM_RELAY_BINARY_NAME}"
}

windows_secret_file() {
  local service="$1"
  local safe_service="${service//[^A-Za-z0-9._-]/_}"
  printf '%s/.config/ha-nova/.%s.dpapi' "${HOME}" "${safe_service}"
}

windows_path() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$1"
    return
  fi

  printf '%s' "$1"
}

powershell_quote() {
  printf "%s" "$1" | sed "s/'/''/g"
}

store_platform_secret() {
  local service="$1"
  local value="$2"
  local secret_file ps_secret_file encoded_value ps_command

  secret_file="$(windows_secret_file "$service")"
  mkdir -p "$(dirname "$secret_file")"
  ps_secret_file="$(powershell_quote "$(windows_path "$secret_file")")"
  encoded_value="$(printf '%s' "$value" | base64 | tr -d '\r\n')"
  ps_command="\$secure = ConvertTo-SecureString -String ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$(powershell_quote "$encoded_value")'))) -AsPlainText -Force; \$blob = ConvertFrom-SecureString -SecureString \$secure; \$parent = Split-Path -LiteralPath '$ps_secret_file' -Parent; if (\$parent) { New-Item -ItemType Directory -Path \$parent -Force | Out-Null }; [IO.File]::WriteAllText('$ps_secret_file', \$blob)"
  powershell.exe -NoProfile -NonInteractive -Command "$ps_command" >/dev/null
}

read_platform_secret() {
  local service="$1"
  local secret_file ps_secret_file ps_command

  secret_file="$(windows_secret_file "$service")"
  if [[ ! -f "$secret_file" ]]; then
    return 0
  fi

  ps_secret_file="$(powershell_quote "$(windows_path "$secret_file")")"
  ps_command="\$blob = Get-Content -LiteralPath '$ps_secret_file' -Raw; if ([string]::IsNullOrWhiteSpace(\$blob)) { exit 0 }; \$secure = ConvertTo-SecureString -String \$blob; \$bstr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR(\$secure); try { [Console]::Out.Write([Runtime.InteropServices.Marshal]::PtrToStringAuto(\$bstr)) } finally { if (\$bstr -ne [IntPtr]::Zero) { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR(\$bstr) } }"
  powershell.exe -NoProfile -NonInteractive -Command "$ps_command" 2>/dev/null || true
}

delete_platform_secret_if_exists() {
  local service="$1"
  local secret_file

  secret_file="$(windows_secret_file "$service")"
  rm -f "$secret_file"
}

copy_to_clipboard() {
  if command -v clip.exe >/dev/null 2>&1; then
    printf '%s' "$1" | clip.exe
    return 0
  fi

  if ! command -v powershell.exe >/dev/null 2>&1; then
    return 1
  fi

  powershell.exe -NoProfile -NonInteractive -Command "[Console]::In.ReadToEnd() | Set-Clipboard" <<<"$1" >/dev/null
}

open_browser() {
  local url="$1"
  if [[ ! -t 0 ]]; then return 0; fi
  if command -v cmd.exe >/dev/null 2>&1; then
    cmd.exe /c start "" "$url" >/dev/null 2>&1 &
    return 0
  fi
  powershell.exe -NoProfile -NonInteractive -Command "Start-Process '$(
    powershell_quote "$url"
  )'" >/dev/null 2>&1 &
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
