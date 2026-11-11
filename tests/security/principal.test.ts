import { mkdtempSync, rmSync } from "node:fs";
import type { IncomingMessage } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { generateCredential } from "../../nova/src/security/device-credential.js";
import {
  openDeviceRegistry,
  type DeviceRegistry,
} from "../../nova/src/security/device-registry.js";
import { BRIDGE_INSTALL_ID, GATE_INSTALL_ID } from "../../nova/src/security/pairing-metadata.js";
import {
  CLOUD_DEVICE_UNAUTHORIZED_MESSAGE,
  PAIRING_REQUIRED_MESSAGE,
  resolveCloudPrincipal,
  resolvePrincipal,
} from "../../nova/src/security/principal.js";

let dir: string;
let registry: DeviceRegistry;
const now = () => 1000;
const deps = () => ({ registry, now });
const auth = (t: string) => `Bearer ${t}`;
const RELAY_ID = "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA";
const OTHER_RELAY_ID = "hanova-relay-v1.BBBBBBBBBBBBBBBBBBBBBB";

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-principal-"));
  registry = openDeviceRegistry(dir);
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

function activeDevice() {
  const c = generateCredential();
  registry.createPending(
    {
      deviceId: c.deviceId,
      secretDigest: c.secretDigest,
      clientInstallId: "i",
      name: "n",
      platform: "p",
      client: "c",
      createdAtMs: 1,
    },
    now(),
  );
  registry.activate(c.deviceId, now());
  return c;
}

describe("principal resolver", () => {
  it("resolves an active device credential over the secure transport", () => {
    const c = activeDevice();
    const r = resolvePrincipal(auth(c.credential), "secure", deps());
    expect(r).toEqual({
      ok: true,
      principal: { kind: "device", deviceId: c.deviceId },
    });
  });

  it("refuses a device credential over plain HTTP with SECURE_TRANSPORT_REQUIRED", () => {
    const c = activeDevice();
    const r = resolvePrincipal(auth(c.credential), "plain", deps());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(403);
  });

  it("rejects a revoked device credential (401)", () => {
    const c = activeDevice();
    registry.revoke(c.deviceId);
    const r = resolvePrincipal(auth(c.credential), "secure", deps());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(401);
  });

  it("answers PAIRING_REQUIRED to the old shared token on either transport", () => {
    const shared = "old-shared-token-value";
    for (const transport of ["plain", "secure"] as const) {
      expect(resolvePrincipal(auth(shared), transport, deps())).toEqual({
        ok: false,
        status: 401,
        code: "PAIRING_REQUIRED",
        message: PAIRING_REQUIRED_MESSAGE,
      });
    }
  });

  it("rejects missing/malformed authorization (401)", () => {
    for (const h of [
      undefined,
      "",
      "Basic xyz",
      "Bearer",
      "Bearer ",
      "Bearer token trailing",
    ]) {
      const r = resolvePrincipal(h, "secure", deps());
      expect(r.ok).toBe(false);
      if (!r.ok) expect(r.status).toBe(401);
    }
  });

  it("rejects an unknown non-device token (401)", () => {
    const r = resolvePrincipal(auth("some-random-token"), "secure", deps());
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.status).toBe(401);
  });

  it("accepts the App's own credentials from loopback only, on no other route", () => {
    for (const installId of [BRIDGE_INSTALL_ID, GATE_INSTALL_ID]) {
      const c = generateCredential();
      registry.createPending({ deviceId: c.deviceId, secretDigest: c.secretDigest, clientInstallId: installId, name: "n", platform: "p", client: "c", createdAtMs: 1 }, now());
      registry.activate(c.deviceId, now());
      const internal = installId === BRIDGE_INSTALL_ID ? "chat-bridge" : "gate";
      expect(resolvePrincipal(auth(c.credential), "secure", deps(), "127.0.0.1")).toEqual({ ok: true, principal: { kind: "device", deviceId: c.deviceId, peer: "127.0.0.1", internal } });
      expect(resolvePrincipal(auth(c.credential), "secure", deps(), "192.168.1.20")).toMatchObject({ ok: false, status: 401 });
      expect(registry.bindCloudUser(c.deviceId, c.secretDigest, "user-1", RELAY_ID).ok).toBe(true);
      const request = { headers: { authorization: auth(c.credential) }, rawHeaders: ["Authorization", auth(c.credential)], socket: { remoteAddress: "172.30.32.2" } } as unknown as IncomingMessage;
      expect(resolveCloudPrincipal(request, "user-1", RELAY_ID, deps())).toMatchObject({ ok: false, status: 401 });
    }
  });

  it("resolves Cloud auth only for the bound HA user and never accepts the old shared token", () => {
    const credential = activeDevice();
    registry.bindCloudUser(
      credential.deviceId,
      credential.secretDigest,
      "user-1",
      RELAY_ID,
    );
    const request = (token: string, rawHeaders?: string[]) =>
      ({
        headers: { authorization: auth(token) },
        rawHeaders: rawHeaders ?? ["Authorization", auth(token)],
      }) as unknown as IncomingMessage;

    expect(
      resolveCloudPrincipal(
        request(credential.credential),
        "user-1",
        RELAY_ID,
        deps(),
      ),
    ).toEqual({
      ok: true,
      principal: { kind: "device", deviceId: credential.deviceId },
    });
    expect(
      resolveCloudPrincipal(
        request(credential.credential),
        "user-2",
        RELAY_ID,
        deps(),
      ).ok,
    ).toBe(false);
    expect(
      resolveCloudPrincipal(
        request(credential.credential),
        "user-1",
        OTHER_RELAY_ID,
        deps(),
      ).ok,
    ).toBe(false);

    expect(
      resolveCloudPrincipal(request("old-shared-token"), "user-1", RELAY_ID, deps()).ok,
    ).toBe(false);
  });

  it("rejects duplicate Authorization headers for Cloud auth", () => {
    const credential = activeDevice();
    registry.bindCloudUser(
      credential.deviceId,
      credential.secretDigest,
      "user-1",
      RELAY_ID,
    );
    const request = {
      headers: { authorization: auth(credential.credential) },
      rawHeaders: [
        "Authorization",
        auth(credential.credential),
        "Authorization",
        auth(credential.credential),
      ],
    } as unknown as IncomingMessage;
    expect(resolveCloudPrincipal(request, "user-1", RELAY_ID, deps()).ok).toBe(
      false,
    );

    const parsedWithoutRawHeader = {
      headers: { authorization: auth(credential.credential) },
      rawHeaders: [],
    } as unknown as IncomingMessage;
    expect(
      resolveCloudPrincipal(parsedWithoutRawHeader, "user-1", RELAY_ID, deps())
        .ok,
    ).toBe(false);
  });

  it("keeps every Cloud device rejection indistinguishable", () => {
    const credential = activeDevice();
    registry.bindCloudUser(
      credential.deviceId,
      credential.secretDigest,
      "user-1",
      RELAY_ID,
    );
    const expected = {
      ok: false,
      status: 401,
      code: "UNAUTHORIZED",
      message: CLOUD_DEVICE_UNAUTHORIZED_MESSAGE,
    };
    const request = (authorization: string | undefined, rawHeaders: string[]) =>
      ({
        headers: authorization === undefined ? {} : { authorization },
        rawHeaders,
      }) as unknown as IncomingMessage;
    const wrongSecret = generateCredential().credential.split(".")[2]!;
    const parts = credential.credential.split(".");
    const invalidCredential = `${parts[0]}.${parts[1]}.${wrongSecret}`;

    const rejected = [
      resolveCloudPrincipal(request(undefined, []), "user-1", RELAY_ID, deps()),
      resolveCloudPrincipal(
        request("Basic malformed", ["Authorization", "Basic malformed"]),
        "user-1",
        RELAY_ID,
        deps(),
      ),
      resolveCloudPrincipal(
        request(auth(credential.credential), [
          "Authorization",
          auth(credential.credential),
          "Authorization",
          auth(credential.credential),
        ]),
        "user-1",
        RELAY_ID,
        deps(),
      ),
      resolveCloudPrincipal(
        request(auth(invalidCredential), [
          "Authorization",
          auth(invalidCredential),
        ]),
        "user-1",
        RELAY_ID,
        deps(),
      ),
      resolveCloudPrincipal(
        request(auth(credential.credential), [
          "Authorization",
          auth(credential.credential),
        ]),
        "user-2",
        RELAY_ID,
        deps(),
      ),
      resolveCloudPrincipal(
        request(auth(credential.credential), [
          "Authorization",
          auth(credential.credential),
        ]),
        "user-1",
        OTHER_RELAY_ID,
        deps(),
      ),
    ];
    registry.revoke(credential.deviceId);
    rejected.push(
      resolveCloudPrincipal(
        request(auth(credential.credential), [
          "Authorization",
          auth(credential.credential),
        ]),
        "user-1",
        RELAY_ID,
        deps(),
      ),
    );

    for (const result of rejected) {
      expect(result).toEqual(expected);
    }
  });
});
