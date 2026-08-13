#!/usr/bin/env bash
# The envelope-contract half of build-cloud-evidence.sh — sourced, not run.
# Everything in this file either validates the maintainer's envelope or moves
# the secret; nothing here decides a qualification.
#
# Expects from the sourcing script: die(), step(), $REPO, $SECRET_NAME.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  echo "cloud-evidence-envelope.sh is sourced by build-cloud-evidence.sh, not run directly" >&2
  exit 64
fi

# Mirror of scripts/release/verify-cloud-release-gate.sh (requiredChecks plus
# the exact-key rules). The gate stays authoritative: drift here only moves
# WHERE a bad envelope is refused (locally instead of in CI) — it can never
# let one pass. tests/onboarding/cloud-evidence-behavior.test.ts pins the two
# check lists equal.
REQUIRED_CHECKS_SORTED="domains_mfa,installed_relay_app,keyrings,lifecycle,parity,redirects_non_disclosure,roles,routing,signing_and_update_matrix,stress_10000"

validate_envelope() {  # <file> <want-tree> <want-commit|""> <want-relay-version> <platforms-newline-list>
  local file="$1" want_tree="$2" want_commit="$3" want_relay="$4" want_platforms="$5"
  local doc got_tree
  [ -f "$file" ] || die "envelope file not found: $file"
  [ "$(wc -c <"$file" | tr -d '[:space:]')" -le 32768 ] || die "envelope exceeds the gate's 32 KiB limit"
  doc="$(jq -c . "$file" 2>/dev/null)" || die "envelope is not valid JSON: $file"
  jq -e 'type == "object"' >/dev/null <<<"$doc" || die "envelope must be a JSON object"
  [ "$(jq -r 'keys_unsorted | sort | join(",")' <<<"$doc")" = "checks,commit_sha,relay_app,schema,tree_sha" ] \
    || die "envelope must contain exactly schema, commit_sha, tree_sha, checks, relay_app"
  # Strict number, like the gate: a JSON string "2" would pass a text compare
  # here and still die in CI.
  jq -e '.schema == 2' >/dev/null <<<"$doc" || die "envelope schema must equal 2 (the number, not a string)"
  jq -e '.commit_sha | type == "string" and test("^[0-9a-f]{40}$")' >/dev/null <<<"$doc" \
    || die "envelope commit_sha must be a 40-character lowercase sha"
  jq -e '.tree_sha | type == "string" and test("^[0-9a-f]{40}$")' >/dev/null <<<"$doc" \
    || die "envelope tree_sha must be a 40-character lowercase sha"
  got_tree="$(jq -r '.tree_sha' <<<"$doc")"
  [ "$got_tree" = "$want_tree" ] \
    || die "envelope tree_sha ($got_tree) does not match the resolved tree ($want_tree)"
  if [ -n "$want_commit" ]; then
    [ "$(jq -r '.commit_sha' <<<"$doc")" = "$want_commit" ] \
      || die "envelope commit_sha ($(jq -r '.commit_sha' <<<"$doc")) does not match the resolved target commit ($want_commit)"
  fi
  [ "$(jq -r '.relay_app | keys_unsorted | sort | join(",")' <<<"$doc")" = "source_commit,source_tree_sha,version" ] \
    || die "envelope relay_app must contain exactly version, source_commit, source_tree_sha"
  [ "$(jq -r '.relay_app.version' <<<"$doc")" = "$want_relay" ] \
    || die "envelope relay_app.version ($(jq -r '.relay_app.version' <<<"$doc")) does not match nova/config.yaml ($want_relay)"
  [ "$(jq -r '.relay_app.source_commit' <<<"$doc")" = "$(jq -r '.commit_sha' <<<"$doc")" ] \
    || die "envelope relay_app.source_commit must equal commit_sha"
  [ "$(jq -r '.relay_app.source_tree_sha' <<<"$doc")" = "$got_tree" ] \
    || die "envelope relay_app.source_tree_sha must equal tree_sha"
  [ "$(jq -r '.checks | keys_unsorted | sort | join(",")' <<<"$doc")" = "$REQUIRED_CHECKS_SORTED" ] \
    || die "envelope checks must contain exactly the gate's required checks plus keyrings"
  jq -e '[.checks | to_entries[] | select(.key != "keyrings") | .value] | all(. == true)' >/dev/null <<<"$doc" \
    || die "every envelope check must be literally true — this script sets no boolean, and the gate rejects anything else"
  [ "$(jq -r '.checks.keyrings | keys_unsorted | sort | join(",")' <<<"$doc")" = "$(sort <<<"$want_platforms" | paste -s -d, -)" ] \
    || die "envelope keyrings keys must exactly match cloud_remote_platforms"
  jq -e '[.checks.keyrings[]] | all(. == true)' >/dev/null <<<"$doc" \
    || die "every envelope keyrings value must be literally true"
  # The bytes that passed, for the caller to write. Re-reading the file after
  # validation would let an edit in between write content that never passed.
  # shellcheck disable=SC2034  # consumed by the sourcing script
  VALIDATED_DOC="$doc"
}

# All-false template for the maintainer to edit; prints ONLY the JSON.
build_template_envelope() {  # <commit> <tree> <relay-version> <platforms-newline-list>
  local commit="$1" tree="$2" relay="$3" platforms="$4" keyrings_false
  keyrings_false="$(for p in $platforms; do printf '{"%s":false}' "$p"; done | jq -s 'add')"
  jq -n \
    --arg commit "$commit" --arg tree "$tree" \
    --arg relay "$relay" --argjson keyrings "$keyrings_false" '
    {
      schema: 2,
      commit_sha: $commit,
      tree_sha: $tree,
      relay_app: { version: $relay, source_commit: $commit, source_tree_sha: $tree },
      checks: {
        parity: false, stress_10000: false, keyrings: $keyrings,
        roles: false, domains_mfa: false, lifecycle: false,
        redirects_non_disclosure: false, installed_relay_app: false,
        routing: false, signing_and_update_matrix: false
      }
    }'
}

# ── the secret, in BOTH places ───────────────────────────────────────────────
# PR runs and merge groups read the `production` environment through the
# trusted broker; the ci-gate job on direct main pushes reads the REPOSITORY
# secret. Setting only one lets the PR go green and turns main red seconds
# after the squash merge, with the stale-evidence message that reads like a
# tree mismatch (docs/releasing.md, observed on #559).
repo_stamp() { gh api "repos/$REPO/actions/secrets/$SECRET_NAME" --jq .updated_at 2>/dev/null || true; }
env_stamp() { gh api "repos/$REPO/environments/production/secrets/$SECRET_NAME" --jq .updated_at 2>/dev/null || true; }

# Serialize the two-write pair against concurrent invocations on this machine
# (parallel agent sessions are normal on this workstation). Two interleaved
# pairs could leave the two locations holding DIFFERENT envelopes while both
# invocations see advancing stamps. Cross-machine racing is not this repo's
# operating shape and is deliberately not attempted. The lock is NOT removed
# when a write fails mid-pair: the secret state is uncertain then, and the
# next invocation must be a deliberate one.
CLOUD_EVIDENCE_WRITE_LOCK="${TMPDIR:-/tmp}/ha-nova-cloud-evidence-write.lock"

set_both_secrets() {  # <envelope-json>
  local body="$1" before_repo before_env after_repo after_env
  mkdir "$CLOUD_EVIDENCE_WRITE_LOCK" 2>/dev/null \
    || die "another invocation appears to be writing the evidence secrets (lock: $CLOUD_EVIDENCE_WRITE_LOCK) — if none is running, remove that directory and re-run"
  before_repo="$(repo_stamp)"; before_env="$(env_stamp)"
  step "writing $SECRET_NAME (repository secret)"
  gh secret set "$SECRET_NAME" --repo "$REPO" --body "$body" \
    || die "cannot write the repository secret — admin auth required"
  step "writing $SECRET_NAME (production environment secret)"
  gh secret set "$SECRET_NAME" --repo "$REPO" --env production --body "$body" \
    || die "cannot write the production environment secret — admin auth required"
  # Verify with GitHub's own clocks only: read updated_at before and after and
  # require movement in both places. A local-time comparison would inherit
  # clock skew; an unreadable stamp after a write is a failure, not a pass.
  # Equality with the before-stamp also fails, deliberately: the before value
  # predates this invocation (one run writes each location once and takes
  # seconds), so an unchanged stamp means the write did not land — a
  # same-second false positive would need two full invocations inside one
  # GitHub clock second, which is not an operational pattern for this tool.
  after_repo="$(repo_stamp)"; after_env="$(env_stamp)"
  { [ -n "$after_repo" ] && [ "$after_repo" != "$before_repo" ]; } \
    || die "repository secret updated_at did not advance (before: '${before_repo:-absent}', after: '${after_repo:-absent}') — verify by hand before relying on the gate"
  { [ -n "$after_env" ] && [ "$after_env" != "$before_env" ]; } \
    || die "production environment secret updated_at did not advance (before: '${before_env:-absent}', after: '${after_env:-absent}') — verify by hand before relying on the gate"
  echo "  repository secret:             updated_at $after_repo"
  echo "  production environment secret: updated_at $after_env"
  rmdir "$CLOUD_EVIDENCE_WRITE_LOCK" 2>/dev/null || true
}
