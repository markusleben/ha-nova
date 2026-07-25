#!/usr/bin/env bash

census_single_deployment_version_id() {
  jq -ser '
    select(length == 1)
    | .[0]
    | select(type == "object")
    | .versions
    | select(
        type == "array"
        and length == 1
        and .[0].percentage == 100
        and (
          .[0].version_id
          | type == "string"
            and test("^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$")
        )
      )
    | .[0].version_id
  '
}

census_deployment_output_version_id() {
  jq -ser '
    [.[] | select(.type == "deploy")]
    | select(length == 1)
    | .[0].version_id
    | select(
        type == "string"
        and test("^[0-9A-Za-z][0-9A-Za-z._-]{0,127}$")
      )
  ' "$1"
}

census_read_current_deployment() {
  local account_id="$1"
  local worker_dir="$2"
  local config_file="$3"
  local worker_name="$4"

  CLOUDFLARE_ENV='' CLOUDFLARE_ACCOUNT_ID="$account_id" \
    npx --yes wrangler@4.113.0 deployments status \
      --cwd "$worker_dir" \
      --config "$config_file" \
      --name "$worker_name" \
      --json
}

census_wait_for_settled_current_version() {
  local baseline_version="$1"
  local account_id="$2"
  local worker_dir="$3"
  local config_file="$4"
  local worker_name="$5"
  local attempt deployment version

  # A failed Wrangler process can leave a Cloudflare deploy in flight. Require
  # the baseline to stay active for a bounded settlement window before treating
  # the failed command as a no-op.
  for ((attempt = 1; attempt <= 15; attempt++)); do
    deployment="$(
      census_read_current_deployment \
        "$account_id" "$worker_dir" "$config_file" "$worker_name"
    )" || return 1
    version="$(census_single_deployment_version_id <<<"$deployment")" \
      || return 1
    if [[ "$version" != "$baseline_version" || "$attempt" -eq 15 ]]; then
      printf '%s\n' "$version"
      return 0
    fi
    sleep 2
  done
}
