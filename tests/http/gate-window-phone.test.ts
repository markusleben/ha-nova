import type { IncomingMessage, ServerResponse } from "node:http";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { askRef, createGateAsks, type GateAsksDeps, type PhoneAnswer, type PhoneText, type PreparedAsk } from "../../nova/src/http/gate-asks.js";
import { createOwnerLimits } from "../../nova/src/http/gate-owners.js";
import type { RouteContext } from "../../nova/src/http/router.js";
import type { Principal } from "../../nova/src/security/principal.js";

const principal: Principal = { kind: "device", deviceId: "computer" };
const link = { service: "mobile_app_phone", device_id: "phone", user_id: "owner" };
const prepared: PreparedAsk = { verdict: "typed", lang: "en", title: "Unlock Front?", body: "Front door", button: "Unlock", items: [{ verdict: "typed", text: "Unlock Front" }] };
const requests = [{ endpoint: "ws", body_b64: Buffer.from('{"type":"call_service","domain":"lock","service":"unlock"}').toString("base64") }];

function context(body: unknown): RouteContext {
  return { request: {} as IncomingMessage, response: {} as ServerResponse, path: "/gate/v1/ask", body, principal };
}

function setup(extra: Partial<GateAsksDeps> = {}, autoSent = true) {
  const calls: { text: PhoneText; wait: number; stop: AbortSignal; sent: () => void; answer: (a: PhoneAnswer | null) => void }[] = [];
  const phoneEnd = vi.fn(async () => true);
  const warn = vi.fn();
  const asks = createGateAsks({
    ask: async () => prepared, prepare: {}, deviceName: () => "Study computer", channels: () => ["page"],
    link: () => link, window: async () => true, now: () => Date.now(), phoneEnd, logger: { warn },
    phone: (_link, text, wait, stop, onSent) => new Promise((answer) => {
      const sent = () => onSent?.();
      calls.push({ text: text as PhoneText, wait, stop, sent, answer });
      if (autoSent) sent();
    }),
    ...extra,
  });
  const open = async (presence?: unknown) => await asks.handler(context({ requests, ...(presence === undefined ? {} : { presence }) })) as { ask_id: string; channels: string[]; expires_at: number };
  const op = async (ask_id: string, op: string) => await asks.handler(context({ ask_id, op })) as { state: string; grant_id?: string };
  return { asks, open, op, calls, phoneEnd, warn };
}

beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date("2026-11-16T00:00:00Z")); });
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });

describe("window and phone orchestration", () => {
  it.each([["away", 0], ["unknown", 20_000], ["present", 60_000]] as const)("asks after the delay for %s and keeps the absolute deadline", async (presence, delay) => {
    const s = setup();
    const opened = await s.open(presence);
    expect(opened.channels).toEqual(["phone", "page", "window"]);
    if (delay > 0) {
      await vi.advanceTimersByTimeAsync(delay - 1);
      expect(s.calls).toHaveLength(0);
      await vi.advanceTimersByTimeAsync(1);
    }
    expect(s.calls).toHaveLength(1);
    expect(s.calls[0]!.wait).toBe(180_000 - delay);
    expect(s.calls[0]!.text.ask_ref).toBe(askRef(opened.ask_id));
    await vi.advanceTimersByTimeAsync(180_000 - delay);
    expect(s.calls[0]!.stop.aborted).toBe(true);
    expect(s.asks.waiting()).toEqual([]);
  });

  it("asks immediately without a window or without presence, and accelerates an away report only once", async () => {
    const without = setup({ window: async () => false });
    expect((await without.open("present")).channels).toEqual(["phone", "page"]);
    expect(without.calls).toHaveLength(1);
    const silent = setup(); // a chat app, SSH without a display, or a CLI that shows no window sends no presence
    expect((await silent.open()).channels).toEqual(["phone", "page", "window"]);
    expect(silent.calls).toHaveLength(1);
    expect(silent.calls[0]!.wait).toBe(180_000);
    const s = setup();
    const opened = await s.open("present");
    await vi.advanceTimersByTimeAsync(20_000);
    await s.op(opened.ask_id, "away");
    await s.op(opened.ask_id, "away");
    expect(s.calls).toHaveLength(1);
    expect(s.calls[0]!.wait).toBe(160_000);
    await vi.advanceTimersByTimeAsync(40_000);
    expect(s.calls).toHaveLength(1);
  });

  it.each(["window", "page", "cancel", "expired"])("never dispatches a delayed phone after %s", async (end) => {
    const s = setup();
    const opened = await s.open("present");
    if (end === "page") s.asks.approve(askRef(opened.ask_id));
    else if (end === "expired") vi.setSystemTime(Date.now() + 180_000);
    else await s.op(opened.ask_id, end);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(s.calls).toHaveLength(0);
    expect(s.asks.waiting()).toEqual([]);
  });

  it("replaces only the old delay and never sends its phone", async () => {
    const s = setup();
    await s.open("present");
    await vi.advanceTimersByTimeAsync(20_000);
    const newer = await s.open("present");
    await vi.advanceTimersByTimeAsync(40_000);
    expect(s.calls).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(20_000);
    expect(s.calls).toHaveLength(1);
    expect(s.calls[0]!.text.ask_ref).toBe(askRef(newer.ask_id));
  });

  it("reserves the winner before recording its end, then aborts the old phone", async () => {
    let finish!: (value: boolean) => void;
    const end = vi.fn(() => new Promise<boolean>((resolve) => { finish = resolve; }));
    const s = setup({ phoneEnd: end });
    const opened = await s.open("away");
    const approved = await s.op(opened.ask_id, "window");
    expect(approved.state).toBe("approved");
    expect(s.calls[0]!.stop.aborted).toBe(false);
    expect(end).toHaveBeenCalledExactlyOnceWith({ ask_ref: askRef(opened.ask_id), seq: 1, outcome: "window", link,
      lang: "en", title: prepared.title, computer: { id: "computer", name: "Study computer" } });
    expect(s.asks.approve(askRef(opened.ask_id))).toBe(false);
    s.calls[0]!.answer({ answer: "approved", user_id: "owner" });
    await vi.advanceTimersByTimeAsync(0);
    expect(await s.op(opened.ask_id, "window")).toEqual(approved);
    expect(end).toHaveBeenCalledTimes(1);
    finish(true);
    await vi.advanceTimersByTimeAsync(0);
    expect(s.calls[0]!.stop.aborted).toBe(true);
  });

  it.each(["throw", "false"])("keeps the grant and hangs up when end recording returns %s", async (failure) => {
    const s = setup({ phoneEnd: () => { if (failure === "throw") throw new Error("worker offline"); return Promise.resolve(false); } });
    const opened = await s.open("away");
    expect((await s.op(opened.ask_id, "window")).state).toBe("approved");
    await vi.advanceTimersByTimeAsync(0);
    expect(s.calls[0]!.stop.aborted).toBe(true);
    expect(s.warn).toHaveBeenCalled();
  });

  it("sends a quiet end even when the question was never sent and sequences winning order", async () => {
    const ends: number[] = [];
    const s = setup({ phoneEnd: async (end) => { ends.push(end.seq); return true; } });
    const first = await s.open("present");
    await s.op(first.ask_id, "window");
    const second = await s.open("present");
    await Promise.all([s.op(second.ask_id, "window"), s.op(second.ask_id, "window")]);
    await vi.advanceTimersByTimeAsync(0);
    expect(s.calls).toHaveLength(0);
    expect(ends).toEqual([1, 2]);
  });

  it.each(["page", "phone", "cancel", "expired", "replace"])("aborts immediately without a window end for %s", async (end) => {
    const s = setup();
    const opened = await s.open("away");
    if (end === "page") s.asks.approve(askRef(opened.ask_id));
    else if (end === "phone") s.calls[0]!.answer({ answer: "approved", user_id: "owner" });
    else if (end === "expired") await vi.advanceTimersByTimeAsync(180_000);
    else if (end === "replace") await s.open("present");
    else await s.op(opened.ask_id, "cancel");
    await vi.advanceTimersByTimeAsync(0);
    expect(s.calls[0]!.stop.aborted).toBe(true);
    expect(s.phoneEnd).not.toHaveBeenCalled();
  });

  it("skips a capped phone while the window and page remain usable up to the computer cap", async () => {
    const s = setup();
    for (let i = 0; i < 60; i++) {
      const opened = await s.open("away");
      expect(opened.channels).toEqual(i < 10 ? ["phone", "page", "window"] : ["page", "window"]);
      expect((await s.op(opened.ask_id, "window")).state).toBe("approved");
    }
    expect(s.calls).toHaveLength(10);
    await expect(s.open()).rejects.toMatchObject({ status: 429, code: "GATE_ASK_LIMIT" });
    const linkOnly = setup({ channels: () => [], window: async () => false });
    for (let i = 0; i < 10; i++) await linkOnly.op((await linkOnly.open()).ask_id, "cancel");
    await expect(linkOnly.open()).rejects.toMatchObject({ status: 429, code: "GATE_PAUSED" }); // callers continue as after a pause
  });

  it("does not charge undelivered delays or positively unasked replies, but charges failed sends", async () => {
    const s = setup({}, false);
    for (let i = 0; i < 20; i++) {
      const opened = await s.open("present");
      await s.op(opened.ask_id, "window");
    }
    expect(s.calls).toHaveLength(0);
    for (let i = 0; i < 12; i++) {
      const opened = await s.open("away");
      s.calls.at(-1)!.answer({ answer: "unavailable", not_asked: true });
      await vi.advanceTimersByTimeAsync(0);
      await s.op(opened.ask_id, "cancel");
    }
    for (let i = 0; i < 10; i++) {
      const opened = await s.open("away");
      s.calls.at(-1)!.answer({ answer: "unavailable" });
      await vi.advanceTimersByTimeAsync(0);
      await s.op(opened.ask_id, "cancel");
    }
    expect((await s.open("away")).channels).toEqual(["page", "window"]);
    expect(s.calls).toHaveLength(22);
  });

  it.each([undefined, 14_000, 15_000])("pauses only after five actual unanswered exposures of %s ms", async (exposure) => {
    const s = setup({}, false);
    for (let i = 0; i < 5; i++) {
      const opened = await s.open("present");
      await vi.advanceTimersByTimeAsync(60_000);
      await vi.advanceTimersByTimeAsync(100_000);
      if (exposure !== undefined) s.calls.at(-1)!.sent();
      await vi.advanceTimersByTimeAsync(exposure ?? 20_000);
      await s.op(opened.ask_id, "cancel");
      s.calls.at(-1)!.sent();
    }
    expect(s.asks.pausedDevices()).toHaveLength(exposure === 15_000 ? 1 : 0);
    if (exposure === 15_000) expect((await s.open()).channels).toEqual(["page", "window"]);
  });

  it("a window answer resets prior misses even before its phone is dispatched", async () => {
    const s = setup();
    for (let i = 0; i < 4; i++) {
      const opened = await s.open("away");
      await vi.advanceTimersByTimeAsync(15_000);
      await s.op(opened.ask_id, "cancel");
    }
    await s.op((await s.open("present")).ask_id, "window");
    const last = await s.open("away");
    await vi.advanceTimersByTimeAsync(15_000);
    await s.op(last.ask_id, "cancel");
    expect(s.asks.pausedDevices()).toEqual([]);
  });

  it("validates presence before charging or rating an ask", async () => {
    const rate = vi.fn(async () => prepared);
    const s = setup({ ask: rate });
    for (let i = 0; i < 60; i++) await expect(s.open("maybe")).rejects.toMatchObject({ status: 400 });
    expect(rate).not.toHaveBeenCalled();
    expect((await s.open()).channels).toContain("window");
  });

  it("releases only its own reservation across equal timestamps, pruning and replacement", () => {
    const limits = createOwnerLimits(() => Date.now());
    const owner = "device:computer";
    const old = limits.reservePhone(owner)!;
    const current = limits.reservePhone(owner)!;
    old(); old();
    for (let i = 0; i < 9; i++) expect(limits.reservePhone(owner)).toBeTypeOf("function");
    expect(limits.reservePhone(owner)).toBeUndefined();
    old();
    expect(limits.reservePhone(owner)).toBeUndefined();
    current();
    expect(limits.reservePhone(owner)).toBeTypeOf("function");
    vi.setSystemTime(Date.now() + 3_600_000);
    expect(limits.reservePhone(owner)).toBeTypeOf("function");
    current();
    for (let i = 0; i < 9; i++) expect(limits.reservePhone(owner)).toBeTypeOf("function");
    expect(limits.reservePhone(owner)).toBeUndefined();
  });
});
