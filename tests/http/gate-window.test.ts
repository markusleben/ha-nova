import type { IncomingMessage, ServerResponse } from "node:http";
import { describe, expect, it, vi } from "vitest";
import { askRef, createGateAsks, type GateAsksDeps, type PhoneAnswer, type PreparedAsk } from "../../nova/src/http/gate-asks.js";
import type { RouteContext } from "../../nova/src/http/router.js";
import type { Principal } from "../../nova/src/security/principal.js";

const principal: Principal = { kind: "device", deviceId: "computer" };
const bytes = Buffer.from(JSON.stringify({ type: "call_service", domain: "lock", service: "unlock", target: { entity_id: "lock.front" } }));
const prepared: PreparedAsk = { verdict: "typed", lang: "en", title: "Unlock Front?", body: "Front door", button: "Unlock", items: [{ verdict: "typed", text: "Unlock Front" }] };
const link = { service: "mobile_app_phone", device_id: "phone", user_id: "owner" };

function context(body: unknown, who = principal, path = "/gate/v1/ask"): RouteContext {
  return { request: {} as IncomingMessage, response: {} as ServerResponse, path, body, principal: who };
}

function setup(extra: Partial<GateAsksDeps> = {}) {
  let time = 1000;
  const window = vi.fn(async () => true);
  const asks = createGateAsks({
    ask: async () => prepared, prepare: {}, deviceName: () => "Computer", channels: () => ["page"],
    link: () => undefined, phone: async () => null, now: () => time, window,
    ...extra,
  });
  const open = async () => await asks.handler(context({ requests: [{ endpoint: "ws", body_b64: bytes.toString("base64") }] })) as { ask_id: string; channels: string[] };
  const op = async (ask_id: string, op: string, who = principal) => await asks.handler(context({ ask_id, op }, who)) as { state: string; grant_id?: string };
  return { asks, window, open, op, tick: (ms: number) => { time += ms; } };
}

describe("the owner's window approves its own exact ask", () => {
  it("offers the window only after a positive eligibility check", async () => {
    expect((await setup().open()).channels).toEqual(["page", "window"]);
    expect((await setup({ window: async () => false }).open()).channels).toEqual(["page"]);
    expect((await setup({ window: async () => { throw new Error("owner read failed"); } }).open()).channels).toEqual(["page"]);
  });

  it("uses the existing exact-byte, single-use grant", async () => {
    const s = setup();
    const { ask_id } = await s.open();
    const approved = await s.op(ask_id, "window");
    expect(approved.state).toBe("approved");
    expect(s.window).toHaveBeenCalledTimes(2);
    const grant = approved.grant_id!;
    expect(s.asks.consume({ kind: "device", deviceId: "other" }, grant, "ws", bytes)).toBe(false);
    expect(s.asks.consume(principal, grant, "core", bytes)).toBe(false);
    expect(s.asks.consume(principal, grant, "ws", Buffer.from(bytes.toString() + " "))).toBe(false);
    expect(s.asks.consume(principal, grant, "ws", bytes)).toBe(true);
    expect(s.asks.consume(principal, grant, "ws", bytes)).toBe(false);
    expect(await s.op(ask_id, "window")).toEqual(approved);
  });

  it("keeps preparation locked while the window check waits", async () => {
    let finish!: (value: boolean) => void;
    const s = setup({ window: () => new Promise((resolve) => { finish = resolve; }) });
    const first = s.open();
    await Promise.resolve();
    await expect(s.open()).rejects.toMatchObject({ status: 429, code: "GATE_ASK_BUSY" });
    finish(true);
    const opened = await first;
    expect(s.asks.waiting().map((ask) => ask.ref)).toEqual([askRef(opened.ask_id)]);
  });

  it("never accepts another principal's or an unknown ask", async () => {
    const s = setup();
    const { ask_id } = await s.open();
    await expect(s.op(ask_id, "window", { kind: "device", deviceId: "other" })).rejects.toMatchObject({ status: 404 });
    await expect(s.op("unknown", "window")).rejects.toMatchObject({ status: 404 });
    await expect(s.op(ask_id, "window", { kind: "shared" })).rejects.toMatchObject({ status: 404 });
    await expect(s.op(ask_id, "window", { ...principal, internal: "gate" })).rejects.toMatchObject({ status: 404 });
  });

  it.each(["off", "unavailable"])("leaves the ask open when eligibility is %s", async (why) => {
    const s = setup();
    const { ask_id } = await s.open();
    if (why === "off") s.window.mockResolvedValue(false);
    else s.window.mockRejectedValue(new Error("owner read failed"));
    await expect(s.op(ask_id, "window")).rejects.toMatchObject({ status: why === "off" ? 409 : 503 });
    expect(s.asks.waiting()).toHaveLength(1);
    expect(s.asks.approve(askRef(ask_id))).toBe(true);
  });

  it.each(["cancel", "page", "expire"])("does not override a racing %s while the owner read waits", async (answer) => {
    let finish!: (value: boolean) => void;
    const s = setup();
    const { ask_id } = await s.open();
    s.window.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const pending = s.op(ask_id, "window");
    if (answer === "cancel") await s.op(ask_id, "cancel");
    else if (answer === "page") s.asks.approve(askRef(ask_id));
    else s.tick(180_000);
    finish(true);
    expect((await pending).state).toBe(answer === "cancel" ? "cancelled" : answer === "page" ? "approved" : "expired");
  });

  it("returns the phone's winning grant even if its owner read later fails", async () => {
    let phoneAnswer!: (answer: PhoneAnswer) => void;
    const s = setup({ link: () => link, phone: () => new Promise((resolve) => { phoneAnswer = resolve; }) });
    const { ask_id } = await s.open();
    let fail!: (reason: Error) => void;
    s.window.mockImplementation(() => new Promise((_resolve, reject) => { fail = reject; }));
    const pending = s.op(ask_id, "window");
    phoneAnswer({ answer: "approved", user_id: link.user_id });
    await Promise.resolve();
    fail(new Error("offline"));
    expect((await pending).state).toBe("approved");
  });

  it("keeps the window available when the phone is the only other channel and fails", async () => {
    const s = setup({ channels: () => [], link: () => link, phone: async () => ({ answer: "unavailable" }) });
    const { ask_id } = await s.open();
    await Promise.resolve();
    expect(s.asks.waiting()).toHaveLength(1);
    expect((await s.op(ask_id, "window")).state).toBe("approved");
  });

  it("never approves a phone-link ask through the window", async () => {
    const s = setup({ setLink: () => true, phone: () => new Promise(() => undefined) });
    const { ask_id } = await s.asks.handler(context({ link }, principal, "/gate/v1/phone")) as { ask_id: string };
    await expect(s.op(ask_id, "window")).rejects.toMatchObject({ status: 409, code: "GATE_WINDOW_OFF" });
  });

  it("lets only a device turn its own window off, with no turn-on op", async () => {
    const off = vi.fn(() => true);
    const s = setup({ windowOff: off });
    expect(await s.asks.handler(context({ op: "window_off" }))).toEqual({ window: false });
    expect(off).toHaveBeenCalledWith(principal);
    await expect(s.asks.handler(context({ op: "window_off" }, { kind: "shared" }))).rejects.toMatchObject({ status: 404 });
    await expect(s.asks.handler(context({ op: "window_on" }))).rejects.toMatchObject({ status: 404 });
  });
});
