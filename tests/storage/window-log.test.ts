// The recent window confirmations (spec §3): the last 50, kept with a
// rename; a write that fails is logged and never blocks the approval.
import { chmodSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

import { createGateAsks, type PreparedAsk } from "../../nova/src/http/gate-asks.js";
import { openWindowLog } from "../../nova/src/storage/window-log.js";

const dirs: string[] = [];
const temp = () => { const dir = mkdtempSync(join(tmpdir(), "ha-nova-window-log-")); dirs.push(dir); return dir; };
afterEach(() => { for (const dir of dirs.splice(0)) { chmodSync(dir, 0o700); rmSync(dir, { recursive: true, force: true }); } });
const entry = (i: number) => ({ atMs: i, computer: { id: "mac", name: "Mac" }, title: `Unlock ${i}?` });

describe("the window confirmation log", () => {
  it("keeps the last 50 across a restart", () => {
    const file = join(temp(), "window-log.json");
    const log = openWindowLog(file, { warn: vi.fn() });
    for (let i = 1; i <= 52; i++) log.append(entry(i));
    expect(log.list().map((e) => e.atMs)).toEqual(Array.from({ length: 50 }, (_, i) => i + 3));
    expect(openWindowLog(file, { warn: vi.fn() }).list()).toEqual(log.list());
    expect(JSON.parse(readFileSync(file, "utf8"))).toHaveLength(50);
  });

  it("starts again, logged, from a file that isn't a log; a missing file is just empty", () => {
    const dir = temp();
    const warn = vi.fn();
    expect(openWindowLog(join(dir, "none.json"), { warn }).list()).toEqual([]);
    expect(warn).not.toHaveBeenCalled();
    for (const bad of ["{", "{}", JSON.stringify([{ atMs: "1", computer: { id: "mac", name: "Mac" }, title: "x" }]), JSON.stringify([{ ...entry(1), title: "x".repeat(1025) }])]) {
      writeFileSync(join(dir, "bad.json"), bad);
      expect(openWindowLog(join(dir, "bad.json"), { warn }).list()).toEqual([]);
    }
    expect(warn).toHaveBeenCalledTimes(4);
  });

  it("logs a failed write and keeps the entry in memory", () => {
    const dir = temp();
    const warn = vi.fn();
    const log = openWindowLog(join(dir, "window-log.json"), { warn });
    chmodSync(dir, 0o500);
    expect(() => log.append(entry(1))).not.toThrow();
    expect(warn).toHaveBeenCalledWith("The window confirmation log could not be saved; the approval stands", expect.anything());
    expect(log.list()).toEqual([entry(1)]);
  });
});

describe("a window approval", () => {
  const prepared: PreparedAsk = { verdict: "typed", lang: "en", title: "Unlock Front?", body: "Front door", button: "Unlock", items: [{ verdict: "typed", text: "Unlock Front" }] };
  const bytes = Buffer.from("{}").toString("base64");
  const run = async (windowLog: (e: unknown) => void, warn = vi.fn()) => {
    const asks = createGateAsks({
      ask: async () => prepared, prepare: {}, deviceName: () => "Mac", channels: () => ["page"],
      link: () => undefined, phone: async () => null, now: () => 5000, window: async () => true, windowLog, logger: { info: vi.fn(), warn },
    });
    const ctx = (body: unknown) => ({ request: {} as IncomingMessage, response: {} as ServerResponse, path: "/gate/v1/ask", body, principal: { kind: "device" as const, deviceId: "mac" } });
    const { ask_id } = await asks.handler(ctx({ presence: "away", requests: [{ endpoint: "ws", body_b64: bytes }] })) as { ask_id: string };
    return { result: await asks.handler(ctx({ ask_id, op: "window" })) as { state: string }, warn };
  };

  it("lands in the log with its time, computer, and title", async () => {
    const seen: unknown[] = [];
    expect((await run((e) => void seen.push(e))).result.state).toBe("approved");
    expect(seen).toEqual([{ atMs: 5000, computer: { id: "mac", name: "Mac" }, title: "Unlock Front?" }]);
  });

  it("stands when the log fails", async () => {
    const { result, warn } = await run(() => { throw new Error("disk full"); });
    expect(result.state).toBe("approved");
    expect(warn).toHaveBeenCalledWith("A window confirmation could not be logged; approval stands", { error: "Error: disk full" });
  });
});
