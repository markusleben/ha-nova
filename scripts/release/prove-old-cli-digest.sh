#!/usr/bin/env bash
# Runs the published v0.31.0 Linux binary's update check against the body
# GoReleaser publishes for v0.36.0 (.goreleaser.yml's stable release header
# and footer), served as api.github.com's latest release, and requires the
# Mac re-pair warning as its headline and first action item (0.32 spec §5.1):
# the 0.31 CLI already installed can't learn a new pre-update check, so its
# own notice must carry the warning. Disposable CI runners only: it points
# api.github.com at this machine for the run.
set -euo pipefail

warning="Updating a Mac from 0.31? HA NOVA asks you to pair it again once."
if [ "${GITHUB_ACTIONS:-}" != true ] || [ "${RUNNER_ENVIRONMENT:-}" != github-hosted ]; then
  echo "Runs only on a disposable GitHub-hosted runner: it edits /etc/hosts." >&2
  exit 2
fi

tmp="$(mktemp -d)"
server=""
cleanup() {
  if [ -n "$server" ]; then sudo kill "$server" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

base="https://github.com/markusleben/ha-nova/releases/download/v0.31.0"
curl -fsSL -o "$tmp/ha-nova-linux-amd64" "$base/ha-nova-linux-amd64"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
(cd "$tmp" && grep ' ha-nova-linux-amd64$' checksums.txt | sha256sum -c - >/dev/null)
chmod +x "$tmp/ha-nova-linux-amd64"

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=api.github.com \
  -addext subjectAltName=DNS:api.github.com \
  -keyout "$tmp/key.pem" -out "$tmp/cert.pem" 2>/dev/null
python3 - .goreleaser.yml >"$tmp/latest.json" <<'PY'
import json, sys
yml = open(sys.argv[1], encoding="utf-8").read()
def block(key):
    rest = yml.split("\nrelease:\n", 1)[1].split("\n  " + key + ": |\n", 1)[1]
    lines = []
    for line in rest.split("\n"):
        if line and not line.startswith("    "):
            break
        lines.append(line[4:])
    return "\n".join(lines).rstrip("\n") + "\n"
header = block("header")
if not header.startswith("{{ if .Prerelease }}") or header.count("{{ else }}") != 1 or not header.rstrip().endswith("{{ end }}"):
    sys.exit("release.header isn't {{ if .Prerelease }}…{{ else }}…{{ end }}")
stable = header.split("{{ else }}", 1)[1].rstrip()[: -len("{{ end }}")].replace("{{ .Tag }}", "v0.36.0")
if "{{" in stable or "## New Features" not in stable:
    sys.exit("the stable release header has a template this proof can't render, or no New Features section")
print(json.dumps({"tag_name": "v0.36.0", "html_url": "https://github.com/markusleben/ha-nova/releases/tag/v0.36.0",
                  "body": stable + "\n" + block("footer"), "published_at": "2026-11-23T00:00:00Z", "assets": []}))
PY
sudo python3 - "$tmp" <<'PY' &
import http.server, os, ssl, sys
root = sys.argv[1]
class Latest(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/repos/markusleben/ha-nova/releases/latest":
            self.send_error(404)
            return
        data = open(os.path.join(root, "latest.json"), "rb").read()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)
    def log_message(self, *args):
        pass
server = http.server.HTTPServer(("127.0.0.1", 443), Latest)
context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.load_cert_chain(os.path.join(root, "cert.pem"), os.path.join(root, "key.pem"))
server.socket = context.wrap_socket(server.socket, server_side=True)
server.serve_forever()
PY
server=$!
echo "127.0.0.1 api.github.com" | sudo tee -a /etc/hosts >/dev/null
for _ in $(seq 1 50); do
  curl -fsS --cacert "$tmp/cert.pem" -o /dev/null https://api.github.com/repos/markusleben/ha-nova/releases/latest && break
  sleep 0.2
done

# Each run gets its own home, so neither reads the other's release cache.
old_cli() {
  local home="$tmp/$1"
  shift
  mkdir -p "$home"
  HOME="$home" XDG_CONFIG_HOME="$home/.config" XDG_CACHE_HOME="$home/.cache" SSL_CERT_FILE="$tmp/cert.pem" \
    "$tmp/ha-nova-linux-amd64" "$@"
}
text="$(old_cli text check-update --quiet 2>&1 || true)"
if ! grep -qF "New: $warning" <<<"$text"; then
  printf '%s\n' "$text" >&2
  echo "v0.31.0's update notice doesn't lead with the Mac re-pair warning." >&2
  exit 1
fi
json="$(old_cli json check-update --json)"
if ! python3 -c 'import json, sys
h = [x for x in json.loads(sys.argv[1]).get("release_highlights") or [] if x.get("kind") == "action"]
sys.exit(0 if h and h[0]["text"].startswith(sys.argv[2]) else 1)' "$json" "$warning"; then
  printf '%s\n' "$json" >&2
  echo "v0.31.0's release_highlights don't lead with the Mac re-pair warning as an action item." >&2
  exit 1
fi
echo "v0.31.0 shows the Mac re-pair warning as its headline and first action item."
