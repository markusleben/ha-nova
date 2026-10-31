// Windows job transport for the VM lane (run.mjs). On the Win11 guest a script read from stdin
// ([Console]::In.ReadToEnd() under sshd and cmd.exe) can stay blocked after EOF, so no job reads
// stdin: each script is copied to the guest user's home and run with -File. Its first statement
// claims the file by renaming it; a job that shows no start within the window is checked by
// renaming the file away. Only one rename can win, so a won void proves the job never started.
// Only that case is retried, once; a second miss reads STALL in the table, never FAIL.
import { rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const PRELUDE = "$ProgressPreference='SilentlyContinue'\n[Console]::OutputEncoding=[Text.Encoding]::UTF8\n";

// ssh(user, cmd, opts) runs a guest command and resolves {code, out}; opts.upload copies that
// local file to cmd (a name in the user's home) instead, opts.signal aborts the call.
export function winTransport({ ssh, dir, shells, windowMs = 30_000 }) {
  let n = 0;
  const retries = [];
  const voided = async (user, name) => (await ssh(user, `powershell.exe -NoProfile -NonInteractive -Command "try { [IO.File]::Move('${name}', '${name}.void') } catch { 'vm-lane: kept'; exit }; Remove-Item -LiteralPath '${name}.void' -Force; 'vm-lane: voided'"`, { cleanup: true, timeoutMs: 60_000 })).out.includes("vm-lane: voided");

  async function attempt(user, shell, ps, o) {
    const name = `vm-lane-${process.pid}-${++n}.ps1`, started = `vm-lane: started ${name}`, local = join(dir, name);
    writeFileSync(local, `\ufefftry { [IO.File]::Move($PSCommandPath, "$PSCommandPath.run") } catch { exit 125 }
Remove-Item -LiteralPath "$PSCommandPath.run" -Force -ErrorAction SilentlyContinue
[Console]::Out.WriteLine('${started}'); [Console]::Out.Flush()
[Console]::InputEncoding=[Text.Encoding]::UTF8
${PRELUDE}${ps}
if ($?) { exit 0 } else { exit 1 }
`, { mode: 0o600 });
    let sent;
    try { sent = await ssh(user, name, { upload: local, cleanup: o.cleanup, timeoutMs: 60_000 }); } finally { rmSync(local, { force: true }); }
    if (sent.code !== 0) { await voided(user, name); return { never: true, code: sent.code, out: sent.out }; }
    const abort = new AbortController();
    let seen = false, probe;
    const timer = setTimeout(() => { probe = voided(user, name).then((won) => { if (won) abort.abort(); return won; }); }, windowMs);
    let r;
    try {
      r = await ssh(user, `"${shells[shell]}" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ${name}`, { ...o, signal: abort.signal, onData: (out, child) => {
        if (!seen && out.includes(started)) { seen = true; clearTimeout(timer); }
        o.onData?.(out, child);
      } });
    } finally { clearTimeout(timer); }
    const won = probe ? await probe : !seen && await voided(user, name);
    return { never: !seen && won, code: r.code, out: r.out.replace(new RegExp(`^${started.replaceAll(".", "\\.")}\\r?\\n`, "m"), "") };
  }

  // A terminal session gets its script on the command line (it reads keys, not a script).
  async function win(user, shell, ps, o = {}) {
    if (o.tty) return ssh(user, `"${shells[shell]}" -NoProfile -ExecutionPolicy Bypass -EncodedCommand ${Buffer.from(PRELUDE + ps, "utf16le").toString("base64")}`, o);
    let r = await attempt(user, shell, ps, o);
    if (r.never) {
      r = await attempt(user, shell, ps, o);
      retries.push(!r.never);
    }
    return r.never ? { code: 125, out: `${r.out}\n[vm-lane: STALL: the job never started on the guest, also not on its one retry]`.trim() } : { code: r.code, out: r.out };
  }

  // Each table row passes through judge: the retries since the previous judged row belong to it.
  // SKIP rows judge nothing on the guest, so the retries wait for the next row.
  function judge(result, detail) {
    if (result === "SKIP" || !retries.length) return { result, detail };
    const lost = retries.filter((ok) => !ok).length, saved = retries.length - lost;
    retries.length = 0;
    if (lost) return { result: "STALL", detail: `a guest job never started, also not on its one retry: harness state, not a product result (check read ${result}) | ${detail}` };
    return { result, detail: `[${saved} job${saved > 1 ? "s" : ""} retried: never started on the first try] ${detail}` };
  }

  return { win, judge };
}
