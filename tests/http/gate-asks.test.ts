import { createServer, request as httpRequest, type IncomingMessage, type ServerResponse } from "node:http";
import type { AddressInfo } from "node:net";
import { describe, expect, it } from "vitest";

import { createGateAsks, gateDigest, gateWithAsks, type GateRequest, type PreparedAsk } from "../../nova/src/http/gate-asks.js";
import { gated, type GateClient } from "../../nova/src/http/gate.js";
import { HttpError } from "../../nova/src/http/errors.js";
import type { RouteContext } from "../../nova/src/http/router.js";
import type { Principal } from "../../nova/src/security/principal.js";

const laptop: Principal = { kind: "device", deviceId: "laptop" };
const phone: Principal = { kind: "device", deviceId: "phone" };
const unlock = { method: "POST", path: "/api/services/lock/unlock", body: { entity_id: ["lock.front"] } };
const typed: PreparedAsk = { verdict: "typed", lang: "en", title: "Unlock “Front”?", body: "Requested by “Mac”.", button: "Unlock", items: [{ verdict: "typed", text: "Unlock “Front”" }] };

function context(principal: Principal | undefined, body: unknown, headers: Record<string, string> = {}): RouteContext {
  const request = { headers, once: () => request, off: () => request } as unknown as IncomingMessage;
  const response = { destroyed: false, once: () => response, off: () => response } as unknown as ServerResponse;
  return { request, response, path: "/gate/v1/ask", body, ...(principal ? { principal } : {}) };
}

function setup(answer: PreparedAsk | null | (() => Promise<PreparedAsk | null>) = typed, channels: string[] = ["phone"], pollMs = 5) {
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
    now: () => now,
    pollMs,
  });
  const ask = async (principal: Principal, requests: unknown[]) =>
    (await asks.handler(context(principal, { requests: requests.map((body) => ({ endpoint: "core", body_b64: Buffer.from(JSON.stringify(body)).toString("base64") })) }))) as Record<string, unknown>;
  const op = async (principal: Principal | undefined, askId: unknown, name: string) => (await asks.handler(context(principal, { ask_id: askId, op: name }))) as Record<string, unknown>;
  return { asks, ask, op, seen, tick: (ms: number) => (now += ms) };
}

async function status(run: () => Promise<unknown>): Promise<number> {
  try {
    await run();
    return 200;
  } catch (error) {
    return (error as HttpError).status;
  }
}

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
    expect(opened).toMatchObject({ needed: true, channels: ["phone"] });
    expect(t.seen[0]!.device).toBe("device laptop");
    const sent = t.seen[0]!.requests[0]!.body; // as the Relay prepares it
    expect(JSON.parse(sent.toString("utf8")).method).toBe("POST");
    expect(await t.op(laptop, opened.ask_id, "wait")).toMatchObject({ state: "waiting" });
    expect(t.asks.approve(String(opened.ask_id))).toBe(true);
    const approved = await t.op(laptop, opened.ask_id, "wait");
    expect(approved).toMatchObject({ state: "approved" });
    const grant = String(approved.grant_id);
    expect(t.asks.consume(phone, grant, "core", sent)).toBe(false); // another device
    expect(t.asks.consume(laptop, grant, "core", Buffer.from(JSON.stringify({ ...unlock, body: { entity_id: ["lock.back"] } })))).toBe(false); // other bytes
    expect(t.asks.consume(laptop, grant, "ws", sent)).toBe(false); // another endpoint
    expect(t.asks.consume(laptop, grant, "core", sent)).toBe(true);
    expect(t.asks.consume(laptop, grant, "core", sent)).toBe(false); // used
    expect(t.asks.approve(String(opened.ask_id))).toBe(false); // approved once
  });

  it("a grant ends after two minutes, an ask after three", async () => {
    const t = setup();
    const first = await t.ask(laptop, [unlock]);
    t.asks.approve(String(first.ask_id));
    const grant = String((await t.op(laptop, first.ask_id, "wait")).grant_id);
    t.tick(120_001);
    expect(t.asks.consume(laptop, grant, "core", t.seen[0]!.requests[0]!.body)).toBe(false);
    const second = await t.ask(laptop, [unlock]);
    t.tick(180_000);
    expect(await t.op(laptop, second.ask_id, "wait")).toEqual({ state: "expired" });
    expect(t.asks.approve(String(second.ask_id))).toBe(false);
  });

  it("a cancel ends the ask for good; a new ask replaces the open one", async () => {
    const t = setup();
    const first = await t.ask(laptop, [unlock]);
    const second = await t.ask(laptop, [unlock]);
    expect(await t.op(laptop, first.ask_id, "wait")).toEqual({ state: "cancelled" });
    expect(await t.op(laptop, second.ask_id, "cancel")).toEqual({ state: "cancelled" });
    expect(t.asks.approve(String(second.ask_id))).toBe(false);
    expect(t.asks.approve(String(first.ask_id))).toBe(false);
  });

  it("a grant covers only the requests the person was shown", async () => {
    const t = setup({ verdict: "typed", lang: "en", title: "Run 2 steps?", body: "Requested by “Mac”.", button: "Run", items: [{ verdict: "pass" }, { verdict: "typed", text: "Unlock “Front”" }] });
    const reload = { method: "POST", path: "/api/services/script/reload", body: {} };
    const opened = await t.ask(laptop, [reload, unlock]);
    t.asks.approve(String(opened.ask_id));
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

  it("an ask belongs to its device; the App's own credentials can't ask", async () => {
    const t = setup();
    const opened = await t.ask(laptop, [unlock]);
    expect(await status(() => t.op(phone, opened.ask_id, "wait"))).toBe(404);
    expect(await status(() => t.op(laptop, "made-up", "wait"))).toBe(404);
    expect(await status(() => t.op(laptop, opened.ask_id, "approve"))).toBe(400);
    expect(await status(() => t.ask({ kind: "device", deviceId: "w", internal: "gate", peer: "127.0.0.1" }, [unlock]))).toBe(404);
    expect(await status(() => t.op(undefined, opened.ask_id, "wait"))).toBe(404);
  });

  it("the gate forwards a typed request only with a matching, unused grant", async () => {
    let forwarded = 0;
    const worker: GateClient = {
      check: async () => ({ verdict: "typed", kind: "write" }),
      redact: async (_e, _req, response) => response,
      ask: async () => typed,
    };
    const { gate, asks, ask } = gateWithAsks(worker, { prepare: {}, deviceName: () => "Mac", channels: () => ["phone"], now: () => 1 });
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
    asks.approve(opened.ask_id!);
    const grant = String(((await ask(context(laptop, { ask_id: opened.ask_id, op: "wait" }))) as Record<string, unknown>).grant_id);
    expect(gateDigest("core", Buffer.from(JSON.stringify(unlock)))).toHaveLength(64);
    expect(await run({ "x-ha-nova-grant": "wrong" })).toBe(428);
    expect(await run({ "x-ha-nova-grant": grant })).toBe(200);
    expect(await run({ "x-ha-nova-grant": grant })).toBe(428);
    expect(forwarded).toBe(1);
  });
});
