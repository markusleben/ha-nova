#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: bash scripts/release/verify-relay-image.sh <exact-commit-sha> <relay-version>

Verifies that relay-image.yml completed successfully for the exact main-push
commit and that GHCR tags :latest, :<relay-version>, and
:sha-<exact-commit-sha> resolve to the same manifest digest.

EOF
}

fail() {
  echo "[verify-relay-image] ERROR: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

if [[ "$#" -ne 2 ]]; then
  usage
  exit 2
fi

commit_sha="$1"
relay_version="$2"
repository="markusleben/ha-nova"

[[ "$commit_sha" =~ ^[0-9a-f]{40}$ ]] || fail "commit SHA must be exactly 40 hexadecimal characters"
[[ "$relay_version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] \
  || fail "relay version must be canonical X.Y.Z"
[[ "$repository" =~ ^[^/[:space:]]+/[^/[:space:]]+$ ]] || fail "invalid GitHub repository: $repository"

require_command gh
require_command docker
require_command jq

gh auth status --hostname github.com >/dev/null 2>&1 || fail "gh is not authenticated for github.com"
docker buildx version >/dev/null 2>&1 || fail "docker buildx is unavailable"

runs_json="$(
  gh api --hostname github.com --method GET \
    "repos/${repository}/actions/workflows/relay-image.yml/runs" \
    -f "head_sha=${commit_sha}" \
    -f branch=main \
    -f event=push \
    -f status=success \
    -f per_page=100
)" || fail "could not query relay-image.yml runs for ${commit_sha}"

run_record="$(
  jq -er --arg sha "$commit_sha" '
    [
      .workflow_runs[]
      | select(
          .head_sha == $sha
          and .head_branch == "main"
          and .event == "push"
          and .status == "completed"
          and .conclusion == "success"
        )
    ]
    | sort_by(.run_number)
    | last
    | {html_url, run_attempt}
    | select(
        (.html_url | type == "string" and length > 0)
        and (.run_attempt | type == "number" and . >= 1)
      )
  ' <<<"$runs_json"
)" || fail "no successful relay-image.yml push run found for exact SHA ${commit_sha}"
run_url="$(jq -er '.html_url' <<<"$run_record")"
run_attempt="$(jq -er '.run_attempt' <<<"$run_record")"

owner="${repository%%/*}"
image_repository="ghcr.io/${owner}/ha-nova-relay"
latest_ref="${image_repository}:latest"
version_ref="${image_repository}:${relay_version}"
sha_ref="${image_repository}:sha-${commit_sha}"

manifest_digest() {
  local image_ref="$1"
  local inspect_json
  local digest

  if ! inspect_json="$(docker buildx imagetools inspect "$image_ref" --format '{{json .}}' 2>&1)"; then
    fail "could not inspect ${image_ref}: ${inspect_json}"
  fi
  if ! digest="$(jq -er '.manifest.digest | select(type == "string" and length > 0)' <<<"$inspect_json")"; then
    fail "${image_ref} did not expose a manifest digest"
  fi
  printf '%s\n' "$digest"
}

latest_digest="$(manifest_digest "$latest_ref")"
version_digest="$(manifest_digest "$version_ref")"
sha_digest="$(manifest_digest "$sha_ref")"

[[ "$latest_digest" == "$version_digest" ]] || fail \
  "digest mismatch: ${latest_ref}=${latest_digest}, ${version_ref}=${version_digest}"
[[ "$version_digest" == "$sha_digest" ]] || fail \
  "digest mismatch: ${version_ref}=${version_digest}, ${sha_ref}=${sha_digest}"

# Mutable registry tags sharing one digest is necessary but not sufficient:
# all three tags could have been moved together. Require BuildKit's SLSA
# provenance for both published platforms to bind the shared manifest back to
# this exact source revision and Relay version.
if ! provenance_json="$(docker buildx imagetools inspect "$sha_ref" --format '{{json .Provenance}}' 2>&1)"; then
  fail "could not inspect provenance for ${sha_ref}: ${provenance_json}"
fi
if ! jq -e --arg sha "$commit_sha" --arg version "$relay_version" --arg run "$run_url" --argjson max_attempt "$run_attempt" '
  . as $root
  | ["linux/amd64", "linux/arm64"] as $platforms
  | ($root | keys | sort) == ($platforms | sort)
    and all($platforms[];
      . as $platform
      | $root[$platform].SLSA.buildDefinition.externalParameters.request.root.request.args["vcs:revision"] == $sha
        and $root[$platform].SLSA.buildDefinition.externalParameters.request.args["build-arg:RELAY_VERSION"] == $version
        and $root[$platform].SLSA.buildDefinition.externalParameters.request.root.request.args["vcs:source"] == "https://github.com/markusleben/ha-nova"
        and $root[$platform].SLSA.buildDefinition.externalParameters.request.args["label:org.opencontainers.image.source"] == "https://github.com/markusleben/ha-nova"
        and $root[$platform].SLSA.buildDefinition.externalParameters.request.args["label:org.opencontainers.image.version"] == $version
        and $root[$platform].SLSA.buildDefinition.externalParameters.request.root.request.args["vcs:localdir:context"] == "nova"
        and $root[$platform].SLSA.buildDefinition.externalParameters.configSource.path == "Dockerfile.standalone"
        and (
          $root[$platform].SLSA.runDetails.builder.id as $builder
          | ($builder | startswith($run + "/attempts/"))
            and (($builder | ltrimstr($run + "/attempts/")) | test("^[1-9][0-9]*$"))
            and (($builder | ltrimstr($run + "/attempts/") | tonumber) <= $max_attempt)
        )
    )
' >/dev/null <<<"$provenance_json"; then
  fail "${sha_ref} provenance does not bind linux/amd64 and linux/arm64 to SHA ${commit_sha} and Relay ${relay_version}"
fi

echo "[verify-relay-image] OK: ${latest_ref}, ${version_ref}, and ${sha_ref} -> ${version_digest}; provenance ${commit_sha}/Relay ${relay_version}"
echo "[verify-relay-image] Workflow: ${run_url}"
