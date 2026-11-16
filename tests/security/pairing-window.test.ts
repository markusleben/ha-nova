import { mkdtempSync, rmSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import * as opaque from "@serenity-kit/opaque";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { createPairV2ArmHandler } from "../../nova/src/http/handlers/pair-v2.js";
import { openDeviceRegistry } from "../../nova/src/security/device-registry.js";
import { opaqueReady, OPAQUE_CLIENT_ID, OPAQUE_SERVER_ID, OPAQUE_KSF } from "../../nova/src/security/opaque-server.js";
import { deriveDirectionKeys, seal } from "../../nova/src/security/pairing-crypto.js";
import { createPairingV1Manager } from "../../nova/src/security/pairing-v1.js";

const owner = { id: "owner", is_owner: true, is_active: true };
const relay = "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA";
let dir: string;
beforeAll(opaqueReady);
beforeEach(() => { dir = mkdtempSync(join(tmpdir(), "ha-nova-pair-window-")); });
afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

describe("owner declaration through sign-in pairing", () => {
  it.each([
    ["local", true, "keychain", true], ["link", true, "credential_manager", true],
    ["local", false, "secret_service", false], ["local", true, "file", false],
    ["local", true, undefined, false],
  ] as const)("binds the arm's declaration to its %s device (%s, %s)", async (device, own, store, expected) => {
    const registry = openDeviceRegistry(dir);
    const manager = createPairingV1Manager({ registry, secureEndpoint: () => ({ spkiPin: "pin", securePort: 8792 }), cloudPairing: true, now: () => 1000 });
    const handler = createPairV2ArmHandler({ manager, relayInstanceId: relay, relayVersion: "test", fetchAuthUsers: async () => [owner], registryCorrupt: () => false, device });
    const request = {
      headers: { "content-type": "application/json", "x-remote-user-id": owner.id, "x-ingress-path": "/ingress" },
      rawHeaders: ["x-remote-user-id", owner.id, "x-ingress-path", "/ingress"], socket: { remoteAddress: "172.30.32.2" },
    } as unknown as IncomingMessage;
    const armed = await handler({ request, response: {} as ServerResponse, path: "/pair/v2/arm", body: { own_computer: own } }) as { pairing_secret: string };
    const password = armed.pairing_secret;
    const login = opaque.client.startLogin({ password });
    const start = manager.startCloud(login.startLoginRequest, owner.id);
    if (!start.ok) throw new Error("pairing did not start");
    const done = opaque.client.finishLogin({ clientLoginState: login.clientLoginState, loginResponse: start.ke2, password, identifiers: { client: OPAQUE_CLIENT_ID, server: OPAQUE_SERVER_ID }, keyStretching: { "argon2id-custom": OPAQUE_KSF } });
    if (!done) throw new Error("pairing login did not finish");
    const hs = Buffer.from(start.handshakeId, "base64url");
    const keys = deriveDirectionKeys(Buffer.from(done.sessionKey, "base64url"), hs);
    const metadata = seal(keys.c2s, hs, "c2s", Buffer.from(JSON.stringify({ name: "Computer", platform: "linux", client: "cli", client_install_id: "install", credential_store: store, own_computer: !own }))).toString("base64url");
    expect(manager.finishCloud(start.handshakeId, done.finishLoginRequest, metadata, owner.id, relay).ok).toBe(true);
    const pending = registry.list()[0]!;
    const activated = device === "link" ? registry.activatePendingForCloud(pending.deviceId, pending.secretDigest, owner.id, relay, 1000) : { ok: true, record: registry.activate(pending.deviceId, 1000) };
    if (!activated.ok) throw new Error("pairing did not activate");
    expect(activated.record.windowConfirm === true).toBe(expected);
    expect(activated.record.ownComputer).toEqual(expected ? { userId: owner.id, atMs: 1000 } : undefined);
    expect(activated.record.credentialStore).toBe(store);
  });
});
