#!/usr/bin/env bash
# One-time host setup for the hosted demo, run as root on a fresh Lightsail
# Ubuntu 24.04 instance by provision.sh's user-data (which cloned the repo to
# /opt/ha-nova at the pinned ref). Idempotent: safe to re-run, e.g. after
# `git -C /opt/ha-nova fetch` with a new --ref.
#
#   bash /opt/ha-nova/scripts/demo/hosted/bootstrap.sh --domain demo.hanova.app --ref <sha|tag>
#
# Holds no secrets: everything secret is generated on the host by reset.sh.
set -euo pipefail

NODE_VERSION=22.23.3
NODE_SHA256=df450af89261115ef9f9e3830c3eeb2cc9213b63c720b1af623cb5dcbe2e02de
DOCKER_GPG_FPR=9DC858229FC7DD38854AE2D88D81803C0EBFCD88
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

die() { echo "bootstrap: $*" >&2; exit 1; }
step() { echo "--- $*" >&2; }

DOMAIN="" REF=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain) DOMAIN="${2:-}"; shift 2 ;;
    --ref) REF="${2:-}"; shift 2 ;;
    *) die "unknown argument: $1" ;;
  esac
done
[[ "$DOMAIN" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || die "--domain is required (e.g. demo.hanova.app)"
[[ "$REF" =~ ^[A-Za-z0-9._/-]+$ ]] || die "--ref is required (commit sha or tag)"
[[ "$(id -u)" == 0 ]] || die "run as root"
# shellcheck source=/dev/null
. /etc/os-release
[[ "$ID" == ubuntu && "$VERSION_ID" == 24.04 ]] || die "expects Ubuntu 24.04 (got $ID $VERSION_ID)"
[[ "$(uname -m)" == x86_64 ]] || die "expects x86_64 (Lightsail micro_3_0)"
[[ "$HERE" == /opt/ha-nova/scripts/demo/hosted ]] || die "expects the clone at /opt/ha-nova (got $HERE)"
export DEBIAN_FRONTEND=noninteractive

step "swap: 1 GB swapfile (1 GB RAM is tight for Home Assistant)"
if ! swapon --show=NAME --noheadings | grep -qx /swapfile; then
  [[ -f /swapfile ]] || fallocate -l 1G /swapfile
  chmod 600 /swapfile
  mkswap /swapfile > /dev/null
  swapon /swapfile
fi
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab

step "ssh: key-only, no root login (first match wins, so this sorts first)"
cat > /etc/ssh/sshd_config.d/10-ha-nova-demo.conf << 'CONF'
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
X11Forwarding no
AllowAgentForwarding no
CONF
sshd -t
systemctl try-reload-or-restart ssh # socket-activated on 24.04: no-op when idle

step "packages: minimal set, fail2ban for sshd, unattended upgrades"
apt-get update -q
apt-get install -y -q ca-certificates curl git gnupg openssl xz-utils fail2ban unattended-upgrades
systemctl enable --now fail2ban
cat > /etc/apt/apt.conf.d/20auto-upgrades << 'CONF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
CONF
# Ubuntu security updates are on by default; add Docker's repo and reboot
# when an update needs it, after the nightly reset (03:30 UTC).
cat > /etc/apt/apt.conf.d/52ha-nova-demo << 'CONF'
Unattended-Upgrade::Origins-Pattern { "origin=Docker,label=Docker CE"; };
Unattended-Upgrade::Automatic-Reboot "true";
Unattended-Upgrade::Automatic-Reboot-Time "04:30";
CONF

step "docker: official repository (key fingerprint pinned), live-restore, no TCP socket"
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
gpg --show-keys --with-colons /etc/apt/keyrings/docker.asc | grep -q "^fpr:::::::::$DOCKER_GPG_FPR:" \
  || die "Docker's apt key does not match fingerprint $DOCKER_GPG_FPR"
chmod a+r /etc/apt/keyrings/docker.asc
echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $VERSION_CODENAME stable" \
  > /etc/apt/sources.list.d/docker.list
apt-get update -q
apt-get install -y -q docker-ce docker-ce-cli containerd.io docker-compose-plugin
install -m 0755 -d /etc/docker
cat > /etc/docker/daemon.json << 'CONF'
{
  "live-restore": true,
  "no-new-privileges": true,
  "log-driver": "local",
  "log-opts": { "max-size": "10m", "max-file": "3" }
}
CONF
systemctl restart docker
# dockerd listens on its unix socket only (the default); fail loud otherwise.
if ss -H -ltnp | grep -q dockerd; then die "dockerd listens on TCP; remove the -H tcp:// setting"; fi

step "node $NODE_VERSION (the test bed's seeding runs on node; checksum pinned)"
if [[ "$(/usr/local/bin/node --version 2> /dev/null || true)" != "v$NODE_VERSION" ]]; then
  tmp="$(mktemp -d)"
  curl -fsSLo "$tmp/node.tar.xz" "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-linux-x64.tar.xz"
  echo "$NODE_SHA256  $tmp/node.tar.xz" | sha256sum -c --quiet -
  rm -rf /opt/node
  install -d /opt/node
  tar -xJf "$tmp/node.tar.xz" -C /opt/node --strip-components=1
  ln -sf /opt/node/bin/node /usr/local/bin/node
  rm -rf "$tmp"
fi

step "demo config and Caddy state (certificates survive every reset)"
install -d -o 1000 -g 1000 -m 0700 /var/lib/ha-nova-demo/caddy/data /var/lib/ha-nova-demo/caddy/config
printf 'DEMO_DOMAIN=%s\nDEMO_REF=%s\n' "$DOMAIN" "$REF" > /etc/ha-nova-demo.env
chmod 0644 /etc/ha-nova-demo.env

step "nightly reset: ha-nova-demo-reset.timer (03:30 UTC)"
cat > /etc/systemd/system/ha-nova-demo-reset.service << 'UNIT'
[Unit]
Description=HA NOVA demo: reset the house to the pristine seed
Requires=docker.service
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/bin/bash /opt/ha-nova/scripts/demo/hosted/reset.sh
TimeoutStartSec=20min
UNIT
cat > /etc/systemd/system/ha-nova-demo-reset.timer << 'UNIT'
[Unit]
Description=HA NOVA demo: nightly reset

[Timer]
OnCalendar=*-*-* 03:30:00 UTC
Persistent=true

[Install]
WantedBy=timers.target
UNIT
systemctl daemon-reload
systemctl enable --now ha-nova-demo-reset.timer

step "first boot of the demo house"
systemctl start ha-nova-demo-reset.service

step "listener check: only 22, 80, 443 may listen beyond loopback"
bad="$(ss -H -ltn | awk '{print $4}' | grep -v -E '^(127\.|\[::1\]|\[::ffff:127\.)' | grep -v -E ':(22|80|443)$' || true)"
[[ -z "$bad" ]] || die "unexpected TCP listeners: $bad"
free -m >&2
echo "bootstrap: done. https://$DOMAIN (login demo / demo) once DNS points here." >&2
