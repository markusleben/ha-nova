// Seeds the app-mode relay's /data like an App that paired one computer and
// once migrated the old shared token (Apps before 0.30): one active device
// with E2E_DEVICE_CREDENTIAL, and a legacy record for RELAY_AUTH_TOKEN. The
// relay must refuse that token and retire the record on its first start.
import { createHash } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";

const digest = (secret) => createHash("sha256").update(secret, "utf8").digest("base64url");
const [, deviceId, secret] = process.env.E2E_DEVICE_CREDENTIAL.split(".");
const now = Date.now();
mkdirSync("/data", { recursive: true });
writeFileSync(
  "/data/device-registry.json",
  JSON.stringify({
    version: 1,
    devices: [{ deviceId, secretDigest: digest(secret), state: "active", clientInstallId: "e2e-install", name: "e2e", platform: "linux", client: "e2e", createdAtMs: now }],
    legacy: { secretDigest: digest(process.env.RELAY_AUTH_TOKEN), createdAtMs: now },
    legacyImportCompleted: true,
    pairingResponses: [],
    cloudRevocations: [],
  }),
  { mode: 0o600 },
);
