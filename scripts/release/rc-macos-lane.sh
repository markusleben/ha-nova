#!/usr/bin/env bash
# The macOS rc lane on a disposable GitHub-hosted runner (never a person's
# Mac): a throwaway keychain, the real relay pairing listeners
# (scripts/e2e/pairing-harness.mjs; functional calls are stubs), and the
# published binaries. It proves, without a person:
#   - the rc binary is signed as com.markusleben.ha-nova.cli2;
#   - v0.31.0 pairs into the keychain, `ha-nova update` reaches the rc, and the
#     rc says the computer needs pairing again, with no keychain prompt and
#     the 0.31 items left untouched;
#   - the rc pairs again into the new cli2 slots, reads its key without a
#     prompt, and a second pairing writes a new generation that it reads;
#   - the key goes only to its paired Relay: a changed pin or an http address
#     in config.json is refused;
#   - `uninstall --purge` removes every cli2 item.
# Left to a person: the old-item "Allow" dialog and the native window.
set -euo pipefail

tag="${1:?usage: rc-macos-lane.sh <vX.Y.Z-rcN> <pairing-harness.mjs of that tag>}"
harness="${2:?usage: rc-macos-lane.sh <vX.Y.Z-rcN> <pairing-harness.mjs of that tag>}"
if [ "${GITHUB_ACTIONS:-}" != true ] || [ "${RUNNER_ENVIRONMENT:-}" != github-hosted ] || [ "$(uname -s)" != Darwin ]; then
  echo "Runs only on a disposable GitHub-hosted macOS runner: it replaces the login keychain." >&2
  exit 2
fi

tmp="$(mktemp -d)"
harness_pid=""
cleanup() {
  if [ -n "$harness_pid" ]; then kill "$harness_pid" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }
# A keychain prompt never answers on a runner, so a hang is a prompt.
bounded() { perl -e 'alarm shift; exec @ARGV or die "exec: $!"' 90 "$@"; }

# A throwaway keychain, unlocked, as the only one in every search list; it
# goes with the runner.
kc="$HOME/Library/Keychains/ha-nova-rc-lane.keychain-db"
security create-keychain -p lane "$kc"
security set-keychain-settings "$kc"
security unlock-keychain -p lane "$kc"
for domain in user common dynamic; do security list-keychains -d "$domain" -s "$kc" || true; done
security default-keychain -s "$kc"
security default-keychain -d user -s "$kc"
echo "keychains: $(security list-keychains | tr -d ' \n'); default: $(security default-keychain | tr -d ' ')"
security show-keychain-info "$kc"
items() { security dump-keychain "$kc" 2>/dev/null | grep -o '"svce"<blob>="[^"]*"' | sed 's/.*="//;s/"$//' | sort; }

mkfifo "$tmp/in" "$tmp/out"
node "$harness" "$tmp/relay" <"$tmp/in" >"$tmp/out" &
harness_pid=$!
exec 3>"$tmp/in" 4<"$tmp/out"
read -r info <&4
field() { python3 -c 'import json, sys; print(json.loads(sys.argv[1])[sys.argv[2]])' "$1" "$2"; }
relay="http://127.0.0.1:$(field "$info" bootstrapPort)"
first_code="$(field "$info" code)"
next_code() { echo >&3; read -r line <&4; field "$line" code; }

bin="$HOME/.local/bin/ha-nova"
export HA_NOVA_NO_SETUP=1
call() { bounded "$bin" relay core --method GET --path /api/ </dev/null; }

curl -fsSL https://raw.githubusercontent.com/markusleben/ha-nova/v0.31.0/install.sh | HA_NOVA_VERSION=v0.31.0 bash
bounded "$bin" pair --relay-url "$relay" --code "$first_code" </dev/null || fail "v0.31.0 didn't pair"
call >/dev/null || fail "v0.31.0 couldn't use its key"
old_items="$(items)"
[ -n "$old_items" ] && ! grep -q '^com.markusleben.ha-nova.cli2/' <<<"$old_items" || fail "v0.31.0 wrote no keychain item, or a cli2 one: $old_items"
pass "v0.31.0 paired into the keychain"

bounded "$bin" update --version "${tag#v}" </dev/null || fail "v0.31.0 couldn't update to $tag"
bounded "$bin" version | grep -qF "${tag#v}" || fail "the installed binary isn't $tag"
bash scripts/release/verify-macos-signature.sh "$(readlink -f "$bin" 2>/dev/null || echo "$bin")"
pass "v0.31.0 updated to the cli2-signed $tag"

if out="$(call 2>&1)"; then fail "$tag used a 0.31 key: $out"; fi
grep -qi "paired again" <<<"$out" || fail "$tag didn't say it needs pairing again: $out"
[ "$(items)" = "$old_items" ] || fail "$tag changed the 0.31 keychain items"
pass "$tag says to pair again, with no prompt, and leaves the 0.31 items"

bounded "$bin" pair --code "$(next_code)" </dev/null || fail "$tag didn't pair again"
call >/dev/null || fail "$tag couldn't read its new key without a prompt"
first_gen="$(items | grep '^com.markusleben.ha-nova.cli2/device/' || true)"
[ -n "$first_gen" ] || fail "$tag kept no device key in a cli2 slot: $(items)"
pass "$tag paired into $(sort -u <<<"$first_gen" | tr '\n' ' ')and read it"

before="$(security dump-keychain "$kc" 2>/dev/null | grep -A12 'com.markusleben.ha-nova.cli2/device/' | grep -o '"acct"<blob>="[^"]*"' | sort)"
bounded "$bin" pair --code "$(next_code)" </dev/null || fail "$tag didn't pair a second time"
after="$(security dump-keychain "$kc" 2>/dev/null | grep -A12 'com.markusleben.ha-nova.cli2/device/' | grep -o '"acct"<blob>="[^"]*"' | sort)"
[ "$before" != "$after" ] || fail "a second pairing wrote no new generation"
call >/dev/null || fail "$tag couldn't read the second generation"
pass "a second pairing wrote a new generation, read without a prompt"

config="$HOME/.config/ha-nova/config.json"
[ -f "$config" ] || config="$(find "$HOME" -path '*ha-nova*' -name config.json -not -path '*/node_modules/*' | head -1)"
cp "$config" "$tmp/config.json"
edit() { python3 - "$config" "$1" "$2" <<'PY'
import json, sys
path, key, value = sys.argv[1:]
doc = json.load(open(path))
def walk(node):
    if isinstance(node, dict):
        if key in node and node[key]:
            node[key] = value if value != "http" else node[key].replace("https://", "http://")
        for v in node.values():
            walk(v)
    elif isinstance(node, list):
        for v in node:
            walk(v)
walk(doc)
json.dump(doc, open(path, "w"))
PY
}
edit relay_spki_pin "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
if out="$(call 2>&1)"; then fail "a changed pin in config.json still sent the key: $out"; fi
cp "$tmp/config.json" "$config"
edit relay_secure_base_url http
if out="$(call 2>&1)"; then fail "an http address in config.json still sent the key: $out"; fi
cp "$tmp/config.json" "$config"
call >/dev/null || fail "the restored config no longer works"
pass "the key goes only to its pinned https Relay"

bounded "$bin" uninstall --purge --yes </dev/null || fail "uninstall --purge failed"
if items | grep -q '^com.markusleben.ha-nova.cli2/'; then fail "purge left cli2 items: $(items)"; fi
pass "uninstall --purge removed every cli2 item"
