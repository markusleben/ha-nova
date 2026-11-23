import { EventEmitter } from "node:events";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createGateWorker } from "../../nova/src/runtime/gate-worker.js";

const link = { service: "mobile_app_phone", device_id: "phone", user_id: "owner" };
const text = { ask_ref: "a".repeat(64), lang: "en", title: "Unlock Front?", body: "Front door", button: "Unlock" };
const end = { ask_ref: text.ask_ref, seq: 1, outcome: "window" as const, link, lang: "en", title: text.title, computer: { id: "computer", name: "Study computer" } };

async function setup(handler: (request: IncomingMessage, response: ServerResponse, body: unknown) => void, panel?: () => Promise<string>) {
  const dir = mkdtempSync(join(process.platform === "win32" ? tmpdir() : "/tmp", "hn-gp-"));
  mkdirSync(join(dir, "gate"));
  const server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => chunks.push(chunk));
    request.on("end", () => handler(request, response, JSON.parse(Buffer.concat(chunks).toString()) as unknown));
  });
  await new Promise<void>((resolve) => server.listen(join(dir, "gate", "worker.sock"), resolve));
  const warn = vi.fn();
  const gate = createGateWorker({
    dataDir: dir, binary: process.execPath, mint: () => ({ relay_url: "https://fixture.invalid", credential: "fixture" }),
    logger: { warn, error: vi.fn() }, respawnMs: 1, ...(panel ? { panel } : {}),
    spawn: (() => {
      const child = Object.assign(new EventEmitter(), { exitCode: null, signalCode: null as string | null,
        kill: () => { child.signalCode = "SIGTERM"; child.emit("exit", null, "SIGTERM"); return true; } });
      return child;
    }) as never,
  });
  gate.start();
  const close = async () => {
    server.closeAllConnections();
    await new Promise<void>((resolve) => server.close(() => resolve()));
    rmSync(dir, { recursive: true, force: true });
  };
  return { gate, warn, close };
}

afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });

describe("the worker phone wire", () => {
  it("announces accepted exposure before the final JSON and carries the correlated ref", async () => {
    let response!: ServerResponse;
    let body: unknown;
    let received!: () => void;
    const requested = new Promise<void>((resolve) => { received = resolve; });
    const s = await setup((_request, reply, value) => { response = reply; body = value; received(); });
    try {
      let accepted!: () => void;
      const exposed = new Promise<void>((resolve) => { accepted = resolve; });
      const onSent = vi.fn(accepted);
      let finished = false;
      const pending = s.gate.phone!(link, text, 5_000, new AbortController().signal, onSent).finally(() => { finished = true; });
      await requested;
      expect(onSent).not.toHaveBeenCalled();
      response.writeHead(200, { "content-type": "application/json", "x-ha-nova-phone-sent": "1" });
      response.flushHeaders();
      await exposed;
      expect(onSent).toHaveBeenCalledTimes(1);
      expect(finished).toBe(false);
      response.end('{"answer":"declined"}');
      expect(await pending).toEqual({ answer: "declined" });
      expect(body).toEqual({ ...text, link, wait_ms: 5_000 });
    } finally { await s.close(); }
  });

  // A tap on the notification opens the ask on NOVA's panel: named only for an ask with a ref, and only a known one.
  it("names NOVA's panel for an ask's ref, never for a link or an unknown panel", async () => {
    for (const [known, words, want] of [["/app/x_ha_nova", false, "/app/x_ha_nova"], ["/", false, undefined], ["/app/x_ha_nova", true, undefined]] as const) {
      const bodies: unknown[] = [];
      const s = await setup((_request, reply, value) => { bodies.push(value); reply.writeHead(200, { "content-type": "application/json" }).end('{"answer":"none"}'); }, async () => known);
      try {
        await s.gate.phone!(link, words ? { words: "link", device: "Study computer" } : text, 5_000, new AbortController().signal);
        expect((bodies[0] as { panel?: string }).panel).toBe(want);
      } finally { await s.close(); }
    }
  });

  it("ignores an exposure header and answer from a worker that was replaced", async () => {
    let response!: ServerResponse;
    let received!: () => void;
    const requested = new Promise<void>((resolve) => { received = resolve; });
    const s = await setup((_request, reply) => { response = reply; received(); });
    try {
      const onSent = vi.fn();
      const pending = s.gate.phone!(link, text, 5_000, new AbortController().signal, onSent);
      await requested;
      s.gate.restart();
      response.writeHead(200, { "x-ha-nova-phone-sent": "1" }).end('{"answer":"approved","user_id":"owner"}');
      expect(await pending).toBeNull();
      expect(onSent).not.toHaveBeenCalled();
    } finally { await s.close(); }
  });

  it("ignores a late header after cancellation and never calls a non-200 exposure accepted", async () => {
    let response!: ServerResponse;
    let received!: () => void;
    const requested = new Promise<void>((resolve) => { received = resolve; });
    const s = await setup((_request, reply) => { response = reply; received(); });
    try {
      const onSent = vi.fn();
      const stop = new AbortController();
      const pending = s.gate.phone!(link, text, 5_000, stop.signal, onSent);
      await requested;
      stop.abort();
      response.writeHead(200, { "x-ha-nova-phone-sent": "1" }).end('{"answer":"none"}');
      expect(await pending).toBeNull();
      expect(onSent).not.toHaveBeenCalled();
      const failed = s.gate.phone!(link, text, 5_000, new AbortController().signal, onSent);
      await new Promise<void>((resolve) => { received = resolve; });
      response.writeHead(503, { "x-ha-nova-phone-sent": "1" }).end('{"answer":"unavailable"}');
      expect(await failed).toBeNull();
      expect(onSent).not.toHaveBeenCalled();
    } finally { await s.close(); }
  });

  it("releases quota only on a positive, valid never-asked answer", async () => {
    let answer: unknown;
    const s = await setup((_request, reply) => reply.writeHead(200).end(JSON.stringify(answer)));
    try {
      for (const [shape, expected] of [
        [{ answer: "none", not_asked: true }, { answer: "none", not_asked: true }],
        [{ answer: "unavailable", not_asked: true }, { answer: "unavailable", not_asked: true }],
        [{ answer: "unavailable" }, { answer: "unavailable" }],
        [{ answer: "unavailable", not_asked: false }, null],
        [{ answer: "unavailable", not_asked: "true" }, null],
        [{ answer: "approved", user_id: "owner", not_asked: true }, null],
        [{ answer: "declined", not_asked: true }, null],
      ] as const) {
        answer = shape;
        expect(await s.gate.phone!(link, text, 5_000, new AbortController().signal)).toEqual(expected);
      }
    } finally { await s.close(); }
  });

  it("records only a positive end acknowledgement and keeps its wire payload", async () => {
    let answer: unknown = { recorded: true };
    let status = 200;
    const seen: unknown[] = [];
    const s = await setup((request, reply, body) => { seen.push({ path: request.url, body }); reply.writeHead(status).end(JSON.stringify(answer)); });
    try {
      expect(await s.gate.phoneEnd!(end)).toBe(true);
      expect(seen[0]).toEqual({ path: "/phone/end", body: end });
      for (const value of [{ recorded: false }, { recorded: "true" }, {}, null]) {
        answer = value;
        expect(await s.gate.phoneEnd!(end)).toBe(false);
      }
      answer = { recorded: true }; status = 503;
      expect(await s.gate.phoneEnd!(end)).toBe(false);
      expect(s.warn).toHaveBeenCalled();
    } finally { await s.close(); }
  });

  it("ends a stalled recording at three seconds rather than the general worker timeout", async () => {
    let received!: () => void;
    const requested = new Promise<void>((resolve) => { received = resolve; });
    const s = await setup(() => received());
    vi.useFakeTimers();
    try {
      let finished = false;
      const pending = s.gate.phoneEnd!(end).finally(() => { finished = true; });
      await requested;
      await vi.advanceTimersByTimeAsync(2_999);
      expect(finished).toBe(false);
      await vi.advanceTimersByTimeAsync(1);
      expect(await pending).toBe(false);
      expect(s.warn).toHaveBeenCalled();
    } finally { await s.close(); }
  });
});
