// The NOVA page for Home Assistant administrators
// (docs/work/2026-11-12-nova-page-admins-spec.md): an administrator opens it,
// connects and revokes computers, and approves; chat apps and the registry
// reset stay with the owner; anyone else, or a role HA NOVA can't read, is
// refused before anything else is read.
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { IncomingMessage, ServerResponse } from "node:http";

import { afterEach, beforeAll, beforeEach, describe, expect, it } from "vitest";

import { createNovaActionHandler, createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import type { ChatApps } from "../../nova/src/runtime/chat-apps.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import { opaqueReady } from "../../nova/src/security/opaque-server.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";
import { BRIDGE_INSTALL_ID } from "../../nova/src/security/pairing-metadata.js";
import { createPairingV1Manager, type PairingV1Manager } from "../../nova/src/security/pairing-v1.js";

beforeAll(async () => {
  await opaqueReady();
});

const OWNER: HaAuthUser = { id: "owner-1", name: "Markus", is_owner: true, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const ADMIN: HaAuthUser = { id: "admin-1", name: "Anna", is_owner: false, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const USER: HaAuthUser = { id: "user-1", name: "Kim", is_owner: false, is_active: true, system_generated: false, group_ids: ["system-users"] };
const now = () => 1000;

let dir: string;
let registry: DeviceRegistry;
let pairing: PairingV1Manager;
let csrf: ReturnType<typeof createCsrfStore>;
let chatCalls: string[];
let users: HaAuthUser[];
let deps: NovaPageDeps;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-novapage-admins-"));
  registry = openDeviceRegistry(dir);
  pairing = createPairingV1Manager({ registry, secureEndpoint: () => ({ spkiPin: "p", securePort: 8792 }), now });
  csrf = createCsrfStore();
  chatCalls = [];
  users = [OWNER, ADMIN, USER];
  // Every chat-app call is recorded: an administrator's page and actions make none.
  const chatApps = new Proxy({}, {
    get: (_, name) => (...args: unknown[]) => {
      chatCalls.push(String(name));
      if (name === "bridgeDeviceId") return null;
      throw new Error(String(name) + " " + JSON.stringify(args));
    },
  }) as unknown as ChatApps;
  deps = {
    fetchAuthUsers: async () => users,
    csrf,
    pairing,
    registry,
    connection: () => ({ haConnected: true }),
    update: async () => ({ version: "0.7.0", versionLatest: "0.7.0", updateAvailable: false, error: false }),
    relayVersion: "0.7.0",
    now,
    chatApps,
    logger: { info: () => undefined, warn: () => undefined, error: () => undefined, debug: () => undefined } as unknown as NonNullable<NovaPageDeps["logger"]>,
  };
});
afterEach(() => rmSync(dir, { recursive: true, force: true }));

function req(userId: string, url = "/"): IncomingMessage {
  const headers = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": userId };
  return { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage;
}

async function call(handler: ReturnType<typeof createNovaPageHandler>, request: IncomingMessage, body: unknown = null) {
  const r = { statusCode: 0, headers: {} as Record<string, unknown>, body: "", setHeader(k: string, v: unknown) { this.headers[k.toLowerCase()] = v; }, end(b?: string) { if (b) this.body = b; } };
  await handler({ request, response: r as unknown as ServerResponse, path: "/home", body });
  return r;
}

const page = (userId: string, url = "/") => call(createNovaPageHandler(deps), req(userId, url));
const act = (userId: string, form: Record<string, string>) => call(createNovaActionHandler(deps), req(userId), form);
const device = (installId: string, name: string) => {
  const { deviceId, secretDigest } = generateCredential();
  registry.createPending({ deviceId, secretDigest, clientInstallId: installId, name, platform: "macOS", client: "ha-nova", createdAtMs: now() }, now());
  registry.activate(deviceId, now());
  return deviceId;
};

describe("NOVA page: administrators", () => {
  it("opens for an administrator without chat apps or the reset", async () => {
    const r = await page(ADMIN.id);
    expect(r.statusCode).toBe(200);
    expect(r.body).toContain("Connect a device");
    expect(r.body).toContain("Chat apps and their settings: only the owner, Markus, can change them.");
    expect(r.body).toContain("Signed in as Anna");
    expect(r.body).not.toContain("Turn on chat apps");
    expect(chatCalls).toEqual(["bridgeDeviceId"]); // no chat-app file is read
  });

  it("lets an administrator connect a computer and revoke one", async () => {
    const r = await page(ADMIN.id);
    const token = /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="generate_code"/.exec(r.body)?.[1];
    expect(token).toBeTruthy();
    expect((await act(ADMIN.id, { action: "generate_code", csrf: token! })).statusCode).toBe(303);
    expect(pairing.getStatus().phase).toBe("active");
    const laptop = device("install-laptop", "Owner's laptop");
    const confirm = await page(ADMIN.id, "/?confirm=revoke_device&device=" + laptop);
    const revoke = /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="revoke_device"/.exec(confirm.body)?.[1];
    expect((await act(ADMIN.id, { action: "revoke_device", csrf: revoke!, device_id: laptop })).statusCode).toBe(303);
    expect(registry.list().some((d) => d.deviceId === laptop)).toBe(false);
  });

  it("refuses every owner action from an administrator, before the token is spent", async () => {
    for (const action of ["reset_registry", "chat_apps_on", "chat_apps_off", "chat_read_only", "chat_codes", "chat_users", "chat_phone_remove"]) {
      const token = csrf.issue(ADMIN.id, action, now());
      const r = await act(ADMIN.id, { action, csrf: token, value: "on", confirm_text: "RESET" });
      expect(r.statusCode, action).toBe(403);
      expect(r.body).toContain("Only the owner, Markus, can do this. Nothing was changed.");
      expect(csrf.consume(ADMIN.id, action, token, now()), action).toBe(true); // still unspent: nothing ran
    }
    expect(chatCalls).toEqual([]);
    expect((await page(ADMIN.id, "/?confirm=reset_registry")).body).not.toContain("RESET");
  });

  it("never revokes the App's own connections, for the owner either", async () => {
    const bridge = device(BRIDGE_INSTALL_ID, "Chat apps");
    const token = csrf.issue(OWNER.id, "revoke_device:" + bridge, now());
    const r = await act(OWNER.id, { action: "revoke_device", csrf: token, device_id: bridge });
    expect(r.headers.location).toContain("err=1");
    expect(registry.list().some((d) => d.deviceId === bridge)).toBe(true);
  });

  it("refuses anyone else, and a role it can't read", async () => {
    const refused: HaAuthUser[] = [
      USER,
      { ...USER, id: "ro-1", group_ids: ["system-read-only"] },
      { ...ADMIN, id: "a-inactive", is_active: false },
      { id: "a-unknown-active", name: "Anna", is_owner: false, system_generated: false, group_ids: ["system-admin"] },
      { ...ADMIN, id: "a-system", system_generated: true },
      { id: "a-no-groups", name: "Anna", is_owner: false, is_active: true, system_generated: false },
      { ...ADMIN, id: "a-string-groups", group_ids: "system-admin" as unknown as string[] },
      { id: "o-unknown-active", name: "Markus", is_owner: true, system_generated: false },
    ];
    users = [OWNER, ...refused];
    for (const who of [...refused.map((u) => u.id), "not-in-the-list"]) {
      for (const r of [await page(who), await act(who, { action: "generate_code", csrf: csrf.issue(who, "generate_code", now()) })]) {
        expect(r.statusCode, who).toBe(403);
        expect(r.body).toContain("Only Home Assistant administrators can open NOVA. Ask the owner, Markus, or another administrator to open it");
        expect(r.body).not.toContain("<form");
      }
    }
    expect(pairing.getStatus().phase).not.toBe("active");
    expect(chatCalls).toEqual([]);
  });

  it("answers 503 when the users can't be read, and keeps a hostile owner name inert", async () => {
    deps.fetchAuthUsers = async () => { throw new Error("ws down"); };
    expect((await page(ADMIN.id)).statusCode).toBe(503);
    deps.fetchAuthUsers = async () => [{ ...OWNER, name: '<img src=x onerror="alert(1)">' }, USER];
    const r = await page(USER.id);
    expect(r.body).toContain("Ask the owner, &lt;img src=x onerror=&quot;alert(1)&quot;&gt;, or another administrator");
    expect(r.body).not.toContain("<img");
  });

  it("says neutrally when it can't tell who opened it", async () => {
    const request = { ...req(ADMIN.id), socket: { remoteAddress: "192.168.1.20" } } as unknown as IncomingMessage;
    const r = await call(createNovaPageHandler(deps), request);
    expect(r.statusCode).toBe(403);
    expect(r.body).toContain("Couldn&#39;t tell who you are. Open NOVA from the Home Assistant sidebar.");
    expect(r.body).not.toContain("Markus");
  });
});
