import { createServer, request as httpRequest, type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { afterEach, describe, expect, it, vi } from "vitest";

import { askRef, createGateAsks, gateDigest, gateWithAsks, type GateAsksDeps, type GateRequest, type PhoneAnswer, type PhoneLink, type LinkWords, type PhoneText, type PreparedAsk } from "../../nova/src/http/gate-asks.js";
import { gated, type GateClient } from "../../nova/src/http/gate.js";
import { HttpError } from "../../nova/src/http/errors.js";
import type { RouteContext } from "../../nova/src/http/router.js";
import type { Principal } from "../../nova/src/security/principal.js";

const laptop: Principal = { kind: "device", deviceId: "laptop" };
const phone: Principal = { kind: "device", deviceId: "phone" };
const unlock = { method: "POST", path: "/api/services/lock/unlock", body: { entity_id: ["lock.front"] } };
const typed: PreparedAsk = { verdict: "typed", lang: "en", title: "Unlock “Front”?", body: "Requested by “Mac”.", button: "Unlock", items: [{ verdict: "typed", text: "Unlock “Front”" }] };

function context(principal: Principal | undefined, body: unknown, headers: Record<string, string> = {}, path = "/gate/v1/ask"): RouteContext {
  const request = { headers, once: () => request, off: () => request } as unknown as IncomingMessage;
  const response = { destroyed: false, once: () => response, off: () => response } as unknown as ServerResponse;
  return { request, response, path, body, ...(principal ? { principal } : {}) };
}

function setup(answer: PreparedAsk | null | (() => Promise<PreparedAsk | null>) = typed, channels: string[] = ["page"], pollMs = 5, extra: Partial<GateAsksDeps> = {}) {
  let now = 1_000;
  const seen: { device: string; requests: GateRequest[] }[] = [];
  const asks = createGateAsks({
    ask: async (device, requests) => {
      seen.push({ device, requests });
      return typeof answer === "function" ? await answer() : answer;
    },
    prepare: { core: (body) => ({ ...(body as object), method: String((body as { method: string }).method).toUpperCase() }) },
    deviceName: (p) => (p.kind === "device" ? `device ${p.deviceId}` : "token"),
    channels: () => channels,
    link: () => undefined,
    phone: async () => null,
    now: () => now,
    pollMs,
    ...extra,
  });
  const ask = async (principal: Principal, requests: unknown[]) =>
    (await asks.handler(context(principal, { requests: requests.map((body) => ({ endpoint: "core", body_b64: Buffer.from(JSON.stringify(body)).toString("base64") })) }))) as Record<string, unknown>;
  const op = async (principal: Principal | undefined, askId: unknown, name: string) => (await asks.handler(context(principal, { ask_id: askId, op: name }))) as Record<string, unknown>;
  const linkTo = async (principal: Principal, to: unknown) => (await asks.handler(context(principal, { link: to }, {}, "/gate/v1/phone"))) as Record<string, unknown>;
  return { asks, ask, op, linkTo, seen, tick: (ms: number) => (now += ms), now: () => now };
}

async function status(run: () => Promise<unknown>): Promise<number> {
  return (await failure(run)).status;
}

async function failure(run: () => Promise<unknown>): Promise<{ status: number; code?: string }> {
  try {
    await run();
    return { status: 200 };
  } catch (error) {
    return { status: (error as HttpError).status, code: (error as HttpError).code };
  }
}

const link: PhoneLink = { service: "mobile_app_iphone", device_id: "ha-iphone", user_id: "markus" };

// phones: each phone ask waits until the test answers it.
function phones() {
  const calls: { link: PhoneLink; text: PhoneText | LinkWords; waitMs: number; stop: AbortSignal; answer: (a: PhoneAnswer | null) => void }[] = [];
  const phone = (l: PhoneLink, text: PhoneText | LinkWords, waitMs: number, stop: AbortSignal) =>
    new Promise<PhoneAnswer | null>((answer) => calls.push({ link: l, text, waitMs, stop, answer }));
  return { calls, phone };
}

const settled = () => new Promise((r) => setImmediate(r));

afterEach(() => {
  vi.useRealTimers();
});

describe("gate asks", () => {
  it("asks only for typed requests, and only where someone can approve", async () => {
    expect(await setup({ verdict: "pass", items: [{ verdict: "pass" }] }).ask(laptop, [unlock])).toEqual({ needed: false });
    expect(await status(() => setup({ verdict: "refuse", items: [{ verdict: "refuse" }] }).ask(laptop, [unlock]))).toBe(422);
    expect(await status(() => setup(null).ask(laptop, [unlock]))).toBe(503);
    expect(await status(() => setup(typed, []).ask(laptop, [unlock]))).toBe(409);
    const t = setup();
    expect(await status(() => t.ask(laptop, []))).toBe(400);
    expect(await status(() => t.ask(laptop, Array(21).fill(unlock)))).toBe(400);
    expect(t.seen).toEqual([]);
  });

  it("an approval grants exactly the asked bytes, once each, to the device that asked", async () => {
    const t = setup();
    const opened = await t.ask(laptop, [{ ...unlock, method: "post" }]);
    expect(opened).toMatchObject({ needed: true, channels: ["page"] });
    expect(t.seen[0]!.device).toBe("device laptop");
    const sent = t.seen[0]!.requests[0]!.body; // as the Relay prepares it
    expect(JSON.parse(sent.toString("utf8")).method).toBe("POST");
    expect(await t.op(laptop, opened.ask_id, "wait")).toMatchObject({ state: "waiting" });
    expect(t.asks.approve(askRef(String(opened.ask_id)))).toBe(true);
    const approved = await t.op(laptop, opened.ask_id, "wait");
    expect(approved).toMatchObject({ state: "approved" });
    const grant = String(approved.grant_id);
    expect(t.asks.consume(phone, grant, "core", sent)).toBe(false); // another device
    expect(t.asks.consume(laptop, grant, "core", Buffer.from(JSON.stringify({ ...unlock, body: { entity_id: ["lock.back"] } })))).toBe(false); // other bytes
    expect(t.asks.consume(laptop, grant, "ws", sent)).toBe(false); // another endpoint
    expect(t.asks.consume(laptop, grant, "core", sent)).toBe(true);
    expect(t.asks.consume(laptop, grant, "core", sent)).toBe(false); // used
    expect(t.asks.approve(askRef(String(opened.ask_id)))).toBe(false); // approved once
  });

  it("a grant ends after two minutes, an ask after three", async () => {
    const t = setup();
    const first = await t.ask(laptop, [unlock]);
    t.asks.approve(askRef(String(first.ask_id)));
    const grant = String((await t.op(laptop, first.ask_id, "wait")).grant_id);
    t.tick(120_001);
    expect(t.asks.consume(laptop, grant, "core", t.seen[0]!.requests[0]!.body)).toBe(false);
    const second = await t.ask(laptop, [unlock]);
    t.tick(180_000);
    expect(await t.op(laptop, second.ask_id, "wait")).toEqual({ state: "expired" });
    expect(t.asks.approve(askRef(String(second.ask_id)))).toBe(false);
  });

  it("a cancel ends the ask for good; a new ask replaces the open one", async () => {
    const t = setup();
    const first = await t.ask(laptop, [unlock]);
    const second = await t.ask(laptop, [unlock]);
    expect(await t.op(laptop, first.ask_id, "wait")).toEqual({ state: "cancelled" });
    expect(await t.op(laptop, second.ask_id, "cancel")).toEqual({ state: "cancelled" });
    expect(t.asks.approve(askRef(String(second.ask_id)))).toBe(false);
    expect(t.asks.approve(askRef(String(first.ask_id)))).toBe(false);
  });

  it("a grant covers only the requests the person was shown", async () => {
    const t = setup({ verdict: "typed", lang: "en", title: "Run 2 steps?", body: "Requested by “Mac”.", button: "Run", items: [{ verdict: "pass" }, { verdict: "typed", text: "Unlock “Front”" }] });
    const reload = { method: "POST", path: "/api/services/script/reload", body: {} };
    const opened = await t.ask(laptop, [reload, unlock]);
    t.asks.approve(askRef(String(opened.ask_id)));
    const grant = String((await t.op(laptop, opened.ask_id, "wait")).grant_id);
    const [passing, shown] = t.seen[0]!.requests;
    expect(t.asks.consume(laptop, grant, "core", passing!.body)).toBe(false); // rated typed later, it needs its own ask
    expect(t.asks.consume(laptop, grant, "core", shown!.body)).toBe(true);
  });

  it("one ask at a time is prepared per device", async () => {
    let answer: (value: PreparedAsk) => void = () => {};
    let calls = 0;
    const t = setup(async () => (++calls === 1 ? await new Promise<PreparedAsk>((resolve) => (answer = resolve)) : typed));
    const first = t.ask(laptop, [unlock]);
    await new Promise((r) => setImmediate(r));
    expect(await status(() => t.ask(laptop, [unlock]))).toBe(429);
    answer(typed);
    expect(await first).toMatchObject({ needed: true });
    expect(await t.ask(laptop, [unlock])).toMatchObject({ needed: true }); // free again
  });

  it("a caller that hangs up ends its long poll", async () => {
    const t = setup(typed, ["phone"], 60_000);
    const opened = await t.ask(laptop, [unlock]);
    let ended!: () => void;
    const pollEnded = new Promise<void>((r) => (ended = r));
    const server = createServer((request, response) => {
      request.resume();
      request.on("end", () => {
        void Promise.resolve(t.asks.handler({ request, response, path: "/gate/v1/ask", body: { ask_id: opened.ask_id, op: "wait" }, principal: laptop })).then(() => ended());
      });
    });
    await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
    try {
      const client = httpRequest({ port: (server.address() as AddressInfo).port, host: "127.0.0.1", method: "POST" });
      client.on("error", () => {});
      client.end("{}");
      await new Promise((r) => setTimeout(r, 50));
      client.destroy();
      await expect(Promise.race([pollEnded.then(() => "ended"), new Promise((r) => setTimeout(() => r("still polling"), 2_000))])).resolves.toBe("ended");
    } finally {
      server.closeAllConnections();
      await new Promise((r) => server.close(r));
    }
  });

  it("the linked phone approves its device's ask, only by its own administrator and only while linked", async () => {
    const p = phones();
    let current: PhoneLink | undefined = link;
    const t = setup(typed, [], 5, { link: (who) => (who.kind === "device" && who.deviceId === "laptop" ? current : undefined), phone: p.phone });
    const first = await t.ask(laptop, [unlock]);
    expect(first.channels).toEqual(["phone"]);
    expect(p.calls[0]).toMatchObject({ link, text: { lang: "en", title: typed.title, body: typed.body, button: typed.button }, waitMs: 180_000 });
    p.calls[0]!.answer({ answer: "approved", user_id: "guest" });
    await settled();
    expect(await t.op(laptop, first.ask_id, "wait")).toMatchObject({ state: "waiting" });
    const second = await t.ask(laptop, [unlock]);
    expect(p.calls[0]!.stop.aborted).toBe(true); // the replaced ask hangs up its phone
    current = { ...link, user_id: "new" };
    p.calls[1]!.answer({ answer: "approved", user_id: "markus" });
    await settled();
    expect(await t.op(laptop, second.ask_id, "wait")).toMatchObject({ state: "waiting" }); // the link changed meanwhile
    current = link;
    const third = await t.ask(laptop, [unlock]);
    p.calls[2]!.answer({ answer: "approved", user_id: "markus" });
    await settled();
    expect(await t.op(laptop, third.ask_id, "wait")).toMatchObject({ state: "approved" });
    expect(p.calls[2]!.stop.aborted).toBe(true);
    expect(await status(() => t.ask(phone, [unlock]))).toBe(409); // another device has no link of its own here
  });

  it("a decline or another person's tap ends the ask; a phone that can't be asked ends it only without another channel", async () => {
    for (const [answer, others, state] of [
      [{ answer: "declined" }, [], "cancelled"],
      [{ answer: "rejected" }, [], "cancelled"],
      [{ answer: "none" }, [], "waiting"],
      [{ answer: "unavailable" }, [], "unavailable"],
      [null, [], "unavailable"],
      [{ answer: "unavailable" }, ["page"], "waiting"],
      [null, ["page"], "waiting"],
    ] as const) {
      const p = phones();
      const t = setup(typed, [...others], 5, { link: () => link, phone: p.phone });
      const opened = await t.ask(laptop, [unlock]);
      p.calls[0]!.answer(answer);
      await settled();
      expect(await t.op(laptop, opened.ask_id, "wait")).toMatchObject({ state });
    }
  });

  it("a device opens at most 10 asks an hour", async () => {
    const t = setup();
    for (let i = 0; i < 10; i++) {
      await t.ask(laptop, [unlock]);
    }
    expect(await failure(() => t.ask(laptop, [unlock]))).toEqual({ status: 429, code: "GATE_ASK_LIMIT" });
    expect(t.seen).toHaveLength(10); // refused before the worker rates it
    expect(await t.ask(phone, [unlock])).toMatchObject({ needed: true });
    t.tick(3_600_000);
    expect(await t.ask(laptop, [unlock])).toMatchObject({ needed: true });
  });

  it("five asks in a row that go unanswered pause the phone for 5 minutes; other channels still ask", async () => {
    const p = phones();
    let others: string[] = [];
    const t = setup(typed, [], 5, { link: () => link, phone: p.phone, channels: () => others });
    for (let i = 0; i < 3; i++) {
      const a = await t.ask(laptop, [unlock]);
      t.tick(180_000);
      expect(await t.op(laptop, a.ask_id, "wait")).toEqual({ state: "expired" });
    }
    const b = await t.ask(laptop, [unlock]);
    t.tick(40_000);
    await t.op(laptop, b.ask_id, "cancel"); // an MCP tool stops waiting at 40 s, unanswered: the fourth
    const c = await t.ask(laptop, [unlock]);
    t.tick(14_000);
    await t.op(laptop, c.ask_id, "cancel"); // the computer's own cancel within 15 s (its window, or the AI): neither counts nor resets
    await t.ask(laptop, [unlock]); // replaced by the next at once: neither counts nor resets
    const e = await t.ask(laptop, [unlock]);
    t.tick(180_000);
    expect(await t.op(laptop, e.ask_id, "wait")).toEqual({ state: "expired" }); // the fifth
    expect(await failure(() => t.ask(laptop, [unlock]))).toEqual({ status: 429, code: "GATE_PAUSED" });
    expect(p.calls).toHaveLength(7);
    others = ["page"];
    expect(await t.ask(laptop, [unlock])).toMatchObject({ channels: ["page"] });
    expect(p.calls).toHaveLength(7);
    t.tick(300_000);
    const f = await t.ask(laptop, [unlock]);
    expect(f.channels).toEqual(["phone", "page"]);
    p.calls[7]!.answer({ answer: "approved", user_id: "markus" });
    await settled();
    expect(await t.op(laptop, f.ask_id, "wait")).toMatchObject({ state: "approved" });
  });

  it("a person's answer resets the run: a decline on the phone or the page, and an approval", async () => {
    const p = phones();
    const t = setup(typed, ["page"], 5, { link: () => link, phone: p.phone });
    const expireFour = async () => {
      for (let i = 0; i < 4; i++) {
        const a = await t.ask(laptop, [unlock]);
        t.tick(180_000);
        await t.op(laptop, a.ask_id, "wait");
      }
    };
    await expireFour();
    await t.ask(laptop, [unlock]);
    p.calls.at(-1)!.answer({ answer: "declined" });
    await settled();
    t.tick(3_600_000); // a new hour for the hourly limit, not for the run
    await expireFour();
    const d = await t.ask(laptop, [unlock]);
    expect(t.asks.cancel(askRef(String(d.ask_id)))).toBe(true); // declined on the NOVA page
    t.tick(3_600_000);
    await expireFour();
    expect(t.asks.pausedDevices()).toEqual([]);
    expect((await t.ask(laptop, [unlock])).channels).toEqual(["phone", "page"]);
    t.tick(180_000);
    t.asks.waiting(); // the fifth in a row
    expect(t.asks.pausedDevices()).toMatchObject([{ deviceId: "laptop", until: t.now() + 300_000 }]);
    expect(t.asks.pausedDevices()[0]!.misses.map((m) => m.what)).toEqual(Array(5).fill(typed.items[0]!.text));
    expect(t.asks.resume("laptop")).toBe(true); // the NOVA page's "ask on the phone again"
    expect(t.asks.resume("laptop")).toBe(false);
    expect(t.asks.pausedDevices()).toEqual([]);
    expect((await t.ask(laptop, [unlock])).channels).toEqual(["phone", "page"]);
  });

  it("asks the worker rated count toward the hour, whatever it answered", async () => {
    const t = setup({ verdict: "pass", items: [{ verdict: "pass" }] });
    for (let i = 0; i < 10; i++) {
      expect(await t.ask(laptop, [unlock])).toEqual({ needed: false });
    }
    expect(await failure(() => t.ask(laptop, [unlock]))).toEqual({ status: 429, code: "GATE_ASK_LIMIT" });
  });

  it("a tap that arrives after the ask's three minutes grants nothing", async () => {
    const p = phones();
    const t = setup(typed, [], 5, { link: () => link, phone: p.phone });
    const opened = await t.ask(laptop, [unlock]);
    t.tick(180_000);
    p.calls[0]!.answer({ answer: "approved", user_id: "markus" });
    await settled();
    expect(await t.op(laptop, opened.ask_id, "wait")).toEqual({ state: "expired" });
  });

  it("an ask expires on its own and hangs up its phone, polled or not", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const p = phones();
    const t = setup(typed, [], 5, { link: () => link, phone: p.phone });
    await t.ask(laptop, [unlock]);
    vi.advanceTimersByTime(180_000);
    expect(p.calls[0]!.stop.aborted).toBe(true);
  });

  it("an ask belongs to its device; the App's own credentials can't ask", async () => {
    const t = setup();
    const opened = await t.ask(laptop, [unlock]);
    expect(await status(() => t.op(phone, opened.ask_id, "wait"))).toBe(404);
    expect(await status(() => t.op(laptop, "made-up", "wait"))).toBe(404);
    expect(await status(() => t.op(laptop, opened.ask_id, "approve"))).toBe(400);
    expect(await status(() => t.ask({ kind: "device", deviceId: "w", internal: "gate", peer: "127.0.0.1" }, [unlock]))).toBe(404);
    expect(await status(() => t.op(undefined, opened.ask_id, "wait"))).toBe(404);
  });

  it("a phone link is set only on the new phone's tap, after the tap of the phone linked now", async () => {
    const p = phones();
    let current: PhoneLink | undefined;
    const sets: unknown[][] = [];
    let accept = true;
    const t = setup(typed, [], 5, {
      link: (who) => (who.kind === "device" && who.deviceId === "laptop" ? current : undefined),
      phone: p.phone,
      setLink: (...a) => (sets.push(a), accept),
    });
    expect(await status(() => t.linkTo({ kind: "shared" }, link))).toBe(404);
    expect(await status(() => setup().linkTo(laptop, link))).toBe(404); // no registry: no link route
    for (const bad of [{ ...link, service: "persistent_notification" }, { ...link, platform: "x".repeat(600_000) }, { ...link, device_id: "d".repeat(129) }, { ...link, user_id: "" }]) {
      expect(await status(() => t.linkTo(laptop, bad))).toBe(400); // the registry could not load it again
    }
    const first = await t.linkTo(laptop, link);
    expect(first).toMatchObject({ needed: true, channels: ["phone"] });
    expect(p.calls[0]).toMatchObject({ link, text: { words: "link", device: "device laptop" }, waitMs: 180_000 });
    expect(t.asks.approve(askRef(String(first.ask_id)))).toBe(false); // the page can't set a link
    p.calls[0]!.answer({ answer: "approved", user_id: "markus" });
    await settled();
    expect(await t.op(laptop, first.ask_id, "wait")).toEqual({ state: "linked" });
    expect(sets).toEqual([[laptop, link, undefined]]);
    current = link;
    expect(await t.linkTo(laptop, link)).toEqual({ needed: false });
    const moved = { service: "mobile_app_pixel", device_id: "ha-pixel", user_id: "anna" };
    const second = await t.linkTo(laptop, moved);
    expect(p.calls[1]!.text).toEqual({ words: "link", device: "device laptop", current: link });
    accept = false; // the link changed meanwhile
    p.calls[1]!.answer({ answer: "approved", user_id: "anna" });
    await settled();
    expect(await t.op(laptop, second.ask_id, "wait")).toEqual({ state: "cancelled" });
    for (const [answer, state] of [[null, "unavailable"], [{ answer: "declined" }, "cancelled"], [{ answer: "approved", user_id: "markus" }, "waiting"]] as const) {
      const opened = await t.linkTo(laptop, moved);
      p.calls.at(-1)!.answer(answer);
      await settled();
      expect(await t.op(laptop, opened.ask_id, "wait")).toMatchObject({ state });
    }
    expect(sets).toHaveLength(2);
  });

  it("a link ask waits for an ask being prepared, and a link that can't be saved sets nothing", async () => {
    const p = phones();
    let release: (a: PreparedAsk) => void = () => {};
    const t = setup(() => new Promise<PreparedAsk>((r) => (release = r)), [], 5, { link: () => undefined, phone: p.phone, setLink: () => { throw new Error("ENOSPC"); } });
    const preparing = t.ask(laptop, [unlock]);
    await settled();
    expect(await failure(() => t.linkTo(laptop, link))).toEqual({ status: 429, code: "GATE_ASK_BUSY" });
    release({ verdict: "pass", items: [{ verdict: "pass" }] });
    await preparing;
    const opened = await t.linkTo(laptop, link);
    p.calls[0]!.answer({ answer: "approved", user_id: "markus" });
    await settled();
    expect(await t.op(laptop, opened.ask_id, "wait")).toEqual({ state: "cancelled" });
  });

  it("link asks count toward the hour and the phone pause", async () => {
    const p = phones();
    const t = setup(typed, [], 5, { link: () => undefined, phone: p.phone, setLink: () => true });
    for (let i = 0; i < 5; i++) {
      const opened = await t.linkTo(laptop, link);
      t.tick(180_000);
      await t.op(laptop, opened.ask_id, "wait");
    }
    expect(await failure(() => t.linkTo(laptop, link))).toEqual({ status: 429, code: "GATE_PAUSED" }); // counted too
    t.tick(3_600_000);
    for (let i = 0; i < 10; i++) {
      await t.linkTo(laptop, link);
      p.calls.at(-1)!.answer({ answer: "approved", user_id: "markus" });
      await settled();
    }
    expect(await failure(() => t.linkTo(laptop, link))).toEqual({ status: 429, code: "GATE_ASK_LIMIT" });
    expect(p.calls).toHaveLength(15);
  });

  it("the gate forwards a typed request only with a matching, unused grant", async () => {
    let forwarded = 0;
    const worker: GateClient = {
      check: async () => ({ verdict: "typed", kind: "write" }),
      redact: async (_e, _req, response) => response,
      ask: async () => typed,
    };
    const { gate, asks, ask } = gateWithAsks(worker, { prepare: {}, deviceName: () => "Mac", channels: () => ["page"], link: () => undefined, now: () => 1 });
    const handler = gated("core", () => {
      forwarded += 1;
      return { status: 200, body: {} };
    }, gate);
    const run = async (headers: Record<string, string>) => {
      const c = context(laptop, unlock, headers);
      (c as { response: unknown }).response = { setHeader() {}, end() {}, statusCode: 0 };
      return await status(() => handler(c) as Promise<unknown>);
    };
    expect(await run({})).toBe(428);
    const opened = (await ask(context(laptop, { requests: [{ endpoint: "core", body_b64: Buffer.from(JSON.stringify(unlock)).toString("base64") }] }))) as Record<string, string>;
    asks.approve(askRef(opened.ask_id!));
    const grant = String(((await ask(context(laptop, { ask_id: opened.ask_id, op: "wait" }))) as Record<string, unknown>).grant_id);
    expect(gateDigest("core", Buffer.from(JSON.stringify(unlock)))).toHaveLength(64);
    expect(await run({ "x-ha-nova-grant": "wrong" })).toBe(428);
    expect(await run({ "x-ha-nova-grant": grant })).toBe(200);
    expect(await run({ "x-ha-nova-grant": grant })).toBe(428);
    expect(forwarded).toBe(1);
  });
});
