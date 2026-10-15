#!/usr/bin/env bash
# Delete what provision.sh created (account 018368605240, profile "personal",
# eu-central-1). Prints every command; executes only with --yes.
#
#   bash scripts/demo/hosted/teardown.sh --keep-ip [--yes]       rebuild: instance only
#   bash scripts/demo/hosted/teardown.sh --dns-removed [--yes]   everything
#
# Rebuild (fresh host from the pinned ref, same IP, DNS untouched):
#   teardown.sh --keep-ip --yes && provision.sh --ref … --alert-email … --abuse-contact … --yes
# A full teardown releases the static IP, which AWS may hand to someone else:
# delete the A record at Namecheap FIRST, or demo.hanova.app would point
# at a stranger's server (subdomain takeover). --dns-removed confirms that.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=aws-common.sh
. "$HERE/aws-common.sh"

MODE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep-ip) MODE=keep-ip; shift ;;
    --dns-removed) MODE=all; shift ;;
    --yes) YES=1; shift ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ -n "$MODE" ]] || die "pass --keep-ip (rebuild) or --dns-removed (full teardown, DNS records already deleted)"
[[ "$YES" == 1 ]] && { command -v aws > /dev/null || die "aws CLI v2 is required"; }
[[ "$YES" == 1 ]] || echo "# dry run: nothing is executed; add --yes to run these" >&2

check_account

if [[ "$YES" == 0 ]] || has lightsail get-instance --instance-name "$INSTANCE"; then
  # Deleting the instance detaches the static IP; the IP stays allocated.
  run lightsail delete-instance --instance-name "$INSTANCE" --force-delete-add-ons
fi

if [[ "$MODE" == all ]]; then
  if [[ "$YES" == 0 ]] || has lightsail get-static-ip --static-ip-name "$STATIC_IP"; then
    run lightsail release-static-ip --static-ip-name "$STATIC_IP"
  fi
  if [[ "$YES" == 0 ]] || has budgets describe-budget --account-id "$ACCOUNT" --budget-name "$BUDGET"; then
    run budgets delete-budget --account-id "$ACCOUNT" --budget-name "$BUDGET"
  fi
else
  echo "# kept: static IP $STATIC_IP (unattached it is billed hourly; reattach soon) and budget $BUDGET" >&2
fi
