#!/usr/bin/env bash

windows_shell_allows_exe() {
  case "${OSTYPE:-}" in
    msys*|cygwin*|win32*) return 0 ;;
  esac

  case "$(uname -s 2>/dev/null || true)" in
    MINGW*|MSYS*|CYGWIN*) return 0 ;;
  esac

  return 1
}

find_runtime_binary() {
  local candidates=(
    "${HOME}/.local/bin/ha-nova"
    "${HOME}/.local/bin/ha-nova.exe"
    "${HOME}/.local/share/ha-nova/ha-nova"
    "${HOME}/.local/share/ha-nova/ha-nova.exe"
  )

  local candidate
  for candidate in "${candidates[@]}"; do
    if [[ -x "${candidate}" ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
    if windows_shell_allows_exe && [[ -f "${candidate}" && "${candidate}" == *.exe ]]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  return 1
}

repo_dev_binary_name() {
  if windows_shell_allows_exe; then
    printf 'ha-nova.exe\n'
    return 0
  fi
  printf 'ha-nova\n'
}

repo_dev_binary_dir() {
  local repo_key
  repo_key="${REPO_ROOT//\//_}"
  repo_key="${repo_key//:/_}"
  printf '%s/.cache/ha-nova/dev-runtime/%s\n' "${HOME}" "${repo_key}"
}

repo_dev_binary_path() {
  printf '%s/%s\n' "$(repo_dev_binary_dir)" "$(repo_dev_binary_name)"
}

repo_dev_binary_meta_path() {
  printf '%s.meta\n' "$(repo_dev_binary_path)"
}

repo_dev_runtime_metadata() {
  local goversion goos goarch cgo_enabled goflags goexperiment
  goversion="$(go env GOVERSION 2>/dev/null || true)"
  goos="$(go env GOOS 2>/dev/null || true)"
  goarch="$(go env GOARCH 2>/dev/null || true)"
  cgo_enabled="$(go env CGO_ENABLED 2>/dev/null || true)"
  goflags="$(go env GOFLAGS 2>/dev/null || true)"
  goexperiment="$(go env GOEXPERIMENT 2>/dev/null || true)"

  printf 'GOVERSION=%s\n' "${goversion}"
  printf 'GOOS=%s\n' "${goos}"
  printf 'GOARCH=%s\n' "${goarch}"
  printf 'CGO_ENABLED=%s\n' "${cgo_enabled}"
  printf 'GOFLAGS=%s\n' "${goflags}"
  printf 'GOEXPERIMENT=%s\n' "${goexperiment}"
}

repo_dev_binary_needs_rebuild() {
  local binary_path="$1"
  local meta_path="$2"
  local current_metadata="$3"

  if [[ ! -x "${binary_path}" ]]; then
    return 0
  fi

  if [[ ! -f "${meta_path}" ]]; then
    return 0
  fi

  if [[ "$(cat "${meta_path}")" != "${current_metadata}" ]]; then
    return 0
  fi

  local source_file
  while IFS= read -r -d '' source_file; do
    if [[ "${source_file}" -nt "${binary_path}" ]]; then
      return 0
    fi
  done < <(
    find "${REPO_ROOT}/cli" -type f \
      \( -name '*.go' -o -name 'go.mod' -o -name 'go.sum' \) \
      -print0
  )

  return 1
}

build_repo_dev_runtime() {
  local binary_path
  local meta_path
  local current_metadata
  binary_path="$(repo_dev_binary_path)"
  meta_path="$(repo_dev_binary_meta_path)"
  current_metadata="$(repo_dev_runtime_metadata)"

  mkdir -p "$(dirname "${binary_path}")"

  if repo_dev_binary_needs_rebuild "${binary_path}" "${meta_path}" "${current_metadata}"; then
    (
      cd "${REPO_ROOT}/cli"
      go build -o "${binary_path}" .
    )
    chmod +x "${binary_path}" 2>/dev/null || true
    printf '%s\n' "${current_metadata}" > "${meta_path}"
  fi

  printf '%s\n' "${binary_path}"
}

exec_repo_dev_runtime() {
  if ! command -v go >/dev/null 2>&1 || [[ ! -f "${REPO_ROOT}/cli/main.go" ]]; then
    return 1
  fi

  local binary_path
  binary_path="$(build_repo_dev_runtime)" || return 1
  exec "${binary_path}" "$@"
}
