import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { SupervisorClient } from "../../nova/src/ha/supervisor-client.js";
import { createNovaActionHandler, createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import { ChatAppsError, chatConnectorUrl, claudeConnectorLink, type ChatApps, type ChatAppsStatus } from "../../nova/src/runtime/chat-apps.js";
import { createIngressListener, type IngressListenerDeps } from "../../nova/src/runtime/ingress-listener.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";
import { createPairingV1Manager } from "../../nova/src/security/pairing-v1.js";

// The owner-only chat-app action: GET/POST /chat-apps/v1/state over ingress
// (the CLI, with the owner's session) and the NOVA page's Chat apps section.

const OWNER: HaAuthUser = { id: "owner-1", name: "Owner", is_owner: true, is_active: true };
const ADMIN: HaAuthUser = { id: "admin-1", name: "Admin", is_owner: false, is_active: true };

let dir: string;
let registry: DeviceRegistry;
let users: () => Promise<HaAuthUser[]>;
let calls: string[];
let turnOnResult: () => Promise<{ restart: boolean }>;
let bridgeId: string | null;
let servers: Server[];

const status: ChatAppsStatus = { available: true, enabled: false, unfinished: false, readOnly: false, chatCodes: true, allowedUsers: null, ownerPhone: null };

function fakeChatApps(): ChatApps {
  return {
    status: () => ({ ...status }),
    turnOn: async (allow) => {
      calls.push(`on:${allow}`);
      return await turnOnResult();
    },
    turnOff: async () => void calls.push("off"),
    setSwitches: (change) => void calls.push(`switches:${JSON.stringify(change)}`),
    setOwnerPhone: (owner, link) => void calls.push(`phone:${owner}:${JSON.stringify(link)}`),
    restartSoon: () => void calls.push("restart"),
    boot: async () => {},
    restartBridge: () => {},
    bridgeDeviceId: () => bridgeId,
  };
}

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-chat-api-"));
  registry = openDeviceRegistry(dir);
  users = async () => [OWNER, ADMIN];
  calls = [];
  turnOnResult = async () => ({ restart: true });
  bridgeId = null;
  servers = [];
});

afterEach(async () => {
  await Promise.all(servers.map((server) => new Promise<void>((resolve) => server.close(() => resolve()))));
  rmSync(dir, { recursive: true, force: true });
});

async function start(chatApps?: ChatApps, peer = "172.30.32.2"): Promise<string> {
  const supervisor = {} as SupervisorClient;
  const deps: IngressListenerDeps = {
    registry,
    pairing: createPairingV1Manager({ registry, secureEndpoint: () => null, now: () => 1_000 }),
    functional: { health: () => ({}), ws: () => ({}), core: () => ({}), files: () => ({}), backups: () => ({}) },
    relayInstanceId: "relay-1",
    relayVersion: "0.11.0",
    cloudRemoteEnabled: false,
    wsClient: { sendMessage: async () => await users(), isConnected: () => true } as unknown as IngressListenerDeps["wsClient"],
    supervisor,
    registryCorrupt: () => false,
    resetRegistry: () => {},
    now: () => 1_000,
    logger: { warn: () => {}, error: () => {} },
    ...(chatApps ? { chatApps } : {}),
  };
  const listener = createIngressListener(deps);
  const server = createServer((request, response) => {
    Object.defineProperty(request.socket, "remoteAddress", { configurable: true, value: peer });
    listener(request, response);
  });
  servers.push(server);
  const port = await new Promise<number>((resolve) => server.listen(0, "127.0.0.1", () => resolve((server.address() as { port: number }).port)));
  return `http://127.0.0.1:${port}`;
}

function state(base: string, user: string, body?: unknown, headers: Record<string, string> = {}): Promise<Response> {
  return fetch(`${base}/chat-apps/v1/state`, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      "x-ingress-path": "/api/hassio_ingress/session",
      "x-remote-user-id": user,
      ...(body === undefined ? {} : { "content-type": "application/json" }),
      ...headers,
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
}

async function expectError(pending: Promise<Response>, code: number, errorCode: string): Promise<void> {
  const response = await pending;
  expect(response.status).toBe(code);
  expect(((await response.json()) as { error: { code: string } }).error.code).toBe(errorCode);
}

describe("chat-app action API", () => {
  it("does not exist while the feature is off", async () => {
    const base = await start();
    expect((await state(base, "owner-1")).status).toBe(404);
    expect((await state(base, "owner-1", { enabled: true })).status).toBe(404);
  });

  it("turns on for the owner, with restart consent, and restarts only after answering", async () => {
    const base = await start(fakeChatApps());
    const response = await state(base, "owner-1", { enabled: true, allow_restart: true });
    expect(response.status).toBe(200);
    expect(response.headers.get("cache-control")).toBe("no-store");
    expect(((await response.json()) as { data: unknown }).data).toMatchObject({ enabled: false, connector_path: "/api/ha_nova/mcp", restarting: true });
    expect(calls).toEqual(["on:true", "restart"]);

    const view = await state(base, "owner-1");
    expect(((await view.json()) as { data: unknown }).data).toMatchObject({ available: true, chat_codes: true, allowed_users: null, restarting: false });
  });

  it("refuses everyone but the owner, browsers, non-JSON, and non-ingress callers", async () => {
    const base = await start(fakeChatApps());
    await expectError(state(base, "admin-1", { enabled: true }), 403, "OWNER_REQUIRED");
    await expectError(state(base, "admin-1"), 403, "OWNER_REQUIRED");
    await expectError(state(base, "unknown", { enabled: false }), 403, "OWNER_REQUIRED");
    users = async () => [{ ...OWNER, is_active: false }];
    await expectError(state(base, "owner-1", { enabled: true }), 403, "OWNER_REQUIRED");
    users = async () => {
      throw new Error("Supervisor unavailable");
    };
    await expectError(state(base, "owner-1", { enabled: true }), 503, "OWNER_CHECK_UNAVAILABLE");
    users = async () => [OWNER];
    await expectError(state(base, "owner-1", { enabled: true }, { "sec-fetch-site": "same-origin" }), 403, "MACHINE_REQUEST_REQUIRED");
    await expectError(state(base, "owner-1", { enabled: true }, { "content-type": "text/plain" }), 415, "UNSUPPORTED_MEDIA_TYPE");
    await expectError(state(await start(fakeChatApps(), "172.30.32.9"), "owner-1", { enabled: true }), 403, "INGRESS_REQUIRED");
    expect(calls).toEqual([]);
  });

  it("validates the body strictly before it changes anything", async () => {
    const base = await start(fakeChatApps());
    for (const body of [[], { enabled: "yes" }, { enabled: true, extra: 1 }, { read_only: 1 }, { owner_phone: { service: "notify.x", device_id: "d".repeat(32), name: "P" } }, { owner_phone: { service: "mobile_app_p", device_id: "nothex", name: "P" } }, { owner_phone: { service: "mobile_app_p", device_id: "d".repeat(32), name: "P", user_id: "admin-1" } }]) {
      await expectError(state(base, "owner-1", body), 400, "VALIDATION_ERROR");
    }
    expect(calls).toEqual([]);
  });

  it("passes consent and plain refusals through, and sets switches and the owner's phone", async () => {
    const base = await start(fakeChatApps());
    turnOnResult = async () => {
      throw new ChatAppsError(409, "RESTART_CONSENT_REQUIRED", "Home Assistant restarts once (about a minute). Ask the owner first.");
    };
    await expectError(state(base, "owner-1", { enabled: true, read_only: true }), 409, "RESTART_CONSENT_REQUIRED");
    expect(calls).toEqual(["on:false"]); // nothing else changed
    calls = [];
    const ok = await state(base, "owner-1", { read_only: true, chat_codes: false, owner_phone: { service: "mobile_app_pixel", device_id: "d".repeat(32), name: "Pixel" } });
    expect(ok.status).toBe(200);
    expect(calls).toEqual(['switches:{"readOnly":true,"chatCodes":false}', `phone:owner-1:{"service":"mobile_app_pixel","device_id":"${"d".repeat(32)}","name":"Pixel"}`]);
    calls = [];
    await state(base, "owner-1", { enabled: false, owner_phone: null });
    expect(calls).toEqual(["off", "phone:owner-1:null"]);
  });
});

describe("NOVA page: Chat apps", () => {
  const csrf = createCsrfStore();
  function pageDeps(chatApps?: ChatApps): NovaPageDeps {
    return {
      fetchAuthUsers: async () => await users(),
      csrf,
      pairing: createPairingV1Manager({ registry, secureEndpoint: () => null, now: () => 1_000 }),
      registry,
      connection: () => ({ haConnected: true }),
      update: async () => ({ version: "0.11.0", versionLatest: "0.11.0", updateAvailable: false, error: false }),
      relayVersion: "0.11.0",
      now: () => 1_000,
      ...(chatApps ? { chatApps } : {}),
    };
  }
  function request(user: string, url = "/"): IncomingMessage {
    const headers = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": user };
    return { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage;
  }
  async function run(handler: ReturnType<typeof createNovaPageHandler>, req: IncomingMessage, body: unknown = null) {
    const out = { statusCode: 0, headers: {} as Record<string, unknown>, body: "", setHeader(k: string, v: unknown) { this.headers[k] = v; }, end(b?: string) { if (b) this.body = b; } };
    await handler({ request: req, response: out as unknown as ServerResponse, path: "/home", body });
    return out;
  }

  it("shows nothing while the feature is off and refuses its actions", async () => {
    const page = await run(createNovaPageHandler(pageDeps()), request("owner-1"));
    expect(page.body).not.toContain("Chat apps");
    const token = csrf.issue("owner-1", "chat_apps_on", 1_000);
    const action = await run(createNovaActionHandler(pageDeps()), request("owner-1"), { action: "chat_apps_on", csrf: token });
    expect(action.statusCode).toBe(400);
  });

  it("turns on with the restart named on the button, then restarts after the redirect", async () => {
    const page = await run(createNovaPageHandler(pageDeps(fakeChatApps())), request("owner-1"));
    expect(page.body).toContain("Home Assistant restarts once (about a minute).");
    const token = /name="csrf" value="([^"]+)"><input type="hidden" name="action" value="chat_apps_on">/.exec(page.body)?.[1];
    expect(token).toBeDefined();
    const action = await run(createNovaActionHandler(pageDeps(fakeChatApps())), request("owner-1"), { action: "chat_apps_on", csrf: token });
    expect(action.statusCode).toBe(303);
    expect(action.headers.location).toBe("/api/hassio_ingress/tok/?chat=restarting");
    expect(calls).toEqual(["on:true", "restart"]);
  });

  it("needs the owner and a fresh form token", async () => {
    users = async () => [{ ...OWNER, is_owner: false }];
    const denied = await run(createNovaActionHandler(pageDeps(fakeChatApps())), request("owner-1"), { action: "chat_apps_off", csrf: csrf.issue("owner-1", "chat_apps_off", 1_000) });
    expect(denied.statusCode).toBe(403);
    users = async () => [OWNER];
    const forged = await run(createNovaActionHandler(pageDeps(fakeChatApps())), request("owner-1"), { action: "chat_apps_off", csrf: csrf.issue("owner-1", "chat_apps_on", 1_000) });
    expect(forged.statusCode).toBe(403);
    expect(calls).toEqual([]);
  });

  it("offers only the way off when this App version can't run chat apps that are on", async () => {
    const stale = { ...fakeChatApps(), status: () => ({ ...status, available: false, enabled: true }) };
    const page = await run(createNovaPageHandler(pageDeps(stale)), request("owner-1"));
    expect(page.body).toContain("can't run chat apps");
    expect(page.body).toContain('value="chat_apps_off"');
    expect(page.body).not.toContain('value="chat_apps_on"');
  });

  it("refuses a switch form without on or off", async () => {
    const action = await run(createNovaActionHandler(pageDeps(fakeChatApps())), request("owner-1"), { action: "chat_read_only", csrf: csrf.issue("owner-1", "chat_read_only", 1_000), value: "maybe" });
    expect(action.headers.location).toBe("/api/hassio_ingress/tok/?err=chat");
    expect(calls).toEqual([]);
  });

  it("names a configuration error plainly", async () => {
    turnOnResult = async () => {
      throw new ChatAppsError(409, "HA_CONFIG_INVALID", "x");
    };
    const action = await run(createNovaActionHandler(pageDeps(fakeChatApps())), request("owner-1"), { action: "chat_apps_on", csrf: csrf.issue("owner-1", "chat_apps_on", 1_000) });
    expect(action.headers.location).toBe("/api/hassio_ingress/tok/?err=chat_config");
    const page = await run(createNovaPageHandler(pageDeps(fakeChatApps())), request("owner-1", "/?err=chat_config"));
    expect(page.body).toContain("configuration has an error");
  });

  it("keeps the console working when the chat-app files are unreadable", async () => {
    const broken = { ...fakeChatApps(), status: () => { throw new Error("chat-apps.json: unexpected end"); } };
    const page = await run(createNovaPageHandler(pageDeps(broken)), request("owner-1"));
    expect(page.statusCode).toBe(200);
    expect(page.body).toContain("Connect a device");
    expect(page.body).not.toContain("<h2>Chat apps</h2>");
  });

  it("never lists the bridge's internal device as a computer to revoke", async () => {
    for (const name of ["Laptop", "Bridge"]) {
      const minted = generateCredential();
      registry.createPending({ deviceId: minted.deviceId, secretDigest: minted.secretDigest, clientInstallId: name, name, platform: "p", client: "c", createdAtMs: 1 }, 1_000);
      registry.activate(minted.deviceId, 1_000);
      bridgeId = minted.deviceId;
    }
    const page = await run(createNovaPageHandler(pageDeps(fakeChatApps())), request("owner-1"));
    expect(page.body).toContain("Laptop");
    expect(page.body).not.toContain("Bridge");
    const armed = await run(createNovaPageHandler(pageDeps(fakeChatApps())), request("owner-1", `/?confirm=revoke_device&device=${encodeURIComponent(bridgeId!)}`));
    expect(armed.body).not.toContain("Confirm revoke");
  });

  it("shows Add to Claude with this home's address once on, and the plain wording when Link can't name it", async () => {
    const on = { ...fakeChatApps(), status: () => ({ ...status, enabled: true }) };
    const url = "https://abc123.ui.nabu.casa/api/ha_nova/mcp";
    const page = await run(createNovaPageHandler({ ...pageDeps(on), connectorUrl: async () => url }), request("owner-1"));
    const href = /<a class="linkbtn primary" href="([^"]+)" target="_blank" rel="noopener noreferrer">Add to Claude<\/a>/.exec(page.body)?.[1];
    expect(href).toBe("https://claude.ai/customize/connectors?modal=add-custom-connector&amp;connectorName=HA%20NOVA&amp;connectorUrl=https%3A%2F%2Fabc123.ui.nabu.casa%2Fapi%2Fha_nova%2Fmcp");
    expect(page.body).toContain(`<p class="address">${url}</p>`);
    expect(page.body).toContain("Adds abc123.ui.nabu.casa</span>");
    for (const connectorUrl of [async () => null, async () => { throw new Error("ws down"); }]) {
      const plain = await run(createNovaPageHandler({ ...pageDeps(on), connectorUrl }), request("owner-1"));
      expect(plain.statusCode).toBe(200);
      expect(plain.body).toContain("can't find an address Claude and ChatGPT can reach");
      expect(plain.body).not.toContain("claude.ai");
    }
    let asked = 0;
    const off = await run(createNovaPageHandler({ ...pageDeps(fakeChatApps()), connectorUrl: async () => (asked++, url) }), request("owner-1"));
    expect(off.body).not.toContain("Add to Claude");
    expect(asked).toBe(0); // off: Home Assistant isn't asked
  });
});

describe("chat-app connector address", () => {
  const ready = { logged_in: true, remote_connected: true, remote_domain: "ABC123.ui.nabu.casa.", prefs: { remote_enabled: true } };
  const noLink = { ...ready, remote_connected: false };
  it("is Home Assistant Link's address while its remote access is on", () => {
    expect(chatConnectorUrl(ready, { external_url: "https://home.example.com" })).toBe("https://abc123.ui.nabu.casa/api/ha_nova/mcp");
    for (const change of [{ logged_in: false }, { remote_connected: false }, { prefs: { remote_enabled: false } }, { remote_domain: "" },
      { remote_domain: "evil.com/x?y" }, { remote_domain: 'a.b"><script>' }, { remote_domain: "localhost" }, { remote_domain: 7 }]) {
      expect(chatConnectorUrl({ ...ready, ...change }, null)).toBeNull();
    }
    expect(chatConnectorUrl(null, null)).toBeNull();
  });
  it("else Home Assistant's external URL, only when it is public HTTPS (vectors shared with the CLI)", () => {
    const { vectors } = JSON.parse(readFileSync(join(__dirname, "../fixtures/chat-connector-base.json"), "utf8")) as { vectors: [string, string | null][] };
    expect(vectors.length).toBeGreaterThan(10);
    for (const [external, want] of vectors) {
      expect(chatConnectorUrl(noLink, { external_url: external }), external).toBe(want === null ? null : `${want}/api/ha_nova/mcp`);
    }
    expect(chatConnectorUrl(noLink, {})).toBeNull();
    expect(chatConnectorUrl(null, { external_url: "https://home.example.com" })).toBe("https://home.example.com/api/ha_nova/mcp");
  });
  it("links to claude.ai only, with the address percent-encoded", () => {
    const link = new URL(claudeConnectorLink("https://x.ui.nabu.casa/a b?c=1&modal=evil#f"));
    expect(link.origin + link.pathname).toBe("https://claude.ai/customize/connectors");
    expect([...link.searchParams]).toEqual([["modal", "add-custom-connector"], ["connectorName", "HA NOVA"], ["connectorUrl", "https://x.ui.nabu.casa/a b?c=1&modal=evil#f"]]);
    expect(link.hash).toBe("");
  });
});
