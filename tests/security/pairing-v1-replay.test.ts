import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import * as opaque from "@serenity-kit/opaque";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

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
  createPairingV1Manager,
  type PairingV1Manager,
  type SecureEndpoint,
} from "../../nova/src/security/pairing-v1.js";

const IDENTIFIERS = { client: OPAQUE_CLIENT_ID, server: OPAQUE_SERVER_ID };
const KSF = { "argon2id-custom": OPAQUE_KSF } as const;
const ENDPOINT: SecureEndpoint = {
  spkiPin: "PINPINPINPINPINPINPINPINPINPINPINPINPINPINP",
  securePort: 8792,
};
const META = {
  name: "MacBook",
  platform: "darwin",
  client: "claude",
  client_install_id: "install-xyz",
};

let dir: string;
let registry: DeviceRegistry;
let clock: number;

function makeManager(
  over: Partial<Parameters<typeof createPairingV1Manager>[0]> = {},
): PairingV1Manager {
  return createPairingV1Manager({
    registry,
    secureEndpoint: () => ENDPOINT,
    now: () => clock,
    generateCodeNumber: () => 473921,
    ...over,
  });
}

function pairAsClient(mgr: PairingV1Manager, code: string, peer: string) {
  const registration = opaque.client.startLogin({ password: code });
  const started = mgr.start(registration.startLoginRequest, peer);
  if (!started.ok) return { started };
  const finishedLogin = opaque.client.finishLogin({
    clientLoginState: registration.clientLoginState,
    loginResponse: started.ke2,
    password: code,
    identifiers: IDENTIFIERS,
    keyStretching: KSF,
  });
  if (!finishedLogin) return { started, clientFinished: false as const };
  const sessionKey = Buffer.from(finishedLogin.sessionKey, "base64url");
  const handshakeId = Buffer.from(started.handshakeId, "base64url");
  const keys = deriveDirectionKeys(sessionKey, handshakeId);
  const encryptedMetadata = seal(
    keys.c2s,
    handshakeId,
    "c2s",
    Buffer.from(JSON.stringify(META)),
  ).toString("base64url");
  const finished = mgr.finish(
    started.handshakeId,
    finishedLogin.finishLoginRequest,
    encryptedMetadata,
    peer,
  );
  if (!finished.ok) {
    return {
      started,
      finished,
      keys,
      hsId: handshakeId,
      ke3: finishedLogin.finishLoginRequest,
    };
  }
  const plaintext = open(
    keys.s2c,
    handshakeId,
    "s2c",
    Buffer.from(finished.responseB64, "base64url"),
  );
  return {
    started,
    finished,
    keys,
    hsId: handshakeId,
    ke3: finishedLogin.finishLoginRequest,
    response: plaintext
      ? (JSON.parse(plaintext.toString("utf8")) as Record<string, unknown>)
      : null,
  };
}

beforeAll(async () => {
  await opaqueReady();
});
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-pairv1-replay-"));
  registry = openDeviceRegistry(dir);
  clock = 1_000_000;
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe("pairing-v1 replay and atomicity", () => {
  it("returns the same ciphertext on an identical finish retry, and generic error on a divergent one", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    const result = pairAsClient(mgr, code, "peer");
    expect(result.finished?.ok).toBe(true);
    const first = (result.finished as { ok: true; responseB64: string })
      .responseB64;
    const retry = mgr.finish(
      result.started!.ok ? result.started!.handshakeId : "",
      result.ke3!,
      "ignored",
      "peer",
    );
    expect(retry.ok).toBe(true);
    if (retry.ok) expect(retry.responseB64).toBe(first);
    expect(
      mgr.finish(
        result.started!.ok ? result.started!.handshakeId : "",
        "AAAA",
        "x",
        "peer",
      ).ok,
    ).toBe(false);
    expect(registry.list()).toHaveLength(1);
  });

  it("persists the pending credential and replay response in one restart-safe registry state", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    const paired = pairAsClient(mgr, code, "peer");
    expect(paired.finished?.ok).toBe(true);
    const first = (paired.finished as { ok: true; responseB64: string })
      .responseB64;
    const onDisk = JSON.parse(
      readFileSync(join(dir, "device-registry.json"), "utf8"),
    ) as { devices: unknown[]; pairingResponses: unknown[] };
    expect(onDisk.devices).toHaveLength(1);
    expect(onDisk.pairingResponses).toHaveLength(1);

    registry = openDeviceRegistry(dir);
    const retry = makeManager().finish(
      paired.started!.ok ? paired.started!.handshakeId : "",
      paired.ke3!,
      "not-needed-for-a-persisted-retry",
      "peer",
    );
    expect(retry).toEqual({ ok: true, responseB64: first });
    expect(registry.list()).toHaveLength(1);
  });

  it("leaves neither durable half when the combined finish commit fails", () => {
    const failingRegistry: DeviceRegistry = {
      ...registry,
      createPendingWithResponse: () => {
        throw new Error("simulated durable write failure");
      },
    };
    const mgr = makeManager({ registry: failingRegistry });
    const { code } = mgr.generateCode();
    const failed = pairAsClient(mgr, code, "peer");
    expect(failed.finished?.ok).toBe(false);
    expect(mgr.getStatus().phase).toBe("active");
    expect(registry.list()).toHaveLength(0);
    expect(
      registry.getPairingResponse(
        failed.started!.ok ? failed.started!.handshakeId : "",
        "local",
        clock,
      ),
    ).toBeNull();
  });

  it("lets exactly one of two concurrent handshakes consume the code", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    const client1 = opaque.client.startLogin({ password: code });
    const started1 = mgr.start(client1.startLoginRequest, "peer-1");
    const client2 = opaque.client.startLogin({ password: code });
    const started2 = mgr.start(client2.startLoginRequest, "peer-2");
    expect(started1.ok && started2.ok).toBe(true);
    if (!started1.ok || !started2.ok) return;

    const finish = (
      client: ReturnType<typeof opaque.client.startLogin>,
      started: typeof started1,
      peer: string,
    ) => {
      if (!started.ok) return false;
      const login = opaque.client.finishLogin({
        clientLoginState: client.clientLoginState,
        loginResponse: started.ke2,
        password: code,
        identifiers: IDENTIFIERS,
        keyStretching: KSF,
      })!;
      const handshakeId = Buffer.from(started.handshakeId, "base64url");
      const keys = deriveDirectionKeys(
        Buffer.from(login.sessionKey, "base64url"),
        handshakeId,
      );
      const encryptedMetadata = seal(
        keys.c2s,
        handshakeId,
        "c2s",
        Buffer.from(JSON.stringify(META)),
      ).toString("base64url");
      return mgr.finish(
        started.handshakeId,
        login.finishLoginRequest,
        encryptedMetadata,
        peer,
      ).ok;
    };

    expect(finish(client1, started1, "peer-1")).toBe(true);
    expect(finish(client2, started2, "peer-2")).toBe(false);
    expect(registry.list()).toHaveLength(1);
  });

  it("rejects malformed KE1 without consuming the cryptographic attempt budget", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    for (let attempt = 0; attempt < 100; attempt += 1) {
      expect(mgr.start("not base64!", "peer").ok).toBe(false);
    }
    expect(pairAsClient(mgr, code, "peer").finished?.ok).toBe(true);
  });
});
