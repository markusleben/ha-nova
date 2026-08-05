import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { IncomingMessage, ServerResponse } from "node:http";

import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

import {
  createNovaActionHandler,
  createNovaPageHandler,
  type NovaPageDeps,
} from "../../nova/src/http/handlers/nova-page.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import {
  openDeviceRegistry,
  type DeviceRegistry,
} from "../../nova/src/security/device-registry.js";
import { opaqueReady } from "../../nova/src/security/opaque-server.js";
import {
  createPairingV1Manager,
  type PairingV1Manager,
} from "../../nova/src/security/pairing-v1.js";

// generateCode() registers an OPAQUE record, which needs the WASM initialized;
// without a prior OPAQUE test in the same worker (as happens under CI test
// sharding) generateCode would fail with a raw wbindgen error.
beforeAll(async () => {
  await opaqueReady();
});
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";

const OWNER: HaAuthUser = {
  id: "owner-1",
  name: "Owner",
  is_owner: true,
  is_active: true,
  system_generated: false,
};

let dir: string;
let registry: DeviceRegistry;
let pairing: PairingV1Manager;
let csrf: ReturnType<typeof createCsrfStore>;
let deps: NovaPageDeps;
const now = () => 1000;

function req(
  over: {
    userId?: string;
    body?: Record<string, string>;
    secFetch?: string;
    owner?: HaAuthUser[];
    url?: string;
  } = {},
): IncomingMessage {
  const headers = {
    // HA sends the ingress BASE path (no page suffix); the console lives at "/".
    "x-ingress-path": "/api/hassio_ingress/tok",
    "x-remote-user-id": over.userId ?? "owner-1",
    ...(over.secFetch ? { "sec-fetch-site": over.secFetch } : {}),
  };
  return {
    url: over.url ?? "/",
    socket: { remoteAddress: "172.30.32.2" },
    headers,
    rawHeaders: Object.entries(headers).flatMap(([name, value]) => [
      name,
      value,
    ]),
  } as unknown as IncomingMessage;
}

interface FakeRes {
  statusCode: number;
  headers: Record<string, string | number>;
  body: string;
  setHeader(k: string, v: string | number): void;
  end(b?: string): void;
}
function res(): FakeRes & ServerResponse {
  const r: FakeRes = {
    statusCode: 0,
    headers: {},
    body: "",
    setHeader(k, v) {
      this.headers[k.toLowerCase()] = v;
    },
    end(b) {
      if (b) this.body = b;
    },
  };
  return r as unknown as FakeRes & ServerResponse;
}

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-novapage-"));
  registry = openDeviceRegistry(dir);
  pairing = createPairingV1Manager({
    registry,
    secureEndpoint: () => ({ spkiPin: "p", securePort: 8792 }),
    now,
  });
  csrf = createCsrfStore();
  deps = {
    fetchAuthUsers: async () => [OWNER],
    csrf,
    pairing,
    registry,
    connection: () => ({ haConnected: true }),
    update: async () => ({
      version: "0.7.0",
      versionLatest: "0.7.0",
      updateAvailable: false,
      error: false,
    }),
    relayVersion: "0.7.0",
    now,
  };
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

async function call(
  handler: ReturnType<typeof createNovaPageHandler>,
  request: IncomingMessage,
  body: unknown = null,
) {
  const r = res();
  await handler({ request, response: r, path: "/home", body });
  return r as unknown as FakeRes;
}

describe("nova-page", () => {
  it("renders the owner console with a generate-code form", async () => {
    const r = await call(createNovaPageHandler(deps), req());
    expect(r.statusCode).toBe(200);
    expect(r.headers["content-security-policy"]).toContain(
      "form-action 'self'",
    );
    expect(r.headers["cache-control"]).toBe("no-store");
    expect(r.body).toContain("Connect a device");
    expect(r.body).toContain('name="csrf"');
  });

  it("auto-refreshes only while a code is active, and never uses JavaScript", async () => {
    // Inactive: no auto-refresh.
    let r = await call(createNovaPageHandler(deps), req());
    expect(r.body).not.toContain('http-equiv="refresh"');
    // Active: a meta refresh appears so the device shows up without a manual reload.
    pairing.generateCode();
    r = await call(createNovaPageHandler(deps), req());
    expect(r.body).toContain('http-equiv="refresh"');
    // Still no JavaScript anywhere (CSP forbids it; refresh is pure HTML).
    expect(r.body).not.toContain("<script");
  });

  it("denies a non-owner (403) and never renders the console", async () => {
    deps.fetchAuthUsers = async () => [{ ...OWNER, is_owner: false }];
    const r = await call(createNovaPageHandler(deps), req());
    expect(r.statusCode).toBe(403);
    expect(r.body).not.toContain("Connect a device");
  });

  it("generates a code via a valid CSRF form (PRG redirect)", async () => {
    const token = csrf.issue("owner-1", "generate_code", now());
    const r = await call(createNovaActionHandler(deps), req(), {
      action: "generate_code",
      csrf: token,
    });
    expect(r.statusCode).toBe(303);
    // The PRG target must keep the ingress base path's trailing slash, or HA 404s it.
    expect(r.headers["location"]).toBe("/api/hassio_ingress/tok/");
    expect(pairing.getStatus().phase).toBe("active");
  });

  it("surfaces a failed owner action via ?err=1 instead of a silent reload", async () => {
    // No mapped secure port -> generateCode throws (the manager rejects it).
    const noEndpoint = createPairingV1Manager({
      registry,
      secureEndpoint: () => null,
      now,
    });
    const token = csrf.issue("owner-1", "generate_code", now());
    const r = await call(
      createNovaActionHandler({ ...deps, pairing: noEndpoint }),
      req(),
      { action: "generate_code", csrf: token },
    );
    expect(r.statusCode).toBe(303);
    expect(String(r.headers["location"])).toBe(
      "/api/hassio_ingress/tok/?err=1",
    );
    expect(noEndpoint.getStatus().phase).toBe("inactive");
  });

  it("renders an error notice when redirected back with ?err=1", async () => {
    const r = await call(
      createNovaPageHandler(deps),
      req({ url: "/home/?err=1" }),
    );
    expect(r.statusCode).toBe(200);
    expect(r.body).toContain("That did not work");
  });

  it("refuses a cross-site POST", async () => {
    const token = csrf.issue("owner-1", "generate_code", now());
    const r = await call(
      createNovaActionHandler(deps),
      req({ secFetch: "cross-site" }),
      { action: "generate_code", csrf: token },
    );
    expect(r.statusCode).toBe(403);
    expect(pairing.getStatus().phase).toBe("inactive");
  });

  it("rejects a missing/invalid CSRF token", async () => {
    const r = await call(createNovaActionHandler(deps), req(), {
      action: "generate_code",
      csrf: "bogus",
    });
    expect(r.statusCode).toBe(403);
    expect(pairing.getStatus().phase).toBe("inactive");
  });

  it("rejects a token replayed for a different action", async () => {
    const token = csrf.issue("owner-1", "generate_code", now());
    const r = await call(createNovaActionHandler(deps), req(), {
      action: "cancel_code",
      csrf: token,
    });
    expect(r.statusCode).toBe(403);
  });

  function addActiveDevice(name = "Mac", installId = "i") {
    const c = generateCredential();
    registry.createPending(
      {
        deviceId: c.deviceId,
        secretDigest: c.secretDigest,
        clientInstallId: installId,
        name,
        platform: "darwin",
        client: "claude",
        createdAtMs: 1,
      },
      now(),
    );
    registry.activate(c.deviceId, now());
    return c;
  }

  function extractCsrf(html: string): string {
    const match = html.match(/name="csrf" value="([^"]+)"/);
    expect(match).not.toBeNull();
    return match![1]!;
  }

  it("revokes a device only through the armed confirm flow", async () => {
    const c = addActiveDevice();

    // The base render arms, it does not fire: no revoke form, no revoke token —
    // only a link to the confirm screen.
    let r = await call(createNovaPageHandler(deps), req());
    expect(r.body).not.toContain('name="action" value="revoke_device"');
    // The & is HTML-escaped inside the href attribute.
    expect(r.body).toContain(`confirm=revoke_device&amp;device=${c.deviceId}`);

    // The confirm render shows the device context and mints the only token.
    r = await call(
      createNovaPageHandler(deps),
      req({ url: `/?confirm=revoke_device&device=${c.deviceId}` }),
    );
    expect(r.body).toContain("Confirm revoke");
    expect(r.body).toContain("Mac");
    expect(r.body).toContain("added");
    expect(r.body).toContain("last used");
    const token = extractCsrf(r.body);

    const ar = await call(createNovaActionHandler(deps), req(), {
      action: "revoke_device",
      csrf: token,
      device_id: c.deviceId,
    });
    expect(ar.statusCode).toBe(303);
    expect(registry.list()).toHaveLength(0);
  });

  it("binds the armed revoke token to the exact device", async () => {
    const a = addActiveDevice("A", "ia");
    const b = addActiveDevice("B", "ib");

    const r = await call(
      createNovaPageHandler(deps),
      req({ url: `/?confirm=revoke_device&device=${a.deviceId}` }),
    );
    const tokenForA = extractCsrf(r.body);

    // A token armed for device A must not revoke device B.
    const cross = await call(createNovaActionHandler(deps), req(), {
      action: "revoke_device",
      csrf: tokenForA,
      device_id: b.deviceId,
    });
    expect(cross.statusCode).toBe(403);
    expect(registry.list()).toHaveLength(2);

    // The token still only works for its own device.
    const ok = await call(createNovaActionHandler(deps), req(), {
      action: "revoke_device",
      csrf: tokenForA,
      device_id: a.deviceId,
    });
    expect(ok.statusCode).toBe(303);
    expect(registry.list().map((d) => d.deviceId)).toEqual([b.deviceId]);
  });

  it("shows created, last used, and a cloud badge per device row", async () => {
    const c = addActiveDevice("Cloudy");
    const RELAY_ID = `hanova-relay-v1.${"a".repeat(22)}`;
    expect(
      registry.bindCloudUser(c.deviceId, c.secretDigest, "ha-user-9", RELAY_ID),
    ).toMatchObject({ ok: true });

    const r = await call(createNovaPageHandler(deps), req());
    expect(r.body).toContain("added");
    // Activation seeds last-used, so the row never claims a working device was
    // never used.
    expect(r.body).not.toContain("last used never");
    expect(r.body).toContain(`<span class="badge">cloud</span>`);

    // The confirm screen names the bound HA user for an informed decision.
    const confirm = await call(
      createNovaPageHandler(deps),
      req({ url: `/?confirm=revoke_device&device=${c.deviceId}` }),
    );
    expect(confirm.body).toContain("ha-user-9");
    expect(confirm.body).toContain("Home Assistant Cloud");
  });

  it("shows a legacy-access section only while a legacy credential exists, and revokes it through the confirm flow", async () => {
    // No legacy credential -> no section, no way to revoke something that
    // does not exist.
    let r = await call(createNovaPageHandler(deps), req());
    expect(r.body).not.toContain("Revoke legacy access");

    registry.importLegacy("legacy-digest-abc", now());
    r = await call(createNovaPageHandler(deps), req());
    expect(r.body).toContain("Revoke legacy access");
    // The base render arms only — no legacy form or token exists yet.
    expect(r.body).not.toContain('name="action" value="revoke_legacy"');
    expect(r.body).toContain("confirm=revoke_legacy");

    // Arm, then confirm with the token minted by the confirm render.
    r = await call(createNovaPageHandler(deps), req({ url: "/?confirm=revoke_legacy" }));
    expect(r.body).toContain("Confirm revoke of legacy access");
    const token = extractCsrf(r.body);
    const ar = await call(createNovaActionHandler(deps), req(), {
      action: "revoke_legacy",
      csrf: token,
    });
    expect(ar.statusCode).toBe(303);
    expect(registry.hasLegacy()).toBe(false);

    // Gone again once revoked — and the armed confirm renders nothing.
    r = await call(createNovaPageHandler(deps), req({ url: "/?confirm=revoke_legacy" }));
    expect(r.body).not.toContain("Revoke legacy access");
  });

  it("gates the registry reset behind the confirm screen plus typed RESET", async () => {
    let corrupt = true;
    let resets = 0;
    deps.registryCorrupt = () => corrupt;
    deps.resetRegistry = () => {
      resets += 1;
      corrupt = false;
    };

    let r = await call(createNovaPageHandler(deps), req());
    expect(r.body).toContain("Recovery needed");
    expect(r.body).toContain("confirm=reset_registry");
    // The base render arms only — no reset token exists yet.
    expect(r.body).not.toContain('name="action" value="reset_registry"');
    // Pairing is disabled while corrupt — no generate button.
    expect(r.body).not.toContain("Connect a device");

    // Wrong confirmation text: token is consumed, nothing is reset, and the
    // owner is told via ?err=confirm.
    r = await call(createNovaPageHandler(deps), req({ url: "/?confirm=reset_registry" }));
    expect(r.body).toContain('name="confirm_text"');
    let token = extractCsrf(r.body);
    let ar = await call(createNovaActionHandler(deps), req(), {
      action: "reset_registry",
      csrf: token,
      confirm_text: "reset",
    });
    expect(ar.statusCode).toBe(303);
    // The mismatch redirect re-arms the confirm screen so the error's "type
    // RESET" instruction has an input to point at.
    expect(String(ar.headers.location)).toContain("err=confirm");
    expect(String(ar.headers.location)).toContain("confirm=reset_registry");
    expect(resets).toBe(0);

    // The consumed token cannot be replayed even with the right text.
    ar = await call(createNovaActionHandler(deps), req(), {
      action: "reset_registry",
      csrf: token,
      confirm_text: "RESET",
    });
    expect(ar.statusCode).toBe(403);
    expect(resets).toBe(0);

    // Re-arm and confirm with the exact text.
    r = await call(createNovaPageHandler(deps), req({ url: "/?confirm=reset_registry" }));
    token = extractCsrf(r.body);
    ar = await call(createNovaActionHandler(deps), req(), {
      action: "reset_registry",
      csrf: token,
      confirm_text: "RESET",
    });
    expect(ar.statusCode).toBe(303);
    expect(resets).toBe(1);

    // After reset the recovery section is gone and pairing is back.
    r = await call(createNovaPageHandler(deps), req());
    expect(r.body).not.toContain("Reset device registry");
    expect(r.body).toContain("Connect a device");
  });

  it("mints no destructive tokens on the base render, and one bound token per confirm", async () => {
    // Many devices: the base render must not mint any revoke tokens at all
    // (arming is a link), so the CSRF cap can never evict working forms.
    for (let i = 0; i < 12; i++) {
      addActiveDevice(`d${i}`, `i${i}`);
    }
    const r = await call(createNovaPageHandler(deps), req());
    const tokens = [...r.body.matchAll(/name="csrf" value="([^"]+)"/g)].map(
      (m) => m[1],
    );
    // Only the pairing form mints a token on the base render.
    expect(tokens).toHaveLength(1);
    expect(csrf.consume("owner-1", "generate_code", tokens[0]!, now())).toBe(true);

    // A confirm render mints exactly one more, bound to that device.
    const target = registry.list()[0]!;
    const confirm = await call(
      createNovaPageHandler(deps),
      req({ url: `/?confirm=revoke_device&device=${target.deviceId}` }),
    );
    const confirmTokens = [...confirm.body.matchAll(/name="csrf" value="([^"]+)"/g)].map(
      (m) => m[1],
    );
    expect(confirmTokens).toHaveLength(2); // pairing + the bound revoke token
    const bound = confirmTokens.find(
      (t) => t !== undefined && csrf.consume("owner-1", `revoke_device:${target.deviceId}`, t, now()),
    );
    expect(bound).toBeDefined();
  });

  it("ignores a confirm request for an unknown or inactive device", async () => {
    const r = await call(
      createNovaPageHandler(deps),
      req({ url: "/?confirm=revoke_device&device=nope" }),
    );
    expect(r.body).not.toContain("Confirm revoke");
    expect(r.body).not.toContain('name="action" value="revoke_device"');
  });
});
