#!/usr/bin/env bash
# HA NOVA Installer — curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/main/install.sh | bash
# Runs entirely in user-space: no sudo, HTTPS only, no dynamic code execution.
set -euo pipefail

REPO_URL="https://github.com/markusleben/ha-nova.git"
INSTALL_DIR="${HOME}/.local/share/ha-nova"
BIN_DIR="${HOME}/.local/bin"
BIN_LINK="${BIN_DIR}/ha-nova"

# ── Helpers ──────────────────────────────────────────────────────────────

banner() {
  echo ""
  echo "  ========================================="
  echo "  HA NOVA Installer"
  echo "  ========================================="
  echo ""
}

info()  { echo "  [ok] $*"; }
warn()  { echo "  [!!] $*"; }
fail()  { echo "  [!!] $*" >&2; exit 1; }

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    return 1
  fi
}

# ── Prerequisites ────────────────────────────────────────────────────────

check_prerequisites() {
  echo "  Checking prerequisites..."

  # macOS only (for now)
  if [[ "$(uname -s)" != "Darwin" ]]; then
    fail "HA NOVA currently supports macOS only."
  fi
  info "macOS detected"

  # Node.js >= 20
  if ! require_cmd node; then
    echo ""
    echo "  [!!] Node.js not found."
    echo ""
    echo "      HA NOVA needs Node.js 20 or newer."
    echo "      Install it from: https://nodejs.org"
    echo "      (Download the LTS version and run the installer)"
    echo ""
    echo "      After installing, close this terminal, open a new one,"
    echo "      and run this command again."
    echo ""
    exit 1
  fi
  local node_major
  node_major="$(node --version | sed -E 's/v([0-9]+)\..*/\1/')"
  if (( node_major < 20 )); then
    echo ""
    echo "  [!!] Node.js version too old (v${node_major})."
    echo ""
    echo "      HA NOVA needs Node.js 20 or newer."
    echo "      Update from: https://nodejs.org"
    echo "      (Download the LTS version and run the installer)"
    echo ""
    echo "      After updating, close this terminal, open a new one,"
    echo "      and run this command again."
    echo ""
    exit 1
  fi
  info "Node.js v${node_major}"

  # npm
  if ! require_cmd npm; then
    echo ""
    echo "  [!!] npm not found."
    echo ""
    echo "      npm should come with Node.js. Try reinstalling Node.js"
    echo "      from: https://nodejs.org"
    echo ""
    exit 1
  fi
  info "npm available"

  # git
  if ! require_cmd git; then
    echo ""
    echo "  [!!] git not found."
    echo ""
    echo "      Install Xcode Command Line Tools:"
    echo "        xcode-select --install"
    echo ""
    echo "      After installing, run this command again."
    echo ""
    exit 1
  fi
  info "git available"

  echo ""
}

# ── Existing Install ─────────────────────────────────────────────────────

handle_existing_install() {
  if [[ ! -d "${INSTALL_DIR}" ]]; then
    return 0
  fi

  # Dir exists but isn't a git repo (interrupted clone / manual copy)
  if [[ ! -d "${INSTALL_DIR}/.git" ]]; then
    warn "Directory exists but is not a git repo: ${INSTALL_DIR}"
    warn "Removing it so we can do a clean install..."
    [[ -n "$INSTALL_DIR" && "$INSTALL_DIR" == *"ha-nova"* ]] || fail "INSTALL_DIR sanity check failed"
    rm -rf "$INSTALL_DIR"
    return 0
  fi

  echo "  Existing HA NOVA installation found at:"
  echo "    ${INSTALL_DIR}"
  echo ""

  if [[ -t 0 ]]; then
    echo "  What would you like to do?"
    echo "    1) Update (git pull + npm install)"
    echo "    2) Reinstall (remove and clone fresh)"
    echo "    3) Cancel"
    echo ""
    printf "  Enter [1-3] (default 1): "
    read -r choice
  else
    choice="1"
  fi

  case "${choice:-1}" in
    2)
      echo ""
      echo "  Removing existing installation..."
      [[ -n "$INSTALL_DIR" && "$INSTALL_DIR" == *"ha-nova"* ]] || fail "INSTALL_DIR sanity check failed"
      rm -rf "$INSTALL_DIR"
      ;;
    3)
      echo "  Cancelled."
      exit 0
      ;;
    *)
      # Default: update (option 1 or invalid input)
      echo ""
      echo "  Updating..."
      git -C "$INSTALL_DIR" pull --ff-only
      (cd "$INSTALL_DIR" && npm install --no-audit --no-fund)
      link_cli
      # Deploy shared tools to ~/.config/ha-nova/
      local config_dir="${HOME}/.config/ha-nova"
      mkdir -p "$config_dir"
      [[ -f "${INSTALL_DIR}/scripts/relay.sh" ]]        && cp "${INSTALL_DIR}/scripts/relay.sh" "${config_dir}/relay" && chmod 755 "${config_dir}/relay"
      [[ -f "${INSTALL_DIR}/scripts/update.sh" ]]       && cp "${INSTALL_DIR}/scripts/update.sh" "${config_dir}/update" && chmod 755 "${config_dir}/update"
      [[ -f "${INSTALL_DIR}/scripts/version-check.sh" ]] && cp "${INSTALL_DIR}/scripts/version-check.sh" "${config_dir}/version-check" && chmod 755 "${config_dir}/version-check"
      [[ -f "${INSTALL_DIR}/version.json" ]]            && cp "${INSTALL_DIR}/version.json" "${config_dir}/version.json"
      echo ""
      info "Updated. Run 'ha-nova doctor' to verify."
      exit 0
      ;;
  esac
}

# ── Install ──────────────────────────────────────────────────────────────

clone_and_install() {
  echo "  Cloning HA NOVA..."
  git clone --depth 1 "$REPO_URL" "$INSTALL_DIR"
  echo ""

  echo "  Installing dependencies..."
  (cd "$INSTALL_DIR" && npm install --no-audit --no-fund)
  echo ""
}

link_cli() {
  mkdir -p "$BIN_DIR"
  ln -sfn "${INSTALL_DIR}/scripts/onboarding/bin/ha-nova" "$BIN_LINK"
  info "CLI linked: ${BIN_LINK}"

  # Check PATH
  case ":${PATH}:" in
    *":${BIN_DIR}:"*) ;;
    *)
      echo ""
      echo "  [!!] ${BIN_DIR} is not in your PATH."
      echo ""
      echo "      Add this line to your ~/.zshrc (or ~/.bashrc):"
      echo "        export PATH=\"\$HOME/.local/bin:\$PATH\""
      echo ""
      echo "      Then run: source ~/.zshrc"
      echo ""
      ;;
  esac
}

# ── Main ─────────────────────────────────────────────────────────────────

main() {
  # When piped from curl, stdin is the script itself — reclaim the terminal
  # so all interactive prompts (existing-install menu, setup wizard) work.
  if [[ ! -t 0 ]]; then
    if [[ -e /dev/tty ]] && exec 3< /dev/tty 2>/dev/null; then
      exec < /dev/tty
      exec 3>&-
    else
      fail "This installer requires an interactive terminal."
    fi
  fi

  banner
  check_prerequisites
  handle_existing_install
  clone_and_install
  link_cli

  echo ""
  info "HA NOVA installed successfully!"
  echo ""
  echo "  Starting setup wizard..."
  echo ""

  # Hand off to the setup wizard
  exec "${BIN_LINK}" setup
}

main "$@"
