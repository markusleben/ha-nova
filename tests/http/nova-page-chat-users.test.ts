import type { IncomingMessage, ServerResponse } from "node:http";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createNovaActionHandler, createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import type { ChatApps, ChatAppsStatus } from "../../nova/src/runtime/chat-apps.js";
import type { ChatUsers } from "../../nova/src/runtime/chat-apps-household.js";
import { ChatPhoneError, type ChatActivity, type ChatPhoneChange, type ChatPhoneJob, type ChatPhones } from "../../nova/src/runtime/chat-phone-control.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import { openDeviceRegistry, type DeviceRegistry } from "../../nova/src/security/device-registry.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";
import { createPairingV1Manager } from "../../nova/src/security/pairing-v1.js";

// The NOVA page's household list (docs/work/2026-10-24-chat-apps-household-
// spec.md): who may use chat apps and how much, saved from the owner's
// session only and checked against Home Assistant's users; each listed
// user's phone through the bridge's control channel; and what chat apps did.

const OWNER = "0".repeat(31) + "1";
const ANNA = "a".repeat(32);
const BEN = "b".repeat(32);
const CLARA = "c".repeat(32);
const EVIL = "e".repeat(32);
const JOB = "f".repeat(32);
const PIXEL = "2".repeat(32);
const HOSTILE = "<img src=x onerror=alert(1)>";

const USERS: HaAuthUser[] = [
  { id: OWNER, name: "Markus", is_owner: true, is_active: true, group_ids: ["system-admin"] },
  { id: ANNA, name: "Anna", is_active: true, group_ids: ["system-users"] },
  { id: BEN, name: "Ben", is_active: true, group_ids: ["system-read-only"] },
  { id: CLARA, name: "Clara", is_active: true, group_ids: ["system-admin"] },
  { id: EVIL, name: HOSTILE, is_active: true, group_ids: ["system-users"] },
  { id: "d".repeat(32), name: "Gone", is_active: false, group_ids: ["system-users"] },
  { id: "9".repeat(32), name: "Supervisor", is_active: true, system_generated: true, group_ids: ["system-admin"] },
];

let dir: string;
let registry: DeviceRegistry;
let csrf: ReturnType<typeof createCsrfStore>;
let status: ChatAppsStatus;
let saved: ChatUsers[];
let started: string[];
let job: ChatPhoneJob;
let activity: ChatActivity[];
let seen: Record<string, string>;
let phones: ChatPhones;
let users: HaAuthUser[];
let startError: Error | null;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "ha-nova-chat-users-page-"));
  registry = openDeviceRegistry(dir);
  csrf = createCsrfStore();
  status = { available: true, enabled: true, unfinished: false, readOnly: false, chatCodes: true, allowedUsers: null, levels: {}, phones: {}, ownerPhone: null, ownerPhoneId: null };
  saved = [];
  started = [];
  job = { status: "running", step: "new", phone: "Anna's Pixel", platform: "android", button: "Erlauben" };
  activity = [];
  seen = {};
  phones = { phones: [{ device_id: PIXEL, name: "Anna's Pixel", platform: "android" }], link: null };
  users = USERS;
  startError = null;
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

function chatApps(): ChatApps {
  return {
    status: () => status,
    turnOn: async () => ({ restart: false }),
    turnOff: async () => {},
    setSwitches: () => {},
    setUsers: async (_owner, next) => void saved.push(next),
    phone: {
      phones: async () => phones,
      start: async (user: string, change: ChatPhoneChange) => {
        if (startError !== null) {
          throw startError;
        }
        started.push(`${user}:${JSON.stringify(change)}`);
        return JOB;
      },
      job: async () => job,
      activity: async () => activity,
      seen: async () => seen,
    },
    restartSoon: () => {},
    boot: async () => {},
    restartBridge: () => {},
    bridgeDeviceId: () => null,
  };
}

function deps(): NovaPageDeps {
  return {
    fetchAuthUsers: async () => users,
    csrf,
    pairing: createPairingV1Manager({ registry, secureEndpoint: () => null, now: () => 1_000 }),
    registry,
    connection: () => ({ haConnected: true }),
    update: async () => ({ version: "0.13.0", versionLatest: "0.13.0", updateAvailable: false, error: false }),
    relayVersion: "0.13.0",
    now: () => Date.parse("2026-10-29T18:30:00Z"),
    chatApps: chatApps(),
    connectorUrl: async () => "https://home.example.org/api/ha_nova/mcp",
  };
}

function request(url = "/", as = OWNER): IncomingMessage {
  const headers = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": as };
  return { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage;
}

async function run(handler: ReturnType<typeof createNovaPageHandler>, req: IncomingMessage, body: unknown = null) {
  const out = { statusCode: 0, headers: {} as Record<string, unknown>, body: "", setHeader(k: string, v: unknown) { this.headers[k] = v; }, end(b?: string) { if (b) this.body = b; } };
  await handler({ request: req, response: out as unknown as ServerResponse, path: "/home", body });
  return out;
}

const page = async (url = "/") => (await run(createNovaPageHandler(deps()), request(url))).body;
const post = async (form: Record<string, string>, as = OWNER) => await run(createNovaActionHandler(deps()), request("/", as), form);
const token = (html: string, action: string): string | undefined =>
  new RegExp(`name="csrf" value="([^"]+)"><input type="hidden" name="action" value="${action}">`).exec(html)?.[1];
const listed = (ids: string[], levels: Record<string, "ask" | "use" | "manage">) => {
  status = { ...status, allowedUsers: [OWNER, ...ids], levels };
};

describe("NOVA page: who can use chat apps", () => {
  it("lists every active login with its role, and only the levels that role allows", async () => {
    const html = await page();
    expect(html).toContain('<h3 id="chat-users">Who can use it</h3>');
    expect(html).toContain("Pick who may use HA NOVA in Claude and ChatGPT. Each person signs in with their own Home Assistant account.");
    expect(html).toContain('<input type="checkbox" checked disabled> <strong>Markus</strong></label><span class="muted meta">Owner · Manage everything</span>');
    expect(html).not.toContain(`name="on_${OWNER}"`); // the owner can't be unticked
    const select = (id: string) => new RegExp(`<select name="level_${id}"[^>]*>(.*?)</select>`).exec(html)?.[1] ?? "";
    expect(select(ANNA)).toBe('<option value="ask">Ask only</option><option value="use" selected>Use the home</option>'); // default when ticked: the cap
    expect(select(BEN)).toBe('<option value="ask" selected>Ask only</option>');
    expect(select(CLARA)).toContain('<option value="manage" selected>Manage everything</option>');
    expect(html).not.toContain("Gone");
    expect(html).not.toContain("Supervisor");
    expect(html).toContain("Someone missing? Each person needs their own Home Assistant login. Add one under Settings → People.");
    expect(html).toContain("Can't change automations, scripts, dashboards, or device settings.");
    expect(html).toContain("without one they're refused, never asked as a code in the chat.");
    expect(html).toContain("users get at most Use the home, read-only users Ask only.");
  });

  it("no longer says that only the owner can use chat apps", async () => {
    const on = await page();
    for (const owner of ["Who can use it: only you", "as its owner", "Sign in as the owner"]) {
      expect(on).not.toContain(owner);
    }
    expect(on).toContain("Then sign in with your own Home Assistant login.");
    status = { ...status, enabled: false };
    const off = await page();
    expect(off).not.toContain("Only you can use it");
    expect(off).toContain("Once they're on, you pick who else in your home may use them.");
  });

  it("renders hostile user, phone, and activity names inert", async () => {
    listed([EVIL], { [EVIL]: "use" });
    status = { ...status, phones: { [EVIL]: HOSTILE } };
    activity = [{ at: "2026-10-29T17:02:00Z", user: EVIL, name: HOSTILE, tool: "ha_call", what: "lock.unlock", targets: [HOSTILE], result: "phone" }];
    const html = await page();
    expect(html).not.toContain(HOSTILE);
    expect(html).toContain("&lt;img src=x onerror=alert(1)&gt;");
    expect(html).toContain(`Phone: “&lt;img src=x onerror=alert(1)&gt;”`);
    expect(html).toContain("Unlocked &lt;img src=x onerror=alert(1)&gt; · confirmed on their phone");
    const panel = await page(`/?chat_user=${EVIL}&chat_phone=pick`);
    expect(panel).not.toContain(HOSTILE);
    expect(panel.slice(panel.indexOf('id="chat-phone"'))).toContain("&lt;img src=x onerror=alert(1)&gt;");
  });

  it("saves the ticked users at their levels, always with the owner", async () => {
    const html = await page();
    const answer = await post({ action: "chat_users", csrf: token(html, "chat_users")!, [`on_${ANNA}`]: "1", [`level_${ANNA}`]: "use", [`on_${BEN}`]: "1", [`level_${BEN}`]: "ask", [`level_${CLARA}`]: "manage" });
    expect(answer.statusCode).toBe(303);
    expect(answer.headers.location).toBe("/api/hassio_ingress/tok/?chat_users=saved#chat-users"); // back at the list, where "Saved." shows
    expect(saved).toEqual([{ allowed_users: [OWNER, ANNA, BEN], levels: { [ANNA]: "use", [BEN]: "ask" }, roles: 1 }]);
    expect(await page("/?chat_users=saved")).toContain('<p class="ok">Saved.</p>');
  });

  it("refuses the whole save for an unknown, inactive, or system user, or a level above the role", async () => {
    for (const [id, level] of [["7".repeat(32), "ask"], ["d".repeat(32), "ask"], ["9".repeat(32), "ask"], [ANNA, "manage"], [BEN, "use"]] as const) {
      const answer = await post({ action: "chat_users", csrf: token(await page(), "chat_users")!, [`on_${ANNA}`]: "1", [`level_${ANNA}`]: "use", [`on_${id}`]: "1", [`level_${id}`]: level });
      expect(answer.headers.location, id).toBe("/api/hassio_ingress/tok/?err=chat_users");
    }
    expect(saved).toEqual([]);
    expect(await page("/?err=chat_users")).toContain("Nothing changed: someone you picked changed in Home Assistant meanwhile");
  });

  it("checks the save against Home Assistant's users read for that request", async () => {
    const html = await page();
    users = USERS.map((u) => (u.id === ANNA ? { ...u, is_active: false } : u)); // turned off after the page was shown
    expect((await post({ action: "chat_users", csrf: token(html, "chat_users")!, [`on_${ANNA}`]: "1", [`level_${ANNA}`]: "use" })).headers.location).toBe("/api/hassio_ingress/tok/?err=chat_users");
    expect(saved).toEqual([]);
  });

  it("refuses a Home Assistant administrator who isn't the owner", async () => {
    const html = await page();
    const answer = await post({ action: "chat_users", csrf: token(html, "chat_users")!, [`on_${CLARA}`]: "1", [`level_${CLARA}`]: "manage" }, CLARA);
    expect(answer.statusCode).toBe(403);
    expect((await run(createNovaPageHandler(deps()), request("/", CLARA))).statusCode).toBe(403);
    expect(saved).toEqual([]);
  });

  it("lets the owner take someone off whom Home Assistant turned off", async () => {
    listed(["d".repeat(32)], { ["d".repeat(32)]: "use" });
    const html = await page();
    expect(html).toContain("<strong>Gone</strong><span class=\"muted meta\">Turned off or removed in Home Assistant · leaves this list when you save</span>");
    await post({ action: "chat_users", csrf: token(html, "chat_users")! });
    expect(saved).toEqual([{}]);
    users = USERS.filter((u) => u.id === OWNER); // only the owner left in Home Assistant, the list still names Gone
    expect(token(await page(), "chat_users")).toBeDefined();
  });

  it("says when someone added again keeps an old phone link", async () => {
    expect(await page("/?err=chat_users_link")).toContain("Nothing changed: HA NOVA couldn't clear an old phone link of someone you added.");
  });

  it("says how someone removed signs their chat app out", async () => {
    listed([ANNA], { [ANNA]: "use" });
    const answer = await post({ action: "chat_users", csrf: token(await page(), "chat_users")!, [`level_${ANNA}`]: "use" });
    expect(saved).toEqual([{}]); // only the owner: no list
    expect(answer.headers.location).toBe(`/api/hassio_ingress/tok/?chat_users=saved&chat_removed=${ANNA}#chat-users`);
    expect(await page(`/?chat_users=saved&chat_removed=${ANNA}`)).toContain("<p class=\"ok\">Saved.</p>"); // the fake keeps the list: Anna is still on it
    expect(await page(`/?chat_users=saved&chat_removed=${ANNA}`)).not.toContain("Anna can't use HA NOVA");
    status = { ...status, allowedUsers: null, levels: {} }; // what the save wrote
    expect(await page(`/?chat_users=saved&chat_removed=${ANNA}`)).toContain("Anna can't use HA NOVA in chat apps anymore. To sign Anna's chat app out of Home Assistant too, Anna removes it under Profile → Security.");
    const odd = await page(`/?chat_users=saved&chat_removed=${OWNER},${"d".repeat(32)},${EVIL}`); // the owner, a turned-off login, a hostile name
    expect(odd).not.toContain("Markus can't use");
    expect(odd).not.toContain("Gone");
    expect(odd).not.toContain(HOSTILE);
    expect(odd).toContain("&lt;img src=x onerror=alert(1)&gt; can't use HA NOVA in chat apps anymore.");
  });

  it("shows each listed user's sign-in, phone, and how they join", async () => {
    listed([ANNA, BEN, CLARA], { [ANNA]: "use", [BEN]: "ask", [CLARA]: "manage" });
    status = { ...status, phones: { [CLARA]: "Clara's iPhone" } };
    seen = { [ANNA]: "2026-10-29T18:00:00Z" };
    const html = await page();
    expect(html).toContain(`User · Signed in · no phone · <a href="/api/hassio_ingress/tok/?chat_user=${ANNA}&amp;chat_phone=pick#chat-phone"`);
    expect(html).toContain("Read-only user · Not signed in yet</span>"); // Ben, Ask only: no phone
    expect(html).toContain(`Administrator · Phone: “Clara&#39;s iPhone” · <a href="/api/hassio_ingress/tok/?chat_user=${CLARA}#chat-phone"`);
    expect(html).toContain("<summary>How Anna joins</summary>");
    expect(html).toContain('add HA NOVA with this link: <span class="address">https://home.example.org/api/ha_nova/mcp</span>');
    expect(html).toContain("<li>Ask Markus to link your phone here.</li>");
    expect(/How Ben joins.*?<\/details>/.exec(html)?.[0]).not.toContain("link your phone");
  });
});

describe("NOVA page: a listed user's phone", () => {
  it("links a phone for a user at Use the home, bound to that user", async () => {
    listed([ANNA], { [ANNA]: "use" });
    const list = await page(`/?chat_user=${ANNA}&chat_phone=pick`);
    expect(list).toContain(`<h3 id="chat-phone">Anna's phone</h3>`);
    expect(list).toContain("<strong>Which phone should confirm for Anna?</strong> The phone you pick asks once. Anna confirms on it, so its Home Assistant app must be signed in with Anna's own login.");
    expect(list).toContain(`<input type="hidden" name="user" value="${ANNA}">`);
    const move = token(list, "chat_phone_move")!;
    const answer = await post({ action: "chat_phone_move", csrf: move, user: ANNA, device_id: PIXEL });
    expect(answer.headers.location).toBe(`/api/hassio_ingress/tok/?chat_job=${JOB}&chat_user=${ANNA}#chat-phone`);
    expect(started).toEqual([`${ANNA}:{"action":"move","phone":{"device_id":"${PIXEL}"}}`]);
  });

  it("never lets a token for one user's phone change another's", async () => {
    listed([ANNA], { [ANNA]: "use" });
    const owners = await page("/?chat_phone=pick"); // the owner's own list
    expect((await post({ action: "chat_phone_move", csrf: token(owners, "chat_phone_move")!, user: ANNA, device_id: PIXEL })).statusCode).toBe(403);
    const forged = csrf.issue(OWNER, `chat_phone_stop:${BEN}`, Date.parse("2026-10-29T18:30:00Z")); // Ben isn't on the list
    expect((await post({ action: "chat_phone_stop", csrf: forged, user: BEN })).headers.location).toBe(`/api/hassio_ingress/tok/?err=chat_phone&chat_user=${BEN}#chat-phone`);
    expect(await page(`/?err=chat_phone&chat_user=${BEN}`)).toContain('<h3 id="chat-phone">Your phone</h3>'); // Ben has no phone panel
    expect(started).toEqual([]);
  });

  it("stays on that user's phone when a change didn't start or its answer was lost", async () => {
    listed([ANNA], { [ANNA]: "use" });
    startError = new ChatPhoneError(504, "CHAT_PHONE_UNKNOWN", "the chat-app bridge's answer was lost");
    const list = await page(`/?chat_user=${ANNA}&chat_phone=pick`);
    const answer = await post({ action: "chat_phone_move", csrf: token(list, "chat_phone_move")!, user: ANNA, device_id: PIXEL });
    expect(answer.headers.location).toBe(`/api/hassio_ingress/tok/?err=chat_phone_unknown&chat_user=${ANNA}#chat-phone`);
    const after = await page(`/?err=chat_phone_unknown&chat_user=${ANNA}`);
    expect(after).toContain("Chat apps: phone change unclear");
    expect(after).toContain(`<h3 id="chat-phone">Anna's phone</h3>`);
  });

  it("shows the owner's phone for a user who can't have one, and says so, never the owner's pick list or job", async () => {
    listed([BEN], { [BEN]: "ask" });
    const ben = await page(`/?chat_user=${BEN}&chat_phone=pick&chat_job=${JOB}`);
    expect(ben).toContain('<h3 id="chat-phone">Your phone</h3>');
    expect(ben).toContain("That person can't have a phone for chat apps now: they aren't on the list at Use the home or above.");
    expect(ben).not.toContain("Which phone should confirm");
    expect(ben).not.toContain("Erlauben");
    expect(await page(`/?chat_user=${CLARA}`)).toContain('<h3 id="chat-phone">Your phone</h3>'); // not on the list
    const forged = csrf.issue(OWNER, `chat_phone_stop:${BEN}`, Date.parse("2026-10-29T18:30:00Z")); // on the list, at Ask only
    expect((await post({ action: "chat_phone_stop", csrf: forged, user: BEN })).headers.location).toBe(`/api/hassio_ingress/tok/?err=chat_phone&chat_user=${BEN}#chat-phone`);
    expect(started).toEqual([]);
  });

  it("quotes the phone's own button from the bridge, in English when an older bridge names none", async () => {
    listed([ANNA], { [ANNA]: "use" });
    expect(await page(`/?chat_job=${JOB}&chat_user=${ANNA}`)).toContain("Ask Anna to check “Anna&#39;s Pixel” and tap “Erlauben” in the notification."); // Anna's phone, not the owner's
    job = { status: "running", step: "new", phone: "Anna's Pixel", platform: "android" };
    expect(await page(`/?chat_job=${JOB}&chat_user=${ANNA}`)).toContain("Ask Anna to check “Anna&#39;s Pixel” and tap “Allow” in the notification.");
    job = { status: "running", step: "old", phone: "iPhone", platform: "ios", button: "<b>Stop</b>" };
    expect(await page(`/?chat_job=${JOB}`)).toContain("then tap “&lt;b&gt;Stop&lt;/b&gt;”.");
  });

  it("names the user in what to do when none of their phones can confirm", async () => {
    listed([ANNA], { [ANNA]: "use" });
    phones = { phones: [], link: null };
    expect(await page(`/?chat_user=${ANNA}`)).toContain("None of Anna's phones can confirm yet. In Home Assistant, add Anna's phone to Anna under Settings → People");
    expect(await page(`/?chat_user=${ANNA}`)).toContain("Confirmations: no phone linked: changes that need Anna's OK are refused."); // no chat code below Manage everything
  });
});

describe("NOVA page: recent changes from chat apps", () => {
  it("shows the bridge's last changes with who, what, and how they ended", async () => {
    activity = [
      { at: "2026-10-29T18:02:00Z", user: ANNA, name: "Anna", tool: "ha_call", what: "lock.unlock", targets: ["Front Door"], result: "phone" },
      { at: "2026-10-29T17:40:00Z", user: ANNA, name: "Anna", tool: "ha_call", what: "light.turn_off", targets: ["Kitchen Lights"], result: "done" },
      { at: "2026-10-28T09:15:00Z", user: BEN, name: "Ben", tool: "ha_call", what: "homeassistant.restart", result: "refused" },
    ];
    const html = await page();
    expect(html).toContain("<h2>Recent changes from chat apps</h2>");
    expect(html).toMatch(/\d\d:02 · Anna · Unlocked Front Door · confirmed on their phone/);
    expect(html).toMatch(/\d\d:40 · Anna · Turned off Kitchen Lights<\/span>/);
    expect(html).toMatch(/2026-10-2\d \d\d:15 · Ben · <code>homeassistant.restart<\/code> · refused/);
    activity = [{ at: "2026-10-29T18:05:00Z", user: BEN, name: "Ben", tool: "ha_call", what: "light.constructor", targets: ["Hall"], result: "toString" }];
    const odd = await page(); // names from the chat never pick up an object's own properties
    expect(odd).toContain("· Ben · <code>light.constructor</code> Hall · toString</span>");
    expect(odd).not.toContain("native code");
    activity = [];
    expect(await page()).not.toContain("Recent changes from chat apps");
  });
});
