#!/usr/bin/env bash
# Client-side release regression matrix: public install of a stable tag, setup
# against the local test bed, client wiring, update to the candidate, uninstall.
#
#   bash scripts/e2e/client-matrix/run.sh --from v0.25.1 --to local
#   bash scripts/e2e/client-matrix/run.sh --from v0.25.0 --to v0.25.1 --lanes linux
#
# Never touches the real ~/.config/ha-nova, ~/.local, client configs, rc files,
# or keychain items: every scenario runs in a throwaway HOME (lane.sh). Talks
# only to the local test bed (127.0.0.1:18123/18791) and GitHub. See README.md.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"
TESTBED="$ROOT/scripts/e2e/testbed"
FROM="" TO="" LANES="" KEEP=0 STARTED_TESTBED=0 SERVER_PID="" REPORTED=0

usage() { sed -n '2,6p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
die() { echo "client-matrix: $*" >&2; exit 1; }

while (($#)); do
  case "$1" in
    --from) FROM="${2:-}"; shift 2 ;;
    --to) TO="${2:-}"; shift 2 ;;
    --lanes) LANES="${2:-}"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    *) usage ;;
  esac
done
TAG_RE='^v[0-9]+\.[0-9]+\.[0-9]+(-rc[0-9]+)?$'
[[ "$FROM" =~ $TAG_RE ]] || usage
[[ "$TO" == local || "$TO" =~ $TAG_RE ]] || usage
if [[ -z "$LANES" ]]; then
  LANES=linux; [[ "$(uname -s)" == Darwin ]] && LANES=macos,linux
fi
for cmd in curl docker; do command -v "$cmd" >/dev/null || die "$cmd is required"; done
if [[ "$TO" == local ]]; then
  for cmd in go node python3 git jq; do command -v "$cmd" >/dev/null || die "$cmd is required for --to local"; done
  bash -c 'mapfile -t _ </dev/null' 2>/dev/null || die "bash >= 4 on PATH is required to build bundles (brew install bash)"
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/ha-nova-client-matrix.XXXXXX")"
WORK="$(cd -P "$WORK" && pwd -P)"
RESULTS="$WORK/results.txt"
: >"$RESULTS"

cleanup() {
  local rc=$?
  ((rc == 0 || REPORTED)) || echo "client-matrix: aborted (exit $rc); work dir: $WORK" >&2
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
  if ((STARTED_TESTBED)); then bash "$TESTBED/down.sh" >/dev/null 2>&1 || echo "client-matrix: testbed down failed" >&2; fi
}
trap cleanup EXIT

# --- test bed: reuse a running one (never tear it down), else start our own --
relay_wd="$(docker inspect ha-nova-testbed-relay --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' 2>/dev/null || true)"
if [[ -n "$relay_wd" && "$(docker inspect ha-nova-testbed-relay --format '{{.State.Running}}')" == true ]]; then
  STATE="$relay_wd/.state"
  echo "client-matrix: reusing running test bed ($relay_wd)"
else
  echo "client-matrix: starting the test bed (torn down at exit)"
  STARTED_TESTBED=1
  bash "$TESTBED/up.sh" >"$WORK/testbed-up.log" 2>&1 || die "testbed up failed: $WORK/testbed-up.log"
  STATE="$TESTBED/.state"
fi
RELAY_TOKEN="$(cat "$STATE/relay-token")" || die "no relay token in $STATE"
curl -fsS -m 120 -H "Authorization: Bearer $RELAY_TOKEN" http://127.0.0.1:18791/health | grep -q '"ok":true' ||
  die "test bed relay on 127.0.0.1:18791 is not healthy"

# Linux lane addressing: Docker Desktop reaches host loopback via
# host.docker.internal; a native Linux host shares its network instead.
DOCKER_NET=(--add-host=host.docker.internal:host-gateway) CHOST=host.docker.internal
if [[ "$(uname -s)" == Linux ]]; then DOCKER_NET=(--network host) CHOST=127.0.0.1; fi

# --- candidate bundles --------------------------------------------------------
TO_VERSION="${TO#v}" PORT=""
if [[ "$TO" == local ]]; then
  echo "client-matrix: building local bundles from HEAD (~2 min cold)"
  TO_VERSION="$(bash "$HERE/build-local.sh" "$WORK/build" "$FROM" 2>"$WORK/build.log")" ||
    die "local build failed: $WORK/build.log"
  PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
  python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$WORK/build/dist/install-bundles" \
    >"$WORK/http.log" 2>&1 &
  SERVER_PID=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do curl -fsS -o /dev/null "http://127.0.0.1:$PORT/" 2>/dev/null && break; sleep 0.5; done
  echo "client-matrix: local candidate is v$TO_VERSION"
fi

# --- lanes --------------------------------------------------------------------
run_macos() {
  [[ "$(uname -s)" == Darwin ]] || { echo "macos|-|lane|SKIP|not a macOS host" >>"$RESULTS"; return; }
  local claude=""
  if command -v claude >/dev/null; then claude="$(cd -P "$(dirname "$(command -v claude)")" && pwd -P)/$(basename "$(command -v claude)")"; fi
  LANE=macos FROM="$FROM" TO="$TO" TO_VERSION="$TO_VERSION" RELAY_TOKEN="$RELAY_TOKEN" \
    BUNDLE_BASE="http://127.0.0.1:$PORT" RELAY_URL=http://127.0.0.1:18791 HA_URL=http://127.0.0.1:18123 \
    WORK="$WORK" RESULTS="$RESULTS" CLAUDE_BIN="$claude" bash "$HERE/lane.sh"
}

run_linux() {
  mkdir -p "$WORK/linux"
  # Root only installs curl and creates the test user; the lane runs unprivileged.
  RELAY_TOKEN="$RELAY_TOKEN" docker run --rm "${DOCKER_NET[@]}" \
    -e FROM="$FROM" -e TO="$TO" -e TO_VERSION="$TO_VERSION" -e RELAY_TOKEN \
    -e BUNDLE_BASE="http://$CHOST:$PORT" -e RELAY_URL="http://$CHOST:18791" -e HA_URL="http://$CHOST:18123" \
    -e HOST_IDS="$(id -u):$(id -g)" \
    -v "$HERE:/harness:ro" -v "$WORK/linux:/out" ubuntu:24.04 bash -c '
      set -e
      apt-get update -qq >/dev/null && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl ca-certificates zsh >/dev/null
      useradd -m tester && mkdir -p /work && chown tester /work /out
      runuser -u tester -- env LANE=linux WORK=/work RESULTS=/out/results.txt \
        FROM="$FROM" TO="$TO" TO_VERSION="$TO_VERSION" RELAY_TOKEN="$RELAY_TOKEN" BUNDLE_BASE="$BUNDLE_BASE" \
        RELAY_URL="$RELAY_URL" HA_URL="$HA_URL" bash /harness/lane.sh || true
      for d in /work/*/logs; do mkdir -p "/out/${d#/work/}"; cp -R "$d/." "/out/${d#/work/}/"; done
      chown -R "$HOST_IDS" /out' >"$WORK/linux/container.log" 2>&1 ||
    echo "linux|-|container|FAIL|docker run failed: $WORK/linux/container.log" >>"$RESULTS"
  [[ -f "$WORK/linux/results.txt" ]] && sed "s#/work/#$WORK/linux/#g" "$WORK/linux/results.txt" >>"$RESULTS"
}

IFS=, read -r -a lanes <<<"$LANES"
for lane in "${lanes[@]}"; do
  start=$SECONDS
  case "$lane" in macos) run_macos ;; linux) run_linux ;; *) die "unknown lane $lane" ;; esac
  echo "client-matrix: $lane lane took $((SECONDS - start)) s"
done

# --- report -------------------------------------------------------------------
REPORTED=1
echo
{ echo "LANE|SCN|STEP|RESULT|DETAIL"; cat "$RESULTS"; } | column -t -s '|'
fails="$(grep -c '|FAIL|' "$RESULTS" || true)"
for lane in "${lanes[@]}"; do
  grep -q "^$lane|" "$RESULTS" || { echo "client-matrix: $lane lane produced no results"; fails=$((fails + 1)); }
done
echo
if ((fails)) || ((KEEP)); then echo "client-matrix: work dir kept: $WORK"; else rm -rf "$WORK"; fi
((fails == 0)) || { echo "client-matrix: $fails FAIL"; exit 1; }
echo "client-matrix: all PASS ($FROM -> $TO)"
