#!/usr/bin/env bash
# App image regression matrix against a THROWAWAY Supervisor — never a real
# Home Assistant. Spec: docs/work/2026-10-15-app-prebuilt-images-spec.md.
#
# Sandbox: Home Assistant's official apps devcontainer (Supervisor on
# docker-in-docker), privileged but isolated: no published ports, anonymous
# volumes, memory-capped, removed on exit. The App is a LOCAL store app, so
# Supervisor runs its real install/update/pull/build code paths.
#
#   (a) fresh install with image:   -> pull, no build
#   (b) update locally built OLD -> NEW via pull -> /data + credential kept
#   (c) rollback: NEW+1 without image: -> local build again, /data kept
#
# OLD  = nova/ from E2E_OLD_REF (default origin/main), always built locally.
# NEW  = this checkout's nova/. E2E_IMAGE_SOURCE=local (default) builds it for
#        the host arch into a registry inside the sandbox (version: OLD+1 patch
#        when equal); E2E_IMAGE_SOURCE=ghcr pulls the published
#        ghcr.io/markusleben/{arch}-ha-nova-relay:<nova/config.yaml version>.
#
# Run: bash scripts/e2e/supervisor-app/run.sh   (~10 min, needs Docker + network)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
# shellcheck source=../../release/app-image-lib.sh
source "$ROOT/scripts/release/app-image-lib.sh"
SOURCE="${E2E_IMAGE_SOURCE:-local}"
OLD_REF="${E2E_OLD_REF:-origin/main}"
DEVCONTAINER="ghcr.io/home-assistant/devcontainer:6-apps"
C="nova-supervisor-e2e-$$"
SLUG="local_ha_nova_relay"
APP_DIR="/mnt/supervisor/apps/local/ha_nova_relay"
TOKEN="e2e-legacy-token-$RANDOM$RANDOM$RANDOM"
WORK="$(mktemp -d)"
FAILED=0

ok() { echo "  ok: $*"; }
bad() { echo "  FAIL: $*" >&2; FAILED=1; }
die() { echo "  FATAL: $*" >&2; exit 1; }
cleanup() {
  echo "--- tearing down"
  docker rm -f -v "$C" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

case "$(uname -m)" in
  arm64 | aarch64) ARCH=aarch64 ;;
  x86_64 | amd64) ARCH=amd64 ;;
  *) die "unsupported host arch $(uname -m)" ;;
esac
in_c() { docker exec "$C" "$@"; }
ha() { in_c ha "$@"; }
bump_patch() { awk -F. '{ printf "%d.%d.%d\n", $1, $2, $3 + 1 }' <<<"$1"; }

OLD_VERSION="$(app_config_version "$OLD_REF")"
NEW_VERSION="$(app_config_version)"
if [[ "$SOURCE" == ghcr ]]; then
  [[ "$(printf '%s\n%s\n' "$OLD_VERSION" "$NEW_VERSION" | sort -V | tail -1)" == "$NEW_VERSION" && "$OLD_VERSION" != "$NEW_VERSION" ]] \
    || die "ghcr mode needs nova/config.yaml ($NEW_VERSION) newer than $OLD_REF ($OLD_VERSION)"
  IMAGE_TEMPLATE="$APP_IMAGE_TEMPLATE"
else
  [[ "$SOURCE" == local ]] || die "E2E_IMAGE_SOURCE must be local or ghcr"
  [[ "$NEW_VERSION" != "$OLD_VERSION" ]] || NEW_VERSION="$(bump_patch "$OLD_VERSION")"
  IMAGE_TEMPLATE="localhost:5000/nova/{arch}-ha-nova-relay"
fi
ROLLBACK_VERSION="$(bump_patch "$NEW_VERSION")"
NEW_REF="${IMAGE_TEMPLATE//\{arch\}/$ARCH}:$NEW_VERSION"
echo "--- matrix: OLD $OLD_VERSION ($OLD_REF, built) -> NEW $NEW_VERSION ($NEW_REF, pulled) -> ROLLBACK $ROLLBACK_VERSION (built)"

# App source trees: old from git, new + rollback from this checkout.
mkdir -p "$WORK/old" "$WORK/new"
git -C "$ROOT" archive "$OLD_REF" nova | tar -x -C "$WORK/old" --strip-components=1
tar -C "$ROOT" --exclude nova/node_modules --exclude nova/dist -cf - nova | tar -x -C "$WORK/new" --strip-components=1
set_config() { # <dir> <version> <image-template or "">
  local cfg="$1/config.yaml"
  grep -v '^image:' "$cfg" | sed -E "s/^version:.*/version: \"$2\"/" >"$cfg.tmp"
  if [[ -n "$3" ]]; then
    awk -v img="image: \"$3\"" '{ print } /^version:/ { print img }' "$cfg.tmp" >"$cfg"
  else
    mv "$cfg.tmp" "$cfg"
  fi
  rm -f "$cfg.tmp"
}
deploy() { # <dir> <version> — becomes the local store app once the store sees it
  in_c rm -rf "$APP_DIR"
  docker cp "$1" "$C:$APP_DIR" >/dev/null
  # A reload also refreshes every git store and can outlive the CLI timeout.
  for i in $(seq 1 20); do
    ha store reload >/dev/null 2>&1 || true
    [[ "$(ha apps info "$SLUG" --raw-json 2>/dev/null | jq -r '.data.version_latest // empty' 2>/dev/null)" == "$2" ]] && return
    sleep 10
  done
  die "store never offered $SLUG $2"
}

if [[ "$SOURCE" == local ]]; then
  echo "--- building NEW $NEW_VERSION for $ARCH"
  base="$(app_build_from | awk -v a="$ARCH" '$1 == a { print $2 }')"
  docker buildx build -q --platform "$(app_arch_platform "$ARCH")" \
    --build-arg "BUILD_FROM=$base" --build-arg "BUILD_ARCH=$ARCH" --build-arg "BUILD_VERSION=$NEW_VERSION" \
    --label "io.hass.version=$NEW_VERSION" --label "io.hass.arch=$ARCH" --label io.hass.type=app \
    --provenance=false --load -t "$NEW_REF" "$ROOT/nova" >/dev/null
fi

echo "--- starting the sandbox Supervisor"
docker run -d --privileged --name "$C" --memory 4g \
  -e SUPERVISOR_CHANNEL=stable \
  -v /var/lib/docker -v /var/lib/containerd -v /mnt/supervisor --tmpfs /tmp \
  "$DEVCONTAINER" >/dev/null
for i in $(seq 1 30); do in_c docker info >/dev/null 2>&1 && break; [[ $i == 30 ]] && die "inner docker did not start"; sleep 2; done
docker exec -d "$C" bash -c 'supervisor_run > /tmp/supervisor.log 2>&1'
# supervisor_run first removes every inner container; start the registry after.
for i in $(seq 1 60); do in_c docker inspect hassio_supervisor >/dev/null 2>&1 && break; [[ $i == 60 ]] && die "Supervisor container did not start"; sleep 2; done
if [[ "$SOURCE" == local ]]; then
  in_c docker pull -q registry:2 >/dev/null
  in_c docker run -d --name e2e-registry -p 127.0.0.1:5000:5000 registry:2 >/dev/null
  docker save "$NEW_REF" | docker exec -i "$C" docker load >/dev/null
  in_c docker push -q "$NEW_REF" >/dev/null
  in_c docker rmi "$NEW_REF" >/dev/null
  docker rmi "$NEW_REF" >/dev/null
fi

for i in $(seq 1 120); do ha store reload >/dev/null 2>&1 && break; [[ $i == 120 ]] && die "Supervisor not ready"; sleep 5; done
ok "Supervisor ready"
in_c mkdir -p "$(dirname "$APP_DIR")"

# grep a captured copy: piping the log into grep -q would SIGPIPE under pipefail.
sv_has() { grep -q "$1" <<<"$(in_c docker logs hassio_supervisor 2>&1)"; }
data_hash() {
  in_c sh -c 'cd "$(ls -d /mnt/supervisor/apps/data/'"$SLUG"' /mnt/supervisor/addons/data/'"$SLUG"' 2>/dev/null | head -1)" && sha256sum device-registry.json tls-cert.pem tls-key.pem relay-instance-id'
}
expect_running() { # <version> <image-repo> <label>
  local version="$1" ref="$2:$1" label="$3" code=""
  ha apps start "$SLUG" >/dev/null 2>&1 || true
  for i in $(seq 1 40); do
    code="$(in_c curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8791/health || true)"
    [[ "$code" == 200 || "$code" == 401 ]] && break
    sleep 2
  done
  [[ "$(in_c curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8791/health)" == 401 ]] \
    && ok "$label: unauthenticated /health is 401" || bad "$label: relay not up / auth not enforced"
  local image
  image="$(in_c docker inspect -f '{{.Config.Image}}|{{index .Config.Labels "io.hass.version"}}' "app_$SLUG" 2>/dev/null \
    || in_c docker inspect -f '{{.Config.Image}}|{{index .Config.Labels "io.hass.version"}}' "addon_$SLUG")"
  [[ "$image" == "$ref|$version" ]] && ok "$label: container runs $ref" || bad "$label: container is $image, want $ref|$version"
}
seed_token() {
  in_c docker exec hassio_cli sh -c 'curl -sf -X POST -H "Authorization: Bearer $SUPERVISOR_TOKEN" -H content-type:application/json -d "{\"options\":{\"relay_auth_token\":\"'"$TOKEN"'\",\"ha_llat\":\"\",\"file_access\":\"off\"}}" http://supervisor/addons/'"$SLUG"'/options' >/dev/null
  ha apps restart "$SLUG" >/dev/null
}
token_works() { # <version> <label>
  local body=""
  for i in $(seq 1 20); do
    body="$(in_c curl -sf -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8791/health || true)"
    [[ -n "$body" ]] && break
    sleep 2
  done
  grep -qF "\"version\":\"$1\"" <<<"$body" && ok "$2: pre-update credential works, reports $1" \
    || bad "$2: credential/version check failed: ${body:-no response}"
}

echo "--- (a) fresh install of NEW with image:"
set_config "$WORK/new" "$NEW_VERSION" "$IMAGE_TEMPLATE"
deploy "$WORK/new" "$NEW_VERSION"
ha apps install "$SLUG" >/dev/null
sv_has "Starting build for .*:$NEW_VERSION" && bad "(a) Supervisor BUILT $NEW_VERSION" || ok "(a) no local build"
sv_has "Downloading docker image ${NEW_REF%:*} with tag $NEW_VERSION" && ok "(a) pulled $NEW_REF" || bad "(a) no pull of $NEW_REF in Supervisor log"
expect_running "$NEW_VERSION" "${NEW_REF%:*}" "(a)"
ha apps uninstall "$SLUG" >/dev/null

echo "--- (b) OLD $OLD_VERSION (local build) -> NEW $NEW_VERSION (pull)"
set_config "$WORK/old" "$OLD_VERSION" ""
deploy "$WORK/old" "$OLD_VERSION"
ha apps install "$SLUG" >/dev/null
expect_running "$OLD_VERSION" "local/$ARCH-addon-ha_nova_relay" "(b) old"
seed_token
token_works "$OLD_VERSION" "(b) old"
before="$(data_hash)"
deploy "$WORK/new" "$NEW_VERSION"
ha apps update "$SLUG" >/dev/null
expect_running "$NEW_VERSION" "${NEW_REF%:*}" "(b) new"
token_works "$NEW_VERSION" "(b) new"
[[ "$(data_hash)" == "$before" ]] && ok "(b) /data byte-identical (registry, TLS identity, instance id)" || bad "(b) /data changed"

echo "--- (c) rollback: $ROLLBACK_VERSION without image: builds locally"
set_config "$WORK/new" "$ROLLBACK_VERSION" ""
deploy "$WORK/new" "$ROLLBACK_VERSION"
ha apps update "$SLUG" >/dev/null
sv_has "Starting build for .*:$ROLLBACK_VERSION" && ok "(c) local build" || bad "(c) Supervisor did not build $ROLLBACK_VERSION"
expect_running "$ROLLBACK_VERSION" "local/$ARCH-addon-ha_nova_relay" "(c)"
token_works "$ROLLBACK_VERSION" "(c)"
[[ "$(data_hash)" == "$before" ]] && ok "(c) /data byte-identical" || bad "(c) /data changed"

[[ "$FAILED" == 0 ]] && echo "--- PASS" || { echo "--- FAIL" >&2; exit 1; }
