// Pairing e2e harness: stands up the REAL relay pairing listeners (plain HTTP
// bootstrap + pinned-TLS device) with a real device registry and TLS identity,
// generates a pairing code, and prints `{bootstrapPort, securePort, code}` on
// one line. The Go pairing e2e (cli/pairing_client_v1_e2e_test.go) then drives
// the REAL shipped CLI client against it: pair -> refuse-over-plain -> activate
// over TLS -> functional 200 -> revoke -> 401.
//
// Functional handlers are stubs (200): the upstream proxy to Home Assistant is
// proven by the disposable-HA e2e; this harness exists to prove the pairing /
// device-credential / SPKI-pinned-TLS / revoke security surface end to end with
// the real code on both sides. Requires `npm run build` first.
import { createServer as createHttpServer } from "node:http";
import { createServer as createHttpsServer } from "node:https";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const dist = join(here, "..", "..", "nova", "dist", "src");

const { createBootstrapListener, createDeviceListener } = await import(join(dist, "runtime", "listeners.js"));
const { openDeviceRegistry } = await import(join(dist, "security", "device-registry.js"));
const { createPairingV1Manager } = await import(join(dist, "security", "pairing-v1.js"));
const { loadOrCreateTlsIdentity } = await import(join(dist, "security", "tls-identity.js"));

const dataDir = mkdtempSync(join(tmpdir(), "nova-pair-harness-"));
const registry = openDeviceRegistry(dataDir);
const tls = await loadOrCreateTlsIdentity(dataDir);

const now = () => Date.now();
let securePort = 0;
const pairing = createPairingV1Manager({
  registry,
  now,
  secureEndpoint: () => (securePort ? { spkiPin: tls.spkiPin, securePort } : null),
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

pairing.generateCode();
const code = pairing.getStatus().code;
if (!code) {
  process.stderr.write("harness failed to generate a pairing code\n");
  process.exit(1);
}

process.stdout.write(`${JSON.stringify({ bootstrapPort, securePort, code })}\n`);
// Keep running until the test kills us; the open servers hold the event loop.
