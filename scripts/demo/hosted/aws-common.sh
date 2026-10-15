# shellcheck shell=bash
# Shared by provision.sh and teardown.sh (sourced; macOS bash 3.2 compatible).
# Every AWS call goes through run/has/q below, which always pass
# --profile personal --region eu-central-1; the account is checked first.
# (AWS Budgets is a global service: the CLI routes it to its single endpoint
# whatever --region says.)

# shellcheck disable=SC2034 # used by the scripts that source this file
PROFILE=personal
REGION=eu-central-1
ACCOUNT=018368605240
INSTANCE=ha-nova-demo
STATIC_IP=ha-nova-demo-ip
BUDGET=ha-nova-demo-monthly
YES=0

# Ambient credentials or another profile must never leak into these calls.
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_PROFILE AWS_DEFAULT_PROFILE

die() { echo "demo: $*" >&2; exit 1; }

# run <aws args…>: print the call (copy-pasteable); execute it only with --yes.
run() {
  local arg
  printf '+ aws'
  for arg in "$@" --profile "$PROFILE" --region "$REGION"; do
    if [[ "$arg" =~ ^[A-Za-z0-9._/:=@,+-]+$ ]]; then printf ' %s' "$arg"; else printf " '%s'" "$arg"; fi
  done
  printf '\n'
  [[ "$YES" == 1 ]] || return 0
  aws "$@" --profile "$PROFILE" --region "$REGION" --output json > /dev/null
}

# converge <aws args…>: like run, but retries for up to 5 min while AWS
# refuses (e.g. an instance that is still being created).
converge() {
  local i
  run "$@" 2> /dev/null && return 0
  for i in $(seq 1 60); do
    sleep 5
    aws "$@" --profile "$PROFILE" --region "$REGION" --output json > /dev/null 2>&1 && return 0
  done
  die "AWS did not accept: aws $*"
}

# q <aws args…>: read-only query, text output. Never runs without --yes.
q() {
  [[ "$YES" == 1 ]] || die "internal: query without --yes"
  aws "$@" --profile "$PROFILE" --region "$REGION" --output text
}

# has <aws get…>: 0 if the resource exists, 1 if AWS says NotFound, else die.
has() {
  local err
  [[ "$YES" == 1 ]] || die "internal: query without --yes"
  if err="$(aws "$@" --profile "$PROFILE" --region "$REGION" --output text 2>&1 > /dev/null)"; then
    return 0
  fi
  [[ "$err" == *NotFoundException* ]] && return 1
  die "aws $*: $err"
}

check_account() {
  run sts get-caller-identity
  [[ "$YES" == 1 ]] || return 0
  local got
  got="$(q sts get-caller-identity --query Account)"
  [[ "$got" == "$ACCOUNT" ]] || die "profile '$PROFILE' is account $got, expected $ACCOUNT; refusing"
}
