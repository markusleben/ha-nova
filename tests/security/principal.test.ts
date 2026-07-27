import { mkdtempSync, rmSync } from "node:fs";
import type { IncomingMessage } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  digestSecret,
  generateCredential,
} from "../../nova/src/security/device-credential.js";
import {
  openDeviceRegistry,
  type DeviceRegistry,
} from "../../nova/src/security/device-registry.js";
import {
  CLOUD_DEVICE_UNAUTHORIZED_MESSAGE,
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

  it("resolves the legacy shared token on either transport while it exists", () => {
    const legacy = "legacy-shared-token-value";
    registry.importLegacy(digestSecret(legacy), now());
    expect(resolvePrincipal(auth(legacy), "plain", deps())).toEqual({
      ok: true,
      principal: { kind: "legacy" },
    });
    expect(resolvePrincipal(auth(legacy), "secure", deps())).toEqual({
      ok: true,
      principal: { kind: "legacy" },
    });
    // After revoke it no longer resolves.
    registry.revokeLegacy();
    expect(resolvePrincipal(auth(legacy), "plain", deps()).ok).toBe(false);
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

  it("resolves Cloud auth only for the bound HA user and never accepts legacy", () => {
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

    registry.importLegacy(digestSecret("legacy"), now());
    expect(
      resolveCloudPrincipal(request("legacy"), "user-1", RELAY_ID, deps()).ok,
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
