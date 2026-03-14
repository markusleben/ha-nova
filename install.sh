#!/usr/bin/env bash
# HA NOVA Installer — curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
set -euo pipefail

REPO_OWNER="markusleben"
REPO_NAME="ha-nova"
LATEST_RELEASE_API="https://api.github.com/repos/markusleben/ha-nova/releases/latest"
RELEASE_BASE_URL="https://github.com/markusleben/ha-nova/releases/download/"
INSTALL_DIR="${HOME}/.local/share/ha-nova"
BIN_DIR="${HOME}/.local/bin"
BIN_LINK="${BIN_DIR}/ha-nova"
LEGACY_UNINSTALL_URL="https://raw.githubusercontent.com/markusleben/ha-nova/main/scripts/legacy-uninstall.sh"
CONFIG_DIR="${HOME}/.config/ha-nova"
STATE_FILE="${CONFIG_DIR}/state.json"
PATH_BLOCK_HEADER="# Added by HA NOVA"
PATH_RC_FILE=""
TMP_DIR=""
PATH_MANAGED="0"

banner() {
  echo ""
  echo "  ========================================="
  echo "  HA NOVA Installer"
  echo "  ========================================="
  echo ""
}

info() { echo "  [ok] $*"; }
warn() { echo "  [!!] $*"; }
fail() { echo "  [!!] $*" >&2; exit 1; }

cleanup() {
  [[ -n "${TMP_DIR}" && -d "${TMP_DIR}" ]] && rm -rf "${TMP_DIR}"
}

trap cleanup EXIT

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required to install HA NOVA."
}

is_current_install() {
  [[ -f "${INSTALL_DIR}/bundle.json" ]]
}

has_legacy_install() {
  [[ -f "${CONFIG_DIR}/onboarding.env" ]] && return 0
  [[ -f "${CONFIG_DIR}/relay" ]] && return 0
  [[ -f "${CONFIG_DIR}/relay.exe" ]] && return 0
  [[ -f "${CONFIG_DIR}/update" ]] && return 0
  [[ -f "${CONFIG_DIR}/update.cmd" ]] && return 0
  [[ -f "${CONFIG_DIR}/version-check" ]] && return 0
  [[ -f "${CONFIG_DIR}/check-update.cmd" ]] && return 0
  [[ -d "${INSTALL_DIR}/scripts/onboarding" && ! -f "${INSTALL_DIR}/bundle.json" ]] && return 0
  return 1
}

abort_for_legacy_install() {
  cat >&2 <<EOF
  [!!] A pre-Go HA NOVA install was detected.
  [!!] This installer does not migrate legacy installs in place.

  Run the cleanup first:
    curl -fsSL ${LEGACY_UNINSTALL_URL} | bash

  Then run this installer again.
EOF
  exit 1
}

has_interactive_tty() {
  if [[ -t 0 ]]; then
    return 0
  fi

  if : </dev/tty 2>/dev/null; then
    return 0
  fi

  return 1
}

require_interactive_tty() {
  if has_interactive_tty; then
    return 0
  fi

  fail "Interactive input required. Re-run in a terminal."
}

extract_json_string() {
  local key="$1"
  local json_file="$2"
  sed -n "s/.*\"${key}\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/p" "$json_file" | head -1
}

compute_sha256() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
    return 0
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
    return 0
  fi
  fail "sha256sum or shasum is required to verify HA NOVA downloads."
}

detect_os() {
  case "$(uname -s)" in
    Darwin) printf '%s\n' "macos" ;;
    Linux) printf '%s\n' "linux" ;;
    *) fail "HA NOVA install.sh currently supports macOS and Linux only." ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64) printf '%s\n' "amd64" ;;
    arm64|aarch64) printf '%s\n' "arm64" ;;
    *) fail "Unsupported architecture: $(uname -m)" ;;
  esac
}

detect_shell_rc() {
  local shell_name
  shell_name="$(basename "${SHELL:-sh}")"

  case "$shell_name" in
    zsh) printf '%s\n' "${HOME}/.zshrc" ;;
    bash)
      if [[ -f "${HOME}/.bash_profile" ]]; then
        printf '%s\n' "${HOME}/.bash_profile"
      else
        printf '%s\n' "${HOME}/.profile"
      fi
      ;;
    *)
      if [[ -f "${HOME}/.profile" ]]; then
        printf '%s\n' "${HOME}/.profile"
      else
        printf '%s\n' "${HOME}/.zshrc"
      fi
      ;;
  esac
}

ensure_bin_dir_on_path() {
  local rc_file
  rc_file="$(detect_shell_rc)"
  PATH_RC_FILE="${rc_file}"

  mkdir -p "${BIN_DIR}" "$(dirname "${rc_file}")"
  touch "${rc_file}"

  case ":${PATH}:" in
    *":${BIN_DIR}:"*) ;;
    *) export PATH="${BIN_DIR}:${PATH}" ;;
  esac

  if grep -Fqx "${PATH_BLOCK_HEADER}" "${rc_file}" 2>/dev/null; then
    info "${BIN_DIR} already configured in ${rc_file}"
    return 0
  fi

  cat >> "${rc_file}" <<'EOF'

# Added by HA NOVA
export PATH="$HOME/.local/bin:$PATH"
EOF
  PATH_MANAGED="1"
  info "Added ${BIN_DIR} to PATH in ${rc_file}"
}

normalize_version_tag() {
  local version="$1"
  if [[ -z "${version}" ]]; then
    fail "Could not determine a HA NOVA release version."
  fi

  if [[ "${version}" == v* ]]; then
    printf '%s\n' "${version}"
  else
    printf 'v%s\n' "${version}"
  fi
}

fetch_latest_version_tag() {
  local json_file="${TMP_DIR}/latest-release.json"
  curl -fsSL "${LATEST_RELEASE_API}" -o "${json_file}"

  local tag
  tag="$(extract_json_string "tag_name" "${json_file}")"
  normalize_version_tag "${tag}"
}

resolve_version_tag() {
  if [[ -n "${HA_NOVA_VERSION:-}" ]]; then
    normalize_version_tag "${HA_NOVA_VERSION}"
    return 0
  fi

  fetch_latest_version_tag
}

bundle_name() {
  local arch os
  os="$(detect_os)"
  arch="$(detect_arch)"

  case "${os}" in
    macos) printf 'ha-nova-macos-%s.tar.gz\n' "${arch}" ;;
    linux) printf 'ha-nova-linux-%s.tar.gz\n' "${arch}" ;;
    *) fail "Unsupported platform: ${os}" ;;
  esac
}

download_bundle() {
  local version_tag="$1"
  local archive_name archive_path checksum_path extract_dir bundle_root expected actual bundle_os bundle_arch bundle_binary
  archive_name="$(bundle_name)"
  archive_path="${TMP_DIR}/${archive_name}"
  checksum_path="${archive_path}.sha256"
  extract_dir="${TMP_DIR}/extract"

  mkdir -p "${extract_dir}"
  curl -fsSL "${RELEASE_BASE_URL}${version_tag}/${archive_name}" -o "${archive_path}"
  curl -fsSL "${RELEASE_BASE_URL}${version_tag}/${archive_name}.sha256" -o "${checksum_path}"
  expected="$(awk '{print $1}' "${checksum_path}" | head -1)"
  actual="$(compute_sha256 "${archive_path}")"
  [[ -n "${expected}" && "${actual}" == "${expected}" ]] || fail "Downloaded bundle checksum verification failed."
  tar -xzf "${archive_path}" -C "${extract_dir}"

  bundle_root="${extract_dir}/ha-nova"
  [[ -d "${bundle_root}" ]] || fail "Downloaded bundle did not contain an installable ha-nova directory."
  [[ -f "${bundle_root}/bundle.json" ]] || fail "Downloaded bundle is missing bundle.json."
  [[ -x "${bundle_root}/ha-nova" ]] || fail "Downloaded bundle is missing the ha-nova binary."
  bundle_os="$(extract_json_string "os" "${bundle_root}/bundle.json")"
  bundle_arch="$(extract_json_string "arch" "${bundle_root}/bundle.json")"
  bundle_binary="$(extract_json_string "binary_name" "${bundle_root}/bundle.json")"
  [[ "${bundle_os}" == "$(detect_os)" ]] || fail "Downloaded bundle OS metadata does not match this machine."
  [[ "${bundle_arch}" == "$(detect_arch)" ]] || fail "Downloaded bundle architecture metadata does not match this machine."
  [[ "${bundle_binary}" == "ha-nova" ]] || fail "Downloaded bundle binary metadata does not match the expected runtime."
  printf '%s\n' "${bundle_root}"
}

install_bundle() {
  local bundle_root="$1"
  local install_parent next_root backup_root
  install_parent="$(dirname "${INSTALL_DIR}")"
  next_root="${install_parent}/.ha-nova-next-$$"
  backup_root="${install_parent}/.ha-nova-old-$$"

  mkdir -p "${install_parent}"
  rm -rf "${next_root}" "${backup_root}"
  mkdir -p "${next_root}"
  cp -R "${bundle_root}/." "${next_root}/"

  if [[ -d "${INSTALL_DIR}" ]]; then
    mv "${INSTALL_DIR}" "${backup_root}"
  fi

  if mv "${next_root}" "${INSTALL_DIR}"; then
    rm -rf "${backup_root}"
    return 0
  fi

  [[ -d "${backup_root}" ]] && mv "${backup_root}" "${INSTALL_DIR}" 2>/dev/null || true
  fail "Could not move the new HA NOVA bundle into place."
}

install_binary() {
  local runtime_bin="${INSTALL_DIR}/ha-nova"
  [[ -x "${runtime_bin}" ]] || fail "Installed bundle is missing the ha-nova runtime."
  mkdir -p "${BIN_DIR}"
  ln -sfn "${runtime_bin}" "${BIN_LINK}"
}

write_state() {
  local version_tag="$1"
  local version="${version_tag#v}"
  mkdir -p "${CONFIG_DIR}"

  if [[ -f "${STATE_FILE}" ]]; then
    return 0
  fi

  cat > "${STATE_FILE}" <<EOF
{
  "schema_version": 1,
  "version": "${version}",
  "install_source": "bundle",
  "installed_clients": [],
  "client_install_modes": {},
  "path_managed": $( [[ "${PATH_MANAGED}" == "1" ]] && printf 'true' || printf 'false' ),
  "path_target": "${PATH_RC_FILE}"
}
EOF
  chmod 600 "${STATE_FILE}"
}

run_setup() {
  local runtime_bin="$1"

  if [[ "${HA_NOVA_NO_SETUP:-0}" == "1" ]]; then
    echo "  Next step: ha-nova setup"
    echo "  Need help later? Run: ha-nova doctor"
    return 0
  fi

  if has_interactive_tty; then
    info "Starting ha-nova setup"
    "${runtime_bin}" setup < /dev/tty
    return 0
  fi

  echo "  Next step: ha-nova setup"
  echo "  Need help later? Run: ha-nova doctor"
}

check_prerequisites() {
  local platform
  platform="$(detect_os)"
  info "${platform} detected"
  require_cmd curl
  info "curl available"
  require_cmd tar
  info "tar available"
}

main() {
  local version_tag bundle_root
  banner
  check_prerequisites
  if ! is_current_install && has_legacy_install; then
    abort_for_legacy_install
  fi
  TMP_DIR="$(mktemp -d)"
  version_tag="$(resolve_version_tag)"
  bundle_root="$(download_bundle "${version_tag}")"
  install_bundle "${bundle_root}"
  install_binary
  ensure_bin_dir_on_path
  write_state "${version_tag}"
  info "Installed HA NOVA ${version_tag}"
  echo ""
  echo "  Need help later? Run: ha-nova doctor"
  run_setup "${BIN_LINK}"
}

main "$@"
