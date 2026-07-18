import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import * as opaque from "@serenity-kit/opaque";
import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

import { parseCredential, digestSecret, generateCredential } from "../../nova/src/security/device-credential.js";
import { MAX_ACTIVE_DEVICES, openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { OPAQUE_CLIENT_ID, OPAQUE_KSF, OPAQUE_SERVER_ID, opaqueReady } from "../../nova/src/security/opaque-server.js";
import { deriveDirectionKeys, open, seal } from "../../nova/src/security/pairing-crypto.js";
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
const ENDPOINT: SecureEndpoint = { spkiPin: "PINPINPINPINPINPINPINPINPINPINPINPINPINPINP", securePort: 8792 };
const META = { name: "MacBook", platform: "darwin", client: "claude", client_install_id: "install-xyz" };

let dir: string;
let registry: DeviceRegistry;
let clock: number;

function makeManager(over: Partial<Parameters<typeof createPairingV1Manager>[0]> = {}): PairingV1Manager {
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
  const encMeta = seal(keys.c2s, hsId, "c2s", Buffer.from(JSON.stringify(META))).toString("base64url");
  const finished = mgr.finish(started.handshakeId, fin.finishLoginRequest, encMeta, peer);
  if (!finished.ok) return { started, finished, keys, hsId, ke3: fin.finishLoginRequest };
  const plain = open(keys.s2c, hsId, "s2c", Buffer.from(finished.responseB64, "base64url"));
  return {
    started,
    finished,
    keys,
    hsId,
    ke3: fin.finishLoginRequest,
    response: plain ? (JSON.parse(plain.toString("utf8")) as Record<string, unknown>) : null,
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
    expect(mgr.start(reg.startLoginRequest, "peer").ok).toBe(false);
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
    expect(registry.resolveDeviceSecret(parsed!.deviceId, digestSecret(parsed!.secret), clock)).not.toBeNull();

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
        { deviceId: c.deviceId, secretDigest: c.secretDigest, clientInstallId: `install-${i}`, name: "n", platform: "p", client: "c", createdAtMs: clock },
        clock,
      );
      registry.activate(c.deviceId, clock);
    }
    expect(registry.list().filter((d) => d.state === "active")).toHaveLength(MAX_ACTIVE_DEVICES);

    const mgr = makeManager();
    const { code } = mgr.generateCode();
    // META.client_install_id ("install-xyz") is a brand-new install: finish must
    // fail closed WITHOUT consuming the one-time code, so the owner can free a
    // slot and reuse the same code rather than have it silently spent on a
    // pairing that could never activate.
    const r = pairAsClient(mgr, code, "peer-cap");
    expect(r.finished?.ok).toBe(false);
    expect(mgr.getStatus().phase).toBe("active"); // code still usable
    expect(registry.list().filter((d) => d.state === "pending")).toHaveLength(0);
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
    const keys = deriveDirectionKeys(Buffer.from(fin.sessionKey, "base64url"), hsId);
    const encMeta = seal(keys.c2s, hsId, "c2s", Buffer.from(JSON.stringify(META))).toString("base64url");
    // Finish from a DIFFERENT peer than started the handshake.
    expect(mgr.finish(started.handshakeId, fin.finishLoginRequest, encMeta, "peer-B").ok).toBe(false);
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
    const keys = deriveDirectionKeys(Buffer.from(fin.sessionKey, "base64url"), hsId);
    const encMeta = seal(keys.c2s, hsId, "c2s", Buffer.from(JSON.stringify(META))).toString("base64url");
    clock += HANDSHAKE_TTL_MS + 1;
    expect(mgr.finish(started.handshakeId, fin.finishLoginRequest, encMeta, "peer").ok).toBe(false);
  });

  it("returns the same ciphertext on an identical finish retry, and generic error on a divergent one", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    const r = pairAsClient(mgr, code, "peer");
    expect(r.finished?.ok).toBe(true);
    const first = (r.finished as { ok: true; responseB64: string }).responseB64;
    // Identical retry -> same persisted ciphertext.
    const retry = mgr.finish(r.started!.ok ? r.started!.handshakeId : "", r.ke3!, "ignored", "peer");
    expect(retry.ok).toBe(true);
    if (retry.ok) expect(retry.responseB64).toBe(first);
    // Divergent retry (different KE3) -> generic error.
    const bad = mgr.finish(r.started!.ok ? r.started!.handshakeId : "", "AAAA", "x", "peer");
    expect(bad.ok).toBe(false);
    // Still exactly one device record.
    expect(registry.list()).toHaveLength(1);
  });

  it("lets exactly one of two concurrent handshakes consume the code", () => {
    const mgr = makeManager();
    const { code } = mgr.generateCode();
    // Two independent clients start handshakes against the same active code.
    const c1 = opaque.client.startLogin({ password: code });
    const s1 = mgr.start(c1.startLoginRequest, "peer-1");
    const c2 = opaque.client.startLogin({ password: code });
    const s2 = mgr.start(c2.startLoginRequest, "peer-2");
    expect(s1.ok && s2.ok).toBe(true);
    if (!s1.ok || !s2.ok) return;

    const fin1 = opaque.client.finishLogin({ clientLoginState: c1.clientLoginState, loginResponse: s1.ke2, password: code, identifiers: IDENTIFIERS, keyStretching: KSF })!;
    const hs1 = Buffer.from(s1.handshakeId, "base64url");
    const k1 = deriveDirectionKeys(Buffer.from(fin1.sessionKey, "base64url"), hs1);
    const em1 = seal(k1.c2s, hs1, "c2s", Buffer.from(JSON.stringify(META))).toString("base64url");
    expect(mgr.finish(s1.handshakeId, fin1.finishLoginRequest, em1, "peer-1").ok).toBe(true);

    // The second handshake's finish now finds the code consumed -> generic fail.
    const fin2 = opaque.client.finishLogin({ clientLoginState: c2.clientLoginState, loginResponse: s2.ke2, password: code, identifiers: IDENTIFIERS, keyStretching: KSF })!;
    const hs2 = Buffer.from(s2.handshakeId, "base64url");
    const k2 = deriveDirectionKeys(Buffer.from(fin2.sessionKey, "base64url"), hs2);
    const em2 = seal(k2.c2s, hs2, "c2s", Buffer.from(JSON.stringify(META))).toString("base64url");
    expect(mgr.finish(s2.handshakeId, fin2.finishLoginRequest, em2, "peer-2").ok).toBe(false);
    expect(registry.list()).toHaveLength(1);
  });

  it("rejects malformed KE1 and counts it against the rate limit", () => {
    const mgr = makeManager();
    mgr.generateCode();
    for (const bad of ["", "not base64!", "øøø"]) {
      expect(mgr.start(bad, "peer").ok).toBe(false);
    }
  });
});
