import type { IncomingMessage, ServerResponse } from "node:http";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createNovaActionHandler, createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import type { ChatApps, ChatAppsStatus } from "../../nova/src/runtime/chat-apps.js";
import { ChatPhoneError, type ChatPhoneChange, type ChatPhoneJob, type ChatPhones } from "../../nova/src/runtime/chat-phone-control.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";
import { createPairingV1Manager } from "../../nova/src/security/pairing-v1.js";

// The NOVA page's chat-app phone (spec docs/work/2026-10-24-chat-app-phone-move-spec.md):
// Link, Change, and Stop through the bridge's control channel. Only these
// forms send override, bound to their single-use CSRF token.

const OWNER: HaAuthUser = { id: "owner-1", name: "Owner", is_owner: true, is_active: true };
const IPHONE = "1".repeat(32);
const PIXEL = "2".repeat(32);
const JOB = "a".repeat(32);

let dir: string;
let registry: DeviceRegistry;
let csrf: ReturnType<typeof createCsrfStore>;
let phones: () => Promise<ChatPhones>;
let job: () => Promise<ChatPhoneJob>;
let start: (change: ChatPhoneChange) => Promise<string | null>;
let started: ChatPhoneChange[];
let chatCodes: boolean;

const ready = (): ChatPhones => ({ phones: [{ device_id: PIXEL, name: "Pixel", platform: "android" }], link: { device_id: IPHONE, name: "iPhone", platform: "ios", state: "ready", why: "" } });

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-chat-phone-page-"));
  registry = openDeviceRegistry(dir);
  csrf = createCsrfStore();
  phones = async () => ready();
  job = async () => ({ status: "running", step: "old", phone: "iPhone", platform: "ios", to: "Pixel" });
  start = async (change) => (change.action === "stop" && change.override ? null : JOB);
  started = [];
  chatCodes = true;
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

function chatApps(): ChatApps {
  const status: ChatAppsStatus = { available: true, enabled: true, unfinished: false, readOnly: false, chatCodes, allowedUsers: null, ownerPhone: "iPhone", ownerPhoneId: IPHONE };
  return {
    status: () => status,
    turnOn: async () => ({ restart: false }),
    turnOff: async () => {},
    setSwitches: () => {},
    phone: {
      phones: async () => await phones(),
      start: async (_user, change) => (started.push(change), await start(change)),
      job: async () => await job(),
    },
    restartSoon: () => {},
    boot: async () => {},
    restartBridge: () => {},
    bridgeDeviceId: () => null,
  };
}

function deps(): NovaPageDeps {
  return {
    fetchAuthUsers: async () => [OWNER],
    csrf,
    pairing: createPairingV1Manager({ registry, secureEndpoint: () => null, now: () => 1_000 }),
    registry,
    connection: () => ({ haConnected: true }),
    update: async () => ({ version: "0.12.2", versionLatest: "0.12.2", updateAvailable: false, error: false }),
    relayVersion: "0.12.2",
    now: () => 1_000,
    chatApps: chatApps(),
    connectorUrl: async () => null,
  };
}

function request(url = "/"): IncomingMessage {
  const headers = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": OWNER.id };
  return { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage;
}

async function run(handler: ReturnType<typeof createNovaPageHandler>, req: IncomingMessage, body: unknown = null) {
  const out = { statusCode: 0, headers: {} as Record<string, unknown>, body: "", setHeader(k: string, v: unknown) { this.headers[k] = v; }, end(b?: string) { if (b) this.body = b; } };
  await handler({ request: req, response: out as unknown as ServerResponse, path: "/home", body });
  return out;
}

const page = async (url = "/") => (await run(createNovaPageHandler(deps()), request(url))).body;
const post = async (form: Record<string, string>) => await run(createNovaActionHandler(deps()), request(), form);
// The form token the page minted for action.
const token = (html: string, action: string): string | undefined =>
  new RegExp(`name="csrf" value="([^"]+)"><input type="hidden" name="action" value="${action}">`).exec(html)?.[1];
const tokens = (html: string): number => html.match(/name="csrf"/g)?.length ?? 0;

describe("NOVA page: the chat-app phone", () => {
  it("offers Change and Stop for a phone that can answer, and names it", async () => {
    const html = await page();
    expect(html).toContain("Confirmations: your phone iPhone asks you.");
    expect(html).toContain('href="/api/hassio_ingress/tok/?chat_phone=pick">Change phone…</a>');
    expect(token(html, "chat_phone_stop")).toBeDefined();
    expect(html).not.toContain('value="chat_phone_move"'); // a move's token exists only in the list
    expect(html).not.toContain('http-equiv="refresh"');
  });

  it("moves to the phone picked, asking the linked phone first, without override", async () => {
    const list = await page("/?chat_phone=pick");
    expect(list).toContain("<strong>Which phone should confirm for chat apps?</strong> “iPhone” asks first, then the phone you pick.");
    expect(list).toContain(`<input type="radio" name="device_id" value="${PIXEL}" required> Pixel`);
    const move = token(list, "chat_phone_move");
    expect(move).toBeDefined();
    const answer = await post({ action: "chat_phone_move", csrf: move!, device_id: PIXEL });
    expect(answer.statusCode).toBe(303);
    expect(answer.headers.location).toBe(`/api/hassio_ingress/tok/?chat_job=${JOB}`);
    expect(started).toEqual([{ action: "move", phone: { device_id: PIXEL } }]);
  });

  it("keeps each token to its own action, and mints override tokens only for a phone that is gone", async () => {
    const list = await page("/?chat_phone=pick");
    expect(list).not.toMatch(/value="chat_phone_(replace|remove)"/);
    expect((await post({ action: "chat_phone_replace", csrf: token(list, "chat_phone_move")!, device_id: PIXEL })).statusCode).toBe(403);
    expect((await post({ action: "chat_phone_remove", csrf: token(list, "chat_phone_stop")! })).statusCode).toBe(403);
    expect(started).toEqual([]);
  });

  it("lists every phone in one form with one token, so a long list never evicts its own", async () => {
    const many = Array.from({ length: 14 }, (_, i) => ({ device_id: i.toString(16).padStart(32, "0"), name: `Phone ${i}`, platform: "" }));
    phones = async () => ({ ...ready(), phones: many });
    const list = await page("/?chat_phone=pick");
    expect(list.match(/type="radio"/g)).toHaveLength(14);
    expect(tokens(list)).toBeLessThanOrEqual(8);
    await post({ action: "chat_phone_move", csrf: token(list, "chat_phone_move")!, device_id: many[0]!.device_id });
    expect(started).toEqual([{ action: "move", phone: { device_id: many[0]!.device_id } }]); // the bridge takes only a phone it lists
  });

  it("follows a running change every 5 s with the tap wording of that phone, then names how it ended", async () => {
    const waiting = await page(`/?chat_job=${JOB}`);
    expect(waiting).toContain('<meta http-equiv="refresh" content="5">');
    expect(waiting).toContain("Check “iPhone” first and press and hold the notification, then tap “Move”. Then “Pixel” asks too.");
    expect(waiting).not.toContain("Change phone…");
    job = async () => ({ status: "running", step: "new", phone: "Pixel", platform: "android", to: "Pixel" });
    expect(await page(`/?chat_job=${JOB}`)).toContain("Check “Pixel” and tap “Allow” in the notification. No button? Pull it down, or tap its arrow.");
    job = async () => ({ status: "declined", step: "old", phone: "iPhone", platform: "ios", to: "Pixel" });
    const ended = await page(`/?chat_job=${JOB}`);
    expect(ended).toContain("Nothing changed. It was cancelled on “iPhone”.");
    expect(ended).not.toContain('http-equiv="refresh"');
    job = async () => ({ status: "done", to: "Pixel" });
    expect(await page(`/?chat_job=${JOB}`)).toContain("Done. Chat apps now ask “Pixel”.");
    job = async () => ({ status: "lost" });
    expect(await page(`/?chat_job=${JOB}`)).toContain("HA NOVA lost track of this change: the NOVA App or its chat-app bridge restarted. The Confirmations line above shows which phone asks now.");
    job = async () => {
      throw new ChatPhoneError(503, "CHAT_PHONE_UNAVAILABLE", "the chat-app bridge can't be reached");
    };
    const unread = await page(`/?chat_job=${JOB}`);
    expect(unread).toContain('<meta http-equiv="refresh" content="5">'); // unread: keeps looking
    expect(unread).toContain('href="/api/hassio_ingress/tok/">Back</a>'); // and lets the owner leave
  });

  it("names the lost-phone way out only for the phone linked now", async () => {
    const LOST = "If that phone is lost, remove it in Home Assistant";
    job = async () => ({ status: "timeout", step: "old", phone: "iPhone", platform: "ios", to: "Pixel" });
    const old = await page(`/?chat_job=${JOB}`);
    expect(old).toContain("Nothing changed. No one tapped in time on “iPhone”. If it's at hand, open its Home Assistant app and try again.");
    expect(old).toContain(LOST);
    job = async () => ({ status: "failed", step: "new", phone: "Pixel", platform: "android", to: "Pixel" });
    const fresh = await page(`/?chat_job=${JOB}`);
    expect(fresh).toContain("Nothing changed. HA NOVA couldn't ask “Pixel”, or couldn't check the answer.");
    expect(fresh).not.toContain(LOST);
  });

  it("stops after the linked phone's OK, and says what chat apps do then", async () => {
    const answer = await post({ action: "chat_phone_stop", csrf: token(await page(), "chat_phone_stop")! });
    expect(answer.headers.location).toBe(`/api/hassio_ingress/tok/?chat_job=${JOB}`);
    expect(started).toEqual([{ action: "stop" }]);
    job = async () => ({ status: "running", step: "old", phone: "iPhone", platform: "ios" });
    expect(await page(`/?chat_job=${JOB}`)).toContain("Check “iPhone” and press and hold the notification, then tap “Stop”.");
    job = async () => ({ status: "done", step: "old", phone: "iPhone", platform: "ios" });
    expect(await page(`/?chat_job=${JOB}`)).toContain("Done. Chat apps no longer ask a phone: changes that need your OK show a code in the chat.");
  });

  it("replaces or removes a phone that can never answer again only with this page's override", async () => {
    phones = async () => ({ ...ready(), link: { device_id: IPHONE, name: "iPhone", platform: "ios", state: "gone", why: "removed" } });
    const html = await page();
    expect(html).toContain("“iPhone” can't answer anymore: it is no longer in Home Assistant. You can link a new phone here without it.");
    expect(html).toContain("Confirmations: “iPhone” can't answer anymore, so changes that need your OK are refused.");
    expect(html).toContain("Stop takes effect at once, without a question: changes that need your OK then show a code in the chat, which your AI assistant can see too.");
    const removed = await post({ action: "chat_phone_remove", csrf: token(html, "chat_phone_remove")! });
    expect(removed.headers.location).toBe("/api/hassio_ingress/tok/?chat_phone=stopped"); // done at once
    const stoppedPhones = phones;
    phones = async () => ({ ...ready(), link: null });
    expect(await page("/?chat_phone=stopped")).toContain("Done. Chat apps no longer ask a phone: changes that need your OK show a code in the chat.");
    phones = stoppedPhones;
    const list = await page("/?chat_phone=pick");
    expect(list).toContain("The phone you pick asks once.");
    const replace = token(list, "chat_phone_replace");
    expect(token(list, "chat_phone_move")).toBeUndefined();
    await post({ action: "chat_phone_replace", csrf: replace!, device_id: PIXEL });
    expect(started).toEqual([{ action: "stop", override: true }, { action: "move", phone: { device_id: PIXEL }, override: true }]);
  });

  it("offers nothing for a phone that is only unreachable now, and says how to get out", async () => {
    phones = async () => ({ ...ready(), link: { device_id: IPHONE, name: "iPhone", platform: "ios", state: "unavailable", why: "unreachable" } });
    const html = await page("/?chat_phone=pick");
    expect(html).toContain("HA NOVA can't ask “iPhone” right now: its Home Assistant app is signed out, or its notifications are off.");
    expect(html).toContain("If that phone is lost, remove it in Home Assistant (Settings → Devices &amp; services → Mobile App). Then you can link a new phone here.");
    expect(html).not.toMatch(/value="chat_phone_(move|replace|stop|remove)"/);
    phones = async () => ({ ...ready(), link: { device_id: IPHONE, name: "iPhone", platform: "ios", state: "unavailable", why: "unread" } });
    expect(await page()).toContain("HA NOVA can't check “iPhone” just now. Reload this page in a moment.");
  });

  it("links a first phone, and names the way when none can confirm or the App signs in as the owner", async () => {
    phones = async () => ({ phones: [{ device_id: PIXEL, name: "Pixel", platform: "" }], link: null });
    expect(await page()).toContain('href="/api/hassio_ingress/tok/?chat_phone=pick">Link a phone…</a>');
    phones = async () => ({ phones: [], link: null });
    expect(await page()).toContain("To confirm with a phone, sign in to the Home Assistant app on it with your own account.");
    phones = async () => {
      throw new ChatPhoneError(409, "OWNER_TOKEN", "the App signs in as the owner");
    };
    expect(await page()).toContain("No phone can confirm for chat apps here: this NOVA App signs in to Home Assistant as the owner");
    phones = async () => {
      throw new ChatPhoneError(503, "CHAT_PHONE_UNAVAILABLE", "the chat-app bridge can't be reached");
    };
    expect(await page()).toContain("HA NOVA can't read this home's phones right now. Reload this page in a moment.");
  });

  it("says plainly why a change didn't start", async () => {
    start = async () => {
      throw new ChatPhoneError(409, "PHONE_CHANGE_RUNNING", "a phone change for this user is running");
    };
    const busy = await post({ action: "chat_phone_stop", csrf: token(await page(), "chat_phone_stop")! });
    expect(busy.headers.location).toBe("/api/hassio_ingress/tok/?err=chat_phone_busy");
    expect(await page("/?err=chat_phone_busy")).toContain("A phone change is already running. Wait until it ends, then try again.");
    start = async () => {
      throw new ChatPhoneError(409, "OLD_PHONE_UNAVAILABLE", "the linked phone can't be asked");
    };
    const changed = await post({ action: "chat_phone_stop", csrf: token(await page(), "chat_phone_stop")! });
    expect(changed.headers.location).toBe("/api/hassio_ingress/tok/?err=chat_phone_changed");
    expect(await page("/?err=chat_phone_changed")).toContain("Nothing changed: the phones or the linked phone changed meanwhile.");
    start = async () => {
      throw new ChatPhoneError(504, "CHAT_PHONE_UNKNOWN", "the chat-app bridge's answer was lost");
    };
    const unknown = await post({ action: "chat_phone_stop", csrf: token(await page(), "chat_phone_stop")! });
    expect(unknown.headers.location).toBe("/api/hassio_ingress/tok/?err=chat_phone_unknown");
    const said = await page("/?err=chat_phone_unknown");
    expect(said).toContain("Chat apps: phone change unclear");
    expect(said).toContain("HA NOVA couldn't tell whether this started, so it may still go ahead.");
  });
});
