import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

import { describe, expect, it } from "vitest";

import { makeTempDir } from "../helpers/temp-dir.js";

// The VM lane's Windows job transport (scripts/e2e/vm-lane/transport.mjs) against a fake guest:
// no VM, no ssh. A job that never starts is retried once; a job that started never is.
type Result = { code: number | null; out: string };
type Opts = { upload?: string; signal?: AbortSignal; onData?: (out: string) => void; tty?: boolean };
type Ssh = (user: string, cmd: string, o?: Opts) => Promise<Result>;
type Transport = {
  win: (user: string, shell: string, ps: string, o?: Opts) => Promise<Result>;
  judge: (result: string, detail: string) => { result: string; detail: string };
};
const { winTransport } = (await import(pathToFileURL(join(process.cwd(), "scripts/e2e/vm-lane/transport.mjs")).href)) as {
  winTransport: (o: { ssh: Ssh; dir: string; shells: Record<string, string>; windowMs?: number }) => Transport;
};

// ok: claims its file, prints the start marker, exits 0. fail: starts, exits 1. hang: starts,
// then times out. silent: starts but its marker never arrives, then times out. stall: never
// claims its file and blocks until the lane aborts the call. refused: ssh fails before a start.
type Job = "ok" | "fail" | "hang" | "silent" | "stall" | "refused";
const WINDOW = 20;

function guest(jobs: Job[], failedUploads: number[] = []) {
  const files = new Map<string, string>();
  const log: string[] = [];
  let uploads = 0, runs = 0;
  const later = (ms: number, r: Result) => new Promise<Result>((done) => setTimeout(() => done(r), ms));
  const ssh: Ssh = async (_user, cmd, o = {}) => {
    if (o.upload) {
      log.push("upload");
      if (failedUploads.includes(uploads++)) return { code: 1, out: "scp: Connection closed" };
      files.set(cmd, readFileSync(o.upload, "utf8"));
      return { code: 0, out: "" };
    }
    const voided = cmd.match(/\[IO\.File\]::Move\('([^']+)'/)?.[1];
    if (voided) {
      log.push("void");
      return { code: 0, out: files.delete(voided) ? "vm-lane: voided" : "vm-lane: kept" };
    }
    if (o.tty) { log.push("tty"); return { code: 0, out: "terminal" }; }
    const name = cmd.match(/-File (\S+)$/)?.[1] ?? "";
    const job = jobs[runs++] ?? "ok";
    log.push(`run ${job}`);
    if (job === "refused") return { code: 255, out: "ssh: connect to host 127.0.0.1 port 2222: Connection refused" };
    if (job === "stall") return new Promise((done) => o.signal?.addEventListener("abort", () => done({ code: null, out: "" })));
    if (!files.delete(name)) return { code: 125, out: "" };
    const started = `vm-lane: started ${name}\r\n`;
    if (job === "silent") return later(WINDOW * 5, { code: null, out: "\n[vm-lane: timed out]\n" });
    o.onData?.(started);
    if (job === "hang") return later(WINDOW * 5, { code: null, out: `${started}\n[vm-lane: timed out]\n` });
    return job === "ok" ? { code: 0, out: `${started}0.27.3\r\n` } : { code: 1, out: `${started}boom\r\n` };
  };
  const dir = makeTempDir("ha-nova-vm-lane-transport-");
  const t = winTransport({ ssh, dir, shells: { ps51: "powershell.exe", ps7: "C:\\Tools\\pwsh-x64\\pwsh.exe" }, windowMs: WINDOW });
  return { ...t, files, log, dir, run: (ps = "ha-nova version") => t.win("tester", "ps51", ps) };
}

describe("vm-lane Windows job transport", () => {
  it("copies the script, runs it with -File, and returns its output without the start marker", async () => {
    const g = guest(["ok"]);
    const r = await g.run();
    expect(r).toEqual({ code: 0, out: "0.27.3\r\n" });
    expect(g.log).toEqual(["upload", "run ok"]);
    expect(g.files.size).toBe(0);
    expect(readdirSync(g.dir)).toEqual([]);
    expect(g.judge("PASS", "0.27.3")).toEqual({ result: "PASS", detail: "0.27.3" });
  });

  it("claims the job file before any payload statement and never reads stdin", async () => {
    const seen: string[] = [];
    const g = guest([]);
    const t = winTransport({ ssh: async (_u, cmd, o = {}) => {
      if (o.upload) seen.push(readFileSync(o.upload, "utf8"));
      else seen.push(cmd);
      return { code: 0, out: "" };
    }, dir: g.dir, shells: { ps51: "powershell.exe" }, windowMs: WINDOW });
    await t.win("tester", "ps51", "'payload'");
    const [script = "", run = ""] = seen;
    expect(script.startsWith("\ufefftry { [IO.File]::Move($PSCommandPath, ")).toBe(true);
    expect(script.indexOf("vm-lane: started")).toBeGreaterThan(script.indexOf("catch { exit 125 }"));
    expect(script.indexOf("'payload'")).toBeGreaterThan(script.indexOf("vm-lane: started"));
    expect(script.trimEnd().endsWith("if ($?) { exit 0 } else { exit 1 }")).toBe(true);
    expect(run).toMatch(/^"powershell\.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File vm-lane-\d+-\d+\.ps1$/);
    expect(seen.join("\n")).not.toMatch(/ReadToEnd|Console\]::In\b/);
  });

  it("retries a job that provably never started, once, and notes it on the next row", async () => {
    const g = guest(["stall", "ok"]);
    const r = await g.run();
    expect(r).toEqual({ code: 0, out: "0.27.3\r\n" });
    expect(g.log).toEqual(["upload", "run stall", "void", "upload", "run ok"]);
    expect(g.files.size).toBe(0);
    expect(g.judge("SKIP", "Codex CLI is not installed")).toEqual({ result: "SKIP", detail: "Codex CLI is not installed" });
    expect(g.judge("PASS", "0.27.3")).toEqual({ result: "PASS", detail: "[1 job retried: never started on the first try] 0.27.3" });
    expect(g.judge("PASS", "next row")).toEqual({ result: "PASS", detail: "next row" });
  });

  it("reads STALL, not FAIL, when the retry never starts either", async () => {
    const g = guest(["stall", "stall"]);
    const r = await g.run();
    expect(r.code).toBe(125);
    expect(r.out).toContain("STALL");
    expect(g.log).toEqual(["upload", "run stall", "void", "upload", "run stall", "void"]);
    expect(g.files.size).toBe(0);
    const row = g.judge("FAIL", "version mismatch");
    expect(row.result).toBe("STALL");
    expect(row.detail).toContain("never started");
    expect(row.detail).toContain("check read FAIL");
    expect(g.judge("PASS", "next row").result).toBe("PASS");
  });

  it("retries when ssh fails before the job starts and the file is still unclaimed", async () => {
    const g = guest(["refused", "ok"]);
    expect(await g.run()).toEqual({ code: 0, out: "0.27.3\r\n" });
    expect(g.log).toEqual(["upload", "run refused", "void", "upload", "run ok"]);
  });

  it("retries an upload that failed without ever running it", async () => {
    const g = guest(["ok"], [0]);
    expect(await g.run()).toEqual({ code: 0, out: "0.27.3\r\n" });
    expect(g.log).toEqual(["upload", "void", "upload", "run ok"]);
    expect(readdirSync(g.dir)).toEqual([]);
  });

  it.each([
    ["fails", "fail" as Job, ["upload", "run fail"], 1],
    ["times out after its start marker", "hang" as Job, ["upload", "run hang"], null],
    ["started but its marker was lost", "silent" as Job, ["upload", "run silent", "void"], null],
  ])("never retries a job that started and then %s", async (_name, job, log, code) => {
    const g = guest([job, "ok"]);
    const r = await g.run();
    expect(r.code).toBe(code);
    expect(r.out).not.toContain("STALL");
    expect(g.log).toEqual(log);
    expect(g.judge("FAIL", "product failure")).toEqual({ result: "FAIL", detail: "product failure" });
  });

  it("keeps terminal sessions on the encoded command line", async () => {
    const g = guest([]);
    const calls: string[] = [];
    const t = winTransport({ ssh: async (_u, cmd, o = {}) => { calls.push(o.upload ? "upload" : cmd); return { code: 0, out: "" }; }, dir: g.dir, shells: { ps7: "pwsh.exe" } });
    await t.win("nova7-abc123", "ps7", "'hi'", { tty: true });
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatch(/^"pwsh\.exe" -NoProfile -ExecutionPolicy Bypass -EncodedCommand \S+$/);
    const encoded = calls[0]?.split(" ").at(-1) ?? "";
    expect(Buffer.from(encoded, "base64").toString("utf16le")).toContain("'hi'");
  });
});
