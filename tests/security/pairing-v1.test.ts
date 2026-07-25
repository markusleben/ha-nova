import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import * as opaque from "@serenity-kit/opaque";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

import {
  parseCredential,
  digestSecret,
  generateCredential,
} from "../../nova/src/security/device-credential.js";
import {
  MAX_ACTIVE_DEVICES,
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
  CONSUMED_NOTICE_TTL_MS,
  HANDSHAKE_TTL_MS,
  PAIR_CODE_TTL_MS,
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

// Drive the OPAQUE client the way the Go CLI will, returning the decrypted
// finish response (or a failure marker).
function pairAsClient(mgr: PairingV1Manager, code: string, peer: string) {
  const reg = opaque.client.startLogin({ password: code });
  const started = mgr.start(reg.startLoginRequest, peer);
  if (!started.ok) return { started };
  const fin = opaque.client.finishLogin({
    clientLoginState: reg.clientLoginState,
    loginResponse: started.ke2,
    password: code,
    identifiers: IDENTIFIERS,
    keyStretching: KSF,
  });
  if (!fin) return { started, clientFinished: false as const };
  const sessionKey = Buffer.from(fin.sessionKey, "base64url");
  const hsId = Buffer.from(started.handshakeId, "base64url");
  const keys = deriveDirectionKeys(sessionKey, hsId);
  const encMeta = seal(
    keys.c2s,
    hsId,
    "c2s",
    Buffer.from(JSON.stringify(META)),
  ).toString("base64url");
  const finished = mgr.finish(
    started.handshakeId,
    fin.finishLoginRequest,
    encMeta,
    peer,
  );
  if (!finished.ok)
    return { started, finished, keys, hsId, ke3: fin.finishLoginRequest };
  const plain = open(
    keys.s2c,
    hsId,
    "s2c",
    Buffer.from(finished.responseB64, "base64url"),
  );
  return {
    started,
    finished,
    keys,
    hsId,
    ke3: fin.finishLoginRequest,
    response: plain
      ? (JSON.parse(plain.toString("utf8")) as Record<string, unknown>)
      : null,
  };
}

beforeAll(async () => {
  await opaqueReady();
});
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-pairv1-"));
  registry = openDeviceRegistry(dir);
  clock = 1_000_000;
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe("pairing-v1 state machine", () => {
  it("is inactive until the owner generates a code; start fails while inactive", () => {
    const mgr = makeManager();
    expect(mgr.getStatus().phase).toBe("inactive");
    const reg = opaque.client.startLogin({ password: "473921" });
    for (let attempt = 0; attempt < 100; attempt += 1) {
      expect(mgr.start(reg.startLoginRequest, "peer").ok).toBe(false);
    }
    const { code } = mgr.generateCode();
    expect(pairAsClient(mgr, code, "peer").finished?.ok).toBe(true);
  });

  it("completes a full happy-path pairing and hands out a usable pending credential", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    expect(mgr.getStatus().phase).toBe("active");

    const r = pairAsClient(mgr, code, "peer-1");
    expect(r.finished?.ok).toBe(true);
    expect(r.response).not.toBeNull();
    expect(r.response!.spki_pin).toBe(ENDPOINT.spkiPin);
    expect(r.response!.secure_port).toBe(ENDPOINT.securePort);

    // The response credential parses and matches the stored pending digest.
    const parsed = parseCredential(r.response!.credential);
    expect(parsed).not.toBeNull();
    expect(parsed!.deviceId).toBe(r.response!.device_id);

    // Consumed; a device record is pending and becomes usable after activation.
    expect(mgr.getStatus().phase).toBe("consumed");
    const list = registry.list();
    expect(list).toHaveLength(1);
    expect(list[0]!.state).toBe("pending");
    registry.activate(parsed!.deviceId, clock);
    expect(
      registry.resolveDeviceSecret(
        parsed!.deviceId,
        digestSecret(parsed!.secret),
        clock,
      ),
    ).not.toBeNull();

    // The "just connected" notice is time-bound: hours later it would be a lie
    // (and contradict an owner-emptied device list), so it decays to inactive.
    clock += CONSUMED_NOTICE_TTL_MS - 1;
    expect(mgr.getStatus().phase).toBe("consumed");
    clock += 1;
    expect(mgr.getStatus().phase).toBe("inactive");
  });

  it("refuses to generate a code when the secure endpoint is unavailable", () => {
    const mgr = makeManager({ secureEndpoint: () => null });
    expect(() => mgr.generateCode()).toThrow(/secure device port/);
  });

  it("allows an owner code without a mapped TLS port when Cloud pairing is enabled", () => {
    const mgr = makeManager({ secureEndpoint: () => null, cloudPairing: true });
    expect(mgr.generateCode().code).toBe("473921");
    expect(mgr.getStatus().phase).toBe("active");
  });

  it("rejects a wrong code at finish and does not consume", () => {
    const mgr = makeManager();
    mgr.generateCode();
    const r = pairAsClient(mgr, "000000", "peer-1");
    // Wrong code: either the client cannot finish or the server rejects.
    expect(r.finished === undefined || r.finished.ok === false).toBe(true);
    expect(mgr.getStatus().phase).toBe("active");
    expect(registry.list()).toHaveLength(0);
  });

  it("rejects a new install at the active cap without consuming the code", () => {
    // Fill the active roster to the cap, each a distinct install.
    for (let i = 0; i < MAX_ACTIVE_DEVICES; i++) {
      const c = generateCredential();
      registry.createPending(
        {
          deviceId: c.deviceId,
          secretDigest: c.secretDigest,
          clientInstallId: `install-${i}`,
          name: "n",
          platform: "p",
          client: "c",
          createdAtMs: clock,
        },
        clock,
      );
      registry.activate(c.deviceId, clock);
    }
    expect(registry.list().filter((d) => d.state === "active")).toHaveLength(
      MAX_ACTIVE_DEVICES,
    );

    const mgr = makeManager();
    const { code } = mgr.generateCode();
    // META.client_install_id ("install-xyz") is a brand-new install: finish must
    // fail closed WITHOUT consuming the one-time code, so the owner can free a
    // slot and reuse the same code rather than have it silently spent on a
    // pairing that could never activate.
    const r = pairAsClient(mgr, code, "peer-cap");
    expect(r.finished?.ok).toBe(false);
    expect(mgr.getStatus().phase).toBe("active"); // code still usable
    expect(registry.list().filter((d) => d.state === "pending")).toHaveLength(
      0,
    );
    expect(registry.list()).toHaveLength(MAX_ACTIVE_DEVICES);
  });

  it("binds a handshake to its peer (a different peer cannot finish it)", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    const reg = opaque.client.startLogin({ password: code });
    const started = mgr.start(reg.startLoginRequest, "peer-A");
    expect(started.ok).toBe(true);
    if (!started.ok) return;
    const fin = opaque.client.finishLogin({
      clientLoginState: reg.clientLoginState,
      loginResponse: started.ke2,
      password: code,
      identifiers: IDENTIFIERS,
      keyStretching: KSF,
    })!;
    const hsId = Buffer.from(started.handshakeId, "base64url");
    const keys = deriveDirectionKeys(
      Buffer.from(fin.sessionKey, "base64url"),
      hsId,
    );
    const encMeta = seal(
      keys.c2s,
      hsId,
      "c2s",
      Buffer.from(JSON.stringify(META)),
    ).toString("base64url");
    // Finish from a DIFFERENT peer than started the handshake.
    expect(
      mgr.finish(started.handshakeId, fin.finishLoginRequest, encMeta, "peer-B")
        .ok,
    ).toBe(false);
  });

  it("expires the code after its TTL (start becomes inactive)", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    clock += PAIR_CODE_TTL_MS + 1;
    const reg = opaque.client.startLogin({ password: code });
    expect(mgr.start(reg.startLoginRequest, "peer").ok).toBe(false);
    expect(mgr.getStatus().phase).toBe("inactive");
  });

  it("expires a handshake after its TTL (finish fails)", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    const reg = opaque.client.startLogin({ password: code });
    const started = mgr.start(reg.startLoginRequest, "peer");
    expect(started.ok).toBe(true);
    if (!started.ok) return;
    const fin = opaque.client.finishLogin({
      clientLoginState: reg.clientLoginState,
      loginResponse: started.ke2,
      password: code,
      identifiers: IDENTIFIERS,
      keyStretching: KSF,
    })!;
    const hsId = Buffer.from(started.handshakeId, "base64url");
    const keys = deriveDirectionKeys(
      Buffer.from(fin.sessionKey, "base64url"),
      hsId,
    );
    const encMeta = seal(
      keys.c2s,
      hsId,
      "c2s",
      Buffer.from(JSON.stringify(META)),
    ).toString("base64url");
    clock += HANDSHAKE_TTL_MS + 1;
    expect(
      mgr.finish(started.handshakeId, fin.finishLoginRequest, encMeta, "peer")
        .ok,
    ).toBe(false);
  });
});
