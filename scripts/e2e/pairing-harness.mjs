// Pairing e2e harness: stands up the REAL relay pairing listeners (plain HTTP
// bootstrap + pinned-TLS device) with a real device registry and TLS identity,
// generates a pairing code, and prints `{bootstrapPort, securePort, code}` on
// one line. The Go pairing e2e (cli/pairing_client_v1_e2e_test.go) then drives
// the REAL shipped CLI client against it: pair -> refuse-over-plain -> activate
// over TLS -> functional 200 -> revoke -> 401.
//
// It also serves the REAL ingress listener behind a stand-in for Home Assistant
// + Supervisor ingress (session cookie -> X-Remote-User-Id of an owner, prefix
// stripped, Supervisor peer address) and prints `ingressPort`, `ingressRoot`,
// and `ingressSession` for the Home Assistant sign-in pairing e2e
// (cli/pairing_signin_v2_e2e_test.go).
//
// Functional handlers are stubs (200): the upstream proxy to Home Assistant is
// proven by the disposable-HA e2e; this harness exists to prove the pairing /
// device-credential / SPKI-pinned-TLS / revoke security surface end to end with
// the real code on both sides. Requires `npm run build` first.
import { createServer as createHttpServer, request as httpRequest } from "node:http";
import { createServer as createHttpsServer } from "node:https";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const dist = join(here, "..", "..", "nova", "dist", "src");

const { createBootstrapListener, createDeviceListener } = await import(join(dist, "runtime", "listeners.js"));
const { createIngressListener } = await import(join(dist, "runtime", "ingress-listener.js"));
const { openDeviceRegistry } = await import(join(dist, "security", "device-registry.js"));
const { createPairingV1Manager } = await import(join(dist, "security", "pairing-v1.js"));
const { loadOrCreateTlsIdentity } = await import(join(dist, "security", "tls-identity.js"));

// The caller owns the data dir (Go passes t.TempDir()): the test SIGKILLs this
// process, so a dir made here could never be removed.
const dataDir = process.argv[2];
if (!dataDir) {
  process.stderr.write("usage: node pairing-harness.mjs <data-dir>\n");
  process.exit(2);
}
const registry = openDeviceRegistry(dataDir);
const tls = await loadOrCreateTlsIdentity(dataDir);

const now = () => Date.now();
let securePort = 0;
const pairing = createPairingV1Manager({
  registry,
  now,
  secureEndpoint: () => (securePort ? { spkiPin: tls.spkiPin, securePort } : null),
  cloudPairing: true,
});

const ok = ({ response }) => {
  response.statusCode = 200;
  response.setHeader("content-type", "application/json");
  response.end(JSON.stringify({ ok: true, data: null }));
};
const functional = { health: ok, ws: ok, core: ok, files: ok, backups: ok };
const deps = { registry, pairingManager: pairing, functional, relayVersion: "e2e", now };

const bootstrap = createHttpServer(createBootstrapListener(deps));
const device = createHttpsServer({ key: tls.keyPem, cert: tls.certPem, minVersion: "TLSv1.3" }, createDeviceListener(deps));

await new Promise((resolve, reject) => {
  device.on("error", reject);
  device.listen(0, "127.0.0.1", resolve);
});
securePort = device.address().port;
await new Promise((resolve, reject) => {
  bootstrap.on("error", reject);
  bootstrap.listen(0, "127.0.0.1", resolve);
});
const bootstrapPort = bootstrap.address().port;

const OWNER = { id: "owner-e2e", name: "Owner", is_owner: true, is_active: true };
const ingressRoot = "/api/hassio_ingress/e2e-ingress-token-0123456789abcdefABCDEF";
const ingressSession = "c".repeat(128);
const ingressListener = createIngressListener({
  registry,
  pairing,
  functional,
  relayInstanceId: "hanova-relay-v1.E2EE2EE2EE2EE2EE2EE2EE",
  relayVersion: "e2e",
  cloudRemoteEnabled: true,
  wsClient: { sendMessage: async () => [OWNER], isConnected: () => true },
  supervisor: { getSelfInfo: async () => ({ version: "e2e", versionLatest: "e2e", updateAvailable: false }) },
  registryCorrupt: () => false,
  resetRegistry: () => {},
  now,
  logger: { warn: () => {}, error: () => {} },
});
const ingress = createHttpServer((req, res) => {
  Object.defineProperty(req.socket, "remoteAddress", { configurable: true, value: "172.30.32.2" });
  ingressListener(req, res);
});
await new Promise((resolve, reject) => {
  ingress.on("error", reject);
  ingress.listen(0, "127.0.0.1", resolve);
});
// Stand-in for Home Assistant + Supervisor ingress: the session cookie becomes
// the owner identity, client-supplied identity headers are dropped.
const front = createHttpServer((req, res) => {
  const cookie = /(?:^|;\s*)ingress_session=([^;]+)/.exec(req.headers.cookie ?? "")?.[1];
  if (!req.url.startsWith(`${ingressRoot}/`) || cookie !== ingressSession) {
    res.statusCode = 401;
    res.end();
    return;
  }
  const headers = { ...req.headers, "x-remote-user-id": OWNER.id, "x-ingress-path": ingressRoot };
  const upstream = httpRequest(
    { host: "127.0.0.1", port: ingress.address().port, method: req.method, path: req.url.slice(ingressRoot.length), headers },
    (answer) => {
      res.writeHead(answer.statusCode ?? 502, answer.headers);
      answer.pipe(res);
    },
  );
  req.pipe(upstream);
});
await new Promise((resolve, reject) => {
  front.on("error", reject);
  front.listen(0, "127.0.0.1", resolve);
});
const ingressPort = front.address().port;

pairing.generateCode();
const code = pairing.getStatus().code;
if (!code) {
  process.stderr.write("harness failed to generate a pairing code\n");
  process.exit(1);
}

process.stdout.write(`${JSON.stringify({ bootstrapPort, securePort, code, ingressPort, ingressRoot, ingressSession })}\n`);
// Each stdin line asks for a fresh one-time code, printed as {"code"} on its own
// line, so one Relay can pair several devices (second-profile e2e).
process.stdin.on("data", () => {
  pairing.generateCode();
  process.stdout.write(`${JSON.stringify({ code: pairing.getStatus().code })}\n`);
});
// Keep running until the test kills us; the open servers hold the event loop.
