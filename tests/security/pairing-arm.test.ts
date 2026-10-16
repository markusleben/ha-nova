import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import * as opaque from "@serenity-kit/opaque";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

import {
  digestSecret,
  parseCredential,
} from "../../nova/src/security/device-credential.js";
import {
  openDeviceRegistry,
  type DeviceRegistry,
} from "../../nova/src/security/device-registry.js";
import {
  OPAQUE_CLIENT_ID,
  OPAQUE_KSF,
  OPAQUE_SERVER_ID,
  opaqueReady,
} from "../../nova/src/security/opaque-server.js";
import {
  deriveDirectionKeys,
  open,
  seal,
} from "../../nova/src/security/pairing-crypto.js";
import {
  PAIR_ARM_TTL_MS,
  createPairingV1Manager,
  type PairingV1Manager,
} from "../../nova/src/security/pairing-v1.js";

// The armed slot behind Home Assistant sign-in pairing
// (docs/work/2026-10-16-ha-sign-in-pairing-spec.md).

const IDENTIFIERS = { client: OPAQUE_CLIENT_ID, server: OPAQUE_SERVER_ID };
const KSF = { "argon2id-custom": OPAQUE_KSF } as const;
const ENDPOINT = { spkiPin: "pin-local", securePort: 8792 };
const RELAY_ID = "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA";
const META = {
  name: "Laptop",
  platform: "linux",
  client: "cli",
  client_install_id: "install-arm-1",
};

let dir: string;
let registry: DeviceRegistry;
let clock: number;

beforeAll(async () => {
  await opaqueReady();
});
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-pair-arm-"));
  registry = openDeviceRegistry(dir);
  clock = 1_000_000;
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

function manager(
  over: Partial<Parameters<typeof createPairingV1Manager>[0]> = {},
): PairingV1Manager {
  return createPairingV1Manager({
    registry,
    secureEndpoint: () => ENDPOINT,
    cloudPairing: true,
    now: () => clock,
    generateCodeNumber: () => 473921,
    ...over,
  });
}

// One full pair-v2 exchange through the ingress entry points.
function pairCloud(mgr: PairingV1Manager, password: string, userId: string) {
  const login = opaque.client.startLogin({ password });
  const started = mgr.startCloud(login.startLoginRequest, userId);
  if (!started.ok) {
    return { started };
  }
  const finishedClient = opaque.client.finishLogin({
    clientLoginState: login.clientLoginState,
    loginResponse: started.ke2,
    password,
    identifiers: IDENTIFIERS,
    keyStretching: KSF,
  });
  if (!finishedClient) {
    return { started, clientFailed: true as const };
  }
  const handshake = Buffer.from(started.handshakeId, "base64url");
  const keys = deriveDirectionKeys(
    Buffer.from(finishedClient.sessionKey, "base64url"),
    handshake,
  );
  const metadata = seal(
    keys.c2s,
    handshake,
    "c2s",
    Buffer.from(JSON.stringify(META)),
  ).toString("base64url");
  const finished = mgr.finishCloud(
    started.handshakeId,
    finishedClient.finishLoginRequest,
    metadata,
    userId,
    RELAY_ID,
  );
  if (!finished.ok) {
    return { started, finished };
  }
  const plaintext = open(
    keys.s2c,
    handshake,
    "s2c",
    Buffer.from(finished.responseB64, "base64url"),
  );
  return {
    started,
    finished,
    response: JSON.parse(plaintext!.toString("utf8")) as Record<
      string,
      unknown
    >,
  };
}

describe("armed sign-in pairing slot", () => {
  it("issues a 256-bit secret that the NOVA page never shows", () => {
    const mgr = manager();
    const armed = mgr.arm("owner-1");
    expect(armed).not.toBeNull();
    expect(armed!.secret).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(armed!.expiresAtMs).toBe(clock + PAIR_ARM_TTL_MS);
    expect(mgr.getStatus()).toEqual({
      phase: "armed",
      expiresAtMs: clock + PAIR_ARM_TTL_MS,
    });
  });

  it("is unavailable without ingress pairing or a secure port", () => {
    expect(manager({ cloudPairing: false }).arm("owner-1")).toBeNull();
    expect(manager({ secureEndpoint: () => null }).arm("owner-1")).toBeNull();
  });

  it("pairs only the arming user and yields an unbound local device with the pin", () => {
    const mgr = manager();
    const { secret } = mgr.arm("owner-1")!;

    expect(pairCloud(mgr, secret, "user-2").started).toEqual({
      ok: false,
      reason: "inactive",
    });

    const paired = pairCloud(mgr, secret, "owner-1");
    expect(paired.finished?.ok).toBe(true);
    expect(Object.keys(paired.response!).sort()).toEqual([
      "credential",
      "device_id",
      "relay_instance_id",
      "secure_port",
      "spki_pin",
    ]);
    expect(paired.response).toMatchObject({
      relay_instance_id: RELAY_ID,
      spki_pin: ENDPOINT.spkiPin,
      secure_port: ENDPOINT.securePort,
    });
    expect(mgr.getStatus().phase).toBe("consumed");

    const [pending] = registry.list();
    expect(pending?.state).toBe("pending");
    expect(pending?.cloudUserId).toBeUndefined();
    expect(pending?.cloudRelayInstanceId).toBeUndefined();
    const credential = parseCredential(String(paired.response!.credential))!;
    expect(
      registry.activatePending(
        credential.deviceId,
        digestSecret(credential.secret),
        clock,
      )?.state,
    ).toBe("active");

    // Single use: the consumed secret starts nothing.
    expect(pairCloud(mgr, secret, "owner-1").started).toEqual({
      ok: false,
      reason: "inactive",
    });
  });

  it("answers the LAN port as inactive without spending the pairing budget", () => {
    const mgr = manager();
    // A typed-code window first spends the whole global budget from the LAN.
    mgr.generateCode();
    for (let peer = 0; peer < 6; peer += 1) {
      for (let attempt = 0; attempt < 5; attempt += 1) {
        mgr.start(opaque.client.startLogin({ password: "000000" }).startLoginRequest, `192.168.1.${peer}`);
      }
    }
    const { secret } = mgr.arm("owner-1")!;
    const login = opaque.client.startLogin({ password: secret });
    for (let attempt = 0; attempt < 40; attempt += 1) {
      expect(mgr.start(login.startLoginRequest, "192.168.1.50")).toEqual({
        ok: false,
        reason: "inactive",
      });
    }
    expect(pairCloud(mgr, secret, "owner-1").finished?.ok).toBe(true);
  });

  it("expires after two minutes", () => {
    const mgr = manager();
    const { secret } = mgr.arm("owner-1")!;
    clock += PAIR_ARM_TTL_MS;
    expect(pairCloud(mgr, secret, "owner-1").started).toEqual({
      ok: false,
      reason: "inactive",
    });
  });

  it("shares the single slot with the NOVA page code: the newer one wins", () => {
    const mgr = manager();
    const { code } = mgr.generateCode();
    const { secret } = mgr.arm("owner-1")!;
    expect(pairCloud(mgr, code, "owner-1").finished?.ok).not.toBe(true);

    const login = opaque.client.startLogin({ password: secret });
    const started = mgr.startCloud(login.startLoginRequest, "owner-1");
    expect(started.ok).toBe(true);
    const fresh = mgr.generateCode();
    expect(mgr.getStatus()).toMatchObject({ phase: "active", code: fresh.code });
    const finishedClient = opaque.client.finishLogin({
      clientLoginState: login.clientLoginState,
      loginResponse: started.ok ? started.ke2 : "",
      password: secret,
      identifiers: IDENTIFIERS,
      keyStretching: KSF,
    })!;
    expect(
      mgr.finishCloud(
        started.ok ? started.handshakeId : "",
        finishedClient.finishLoginRequest,
        "AAAA",
        "owner-1",
        RELAY_ID,
      ),
    ).toEqual({ ok: false, reason: "invalid" });
    expect(registry.list()).toHaveLength(0);
  });

  it("keeps typed-code pairing v2 user-bound without local TLS material", () => {
    const mgr = manager();
    const { code } = mgr.generateCode();
    const paired = pairCloud(mgr, code, "user-1");
    expect(Object.keys(paired.response!).sort()).toEqual([
      "credential",
      "device_id",
      "relay_instance_id",
    ]);
    expect(registry.list()[0]).toMatchObject({
      state: "pending",
      cloudUserId: "user-1",
      cloudRelayInstanceId: RELAY_ID,
    });
  });
});
