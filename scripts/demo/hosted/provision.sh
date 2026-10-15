#!/usr/bin/env bash
# Create the hosted demo on AWS Lightsail (account 018368605240, profile
# "personal", eu-central-1): Ubuntu 24.04 micro_3_0 instance (IPv4 only),
# static IPv4, firewall (80/443 public; 22 only for Lightsail browser SSH and
# --ssh-cidr), IMDSv2 with hop limit 1, and a 10 USD monthly budget alert.
#
#   bash scripts/demo/hosted/provision.sh --ref <40-hex sha> --alert-email you@example.com \
#     --abuse-contact abuse@example.com [--ssh-cidr 203.0.113.7/32]
#   … add --yes to execute; without it every command is only printed.
#
# Idempotent: existing resources are kept, the firewall is replaced as a whole.
# The ref must exist on GitHub and contain scripts/demo/hosted (a merge commit on main).
# User-data carries no secrets (it is readable via instance metadata).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=aws-common.sh
. "$HERE/aws-common.sh"

REPO_URL=https://github.com/markusleben/ha-nova.git
DOMAIN=demo.hanova.app
AZ=eu-central-1a
BLUEPRINT=ubuntu_24_04
BUNDLE=micro_3_0
REF="" EMAIL="" SSH_CIDR="" ABUSE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ref) REF="${2:-}"; shift 2 ;;
    --alert-email) EMAIL="${2:-}"; shift 2 ;;
    --ssh-cidr) SSH_CIDR="${2:-}"; shift 2 ;;
    --abuse-contact) ABUSE="${2:-}"; shift 2 ;;
    --domain) DOMAIN="${2:-}"; shift 2 ;;
    --yes) YES=1; shift ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ "$REF" =~ ^[0-9a-f]{40}$ ]] || die "--ref must be a full 40-hex commit sha (tags and branches can move)"
[[ "$ABUSE" =~ ^[A-Za-z0-9@._:/+-]+$ ]] || die "--abuse-contact <mail or URL> is required (shown on /abuse)"
[[ "$EMAIL" =~ ^[^@[:space:]\"\']+@[^@[:space:]\"\']+\.[a-z]+$ ]] || die "--alert-email is required (budget alerts)"
[[ "$DOMAIN" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || die "invalid --domain"
if [[ -n "$SSH_CIDR" ]]; then
  [[ "$SSH_CIDR" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}/(2[4-9]|3[0-2])$ ]] || die "--ssh-cidr must be one IPv4 /24../32 (e.g. 203.0.113.7/32)"
fi
[[ "$YES" == 1 ]] && { command -v aws > /dev/null || die "aws CLI v2 is required"; }
[[ "$YES" == 1 ]] || echo "# dry run: nothing is executed; add --yes to run these" >&2

USER_DATA="$(mktemp)"
trap 'rm -f "$USER_DATA"' EXIT
cat > "$USER_DATA" << EOF_UD
#!/bin/bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update -q
apt-get install -y -q git
[[ -d /opt/ha-nova/.git ]] || git clone --quiet $REPO_URL /opt/ha-nova
git -C /opt/ha-nova checkout --quiet --detach $REF
bash /opt/ha-nova/scripts/demo/hosted/bootstrap.sh --domain $DOMAIN --ref $REF --abuse-contact $ABUSE
EOF_UD

check_account

# 1. Instance, IPv4 only: Docker's userland proxy would hand every IPv6
#    visitor to Home Assistant as one bridge address (Lightsail's default key
#    pair for browser SSH).
if [[ "$YES" == 0 ]] || ! has lightsail get-instance --instance-name "$INSTANCE"; then
  echo "# user-data ($USER_DATA):" >&2
  sed 's/^/#   /' "$USER_DATA" >&2
  run lightsail create-instances --instance-names "$INSTANCE" --availability-zone "$AZ" \
    --blueprint-id "$BLUEPRINT" --bundle-id "$BUNDLE" --ip-address-type ipv4 \
    --user-data "file://$USER_DATA" --tags key=project,value=ha-nova-demo
fi

# 2. Immediately, and on EVERY run (re-running converges after an abort):
#    the firewall, replaced as a whole, and IMDSv2. Lightsail's default opens
#    22 and 80 to everyone while the instance boots; converge retries until
#    the API accepts the call for a still-pending instance, not until it runs.
#    80/443 (tcp + udp for HTTP/3) public; 22 only for Lightsail browser SSH
#    (+ --ssh-cidr). IMDSv2 only, hop limit 1: containers never reach it.
ssh_cidrs='[]'
[[ -n "$SSH_CIDR" ]] && ssh_cidrs="[\"$SSH_CIDR\"]"
pub='"cidrs":["0.0.0.0/0"]'
converge lightsail put-instance-public-ports --instance-name "$INSTANCE" --port-infos \
  "[{\"fromPort\":80,\"toPort\":80,\"protocol\":\"tcp\",$pub},{\"fromPort\":443,\"toPort\":443,\"protocol\":\"tcp\",$pub},{\"fromPort\":443,\"toPort\":443,\"protocol\":\"udp\",$pub},{\"fromPort\":22,\"toPort\":22,\"protocol\":\"tcp\",\"cidrs\":$ssh_cidrs,\"cidrListAliases\":[\"lightsail-connect\"]}]"
converge lightsail update-instance-metadata-options --instance-name "$INSTANCE" \
  --http-tokens required --http-endpoint enabled --http-put-response-hop-limit 1

# 3. Static IPv4, attached once the instance runs.
if [[ "$YES" == 0 ]] || ! has lightsail get-static-ip --static-ip-name "$STATIC_IP"; then
  run lightsail allocate-static-ip --static-ip-name "$STATIC_IP"
fi
if [[ "$YES" == 1 ]]; then
  for i in $(seq 1 60); do
    [[ "$(q lightsail get-instance-state --instance-name "$INSTANCE" --query state.name)" == running ]] && break
    [[ "$i" == 60 ]] && die "instance did not reach 'running'"
    sleep 5
  done
fi
if [[ "$YES" == 0 ]] || [[ "$(q lightsail get-static-ip --static-ip-name "$STATIC_IP" --query staticIp.attachedTo)" != "$INSTANCE" ]]; then
  run lightsail attach-static-ip --static-ip-name "$STATIC_IP" --instance-name "$INSTANCE"
fi

# 4. Post-condition, not trust: an empty cidrs list must not have meant "all".
if [[ "$YES" == 1 ]]; then
  ports="$(q lightsail get-instance-port-states --instance-name "$INSTANCE" --query 'portStates[].fromPort' | tr '\t' '\n' | sort -u | tr '\n' ' ')"
  [[ "$ports" == "22 443 80 " ]] || die "unexpected open ports: $ports"
  # shellcheck disable=SC2016 # JMESPath backticks, not shell
  ssh_state="$(q lightsail get-instance-port-states --instance-name "$INSTANCE" --query 'portStates[?fromPort==`22`].[cidrs,ipv6Cidrs]')"
  [[ "$ssh_state" != *0.0.0.0/0* && "$ssh_state" != *::/0* ]] || die "port 22 is open to the internet: $ssh_state"
  tokens="$(q lightsail get-instance --instance-name "$INSTANCE" --query instance.metadataOptions.httpTokens)"
  [[ "$tokens" == required ]] || die "IMDSv2 not enforced (httpTokens=$tokens)"
fi

# 5. Budget: account-wide monthly cost, mail at 80 % actual and 100 % forecast.
if [[ "$YES" == 0 ]] || ! has budgets describe-budget --account-id "$ACCOUNT" --budget-name "$BUDGET"; then
  sub="\"Subscribers\":[{\"SubscriptionType\":\"EMAIL\",\"Address\":\"$EMAIL\"}]"
  run budgets create-budget --account-id "$ACCOUNT" \
    --budget "{\"BudgetName\":\"$BUDGET\",\"BudgetLimit\":{\"Amount\":\"10\",\"Unit\":\"USD\"},\"TimeUnit\":\"MONTHLY\",\"BudgetType\":\"COST\"}" \
    --notifications-with-subscribers "[{\"Notification\":{\"NotificationType\":\"ACTUAL\",\"ComparisonOperator\":\"GREATER_THAN\",\"Threshold\":80,\"ThresholdType\":\"PERCENTAGE\"},$sub},{\"Notification\":{\"NotificationType\":\"FORECASTED\",\"ComparisonOperator\":\"GREATER_THAN\",\"Threshold\":100,\"ThresholdType\":\"PERCENTAGE\"},$sub}]"
fi

if [[ "$YES" == 1 ]]; then
  ipv4="$(q lightsail get-static-ip --static-ip-name "$STATIC_IP" --query staticIp.ipAddress)"
  cat >&2 << EOF_NEXT

Provisioned. Next (manual, at Namecheap): A $DOMAIN -> $ipv4 (no AAAA: IPv4 only)
Bootstrap takes ~10 min; watch it in Lightsail browser SSH:
  sudo tail -f /var/log/cloud-init-output.log
EOF_NEXT
fi
