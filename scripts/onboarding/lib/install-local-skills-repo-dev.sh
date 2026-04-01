#!/usr/bin/env bash

install_repo_dev_helpers() {
  local target="$1"
  local relay_cli_target="${HOME}/.config/ha-nova/relay"
  local relay_binary_target="${relay_cli_target}"
  mkdir -p "${HOME}/.config/ha-nova"

  if [[ "${CURRENT_PLATFORM_ID}" != "windows" ]]; then
    write_repo_cli_wrapper "${relay_cli_target}" "relay"
    log "[${target}] Installed relay wrapper: ${relay_cli_target}"
  else
    relay_binary_target="${HOME}/.config/ha-nova/relay.exe"
    local bundled_relay
    if bundled_relay="$(bundled_relay_path)"; then
      cp "${bundled_relay}" "${relay_binary_target}"
      chmod 755 "${relay_binary_target}"
      log "[${target}] Installed bundled relay CLI: ${relay_binary_target}"
    else
      write_repo_cli_wrapper "${relay_cli_target}" "relay"
      relay_binary_target=""
      log "[${target}] Installed relay wrapper: ${relay_cli_target}"
    fi
  fi

  if [[ "${CURRENT_PLATFORM_ID}" == "windows" && -n "${relay_binary_target}" && -f "${relay_binary_target}" ]]; then
    cat > "${relay_cli_target}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec "${SCRIPT_DIR}/relay.exe" "$@"
EOF
    chmod 755 "${relay_cli_target}"
  fi

  write_repo_cli_wrapper "${HOME}/.config/ha-nova/version-check" "check-update" "--quiet"
  cp "${REPO_ROOT}/version.json" "${HOME}/.config/ha-nova/version.json"
  log "[${target}] Installed version-check + version.json"
}
