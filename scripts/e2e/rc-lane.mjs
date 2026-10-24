#!/usr/bin/env node
// The standard release-candidate check, in one command (docs/releasing.md §4):
//
//   npm run rc-lane -- <tag> [--keep]        e.g. npm run rc-lane -- v0.28.0-rc1
//
//   a  client matrix --from <last stable> and --from v0.24.0, --to <tag>, lanes macos,linux
//   b  phone lane (testbed/phone.mjs) with the tag's published binary for this host
//   c  chat bridge: ha-nova serve over HTTP, server instructions stripped (as claude.ai),
//      headless Claude, EN + DE, unlock + batch: the code(s) must come last.
//      SKIP without `claude` or $HOME/.claude-ha-nova-test
//   d  slow check (#827): 1,500 automations + a call_service listener; an unlock plan
//      hits the read budget and still applies with its first code
//
// Throwaway only: its own test bed from this checkout (it stops when any bed is
// running, from any tree), CLI homes inside the bed's .state or the work dir
// with the file test keyring (never the host keychain, never a real Home
// Assistant). Cleans up everything at exit; --keep keeps the work dir.
// Prints a summary table; exits non-zero on any FAIL.
import { execFileSync, spawn } from "node:child_process";
import { createHash, randomBytes } from "node:crypto";
import { closeSync, existsSync, mkdirSync, mkdtempSync, openSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import http from "node:http";
import net from "node:net";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const TESTBED = join(ROOT, "scripts/e2e/testbed");
const STATE = join(TESTBED, ".state");
const REPO = "markusleben/ha-nova";
const TAG_RE = /^v\d+\.\d+\.\d+(-rc\d+)?$/;
const [tag, ...flags] = process.argv.slice(2);
if (!TAG_RE.test(tag ?? "") || flags.some((f) => f !== "--keep")) {
  console.error("usage: npm run rc-lane -- <vX.Y.Z[-rcN]> [--keep]");
  process.exit(2);
}
const KEEP = flags.includes("--keep");
const WORK = mkdtempSync(join(tmpdir(), "ha-nova-rc-lane-"));
const rows = [];
const procs = new Set();
let startedBed = false;
let proxy;

// ---- helpers --------------------------------------------------------------------
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const say = (line) => console.log(`rc-lane: ${line}`);
function row(step, check, result, detail, log) {
  rows.push({ step, check, result, detail, log });
  say(`${step} ${check}: ${result}${detail ? ` (${detail})` : ""}`);
}
function tagLt(a, b) { // vX.Y.Z-rcN sorts before vX.Y.Z
  const parse = (t) => { const [v, rc] = t.slice(1).split("-rc"); return [...v.split(".").map(Number), rc === undefined ? Infinity : Number(rc)]; };
  const x = parse(a), y = parse(b);
  for (let i = 0; i < 4; i++) if (x[i] !== y[i]) return x[i] < y[i];
  return false;
}
// run: a child process with its output in a log; resolves {code, out}.
function run(cmd, args, { log, env = process.env, cwd = ROOT, timeoutMs = 30 * 60_000 } = {}) {
  const fd = openSync(log, "a");
  const child = spawn(cmd, args, { cwd, env, stdio: ["ignore", fd, fd] });
  procs.add(child);
  const timer = setTimeout(() => child.kill(), timeoutMs);
  return new Promise((done) => child.on("close", (code) => {
    clearTimeout(timer);
    procs.delete(child);
    closeSync(fd);
    done({ code, out: readFileSync(log, "utf8") });
  }));
}
const freePort = () => new Promise((done) => { const s = net.createServer().listen(0, "127.0.0.1", () => { const { port } = s.address(); s.close(() => done(port)); }); });
const cli = (args, log) => run("bash", [join(TESTBED, "cli.sh"), ...args], { log, env: { ...process.env, TESTBED_CLI_BIN: join(WORK, "bin/ha-nova"), TESTBED_CLI_HOME: join(STATE, "rc-home") } });
// The CLI prints its envelope as one JSON line; stderr lines may surround it.
const json = (text) => { try { return JSON.parse(text.split("\n").find((l) => l.startsWith("{")) ?? "{}"); } catch { return {}; } };

function cleanup() {
  for (const p of procs) p.kill();
  proxy?.close();
  if (startedBed) {
    try { execFileSync("bash", [join(TESTBED, "down.sh")], { stdio: "ignore" }); } catch { console.error(`rc-lane: test bed teardown failed; run: bash ${TESTBED}/down.sh`); }
  }
  if (KEEP) say(`work dir kept: ${WORK}`);
  else rmSync(WORK, { recursive: true, force: true });
}
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => { cleanup(); process.exit(130); });

// ---- setup: the tag's binary and a fresh test bed of our own --------------------
async function fetchBinary() {
  const os = { darwin: "darwin", linux: "linux" }[process.platform];
  const arch = { arm64: "arm64", x64: "amd64" }[process.arch];
  if (!os || !arch) throw new Error(`no published binary for ${process.platform}/${process.arch}`);
  const asset = `ha-nova-${os}-${arch}`;
  const get = async (name) => {
    const res = await fetch(`https://github.com/${REPO}/releases/download/${tag}/${name}`);
    if (!res.ok) throw new Error(`download ${tag}/${name}: HTTP ${res.status}`);
    return Buffer.from(await res.arrayBuffer());
  };
  const [bin, sums] = await Promise.all([get(asset), get("checksums.txt")]);
  const want = sums.toString().split("\n").find((l) => l.trim().endsWith(` ${asset}`))?.split(/\s+/)[0];
  if (!want || createHash("sha256").update(bin).digest("hex") !== want) throw new Error(`${asset} does not match ${tag}'s checksums.txt`);
  mkdirSync(join(WORK, "bin"));
  writeFileSync(join(WORK, "bin/ha-nova"), bin, { mode: 0o755 });
}

function startBed() {
  const running = execFileSync("docker", ["ps", "-aq", "--filter", "label=com.docker.compose.project=ha-nova-testbed"], { encoding: "utf8" }).trim();
  if (running) {
    const wd = execFileSync("docker", ["inspect", "--format", '{{index .Config.Labels "com.docker.compose.project.working_dir"}}', ...running.split("\n")], { encoding: "utf8" }).split("\n")[0];
    throw new Error(`a test bed is already running (${wd}). rc-lane needs a fresh one of its own and never reuses or stops another. If it is yours: bash ${wd}/down.sh`);
  }
  if (existsSync(STATE)) throw new Error(`${STATE} exists from an earlier run; bash ${TESTBED}/down.sh first`);
  say("starting the test bed (torn down at exit)");
  try {
    const fd = openSync(join(WORK, "testbed-up.log"), "a");
    execFileSync("bash", [join(TESTBED, "up.sh")], { stdio: ["ignore", fd, fd] });
  } finally {
    startedBed = existsSync(STATE); // up.sh got past its checks: whatever runs now is ours
  }
}

// ---- a: client matrix ------------------------------------------------------------
async function matrix() {
  const res = await fetch(`https://api.github.com/repos/${REPO}/releases?per_page=100`);
  if (!res.ok) throw new Error(`GitHub releases: HTTP ${res.status}`);
  const stable = (await res.json()).filter((r) => !r.draft && !r.prerelease && TAG_RE.test(r.tag_name) && tagLt(r.tag_name, tag)).map((r) => r.tag_name);
  const last = stable.reduce((a, b) => (tagLt(a, b) ? b : a), stable[0]);
  if (!last) throw new Error(`no stable release older than ${tag}`);
  for (const from of [...new Set([last, "v0.24.0"])].filter((f) => tagLt(f, tag))) {
    const log = join(WORK, `matrix-${from}.log`);
    const t = Date.now();
    const { code, out } = await run("bash", [join(ROOT, "scripts/e2e/client-matrix/run.sh"), "--from", from, "--to", tag, "--lanes", "macos,linux"], { log });
    const count = (r) => (out.match(new RegExp(`^(macos|linux)\\s+\\S+\\s+\\S+\\s+${r}\\b`, "gm")) ?? []).length;
    row("a", `client matrix ${from} -> ${tag}`, code === 0 ? "PASS" : "FAIL", `${count("PASS")} PASS, ${count("FAIL")} FAIL, ${count("SKIP")} SKIP, ${Math.round((Date.now() - t) / 1000)} s`, log);
  }
}

// ---- b: phone lane ---------------------------------------------------------------
async function phone() {
  const log = join(WORK, "phone.log");
  const { code, out } = await run("node", [join(TESTBED, "phone.mjs")], { log, env: { ...process.env, TESTBED_CLI_BIN: join(WORK, "bin/ha-nova") } });
  const n = (r) => (out.match(new RegExp(`^${r}  `, "gm")) ?? []).length;
  row("b", `phone lane, ${tag} binary`, code === 0 ? "PASS" : "FAIL", `${n("PASS")} PASS, ${n("FAIL")} FAIL: link, move, stop, confirm, result on iOS + Android`, log);
}

// ---- c: chat bridge, code last ----------------------------------------------------
const PROMPTS = {
  "EN unlock": "Unlock the front door in the demo house.",
  "DE unlock": "Schließ im Demo-Haus die Front Door auf.",
  "EN batch": "In the demo house, unlock the front door and open the garage door.",
  "DE batch": "Im Demo-Haus: Schließ die Front Door auf und öffne die Garage Door.",
};
const TOOLS = ["ha_skill", "ha_find", "ha_get", "ha_history", "ha_map", "ha_read", "ha_trace", "ha_plan", "ha_call"].map((t) => `mcp__hanova__${t}`).join(",");

// The code(s) are the last lines: every line from the first code on is a
// ("<name>: ")**NNNN** line, one per change.
function codeLast(text, codes) {
  const lines = text.split("\n").filter((l) => l.trim());
  const first = lines.findIndex((l) => /\*\*\d{4}\*\*/.test(l));
  const tail = first < 0 ? [] : lines.slice(first);
  const ok = tail.length === codes && tail.every((l) => /^(?:[^*\n]+: )?\*\*\d{4}\*\*\s*$/.test(l));
  return { ok, last: (lines.at(-1) ?? "(empty)").replace(/\d{4}/g, "NNNN").slice(-60) };
}

async function chat() {
  const config = join(homedir(), ".claude-ha-nova-test");
  let hasClaude = true;
  try { execFileSync("claude", ["--version"], { stdio: "ignore" }); } catch { hasClaude = false; }
  if (!hasClaude || !existsSync(config)) return row("c", "chat bridge code last", "SKIP", hasClaude ? `${config} missing` : "no claude on PATH");
  const home = join(WORK, "bridge-home"), data = join(WORK, "bridge-data"), secret = randomBytes(32).toString("hex"), user = randomBytes(16).toString("hex");
  mkdirSync(join(home, ".config/ha-nova"), { recursive: true, mode: 0o700 });
  mkdirSync(data, { mode: 0o700 });
  writeFileSync(join(home, ".config/ha-nova/demo.json"), JSON.stringify({ ha_url: "http://127.0.0.1:18123", relay_url: "http://127.0.0.1:18791", relay_token: readFileSync(join(STATE, "relay-token"), "utf8").trim() }), { mode: 0o600 });
  writeFileSync(join(data, "chat-apps.json"), JSON.stringify({ allowed_users: [user] }), { mode: 0o600 });
  writeFileSync(join(WORK, "secret"), secret, { mode: 0o600 });
  const [bridgePort, proxyPort] = [await freePort(), await freePort()];
  const log = join(WORK, "chat.log");
  run(join(WORK, "bin/ha-nova"), ["serve", "--server", "demo", "--listen", `127.0.0.1:${bridgePort}`, "--data-dir", data, "--secret-file", join(WORK, "secret")], {
    log, env: { PATH: process.env.PATH, HOME: home, XDG_CONFIG_HOME: join(home, ".config"), XDG_CACHE_HOME: join(home, ".cache"), HA_NOVA_NO_BROWSER: "1",
      HA_NOVA_ALLOW_INSECURE_TEST_KEYRING: "1", HA_NOVA_TEST_KEYRING_FILE: join(home, ".test-relay-token"), HA_NOVA_KEYRING_SERVICE: "ha-nova-rc-lane" },
  });
  // claude.ai passes the model no server instructions: drop them on the way.
  let stripped = 0;
  const strip = (m) => { if (m?.result && "instructions" in m.result) { delete m.result.instructions; stripped++; } };
  proxy = http.createServer(async (req, res) => {
    const chunks = [];
    for await (const c of req) chunks.push(c);
    const headers = { ...req.headers };
    delete headers.host;
    delete headers["content-length"];
    try {
      const r = await fetch(`http://127.0.0.1:${bridgePort}${req.url}`, { method: req.method, headers, body: ["GET", "HEAD"].includes(req.method) ? undefined : Buffer.concat(chunks) });
      const text = (await r.text()).replace(/^(data: )?(\{.*\})$/gm, (line, prefix = "", body) => {
        try { const m = JSON.parse(body); [m].flat().forEach(strip); return prefix + JSON.stringify(m); } catch { return line; }
      });
      const out = {};
      r.headers.forEach((v, k) => { if (!["content-length", "content-encoding", "transfer-encoding"].includes(k)) out[k] = v; });
      res.writeHead(r.status, out).end(text);
    } catch (err) {
      res.writeHead(502).end(String(err));
    }
  }).listen(proxyPort, "127.0.0.1");
  for (let i = 0; i < 60 && !(await fetch(`http://127.0.0.1:${bridgePort}/mcp`).then(() => true, () => false)); i++) await sleep(500);
  const mcp = join(WORK, "mcp.json");
  writeFileSync(mcp, JSON.stringify({ mcpServers: { hanova: { type: "http", url: `http://127.0.0.1:${proxyPort}/mcp`, headers: { Authorization: `Bearer ${secret}`, "X-Ha-User-Id": user } } } }), { mode: 0o600 });
  mkdirSync(join(WORK, "claude-cwd"));
  for (const [name, prompt] of Object.entries(PROMPTS)) {
    const out = join(WORK, `claude-${name.replace(" ", "-")}.json`);
    const t = Date.now();
    const { code } = await run("claude", ["-p", prompt, "--model", "sonnet", "--strict-mcp-config", "--mcp-config", mcp, "--tools", "",
      "--allowedTools", TOOLS, "--disallowedTools", "mcp__hanova__ha_apply", "--output-format", "json", "--no-session-persistence"],
    { log: out, cwd: join(WORK, "claude-cwd"), timeoutMs: 5 * 60_000, env: { ...process.env, CLAUDE_CONFIG_DIR: config, ENABLE_CLAUDEAI_MCP_SERVERS: "false" } });
    const reply = json(readFileSync(out, "utf8")).result ?? "";
    const { ok, last } = codeLast(reply, name.endsWith("batch") ? 2 : 1);
    const secs = Math.round((Date.now() - t) / 1000);
    row("c", `chat bridge code last, ${name}`, code === 0 && ok && stripped > 0 ? "PASS" : "FAIL",
      `${stripped ? "instructions stripped" : "NO instructions stripped"}, last line ${JSON.stringify(last)}, ${secs} s`, out);
  }
}

// ---- d: slow check (#827) -----------------------------------------------------------
async function slow() {
  const log = join(WORK, "slow.log");
  const fillers = Array.from({ length: 1500 }, (_, i) => `- id: rclane_filler_${i}\n  alias: RC lane filler ${i}\n  triggers:\n  - trigger: event\n    event_type: rclane_filler_${i}\n  actions:\n  - action: input_boolean.toggle\n    target:\n      entity_id: input_boolean.guest_mode\n`).join("")
    + "- id: rclane_listener\n  alias: RC lane call_service listener\n  triggers:\n  - trigger: event\n    event_type: call_service\n  conditions:\n  - condition: template\n    value_template: \"{{ false }}\"\n  actions:\n  - action: input_boolean.toggle\n    target:\n      entity_id: input_boolean.guest_mode\n";
  execFileSync("docker", ["exec", "-i", "ha-nova-testbed-ha", "sh", "-c", "printf '\\n' >> /config/automations.yaml && cat >> /config/automations.yaml"], { input: fillers });
  const llat = readFileSync(join(STATE, "ha-llat"), "utf8").trim();
  const ha = (path, method = "GET") => fetch(`http://127.0.0.1:18123${path}`, { method, headers: { authorization: `Bearer ${llat}`, "content-type": "application/json" }, body: method === "POST" ? "{}" : undefined });
  await ha("/api/services/automation/reload", "POST");
  let on = 0;
  for (let i = 0; i < 120 && on < 1500; i++, await sleep(1000)) on = (await (await ha("/api/states")).json()).filter((s) => s.entity_id.startsWith("automation.") && s.state === "on").length;
  await cli(["version"], log);
  const cfg = join(STATE, "rc-home/.config/ha-nova/config.json");
  writeFileSync(cfg, JSON.stringify({ ...JSON.parse(readFileSync(cfg, "utf8")), confirm_window: "off" }), { mode: 0o600 }); // codes in chat: no window on this desktop
  await cli(["call", "lock.lock", "lock.front_door"], log);
  let t = Date.now();
  const plan = json((await cli(["plan", "call", "lock.unlock", "lock.front_door"], join(WORK, "slow-plan.json"))).out);
  const planSecs = (Date.now() - t) / 1000;
  const limit = (plan.reasons ?? []).some((r) => r.code === "LIMIT_CUT");
  t = Date.now();
  const applied = plan.plan?.code ? json((await cli(["apply", "--plan", plan.plan.id, "--confirm", plan.plan.code], join(WORK, "slow-apply.json"))).out) : {};
  const hit = limit || planSecs >= 25;
  row("d", "slow check: first code applies (#827)", hit && applied.status === "verified" ? "PASS" : "FAIL",
    `${on} automations on, plan ${planSecs.toFixed(1)} s${limit ? " LIMIT_CUT" : ""}${hit ? "" : " (budget NOT hit)"}, apply ${((Date.now() - t) / 1000).toFixed(1)} s -> ${applied.status ?? plan.status}${applied.error ? ` ${applied.error.code}` : ""}`, join(WORK, "slow-apply.json"));
  await cli(["call", "lock.lock", "lock.front_door"], log);
}

// ---- main ---------------------------------------------------------------------------
const started = Date.now();
let crashed = false;
try {
  const head = execFileSync("git", ["-C", ROOT, "rev-parse", "--short", "HEAD"], { encoding: "utf8" }).trim();
  const relay = (ref) => { try { return execFileSync("git", ["-C", ROOT, "show", `${ref}:nova/config.yaml`], { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).match(/^version:\s*"?([^"\n]+)/m)?.[1]; } catch { return undefined; } };
  say(`candidate ${tag}; harness and test bed Relay ${relay("HEAD")} from this checkout (${head}); work dir ${WORK}`);
  if (relay(tag) && relay(tag) !== relay("HEAD")) say(`NOTE: ${tag} ships Relay ${relay(tag)}; run from a checkout at the tag to test that one`);
  await fetchBinary();
  startBed();
  for (const [step, fn] of [["a", matrix], ["b", phone], ["c", chat], ["d", slow]]) {
    try { await fn(); } catch (err) { row(step, "step ran", "FAIL", err.message); }
  }
} catch (err) {
  crashed = true;
  row("-", "setup", "FAIL", err.message);
}
const failed = rows.filter((r) => r.result === "FAIL");
for (const r of failed.filter((r) => r.log && existsSync(r.log))) {
  console.log(`\n--- ${r.step} ${r.check}: last lines of ${r.log}`);
  console.log(readFileSync(r.log, "utf8").split("\n").slice(-40).join("\n"));
}
const w = (k) => Math.max(k.length, ...rows.map((r) => String(r[k]).length));
console.log(`\n${"STEP".padEnd(4)}  ${"CHECK".padEnd(w("check"))}  RESULT  DETAIL`);
for (const r of rows) console.log(`${r.step.padEnd(4)}  ${r.check.padEnd(w("check"))}  ${r.result.padEnd(6)}  ${r.detail ?? ""}`);
cleanup();
console.log(`\nrc-lane ${tag}: ${failed.length ? `${failed.length} FAIL` : "all PASS"} in ${Math.round((Date.now() - started) / 60000)} min${crashed ? " (setup failed)" : ""}`);
process.exit(failed.length ? 1 : 0);
