#!/usr/bin/env node
// npm run vm-lane -- <tag> [--from <stable>] [--wait <0..60>] [--no-ubuntu]
// Throwaway UTM VMs only; every HA NOVA command runs in a guest, never on macOS.
// Contract and limits: docs/work/2026-10-26-vm-lane-spec.md; release gate: docs/releasing.md §4.
import { execFileSync, spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { appendFileSync, existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import net from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { setTimeout as pause } from "node:timers/promises";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const REPO = "markusleben/ha-nova";
const fetch = (url, o = {}) => globalThis.fetch(url, { signal: AbortSignal.timeout(60_000), ...o }); // a stalled VM fails a step, never hangs the lane
const TAG_RE = /^v\d+\.\d+\.\d+(-rc\d+)?$/;
// docs/internal is gitignored: it lives in the main checkout, also when this runs from a worktree.
const MAIN = dirname(resolve(ROOT, execFileSync("git", ["-C", ROOT, "rev-parse", "--git-common-dir"], { encoding: "utf8" }).trim()));
const env = (k, d) => process.env[k] || d;
const INTERNAL = env("VM_LANE_INTERNAL", join(MAIN, "docs/internal"));
const TOOLS = join(INTERNAL, "vm-tools");
const CFG = {
  creds: env("VM_LANE_HAOS_CREDS", join(INTERNAL, "haos-test-credentials.md")),
  ha: env("VM_LANE_HA_URL", "http://127.0.0.1:8123"),
  guestHa: env("VM_LANE_GUEST_HA_URL", "http://10.0.2.2:8123"),
  guestRelay: env("VM_LANE_GUEST_RELAY_URL", "http://10.0.2.2:8791"),
  location: env("VM_LANE_HAOS_LOCATION", "Home"),
  haVersion: env("VM_LANE_HA_VERSION", "2026.10.2"),
  osVersion: env("VM_LANE_HAOS_VERSION", "18.3"),
  app: env("VM_LANE_APP_SLUG", "2368fcfa_ha_nova_relay"),
  win: { port: env("VM_LANE_WIN_PORT", "2222"), user: env("VM_LANE_WIN_ADMIN", "tester"), key: env("VM_LANE_WIN_KEY", join(TOOLS, "win11/vmkey")), knownHosts: env("VM_LANE_WIN_KNOWN_HOSTS", join(TOOLS, "win11/known_hosts")) },
  pwsh7: env("VM_LANE_WIN_PWSH7", "C:\\Tools\\pwsh-x64\\pwsh.exe"),
  ubuntu: { port: env("VM_LANE_UBUNTU_PORT", "2223"), user: env("VM_LANE_UBUNTU_USER", "tester"), key: env("VM_LANE_UBUNTU_KEY", join(TOOLS, "ubuntu/vmkey")), knownHosts: env("VM_LANE_UBUNTU_KNOWN_HOSTS", join(TOOLS, "ubuntu/known_hosts")) },
};
const VMS = { win: ["HA NOVA Win11 Test", CFG.win.port], haos: ["HA NOVA HAOS Test", 8123], ubuntu: ["HA NOVA Ubuntu Test", CFG.ubuntu.port] };

const [tag, ...flags] = process.argv.slice(2);
const opt = (name) => { const i = flags.indexOf(name); return i >= 0 ? flags[i + 1] : undefined; };
const badFlag = flags.some((f, i) => !["--from", "--wait"].includes(flags[i - 1]) && !["--from", "--wait", "--no-ubuntu"].includes(f));
if (!TAG_RE.test(tag ?? "") || badFlag || (flags.includes("--from") && !/^v\d+\.\d+\.\d+$/.test(opt("--from") ?? "")) || (flags.includes("--wait") && (!/^\d+$/.test(opt("--wait") ?? "") || Number(opt("--wait")) > 60))) {
  console.error("usage: npm run vm-lane -- <vX.Y.Z[-rcN]> [--from <vX.Y.Z>] [--wait <0..60>] [--no-ubuntu]");
  process.exit(2);
}
const WORK = mkdtempSync(join(tmpdir(), "ha-nova-vm-lane-"));
const PASSWORDS = {};
const ASKPASS = join(WORK, "askpass.sh");
writeFileSync(ASKPASS, '#!/bin/sh\nprintf "%s\\n" "$VM_LANE_PW"\n', { mode: 0o700 });
const rows = [];
const lease = net.createServer();
const children = new Set();
let stopping = false, cleaning = false;
const cancellation = new AbortController();
for (const signal of ["SIGINT", "SIGTERM"]) process.on(signal, () => { stopping = true; cancellation.abort(); for (const child of children) child.kill("SIGTERM"); });
const say = (line) => console.log(`vm-lane: ${line}`);
const sleep = (ms) => pause(ms, undefined, { signal: cleaning ? undefined : cancellation.signal });
function row(step, check, result, detail = "") {
  rows.push({ step, check, result, detail: String(detail).replace(/\s+/g, " ").trim().slice(0, 240) });
  say(`${step} ${check}: ${result}${detail ? ` (${rows.at(-1).detail})` : ""}`);
  return result === "PASS";
}
const check = (step, name, ok, detail) => row(step, name, ok ? "PASS" : "FAIL", detail);
const verLt = (a, b) => { // vX.Y.Z-rcN sorts before vX.Y.Z
  const p = (t) => { const [v, rc] = t.replace(/^v/, "").split("-rc"); return [...v.split(".").map(Number), rc === undefined ? Infinity : Number(rc)]; };
  const x = p(a), y = p(b);
  for (let i = 0; i < 4; i++) if (x[i] !== y[i]) return x[i] < y[i];
  return false;
};
// Terminal escapes, and PowerShell's CLIXML error stream back to plain text.
const clean = (s) => Object.values(PASSWORDS).reduce((text, secret) => text.replaceAll(secret, "[redacted]"), s).replace(/#< CLIXML\r?\n?|<\/?Objs[^>]*>/g, "").replace(/<S S="\w+">([\s\S]*?)<\/S>/g, "$1").replace(/_x000D__x000A_/g, "\n")
  .replace(/_x001B_\[[0-9;]*m/g, "").replace(/&gt;/g, ">").replace(/&lt;/g, "<").replace(/&amp;/g, "&").replace(/\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07/g, "");
const tail = (s, n = 2) => clean(s).split(/\r?\n/).map((l) => l.trim()).filter(Boolean).slice(-n).join(" | ");

// ---- ssh into a VM -------------------------------------------------------------------
// Resolves {code, out}; out also lands in WORK/<log>. tty: an interactive session whose
// onData sees the output so far and decides when to hang up.
function ssh(vm, user, cmd, { input, tty = false, timeoutMs = 10 * 60_000, log = "ssh.log", onData, cleanup = false } = {}) {
  if (stopping && !cleaning && !cleanup) throw new Error("lane interrupted; proceeding to cleanup");
  const pw = PASSWORDS[user];
  const auth = pw ? ["-o", "PubkeyAuthentication=no", "-o", "PreferredAuthentications=password", "-o", "NumberOfPasswordPrompts=1"] : ["-i", vm.key, "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes"];
  const args = [...auth, "-o", "StrictHostKeyChecking=accept-new", "-o", `UserKnownHostsFile=${vm.knownHosts}`, "-o", "LogLevel=ERROR",
    "-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15", "-p", String(vm.port), ...(tty ? ["-tt"] : []), `${user}@127.0.0.1`, cmd];
  return new Promise((done) => {
    const child = spawn("ssh", args, { stdio: ["pipe", "pipe", "pipe"], env: pw ? { ...process.env, SSH_ASKPASS: ASKPASS, SSH_ASKPASS_REQUIRE: "force", VM_LANE_PW: pw } : process.env });
    children.add(child);
    child.stdin.on("error", () => {});
    let out = "";
    const timer = setTimeout(() => { out += "\n[vm-lane: timed out]\n"; child.kill("SIGKILL"); }, timeoutMs);
    const take = (d) => { out += d; onData?.(out, child); };
    child.stdout.on("data", take);
    child.stderr.on("data", take);
    child.on("close", (code) => { clearTimeout(timer); children.delete(child); appendFileSync(join(WORK, log), `\n$ ${user}: ${cmd.slice(0, 200)}\n${clean(out)}\n[exit ${code}]\n`); done({ code, out: clean(out) }); });
    if (!tty) child.stdin.end(input ?? "");
  });
}
const SHELL = { ps7: CFG.pwsh7, ps51: "powershell.exe" };
const PRELUDE = "$ProgressPreference='SilentlyContinue'\n[Console]::OutputEncoding=[Text.Encoding]::UTF8\n";
// A terminal session gets its script on the command line; every other script goes over stdin
// (cmd.exe caps a command line at 8191 characters) and runs as one script block.
const win = (user, shell, ps, o = {}) => o.tty
  ? ssh(CFG.win, user, `"${SHELL[shell]}" -NoProfile -ExecutionPolicy Bypass -EncodedCommand ${Buffer.from(PRELUDE + ps, "utf16le").toString("base64")}`, { log: `win-${user}.log`, ...o })
  : ssh(CFG.win, user, `"${SHELL[shell]}" -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command "[Console]::InputEncoding=[Text.Encoding]::UTF8; & ([scriptblock]::Create([Console]::In.ReadToEnd()))"`, { log: `win-${user}.log`, input: PRELUDE + ps, ...o });
const admin = (ps, o) => win(CFG.win.user, "ps51", `$ErrorActionPreference='Stop'\n${ps}`, o);
// HA NOVA fails closed in session 0 (SSH, services): no Credential Manager, no window. A
// lane user's command runs in the console session instead: a one-shot task of the signed-in
// admin starts it there with the lane user's password (secondary logon), and the admin's SSH
// session waits for it. The job rebuilds the user's own environment from its token (secondary
// logon inherits the admin's) and refuses to run if anything still points at another profile.
let jobs = 0;
function desk(u, shell, ps, { timeoutMs = 5 * 60_000 } = {}) {
  const id = `${process.pid}-${++jobs}`, d = "C:\\Tools\\vm-lane\\jobs";
  const job = `$ProgressPreference='SilentlyContinue'
$env:USERPROFILE=[Environment]::GetFolderPath('UserProfile'); $env:APPDATA=[Environment]::GetFolderPath('ApplicationData'); $env:LOCALAPPDATA=[Environment]::GetFolderPath('LocalApplicationData')
$env:HOMEPATH=$env:USERPROFILE.Substring(2); $env:USERNAME='${u}'; $env:TEMP="$env:LOCALAPPDATA\\Temp"; $env:TMP=$env:TEMP
$env:Path=[Environment]::GetEnvironmentVariable('Path','Machine')+';'+[Environment]::GetEnvironmentVariable('Path','User')
foreach ($k in 'CLAUDE_CONFIG_DIR','CODEX_HOME','OneDrive') { Remove-Item "env:$k" -ErrorAction SilentlyContinue }
Get-ChildItem env: | Where-Object { $_.Value -like '*\\Users\\${CFG.win.user}*' } | ForEach-Object { Remove-Item "env:$($_.Name)" }
if ((whoami) -notlike '*\\${u}' -or $env:USERPROFILE -ne 'C:\\Users\\${u}' -or $env:LOCALAPPDATA -notlike 'C:\\Users\\${u}\\*' -or $env:APPDATA -notlike 'C:\\Users\\${u}\\*') { 'vm-lane: job environment is not the lane user''s own; refusing'; exit 97 }
Set-Location $env:USERPROFILE
${ps}`;
  const launch = `$c=New-Object System.Management.Automation.PSCredential('${u}',(ConvertTo-SecureString '${PASSWORDS[u]}' -AsPlainText -Force))
try { Start-Process -FilePath '${SHELL[shell]}' -ArgumentList '-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "${d}\\${id}-w.ps1"' -Credential $c -LoadUserProfile -WorkingDirectory 'C:\\Tools\\vm-lane' -RedirectStandardOutput '${d}\\${id}.out' -RedirectStandardError '${d}\\${id}.err' -Wait } catch { Set-Content '${d}\\${id}.err' "$_"; [IO.File]::WriteAllText('${d}\\${id}.rc', '98') }`;
  const here = (text) => `@'\n${text}\n'@`;
  return admin(`$d='${d}'; $id='${id}'; New-Item -ItemType Directory -Force $d | Out-Null
Set-Content "$d\\$id.ps1" -Encoding UTF8 -Value ${here(job)}
Set-Content "$d\\$id-w.ps1" -Encoding UTF8 -Value ${here(`try { & '${d}\\${id}.ps1'; $rc=if ($null -eq $LASTEXITCODE) { 0 } else { $LASTEXITCODE } } catch { Write-Output "$_"; $rc=99 }; [IO.File]::WriteAllText('${d}\\${id}.rc', [string]$rc)`)}
New-Item -ItemType File -Path "$d\\$id-l.ps1" | Out-Null
icacls "$d\\$id-l.ps1" /inheritance:r /grant '${CFG.win.user}:F' '*S-1-5-32-544:F' '*S-1-5-18:F' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'cannot restrict the temporary lane credential file' }
Set-Content "$d\\$id-l.ps1" -Encoding UTF8 -Value ${here(launch)}
$a=New-ScheduledTaskAction -Execute powershell.exe -Argument "-NoProfile -WindowStyle Hidden -ExecutionPolicy Bypass -File $d\\$id-l.ps1"
Register-ScheduledTask -TaskName "vm-lane-$id" -Action $a -Principal (New-ScheduledTaskPrincipal -UserId '${CFG.win.user}' -LogonType Interactive -RunLevel Limited) -Force | Out-Null
Start-ScheduledTask -TaskName "vm-lane-$id"
$end=(Get-Date).AddSeconds(${Math.round(timeoutMs / 1000)}); while ((-not (Test-Path "$d\\$id.rc") -or (Get-ScheduledTask -TaskName "vm-lane-$id").State -eq 'Running') -and (Get-Date) -lt $end) { Start-Sleep 2 }
if (-not (Test-Path "$d\\$id.rc") -or (Get-ScheduledTask -TaskName "vm-lane-$id").State -eq 'Running') {
  Stop-ScheduledTask -TaskName "vm-lane-$id"
  Get-Process -IncludeUserName -ErrorAction SilentlyContinue | Where-Object UserName -like '*\\${u}' | Stop-Process -Force
  Start-Sleep 2
  if (Get-Process -IncludeUserName -ErrorAction SilentlyContinue | Where-Object UserName -like '*\\${u}') { throw 'timed-out lane processes survived; stop before continuing' }
}
Unregister-ScheduledTask -TaskName "vm-lane-$id" -Confirm:$false
foreach ($f in "$d\\$id.out","$d\\$id.err") { if (Test-Path $f) { Get-Content $f -Encoding UTF8 } }
$rc = if (Test-Path "$d\\$id.rc") { [int](Get-Content "$d\\$id.rc") } else { 'vm-lane: job timed out'; 124 }
for ($i=0; $i -lt 10; $i++) { try { Remove-Item "$d\\$id*" -Force -ErrorAction Stop; break } catch { if ($i -eq 9) { throw }; Start-Sleep 2 } }
exit $rc`, { timeoutMs: timeoutMs + 60_000, log: `win-${u}.log` }).then((result) => {
    if (result.out.includes("timed-out lane processes survived")) { stopping = true; throw new Error("unsafe guest state: timed-out processes survived; no more lane steps will run"); }
    return result;
  });
}
const ubuntu = (script, o = {}) => ssh(CFG.ubuntu, CFG.ubuntu.user, "bash -s", { input: script, log: "ubuntu.log", ...o });

// ---- the throwaway Home Assistant (HAOS VM) -----------------------------------------
let llat;
function ws(msgs, timeoutMs = 30_000) {
  return new Promise((done, fail) => {
    const sock = new WebSocket(`${CFG.ha.replace(/^http/, "ws")}/api/websocket`);
    const out = []; let id = 0, n = 0;
    const timer = setTimeout(() => { sock.close(); fail(new Error(`HA websocket timed out (${msgs.map((m) => m.endpoint ?? m.type).join(", ")})`)); }, timeoutMs);
    sock.onerror = () => { clearTimeout(timer); fail(new Error("HA websocket error")); };
    sock.onmessage = (e) => {
      const m = JSON.parse(e.data);
      if (m.type === "auth_required") sock.send(JSON.stringify({ type: "auth", access_token: llat }));
      else if (m.type === "auth_invalid") { clearTimeout(timer); sock.close(); fail(new Error("the creds file's token is not valid on this Home Assistant")); }
      else if (m.type === "auth_ok") msgs.forEach((x) => sock.send(JSON.stringify({ id: ++id, ...x })));
      else if (m.type === "result") {
        out[m.id - 1] = m.success ? m.result : { error: m.error };
        if (++n === msgs.length) { clearTimeout(timer); sock.close(); done(out); }
      }
    };
  });
}
const sup = (endpoint, method = "get", data) => ws([{ type: "supervisor/api", endpoint, method, ...(data ? { data } : {}) }], 5 * 60_000).then(([r]) => r);
const states = async (ids) => Object.fromEntries((await (await fetch(`${CFG.ha}/api/states`, { headers: { authorization: `Bearer ${llat}` } })).json()).filter((s) => ids.includes(s.entity_id)).map((s) => [s.entity_id, s.state]));

// The owner's NOVA page over ingress, through its own forms (as the browser uses it).
async function nova() {
  const [sess, info] = await ws([{ type: "supervisor/api", endpoint: "/ingress/session", method: "post" }, { type: "supervisor/api", endpoint: `/addons/${CFG.app}/info`, method: "get" }]);
  if (!sess?.session || !info?.ingress_url?.startsWith("/api/hassio_ingress/")) throw new Error("Supervisor did not return a valid NOVA ingress session");
  const base = CFG.ha + info.ingress_url, cookie = `ingress_session=${sess.session}`;
  const get = async (q = "") => {
    const response = await fetch(base + q, { headers: { cookie } });
    if (!response.ok) throw new Error(`NOVA ingress HTTP ${response.status}; refusing to infer device ownership`);
    const html = await response.text();
    if (!html.includes("<title>NOVA</title>") || !/<footer>Signed in as [\s\S]*Relay [^<]+<\/footer>/.test(html) || /registry is damaged|Device access is disabled/.test(html)) throw new Error("NOVA owner page is unrecognized or registry recovery is required");
    return html;
  };
  const post = async (html, action) => {
    const form = html.match(/<form method="post" action="[^"]+">[\s\S]*?<\/form>/g)?.find((f) => f.includes(`name="action" value="${action}"`));
    if (!form) throw new Error(`NOVA page has no ${action} form`);
    const fields = Object.fromEntries([...form.matchAll(/name="([^"]+)" value="([^"]*)"/g)].map((m) => [m[1], m[2].replace(/&amp;/g, "&")]));
    const r = await fetch(CFG.ha + form.match(/action="([^"]+)"/)[1].replace(/&amp;/g, "&"), { method: "POST", redirect: "manual",
      headers: { cookie, "content-type": "application/x-www-form-urlencoded", origin: CFG.ha, "sec-fetch-site": "same-origin" }, body: new URLSearchParams(fields).toString() });
    if (r.status >= 400) throw new Error(`NOVA page ${action}: HTTP ${r.status}`);
  };
  const devices = (html) => [...html.matchAll(/<li><span class="device"><strong>([^<]*)<\/strong>[\s\S]*?device=([^"&]+)"/g)].map((m) => ({ name: m[1], id: decodeURIComponent(m[2]) }));
  return {
    devices: async () => {
      const html = await get(), list = devices(html);
      if (!list.length && !html.includes("No devices are connected yet.")) throw new Error("NOVA device list is unrecognized; refusing an empty baseline");
      return list;
    },
    code: async () => {
      let html = await get();
      if (!html.includes('value="generate_code"')) { await post(html, "cancel_code"); html = await get(); }
      await post(html, "generate_code");
      const code = (await get()).match(/<p class="code">([^<]+)<\/p>/)?.[1].replace(/\D/g, "");
      if (code?.length !== 6) throw new Error("the NOVA page showed no six-digit code");
      return code;
    },
    cancelCode: async () => { const html = await get(); if (html.includes('value="cancel_code"')) await post(html, "cancel_code"); },
    revoke: async (id) => post(await get(`?confirm=revoke_device&device=${encodeURIComponent(id)}`), "revoke_device"),
  };
}

// ---- preflight -------------------------------------------------------------------------
const up = (port) => new Promise((done) => { const s = net.connect({ host: "127.0.0.1", port: Number(port), timeout: 3000 }, () => { s.destroy(); done(true); }); s.on("error", () => done(false)); s.on("timeout", () => { s.destroy(); done(false); }); });
async function waitForVms() {
  const deadline = Date.now() + Number(opt("--wait") ?? 0) * 60_000;
  for (;;) {
    if (stopping) throw new Error("VM wait cancelled");
    const down = [];
    for (const k of ["win", "haos"]) if (!(await up(VMS[k][1]))) down.push(k);
    if (!down.length || Date.now() >= deadline) return down;
    say(`waiting for ${down.map((k) => `"${VMS[k][0]}"`).join(" and ")} (start it in UTM); next check in 2 min`);
    await sleep(Math.min(120_000, Math.max(1000, deadline - Date.now())));
  }
}

function hostNote() {
  const sh = (cmd) => { try { return execFileSync("bash", ["-c", cmd], { encoding: "utf8" }).trim(); } catch { return ""; } };
  const free = Number(sh("memory_pressure | sed -n 's/.*free percentage: \\([0-9]*\\)%.*/\\1/p'"));
  const [total, used] = (sh("sysctl -n vm.swapusage").match(/total = ([\d.]+)M\s+used = ([\d.]+)M/) ?? []).slice(1).map(Number);
  const ok = free >= 30 && (!total || used / total < 0.5);
  row("p", "host memory/disk (note only)", ok ? "PASS" : "WARN", `memory free ${free}%, swap ${used ?? "?"}/${total ?? "?"} MB, disk free ${sh("df -h / | awk 'NR==2{print $4}'")}; VM start rule: free >= 30% and swap < 50%`);
}

async function haosIdentity() {
  if (CFG.ha !== "http://127.0.0.1:8123" || CFG.guestHa !== "http://10.0.2.2:8123" || CFG.guestRelay !== "http://10.0.2.2:8791") throw new Error("refusing non-runbook HA/Relay URLs; only the throwaway VM loopback forwards are allowed");
  let owner = "";
  try { owner = execFileSync("lsof", ["-nP", "-iTCP@127.0.0.1:8123", "-sTCP:LISTEN", "-Fc"], { encoding: "utf8" }); } catch { owner = ""; }
  const pid = owner.match(/^p(\d+)/m)?.[1];
  const command = pid ? execFileSync("ps", ["-p", pid, "-o", "command="], { encoding: "utf8" }) : "";
  const isQemu = /^cQEMU/im.test(owner) && command.includes("HA NOVA HAOS Test.utm") && command.includes("D40C0C67-1385-466F-85B9-7BA33EC8522B");
  if (!isQemu) throw new Error("HAOS listener owner/UUID mismatch; refusing to read or send the VM credential");
  llat = readFileSync(CFG.creds, "utf8").match(/^- Long-lived token \(nova-vm-setup\): `([^`]+)`/m)?.[1];
  if (!llat) throw new Error(`no nova-vm-setup token in ${CFG.creds}`);
  const [conf, entries, appInfo, os] = await ws([{ type: "get_config" }, { type: "config_entries/get", domain: "demo" },
    { type: "supervisor/api", endpoint: `/addons/${CFG.app}/info`, method: "get" }, { type: "supervisor/api", endpoint: "/os/info", method: "get" }]);
  const ok = isQemu && conf.location_name === CFG.location && conf.version === CFG.haVersion && os?.version === CFG.osVersion && conf.time_zone === "Europe/Berlin" && entries?.length > 0 && Boolean(appInfo?.version);
  check("p", "HAOS VM is the throwaway one", ok, `127.0.0.1:8123 owner ${isQemu ? "QEMU" : "NOT QEMU"}, creds token ok, location "${conf.location_name}", ${conf.time_zone}, HA ${conf.version}, HAOS ${os?.version ?? "?"}, demo entry ${entries?.length ? "yes" : "NO"}, App ${appInfo?.version ?? "missing"} ${appInfo?.state ?? ""}`);
  return ok ? appInfo : null;
}
function registryHash() {
  if (!/^[a-z0-9_]+$/.test(CFG.app)) throw new Error("invalid VM App slug");
  const script = 'const fs=require("node:fs"),crypto=require("node:crypto"); const data=JSON.parse(fs.readFileSync("/data/device-registry.json","utf8")); for(const device of data.devices)delete device.lastUsedAtMs; const sorted=(v)=>Array.isArray(v)?v.map(sorted):v&&typeof v==="object"?Object.fromEntries(Object.keys(v).sort().map(k=>[k,sorted(v[k])])):v; console.log(crypto.createHash("sha256").update(JSON.stringify(sorted(data))).digest("hex"));';
  const output = execFileSync("python3", [join(TOOLS, "haos/sh.py"), `docker exec app_${CFG.app} node -e '${script}'`, "15"], { encoding: "utf8", timeout: 25_000 });
  const hash = output.match(/^([0-9a-f]{64})\s/m)?.[1];
  if (!hash) throw new Error("cannot hash the throwaway App registry through its serial helper");
  return hash;
}

async function appVersion(appInfo) {
  const res = await fetch(`https://raw.githubusercontent.com/${REPO}/${tag}/version.json`);
  if (!res.ok) throw new Error(`${tag}/version.json: HTTP ${res.status}`);
  const min = (await res.json()).min_relay_version;
  let have = appInfo.version;
  if (verLt(`v${have}`, `v${min}`)) {
    await sup("/store/reload", "post");
    await sup(`/store/addons/${CFG.app}/update`, "post", { backup: false }); // the WS answer can error out while the update still runs: judge by the result
    let info = {};
    for (let i = 0; i < 40 && !(info.state === "started" && !verLt(`v${info.version}`, `v${min}`)); i++) { await sleep(5000); info = await sup(`/addons/${CFG.app}/info`); }
    have = info.version;
    row("p", "NOVA App updated for candidate", info.state === "started" ? "PASS" : "FAIL", `${appInfo.version} -> ${have} ${info.state}; restored from backup at exit`);
  }
  const old = verLt(`v${have}`, `v${min}`);
  return check("p", `NOVA App >= ${tag} min_relay_version`, !old, `App ${have}, min ${min}`);
}

async function winIdentity() {
  const r = await admin("$c=Get-CimInstance Win32_ComputerSystem; $o=Get-CimInstance Win32_OperatingSystem; \"$($c.Manufacturer)|$($o.Caption)|$($o.BuildNumber)|$env:PROCESSOR_ARCHITECTURE|$([math]::Round((Get-PSDrive C).Free/1GB,1))\"");
  const [maker, caption, build, arch, free] = r.out.trim().split("|");
  return check("p", "Win11 VM reachable (admin SSH)", r.code === 0 && /QEMU/i.test(maker) && /Windows 11/.test(caption), r.code === 0 ? `${caption} ${build} ${arch}, maker ${maker}, C: free ${free} GB` : tail(r.out));
}

// Outbound LAN block inside the Win11 VM (it has no other isolation): the terminal setup
// check must not discover the maintainer's real Home Assistant over mDNS. Stays in place.
async function winIsolation() {
  const r = await admin(`$g='HA NOVA vm-lane isolation'
Get-NetFirewallRule -Group $g -ErrorAction SilentlyContinue | Remove-NetFirewallRule
  New-NetFirewallRule -Group $g -DisplayName "$g IPv4 LAN" -Direction Outbound -Action Block -RemoteAddress @('10.0.0.0-10.0.1.255','10.0.3.0-10.255.255.255','172.16.0.0/12','192.168.0.0/16','100.64.0.0/10','169.254.0.0/16','224.0.0.0/4','255.255.255.255') | Out-Null
  New-NetFirewallRule -Group $g -DisplayName "$g IPv6" -Direction Outbound -Action Block -RemoteAddress @('2000::/3','fc00::/7','fec0::/10') | Out-Null
  New-NetFirewallRule -Group $g -DisplayName "$g mDNS LLMNR SSDP" -Direction Outbound -Action Block -Protocol UDP -RemotePort @(5353,5355,1900) | Out-Null
$on=@(Get-NetFirewallRule -Group $g | Where-Object Enabled -eq 'True').Count
$off=@(Get-NetFirewallProfile | Where-Object Enabled -eq $false).Count
"rules=$on profiles_off=$off"`);
  const m = r.out.match(/rules=(\d+) profiles_off=(\d+)/);
  return check("p", "Win11 LAN isolation (fresh outbound block rules)", r.code === 0 && m?.[1] === "3" && m?.[2] === "0", m ? `${m[1]}/3 outbound block rules recreated, ${m[2]} firewall profiles off` : tail(r.out));
}

// ---- Windows -----------------------------------------------------------------------------
// Fresh standard users run native credential operations in the console session.
const LANE_RE = "^nova(-lane[57]|[57]-[0-9a-f]{6})$";
const laneUsers = (u, pw) => `$re='${LANE_RE}'
Get-ScheduledTask | Where-Object TaskName -like 'vm-lane-*' | ForEach-Object { Stop-ScheduledTask -TaskName $_.TaskName; Unregister-ScheduledTask -TaskName $_.TaskName -Confirm:$false }
if (Test-Path C:\\Tools\\vm-lane\\jobs) { Remove-Item C:\\Tools\\vm-lane\\jobs\\* -Force }
${!u ? "Remove-Item C:\\Tools\\vm-lane\\bin -Recurse -Force -ErrorAction SilentlyContinue" : ""}
$names=@(Get-LocalUser | Where-Object Name -match $re | ForEach-Object Name)
Get-Process -IncludeUserName -ErrorAction SilentlyContinue | Where-Object { ($_.UserName -split '\\\\')[-1] -match $re } | Stop-Process -Force -ErrorAction SilentlyContinue
foreach ($n in $names) { Remove-LocalUser -Name $n }
Start-Sleep 2
$kept=0; foreach ($p in Get-CimInstance Win32_UserProfile | Where-Object { (Split-Path $_.LocalPath -Leaf) -match $re }) { try { $p | Remove-CimInstance -ErrorAction Stop } catch { $kept++ } }
"lane_users_removed=$($names.Count) profiles_kept=$kept"
${u ? `New-LocalUser -Name '${u}' -Password (ConvertTo-SecureString '${pw}' -AsPlainText -Force) -PasswordNeverExpires -AccountNeverExpires -Description 'HA NOVA vm-lane throwaway' | Out-Null
Add-LocalGroupMember -SID 'S-1-5-32-545' -Member '${u}'
New-Item -ItemType Directory -Force C:\\Tools\\vm-lane\\bin | Out-Null
Remove-Item C:\\Tools\\vm-lane\\bin\\codex.cmd -ErrorAction SilentlyContinue
$cc="C:\\Users\\${CFG.win.user}\\.local\\bin\\claude.exe"
if (Test-Path $cc) { Copy-Item $cc C:\\Tools\\vm-lane\\bin\\claude.exe -Force } else { Remove-Item C:\\Tools\\vm-lane\\bin\\claude.exe -ErrorAction SilentlyContinue }
"claude=$(Test-Path C:\\Tools\\vm-lane\\bin\\claude.exe) git=$(Test-Path 'C:\\Program Files\\Git\\bin\\bash.exe')"` : ""}`;

const INSTALL = (t) => `$env:HA_NOVA_VERSION='${t}'; irm https://raw.githubusercontent.com/${REPO}/${t}/install.ps1 | iex`;
const HN = "$env:LOCALAPPDATA\\Programs\\ha-nova\\ha-nova.exe";
const MARK = "$env:LOCALAPPDATA\\ha-nova\\uninstall-status.json";
// Windows finishes update and uninstall in a background helper, and closing an SSH session
// kills its processes: so the command and the wait share one session, as in a terminal left open.
const waitFor = (cond, secs) => `$d=(Get-Date).AddSeconds(${secs}); while (-not (${cond}) -and (Get-Date) -lt $d) { Start-Sleep 3 }`;
const thenWait = (args, cond, report) => `ha-nova ${args}; $rc=$LASTEXITCODE; ${waitFor(cond, 180)}; "rc=$rc ${report}"`;
const gone = `-not (Test-Path "$env:LOCALAPPDATA\\Programs\\ha-nova") -and -not (Test-Path "${MARK}")`;

async function windowsRun(shell, stable) {
  const u = `nova${shell === "ps7" ? 7 : 5}-${randomBytes(3).toString("hex")}`;
  PASSWORDS[u] = `Nv1-${randomBytes(12).toString("hex")}`;
  const S = shell === "ps7" ? "w7" : "w5";
  const label = shell === "ps7" ? "PowerShell 7 x64" : "PowerShell 5.1";
  const want = tag.slice(1);
  const run = (ps, o) => desk(u, shell, ps, o);  // ha-nova runs in the console session, in this run's shell
  const ps7 = (ps, o) => win(u, "ps7", ps, o);   // installs: ARM64 Windows needs the x64 PowerShell
  const hn = (args, o) => run(`ha-nova ${args}; exit $LASTEXITCODE`, o);
  const made = await admin(laneUsers(u, PASSWORDS[u]), { timeoutMs: 3 * 60_000 });
  await win(u, shell, "[Environment]::SetEnvironmentVariable('Path','C:\\Tools\\vm-lane\\bin','User'); [Environment]::SetEnvironmentVariable('DISABLE_AUTOUPDATER','1','User')");
  const who = await run("\"$(whoami)|$((New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())).IsInRole('Administrators'))|$($PSVersionTable.PSVersion)|$env:PROCESSOR_ARCHITECTURE|$env:USERPROFILE|$([Diagnostics.Process]::GetCurrentProcess().SessionId)\"");
  const [whoami, isAdmin, psv, arch, profile, session] = who.out.trim().split(/\r?\n/).at(-1).split("|");
  if (!check(S, `${label}: fresh standard user, console session`, made.code === 0 && who.code === 0 && isAdmin === "False" && profile === `C:\\Users\\${u}` && Number(session) > 0, `${whoami} admin=${isAdmin}, PowerShell ${psv} ${arch}, ${profile}, session ${session}; ${(made.out.match(/claude=\S+ git=\S+/) ?? [tail(who.out + made.out, 1)])[0]}`)) return;
  const hasClaude = /claude=True/.test(made.out);
  if (!check(S, "real supported client ready", hasClaude, hasClaude ? "Claude Code executable copied from the VM's installed client; Git Bash installed" : "install Claude Code and Git for the VM admin before running the lane")) return;

  if (shell === "ps51") { // ARM64 Windows PowerShell: the installer must refuse with the x64 how-to and install nothing
    const r = await run(INSTALL(tag));
    const left = await run(`Test-Path "${HN}"`);
    check(S, "5.1 one-liner on ARM64: x64 how-to, nothing installed", r.code !== 0 && /x64 PowerShell/.test(r.out) && /False/.test(left.out), tail(r.out.split(/#< CLIXML|Exception:/)[0], 2));
  }

  for (const t of [tag, stable]) {
    let seen = false;
    const tty = await ps7(INSTALL(t), { tty: true, timeoutMs: 6 * 60_000, onData: (out, child) => {
      if (!seen && /HA NOVA Setup[\s\S]*Which AI app do you use\?/.test(clean(out))) { seen = true; child.stdin.write("\x03"); }
    } });
    await admin(`Get-Process -IncludeUserName -ErrorAction SilentlyContinue | Where-Object { $_.UserName -like "*\\${u}" -and $_.ProcessName -like 'ha-nova*' } | Stop-Process -Force -ErrorAction SilentlyContinue`);
    if (!check(S, `${t} public one-liner: setup auto-starts`, tty.code === 0 && seen && tty.out.includes(`Installed HA NOVA v${t.slice(1)}`) && !/No interactive terminal detected|Next step: ha-nova setup|No supported AI client|\[!!\]/.test(tty.out), seen ? "real installer -> HA NOVA Setup -> Which AI app do you use? (cancelled by lane)" : tail(tty.out, 3))) return;
  }

  const v1 = await hn("version");
  check(S, "ha-nova on PATH, version", v1.code === 0 && v1.out.trim() === stable.slice(1), tail(v1.out, 1));
  const code = await (await nova()).code();
  const pair = await hn(`pair --relay-url ${CFG.guestRelay} --code ${code}`, { timeoutMs: 3 * 60_000 });
  check(S, "pair with a six-digit code from the NOVA page", pair.code === 0, tail(pair.out, 2));
  const hasCodex = (await run("if (Get-Command codex -ErrorAction SilentlyContinue) { 'codex-present' }")).out.includes("codex-present");
  for (const c of [...(hasClaude ? ["claude"] : []), ...(hasCodex ? ["codex"] : [])]) {
    const r = await hn(`setup ${c} --non-interactive --ha-url ${CFG.guestHa}`, { timeoutMs: 5 * 60_000 });
    check(S, `setup ${c} (non-interactive, paired device)`, r.code === 0, tail(r.out, 2));
  }
  if (!hasClaude) row(S, "setup claude", "SKIP", `no C:\\Users\\${CFG.win.user}\\.local\\bin\\claude.exe on the VM`);
  const d1 = await hn("doctor", { timeoutMs: 3 * 60_000 });
  check(S, `doctor on ${stable}`, d1.code === 0, tail(d1.out, 1));

  const upd = await run(thenWait(`update --version ${tag}`, `(& "${HN}" version) -eq '${want}'`, `now=$(& "${HN}" version)`), { timeoutMs: 6 * 60_000 });
  check(S, `ha-nova update --version ${tag} (from ${stable})`, upd.code === 0 && upd.out.split(/\r?\n/).includes(`rc=0 now=${want}`), tail(upd.out, 3));
  await core(S, hn, run, hasClaude, hasCodex, want);

  const re = await ps7(INSTALL(tag), { timeoutMs: 6 * 60_000 });
  const d3 = await hn("doctor", { timeoutMs: 3 * 60_000 });
  check(S, `${tag} one-liner rerun: installs, still paired`, re.code === 0 && new RegExp(`Installed HA NOVA v${want}`).test(re.out) && d3.code === 0, `${tail(re.out, 1)}; doctor: ${tail(d3.out, 1)}`);

  const un = await run(thenWait("uninstall --yes", gone, `runtime=$(Test-Path "$env:LOCALAPPDATA\\Programs\\ha-nova") marker=$(Test-Path "${MARK}") config=$(Test-Path "$env:APPDATA\\ha-nova\\config.json") state=$(Test-Path "$env:APPDATA\\ha-nova\\state.json") cache=$(Test-Path "$env:LOCALAPPDATA\\ha-nova\\cache")`), { timeoutMs: 6 * 60_000 });
  check(S, "uninstall --yes: runtime/state/cache/marker gone, config kept", /rc=0 runtime=False marker=False config=True state=False cache=False/.test(un.out), tail(un.out, 2));

  const seed = await run(`New-Item -ItemType Directory -Force (Split-Path "${MARK}") | Out-Null; Set-Content -LiteralPath "${MARK}" -Encoding UTF8 -Value '{"status":"failed","mode":"standard","error_summary":"stale uninstall marker seeded by vm-lane","remaining_paths":[]}'; if ((Get-Content "${MARK}" -Raw | ConvertFrom-Json).status -ne 'failed') { exit 1 }; 'stale-marker-seeded'`);
  check(S, "stale uninstall marker seeded", seed.code === 0 && seed.out.includes("stale-marker-seeded"), tail(seed.out, 1));
  const st = await ps7(INSTALL(tag), { timeoutMs: 6 * 60_000 });
  const st2 = await run(`"marker=$(Test-Path "${MARK}")"; & "${HN}" doctor; exit $LASTEXITCODE`, { timeoutMs: 3 * 60_000 });
  check(S, "stale uninstall marker: one-liner installs, kept pairing works", st.code === 0 && /Installed HA NOVA/.test(st.out) && /marker=False/.test(st2.out) && st2.code === 0, `${tail(st.out.split(/#< CLIXML/)[0], 1)}; ${tail(st2.out, 1)}`);

  const before = (await (await nova()).devices()).length;
  const pg = await run(thenWait("uninstall --yes --purge", gone, `runtime=$(Test-Path "$env:LOCALAPPDATA\\Programs\\ha-nova") marker=$(Test-Path "${MARK}") config=$(Test-Path "$env:APPDATA\\ha-nova") data=$(Test-Path "$env:LOCALAPPDATA\\ha-nova") left=$(@(Get-ChildItem -Force $env:LOCALAPPDATA,$env:APPDATA -Filter '*ha-nova*' | Where-Object Name -notmatch '^\.ha-nova-' | ForEach-Object Name) -join ',')`), { timeoutMs: 6 * 60_000 });
  const devs = (await (await nova()).devices()).length;
  check(S, "uninstall --purge: all gone, device revoked", /rc=0 runtime=False marker=False config=False data=False left=\s*$/.test(pg.out.trim()) && devs === before - 1, `${tail(pg.out, 1)}; NOVA devices ${before} -> ${devs}`);
}

// Doctor, a read, an ordinary call, client wiring: on the candidate.
async function core(S, hn, run, hasClaude, hasCodex, want) {
  const d = await hn("doctor", { timeoutMs: 3 * 60_000 });
  check(S, `doctor on ${tag}`, d.code === 0, tail(d.out, 1));
  const g = await hn("get lock.front_door");
  check(S, "read: ha-nova get lock.front_door", g.code === 0 && /"state"\s*:\s*"(locked|unlocked)"/.test(g.out), tail(g.out, 1).slice(0, 120));
  const on = await hn("call light.turn_on light.bed_light");
  const off = await hn("call light.turn_off light.bed_light");
  const st = (r) => r.out.match(/"status"\s*:\s*"([^"]+)"/)?.[1] ?? `exit ${r.code}`;
  check(S, "ordinary call: light.turn_on / turn_off bed light", on.code === 0 && off.code === 0 && st(on) === "verified" && st(off) === "verified", `${st(on)} / ${st(off)}`);
  const w = await run(`$b=Get-ChildItem "$env:LOCALAPPDATA\\Programs\\ha-nova" -Recurse -Filter SKILL.md | Where-Object { $_.Directory.Name -eq 'ha-nova' } | Select-Object -First 1
$c=Join-Path $HOME '.agents\\skills\\ha-nova\\ha-nova\\SKILL.md'
"codex=$([bool]((Test-Path $c) -and $b -and ((Get-FileHash $c).Hash -eq (Get-FileHash $b.FullName).Hash)))"
${hasClaude ? "claude plugin list 2>&1 | Out-String" : ""}`);
  if (hasCodex) check(S, "codex wiring: skills match the installed bundle", /codex=True/.test(w.out), (w.out.match(/codex=\S+/) ?? [tail(w.out, 1)])[0]);
  else row(S, "Codex CLI wiring", "SKIP", "Codex CLI is not installed on this VM; no client stub used");
  if (!hasClaude) return;
  const lines = w.out.split(/\r?\n/);
  const block = lines.slice(Math.max(0, lines.findIndex((l) => l.includes("ha-nova@ha-nova")))).slice(0, 5).join(" ");
  check(S, "Claude Code wiring: ha-nova@ha-nova at the candidate", block.includes("ha-nova@ha-nova") && block.includes(`Version: ${want.split("-rc")[0]}`) && new RegExp(`releases[\\\\/]+${tag}`).test(block) && /enabled/i.test(block), block.slice(0, 160));
}

// ---- Ubuntu ----------------------------------------------------------------------------
async function ubuntuRun(stable) {
  const S = "u";
  const fw = await ubuntu("sudo -n nft -s list table inet hanova_isolation");
  const output = fw.out.split("chain output {")[1]?.split("chain input {")[0]?.replace(/\}\s*$/, "").replace(/comment "[^"]*"|\bcounter\b/g, "").replace(/\s+/g, " ").trim();
  const expected = 'type filter hook output priority filter; policy accept; oif "lo" accept udp dport 5353 drop ip daddr 10.0.2.0/24 accept ip daddr @blocked4 reject meta nfproto ipv6 icmpv6 type { nd-router-solicit, nd-neighbor-solicit, nd-neighbor-advert } accept meta nfproto ipv6 reject';
  const blocked = fw.out.match(/elements = \{([^}]+)\}/)?.[1].replace(/\s+/g, "").split(",").sort();
  const want = ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "169.254.0.0/16", "224.0.0.0/4", "255.255.255.255"].sort();
  if (!check(S, "Ubuntu LAN isolation (effective nftables rules)", fw.code === 0 && output === expected && JSON.stringify(blocked) === JSON.stringify(want), "requires the runbook's ordered output rules and complete blocked4 set")) return;
  const H = (await ubuntu("mktemp -d /tmp/vm-lane-XXXXXX")).out.trim();
  if (!/^\/tmp\/vm-lane-\w+$/.test(H)) return row(S, "throwaway HOME", "FAIL", H);
  const installed = await ubuntu('export PATH="$HOME/.opencode/bin:$HOME/.local/bin:$PATH"; for c in opencode codex; do p=$(command -v "$c") && { printf "%s|%s" "$c" "$p"; break; }; done');
  const [client, executable] = installed.out.trim().split("|");
  if (!/^(opencode|codex)$/.test(client) || !/^\/[a-zA-Z0-9_./-]+$/.test(executable ?? "")) { await ubuntu(`rmdir ${H}`); return row(S, "installed Ubuntu client", "FAIL", "install OpenCode or Codex in the throwaway VM before running its lane"); }
  const ENV = `env -i HOME=${H}/home USER=$(id -un) LOGNAME=$(id -un) LANG=C.UTF-8 TERM=dumb SHELL=/bin/bash XDG_CONFIG_HOME=${H}/home/.config XDG_DATA_HOME=${H}/home/.local/share XDG_CACHE_HOME=${H}/home/.cache XDG_STATE_HOME=${H}/home/.local/state PATH=${H}/home/.local/bin:${H}/bin:/usr/local/bin:/usr/bin:/bin`;
  const sh = (cmd, o) => ubuntu(`set -o pipefail; ${cmd}`, o);
  const hn = (args, o) => sh(`${ENV} ha-nova ${args} </dev/null`, o);
  const install = (t) => sh(`curl -fsSL https://raw.githubusercontent.com/${REPO}/${t}/install.sh | ${ENV} HA_NOVA_VERSION=${t} bash`, { timeoutMs: 6 * 60_000 });
  try {
    await sh(`mkdir -p ${H}/home ${H}/bin && ln -s ${executable} ${H}/bin/${client}`);
    const i1 = await install(stable);
    const v1 = await hn("version");
    check(S, `${stable} one-liner (no terminal): installs, names the next step`, i1.code === 0 && v1.out.trim() === stable.slice(1) && /ha-nova setup/.test(i1.out), `${tail(i1.out, 1)}; version ${tail(v1.out, 1)}`);
    const code = await (await nova()).code();
    const p = await hn(`pair --relay-url ${CFG.guestRelay} --code ${code} --credential-store=file`, { timeoutMs: 3 * 60_000 });
    check(S, "pair with a six-digit code from the NOVA page", p.code === 0, tail(p.out, 2));
    const s = await hn(`setup ${client} --non-interactive --ha-url ${CFG.guestHa}`, { timeoutMs: 5 * 60_000 });
    check(S, `setup ${client} (headless, paired device)`, s.code === 0, tail(s.out, 2));
    const up1 = await hn(`update --version ${tag}`, { timeoutMs: 5 * 60_000 });
    const v2 = await hn("version");
    check(S, `ha-nova update --version ${tag} (from ${stable})`, up1.code === 0 && v2.out.trim() === tag.slice(1), `${tail(up1.out, 1)}; now ${tail(v2.out, 1)}`);
    const d = await hn("doctor", { timeoutMs: 3 * 60_000 });
    check(S, `doctor on ${tag}`, d.code === 0, tail(d.out, 1));
    const g = await hn("get lock.front_door");
    check(S, "read: ha-nova get lock.front_door", g.code === 0 && /"state"\s*:\s*"(locked|unlocked)"/.test(g.out), tail(g.out, 1).slice(0, 120));
    const on = await hn("call light.turn_on light.bed_light"), off = await hn("call light.turn_off light.bed_light");
    check(S, "ordinary call: light.turn_on / turn_off bed light", on.code === 0 && off.code === 0 && /"status":"verified"/.test(on.out) && /"status":"verified"/.test(off.out), `exit ${on.code} / ${off.code}`);
    const destination = client === "codex" ? ".agents/skills" : ".config/opencode/skills";
    const w = await sh(`cmp -s ${H}/home/${destination}/ha-nova/ha-nova/SKILL.md ${H}/home/.local/share/ha-nova/skills/ha-nova/SKILL.md && echo wired`);
    check(S, `${client} wiring: skills match the installed bundle`, /wired/.test(w.out), tail(w.out, 1));
    const i2 = await install(tag);
    const d2 = await hn("doctor", { timeoutMs: 3 * 60_000 });
    check(S, `${tag} one-liner rerun: installs, still paired`, i2.code === 0 && d2.code === 0, `${tail(i2.out, 1)}; doctor: ${tail(d2.out, 1)}`);
    const before = (await (await nova()).devices()).length;
    const pg = await hn("uninstall --yes --purge", { timeoutMs: 3 * 60_000 });
    const left = await sh(`find ${H}/home -name '*ha-nova*' ! -name '.ha-nova-*' | head -5`);
    const devs = (await (await nova()).devices()).length;
    check(S, "uninstall --purge: all gone, device revoked", pg.code === 0 && !left.out.trim() && devs === before - 1, `${tail(pg.out, 1)}; leftovers: ${left.out.trim() || "none"}; NOVA devices ${before} -> ${devs}`);
  } finally {
    await sh(`rm -rf ${H}`, { cleanup: true });
  }
}

// ---- main ----------------------------------------------------------------------------------
const started = Date.now();
let start = null, backupSlug, users = false;
try {
  await new Promise((done, fail) => { lease.once("error", () => fail(new Error("VM lane lock 127.0.0.1:4557 is busy; do not run two lanes against the same VMs"))); lease.listen(4557, "127.0.0.1", done); });
  say(`candidate ${tag}; VM tools ${TOOLS}; work dir ${WORK}`);
  for (const f of [CFG.creds, CFG.win.key]) if (!existsSync(f)) throw new Error(`missing ${f} (VM_LANE_* overrides the paths)`);
  hostNote();
  const down = await waitForVms();
  if (stopping) throw new Error("VM lane cancelled before preflight");
  if (down.length) throw new Error(`not running: ${down.map((k) => `"${VMS[k][0]}"`).join(", ")}. Start it in UTM, then rerun (or pass --wait 60)`);
  let stable = opt("--from");
  if (!stable) {
    const rel = await (await fetch(`https://api.github.com/repos/${REPO}/releases?per_page=100`)).json();
    stable = rel.filter((r) => !r.draft && !r.prerelease && TAG_RE.test(r.tag_name) && verLt(r.tag_name, tag)).map((r) => r.tag_name).reduce((a, b) => (!a || verLt(a, b) ? b : a), "");
  }
  if (!TAG_RE.test(stable ?? "") || !verLt(stable, tag)) throw new Error(`no stable baseline older than ${tag} (pass --from vX.Y.Z)`);
  say(`baseline ${stable}`);
  const appInfo = await haosIdentity();
  if (!appInfo) throw new Error("refusing to write: the HAOS VM check failed");
  const snapshot = { registry: registryHash(), version: appInfo.version, devices: (await (await nova()).devices()).map((d) => d.id), states: await states(["lock.front_door", "light.bed_light"]) };
  const sources = (await ws([{ type: "entity/source" }]))[0];
  if (sources["lock.front_door"]?.domain !== "demo" || sources["light.bed_light"]?.domain !== "demo") throw new Error("refusing writes: lane entities are not from the demo integration");
  const flows = (await ws([{ type: "config_entries/flow/progress" }]))[0].length;
  if (!check("p", "HAOS discovery flows pending", flows === 0, `${flows}; ignore new real-LAN discoveries in the throwaway VM before running`)) throw new Error("pending discovery flows; no lane writes");
  const backup = await sup("/backups/new/partial", "post", { name: `vm-lane ${tag} ${new Date().toISOString().slice(0, 16)}`, addons: [CFG.app], homeassistant: false });
  check("p", "HAOS backup of the NOVA App (device registry)", Boolean(backup?.slug), backup?.slug ? `slug ${backup.slug}; at start ${snapshot.devices.length} devices, ${JSON.stringify(snapshot.states)}` : JSON.stringify(backup));
  backupSlug = backup?.slug;
  if (!backupSlug) throw new Error("preflight failed; no lane writes");
  start = snapshot;
  if (stopping || !(await appVersion(appInfo))) throw new Error("preflight failed or interrupted");
  if (await winIdentity() && await winIsolation()) {
    users = true;
    for (const shell of ["ps7", "ps51"]) {
      try { await windowsRun(shell, stable); } catch (err) { row(shell === "ps7" ? "w7" : "w5", "run", "FAIL", err.message); }
    }
  }
  if (flags.includes("--no-ubuntu")) row("u", "Ubuntu", "SKIP", "--no-ubuntu");
  else if (!(await up(CFG.ubuntu.port))) row("u", "Ubuntu", "SKIP", `"${VMS.ubuntu[0]}" not running (optional)`);
  else try { await ubuntuRun(stable); } catch (err) { row("u", "run", "FAIL", err.message); }
} catch (err) {
  row("-", "setup", "FAIL", err.message);
}

// ---- cleanup: leave the VMs as found -----------------------------------------------------------
cleaning = true;
if (users) {
  const r = await admin(laneUsers(), { timeoutMs: 3 * 60_000 });
  const kept = Number(r.out.match(/profiles_kept=(\d+)/)?.[1] ?? NaN);
  row("c", "Windows lane users deleted", r.code !== 0 ? "FAIL" : kept ? "WARN" : "PASS", r.code !== 0 ? tail(r.out, 1) : `${r.out.match(/lane_users_removed=\d+/)?.[0]}${kept ? `; ${kept} profile folder(s) stay until the VM reboots (Windows keeps them loaded); the next run sweeps them` : ""}`);
}
if (start) {
  try {
    const page = await nova();
    await page.cancelCode();
    const extra = (await page.devices()).filter((d) => !start.devices.includes(d.id));
    for (const d of extra) await page.revoke(d.id);
    const leftover = (await page.devices()).filter((d) => !start.devices.includes(d.id));
    check("c", "lane devices revoked on the NOVA page", !leftover.length, `${extra.length} left by the runs and revoked here${extra.length ? ` (${extra.map((d) => d.name).join(", ")})` : ""}${leftover.length ? `; STILL LISTED: ${leftover.map((d) => d.name).join(", ")}` : ""}`);
    const now = await states(Object.keys(start.states));
    for (const [id, st] of Object.entries(start.states)) {
      const service = { locked: "lock", unlocked: "unlock", on: "turn_on", off: "turn_off" }[st];
      if (now[id] !== st && service) await fetch(`${CFG.ha}/api/services/${id.split(".")[0]}/${service}`, { method: "POST", headers: { authorization: `Bearer ${llat}`, "content-type": "application/json" }, body: JSON.stringify({ entity_id: id }) });
    }
    await sleep(2000);
    const end = await states(Object.keys(start.states));
    check("c", "entity states as found", JSON.stringify(end) === JSON.stringify(start.states), JSON.stringify(end));
  } catch (err) {
    row("c", "HAOS cleanup", "FAIL", err.message);
  }
}
if (backupSlug) {
  try {
    const restored = await sup(`/backups/${backupSlug}/restore/partial`, "post", { addons: [CFG.app], homeassistant: false });
    let info;
    for (let i = 0; i < 60; i++) { info = await sup(`/addons/${CFG.app}/info`); if (info.state === "started" && info.version === start.version) break; await sleep(3000); }
    const ids = (await (await nova()).devices()).map((d) => d.id).sort();
    const sameRegistry = registryHash() === start.registry;
    const matched = check("c", "HAOS App and credential registry match snapshot", info.state === "started" && info.version === start.version && sameRegistry && JSON.stringify(ids) === JSON.stringify([...start.devices].sort()), `backup ${backupSlug}; App ${info.version} ${info.state}; original device IDs ${ids.length}; registry SHA-256 ${sameRegistry ? "unchanged" : "DIFFERS"} (only lastUsedAtMs excluded)${!sameRegistry && restored?.error ? JSON.stringify(restored.error) : ""}`);
    if (matched) {
      await sup(`/backups/${backupSlug}`, "delete");
      const listing = await sup("/backups/info");
      check("c", "lane backup removed after verified restoration", Array.isArray(listing.backups) && !listing.backups.some((b) => b.slug === backupSlug), backupSlug);
    }
  } catch (err) { row("c", "HAOS NOVA App backup restore", "FAIL", `${err.message}; recovery backup ${backupSlug}`); }
}
say("the lane never starts or stops a VM: the running ones stay running (the maintainer stops them in UTM)");
const w = (k) => Math.max(k.length, ...rows.map((r) => String(r[k]).length));
console.log(`\n${"STEP".padEnd(4)}  ${"CHECK".padEnd(w("check"))}  RESULT  DETAIL`);
for (const r of rows) console.log(`${r.step.padEnd(4)}  ${r.check.padEnd(w("check"))}  ${r.result.padEnd(6)}  ${r.detail}`);
const failed = rows.filter((r) => r.result === "FAIL").length;
const other = ["SKIP", "WARN"].map((k) => [k, rows.filter((r) => r.result === k).length]).filter(([, n]) => n).map(([k, n]) => `, ${n} ${k}`).join("");
console.log(`\nvm-lane ${tag}: ${failed ? `${failed} FAIL` : "all PASS"}${other} in ${Math.round((Date.now() - started) / 60000)} min${failed ? `; logs: ${WORK}` : ""}`);
if (!failed) rmSync(WORK, { recursive: true, force: true });
lease.close();
process.exit(failed ? 1 : 0);
