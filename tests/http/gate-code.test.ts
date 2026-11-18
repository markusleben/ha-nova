// The chat code (MAR-244): an owner-chosen way for a computer nobody sits
// at to confirm its own exact ask, with four digits the App draws, shows
// once, and checks itself, bound to the owner's choice and its revision.
import type { IncomingMessage, ServerResponse } from "node:http";
import { describe, expect, it, vi } from "vitest";
import { askRef, createGateAsks, type GateAsksDeps, type PreparedAsk } from "../../nova/src/http/gate-asks.js";
import type { RouteContext } from "../../nova/src/http/router.js";
import type { Principal } from "../../nova/src/security/principal.js";
import type { CodeMode } from "../../nova/src/security/window-confirm.js";

const principal: Principal = { kind: "device", deviceId: "headless" };
const bytes = Buffer.from(JSON.stringify({ type: "call_service", domain: "lock", service: "unlock", target: { entity_id: "lock.front" } }));
const prepared: PreparedAsk = { verdict: "typed", lang: "en", title: "Unlock Front?", body: "Front door", button: "Unlock", items: [{ verdict: "typed", text: "Unlock Front" }] };

function context(body: unknown, who = principal): RouteContext {
  return { request: {} as IncomingMessage, response: {} as ServerResponse, path: "/gate/v1/ask", body, principal: who };
}

type Opened = { ask_id: string; channels: string[]; code?: string };
type State = { state: string; grant_id?: string };

function setup(extra: Partial<GateAsksDeps> = {}) {
  let time = 1000;
  const state = { mode: "on" as CodeMode, rev: 4, userId: "owner" };
  const codeOwner = vi.fn(async (_userId: string) => true);
  const windowLog = vi.fn();
  const asks = createGateAsks({
    ask: async () => prepared, prepare: {}, deviceName: () => "Headless", channels: () => ["page"],
    link: () => undefined, phone: async () => null, now: () => time, window: async () => false,
    code: () => ({ ...state }), codeOwner, windowLog,
    ...extra,
  });
  const open = async (accept: unknown = true, who = principal) => await asks.handler(context({ requests: [{ endpoint: "ws", body_b64: bytes.toString("base64") }], ...(accept === null ? {} : { accept_code: accept }) }, who)) as Opened;
  const op = async (ask_id: string, op: string, code?: unknown) => await asks.handler(context({ ask_id, op, ...(code === undefined ? {} : { code }) })) as State;
  return { asks, state, codeOwner, windowLog, open, op, tick: (ms: number) => { time += ms; } };
}

describe("the chat code on an ask", () => {
  it("draws four digits only for a CLI that asked, while the owner's choice holds", async () => {
    const s = setup();
    const opened = await s.open();
    expect(opened.channels).toEqual(["page", "code"]);
    expect(opened.code).toMatch(/^[0-9]{4}$/);
    expect(s.codeOwner).toHaveBeenCalledWith("owner");
    for (const accept of [null, false, "true", 1]) {
      const other = await setup().open(accept);
      expect(other.channels).toEqual(["page"]);
      expect(other).not.toHaveProperty("code");
    }
  });

  it.each([
    ["the owner can't be read", { codeOwner: async () => { throw new Error("read failed"); } }],
    ["the consenting user is no longer the owner", { codeOwner: async () => false }],
    ["a phone is linked", { code: () => ({ mode: "phone" as CodeMode, rev: 4, userId: "owner" }) }],
    ["the code is off", { code: () => ({ mode: "off" as CodeMode, rev: 4, userId: "" }) }],
  ])("draws none when %s", async (_, extra) => {
    const opened = await setup(extra as Partial<GateAsksDeps>).open();
    expect(opened.channels).toEqual(["page"]);
    expect(opened).not.toHaveProperty("code");
  });

  it("draws none when the choice changed while the owner was read", async () => {
    const s = setup();
    s.codeOwner.mockImplementation(async () => { s.state.rev += 2; return true; }); // off, then on again
    expect(await s.open()).not.toHaveProperty("code");
  });

  it("never shows the code again: not in wait, not on the page", async () => {
    const s = setup();
    const { ask_id, code } = await s.open();
    s.tick(180_000);
    const waited = await s.op(ask_id, "wait");
    expect(JSON.stringify([waited, s.asks.waiting()])).not.toContain(code!);
  });

  it("grants the exact ask once for the right code, logged as a chat code", async () => {
    const s = setup();
    const { ask_id, code } = await s.open();
    const approved = await s.op(ask_id, "code", code);
    expect(approved.state).toBe("approved");
    expect(s.asks.consume(principal, approved.grant_id!, "ws", bytes)).toBe(true);
    expect(s.asks.consume(principal, approved.grant_id!, "ws", bytes)).toBe(false);
    expect(s.windowLog).toHaveBeenCalledWith(expect.objectContaining({ code: true, computer: { id: "headless", name: "Headless" } }));
    expect(JSON.stringify(s.windowLog.mock.calls)).not.toContain(`"${code}"`);
    expect(await s.op(ask_id, "code", code)).toEqual(approved); // its end, never a second grant
  });

  it("refuses wrong codes, keeps the ask, and cancels on the third", async () => {
    const s = setup();
    const { ask_id, code } = await s.open();
    const wrong = code === "0000" ? "0001" : "0000";
    for (const bad of [wrong, 1234, ` ${code}`]) {
      await expect(s.op(ask_id, "code", bad)).rejects.toMatchObject({ status: 409, code: "GATE_CODE_WRONG" });
    }
    expect((await s.op(ask_id, "wait")).state).toBe("cancelled");
    expect((await s.op(ask_id, "code", code)).state).toBe("cancelled");
  });

  it("a third wrong code that races the right one wins", async () => {
    const s = setup();
    const { ask_id, code } = await s.open();
    const wrong = code === "0000" ? "0001" : "0000";
    await expect(s.op(ask_id, "code", wrong)).rejects.toMatchObject({ code: "GATE_CODE_WRONG" });
    await expect(s.op(ask_id, "code", wrong)).rejects.toMatchObject({ code: "GATE_CODE_WRONG" });
    let finish!: (ok: boolean) => void;
    s.codeOwner.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const right = s.op(ask_id, "code", code);
    await expect(s.op(ask_id, "code", wrong)).rejects.toMatchObject({ code: "GATE_CODE_WRONG" });
    finish(true);
    expect((await right).state).toBe("cancelled");
  });

  it.each([
    ["turned off", (s: ReturnType<typeof setup>) => { s.state.mode = "off"; s.state.rev += 1; }],
    ["turned off and on again", (s: ReturnType<typeof setup>) => { s.state.rev += 2; }],
    ["ended by a linked phone", (s: ReturnType<typeof setup>) => { s.state.mode = "phone"; s.state.rev += 1; }],
    ["given by another owner", (s: ReturnType<typeof setup>) => { s.state.userId = "new-owner"; s.state.rev += 1; }],
  ])("grants nothing when the choice was %s during the owner read", async (_, change) => {
    const s = setup();
    const { ask_id, code } = await s.open();
    s.codeOwner.mockImplementation(async () => { change(s); return true; });
    await expect(s.op(ask_id, "code", code)).rejects.toMatchObject({ status: 409, code: "GATE_CODE_OFF" });
    expect(s.asks.waiting()).toHaveLength(1);
  });

  it("grants nothing when the owner can't be read or changed", async () => {
    const s = setup();
    const { ask_id, code } = await s.open();
    s.codeOwner.mockRejectedValueOnce(new Error("read failed"));
    await expect(s.op(ask_id, "code", code)).rejects.toMatchObject({ status: 503, code: "GATE_UNAVAILABLE" });
    s.codeOwner.mockResolvedValueOnce(false);
    await expect(s.op(ask_id, "code", code)).rejects.toMatchObject({ status: 409, code: "GATE_CODE_OFF" });
    expect(s.asks.waiting()).toHaveLength(1);
  });

  it.each(["cancel", "page", "expire"])("a racing %s during the owner read wins", async (answer) => {
    let finish!: (ok: boolean) => void;
    const s = setup();
    const { ask_id, code } = await s.open();
    s.codeOwner.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    const pending = s.op(ask_id, "code", code);
    if (answer === "cancel") await s.op(ask_id, "cancel");
    else if (answer === "page") s.asks.approve(askRef(ask_id));
    else s.tick(180_000);
    finish(true);
    expect((await pending).state).toBe(answer === "cancel" ? "cancelled" : answer === "page" ? "approved" : "expired");
    expect(s.windowLog).not.toHaveBeenCalled();
  });

  it("an ask without a code takes none, and the owner turning it off ends waiting code asks", async () => {
    const s = setup();
    const plain = await s.open(false);
    await expect(s.op(plain.ask_id, "code", "1234")).rejects.toMatchObject({ status: 409, code: "GATE_CODE_OFF" });
    const { ask_id } = await s.open();
    s.asks.endCode("other");
    expect(s.asks.waiting().map((a) => a.ref)).toEqual([askRef(ask_id)]);
    s.asks.endCode("headless");
    expect(s.asks.waiting()).toEqual([]);
    expect((await s.op(ask_id, "wait")).state).toBe("cancelled");
  });

  it("never draws one for the shared token or the App's own connections", async () => {
    const s = setup();
    expect(await s.open(true, { kind: "shared" })).not.toHaveProperty("code");
    await expect(s.open(true, { ...principal, internal: "gate" })).rejects.toMatchObject({ status: 404 });
  });
});
