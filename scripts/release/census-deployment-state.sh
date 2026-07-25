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
