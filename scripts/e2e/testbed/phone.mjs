#!/usr/bin/env node
// Phone lane: the push payloads HA NOVA sends to iPhone and Android phones,
// as Home Assistant hands them to the push relay. Opt-in on a running bed:
//
//   TESTBED_PHONE=1 npm run testbed:verify     (or: node scripts/e2e/testbed/phone.mjs)
//
// Fixture (once per bed, seed.mjs phones): two FAKE Companion phones of the
// admin "Phone Tester" (maker Apple / Google) whose push_url is the bed's own
// push service (push.py, 127.0.0.1:18792), and the Relay signed in as the
// admin "Testbed Relay" (on the owner's token phone confirmation is off by
// design). Nothing reaches a real phone. A "tap" is what the Companion app
// does: fire_event on the phone's own webhook (its user, origin REMOTE).
// The CLI runs with its own home (.state/phone-home, windows off), so the
// link never leaks into the verify home. TESTBED_CLI_BIN picks the binary.
// Prints PASS/FAIL lines; exits non-zero on any FAIL.
import { execFileSync, spawn } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const STATE = join(HERE, ".state");
const HOME = join(STATE, "phone-home");
const HA = "http://127.0.0.1:18123";
const PUSH = "http://127.0.0.1:18792";
const PHONES = { ios: "Testbed iPhone", android: "Testbed Pixel" };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const read = (name) => readFileSync(join(STATE, name), "utf8").trim();
let failed = 0;

function report(name, problems) {
  if (problems.length) failed++;
  console.log(problems.length ? `FAIL  ${name}: ${problems.join("; ")}` : `PASS  ${name}`);
}

async function waitFor(what, check, ms = 60_000) {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(500)) {
    if (await check().catch(() => false)) return;
  }
  throw new Error(`timed out waiting for ${what}`);
}

async function fixture() {
  if (!existsSync(join(STATE, "relay-token"))) throw new Error("test bed not running; npm run testbed:up first");
  // It recreates the Relay: only ever on this tree's own bed.
  const wd = execFileSync("docker", ["inspect", "--format", '{{index .Config.Labels "com.docker.compose.project.working_dir"}}', "ha-nova-testbed-ha"], { encoding: "utf8" }).trim();
  if (wd !== HERE) throw new Error(`the running test bed is not this tree's (${wd}); nothing changed`);
  const file = join(STATE, "phone.json");
  if (!existsSync(file)) {
    const out = execFileSync("node", [join(HERE, "seed.mjs"), "phones", HA, read("ha-llat")], { encoding: "utf8", stdio: ["ignore", "pipe", "inherit"] });
    writeFileSync(file, out, { mode: 0o600 });
  }
  const fx = JSON.parse(readFileSync(file, "utf8"));
  // Compose recreates the Relay only when its token changed.
  execFileSync("docker", ["compose", "-f", join(HERE, "docker-compose.yml"), "up", "-d", "--no-deps", "--no-build", "relay", "push"], {
    env: { ...process.env, RELAY_AUTH_TOKEN: read("relay-token"), HA_LLAT: fx.relay_llat }, stdio: ["ignore", "ignore", "pipe"], // its error says why
  });
  const health = async () => (await (await fetch("http://127.0.0.1:18791/health", { headers: { authorization: `Bearer ${read("relay-token")}` } })).json()).data?.status === "ok"; // it connects to Home Assistant on first use
  await waitFor("the Relay as Testbed Relay", health);
  // Docker's own health check too (30 s interval): a client matrix after this lane needs "healthy".
  await waitFor("the Relay container healthy", async () => execFileSync("docker", ["inspect", "-f", "{{.State.Health.Status}}", "ha-nova-testbed-relay"], { encoding: "utf8" }).trim() === "healthy", 90_000);
  await waitFor("the push service", async () => (await fetch(`${PUSH}/pushes`)).ok);
  // A fresh home every run: a link left by an aborted run never carries over.
  rmSync(HOME, { recursive: true, force: true });
  mkdirSync(HOME, { recursive: true });
  await cli(["version"]).done;
  const config = join(HOME, ".config/ha-nova/config.json");
  writeFileSync(config, JSON.stringify({ ...JSON.parse(readFileSync(config, "utf8")), confirm_window: "off" }), { mode: 0o600 });
}

const children = new Set();
function cli(args) {
  const child = spawn("bash", [join(HERE, "cli.sh"), ...args], { env: { ...process.env, TESTBED_CLI_HOME: HOME }, stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "", stderr = "";
  child.stdout.on("data", (d) => (stdout += d));
  child.stderr.on("data", (d) => (stderr += d));
  children.add(child);
  child.on("close", () => children.delete(child));
  return { child, done: new Promise((resolve) => child.on("close", (code) => resolve({ code, stdout, stderr }))) };
}

// An unanswered ask must not outlive the lane, however it ends.
process.on("exit", () => { for (const child of children) child.kill(); });
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => process.exit(130));

let seen = 0;
async function nextPush() {
  let list = [];
  await waitFor("a push", async () => (list = await (await fetch(`${PUSH}/pushes`)).json()).length > seen, 30_000);
  const push = list[seen++];
  if (push.body.message === "clear_notification") return nextPush(); // a newer CLI clears its last result first
  return { ...push, platform: push.path.split("/").pop(), data: push.body.data ?? {} };
}

async function tap(push) {
  const res = await fetch(`${HA}/api/webhook/${push.body.registration_info.webhook_id}`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ type: "fire_event", data: { event_type: "mobile_app_notification_action", event_data: { action: push.data.actions[0].action } } }),
  });
  if (!res.ok) throw new Error(`tap -> HTTP ${res.status}`);
}

// The line naming button says this phone's gesture (cli/confirm_phone_text.go
// phoneTapLine): press and hold on iOS, pull down / its arrow on Android.
// Matched loosely, so a copy edit doesn't break the lane; the wrong phone's
// gesture or the both-phones line does.
const gesture = (platform, button, text) => text.split("\n").filter((l) => l.includes(`“${button}”`)).some((l) => platform === "ios"
  ? l.includes(`Press and hold the notification, then tap “${button}”`)
  : l.includes("tap its arrow") && !l.includes("Press and hold"));

const tags = new Set();
// One question push against the contract; returns it for the tap.
async function question(platform, button, { unlock, urgent }) {
  const push = await nextPush();
  const { body: b, data: d } = push, a = d.actions ?? [], p = [];
  const want = (ok, why) => ok || p.push(why);
  want(push.platform === platform, `went to ${push.platform}`);
  want(a.length === 2 && a[0].title === button && a[1].title === "Cancel", `buttons ${JSON.stringify(a.map((x) => x.title))}`);
  want(a[0]?.destructive === true, "first button not destructive");
  want(a.every((x) => /^HANOVA_(OK|NO)_[0-9a-f]{32}$/.test(x.action)), "action ids");
  const auth = a[0]?.authenticationRequired;
  if (!unlock) want(auth === undefined, `authenticationRequired ${JSON.stringify(auth)} on a plan confirm`);
  else if (platform === "ios") want(auth === true, `authenticationRequired ${JSON.stringify(auth)}, iOS needs the bool true`);
  else want(auth === "true", `authenticationRequired ${JSON.stringify(auth)}, Android needs the string "true"`);
  if (platform === "android") want(a.every((x) => Object.entries(x).every(([k, v]) => k === "destructive" || typeof v === "string")), "an action value is not a string (FCM data takes strings only)");
  // The hint: last on iOS, first on Android (a collapsed notification shows one line).
  const m = b.message ?? "", pull = m.indexOf("Pull down");
  want(platform === "ios" ? m.endsWith("Press and hold to answer.") : pull >= 0 && pull < m.indexOf("Requested") && !m.includes("Press and hold"), `message ${JSON.stringify(m)}`);
  want(b.title && [...b.title].length <= 80, `title ${JSON.stringify(b.title)}`);
  want(d.channel === "HA NOVA" && d.importance === "high" && d.priority === "high", `channel/importance/priority ${d.channel}/${d.importance}/${d.priority}`);
  want(d.push?.["interruption-level"] === (urgent ? "time-sensitive" : "active"), `interruption-level ${d.push?.["interruption-level"]}`);
  want(Number.isInteger(d.ttl) && d.ttl > 0 && Number.isInteger(d.timeout) && d.timeout > 0 && /^\d+$/.test(d.apns_headers?.["apns-expiration"] ?? ""), "ttl/timeout/apns-expiration");
  want(/^ha-nova-(link|setting|p_[0-9a-f]+)-[0-9a-f]{8}$/.test(d.tag) && !tags.has(d.tag), `tag ${d.tag} is not its own`);
  tags.add(d.tag);
  report(`${button} question on ${PHONES[platform]}: ${unlock ? (platform === "ios" ? "bool" : "string") + " authenticationRequired" : "no unlock"}, ${platform} hint`, p);
  await tap(push);
  return push;
}

// The result replaces exactly its question: same tag, no buttons.
async function result(ask, prefix) {
  const push = await nextPush();
  const { body: b, data: d } = push, p = [];
  const want = (ok, why) => ok || p.push(why);
  want(push.platform === ask.platform, `went to ${push.platform}`);
  want(b.title?.startsWith(prefix), `title ${JSON.stringify(b.title)}`);
  want(d.tag === ask.data.tag, `tag ${d.tag}, question had ${ask.data.tag}`);
  want(d.actions === undefined, "has buttons");
  want(d.channel === "HA NOVA" && d.importance === "high" && d.push?.["interruption-level"] === "active", "channel/importance/interruption-level");
  report(`result on ${PHONES[ask.platform]}: “${prefix}…”, the question's tag`, p);
}

function finished(name, run, { code = 0, out = "", tap }) {
  const p = [];
  if (run.code !== code) p.push(`exit ${run.code}, want ${code}`);
  if (out && !run.stdout.includes(out)) p.push(`stdout lacks ${JSON.stringify(out)}: ${run.stdout.slice(0, 300)}`);
  if (tap && !gesture(...tap, run.stderr)) p.push(`stderr lacks the ${tap[0]} way to tap “${tap[1]}”: ${run.stderr.slice(0, 300)}`);
  report(name, p);
}

async function link(platform) {
  const run = cli(["setup", "confirm-phone", PHONES[platform]]);
  const ask = await question(platform, "Allow", { unlock: true });
  await result(ask, "Linked");
  finished(`link ${PHONES[platform]}`, await run.done, { out: `Linked “${PHONES[platform]}”`, tap: [platform, "Allow"] });
}

async function move(from, to) {
  const run = cli(["setup", "confirm-phone", PHONES[to]]);
  const ask = await question(from, "Move", { unlock: true });
  await result(ask, "Approved");
  const linkAsk = await question(to, "Allow", { unlock: true });
  await result(linkAsk, "Linked");
  finished(`move ${PHONES[from]} -> ${PHONES[to]}`, await run.done, { out: `Linked “${PHONES[to]}”`, tap: [from, "Move"] });
}

async function stop(platform) {
  const run = cli(["setup", "confirm-phone", "off"]);
  const ask = await question(platform, "Stop", { unlock: true });
  await result(ask, "Approved");
  finished(`stop on ${PHONES[platform]}`, await run.done, { out: "Phone confirmation is off", tap: [platform, "Stop"] });
}

async function confirmUnlock(platform) {
  await cli(["call", "lock.lock", "lock.front_door"]).done; // locked again (ordinary, no confirmation)
  const planned = await cli(["plan", "call", "lock.unlock", "lock.front_door"]).done;
  const plan = JSON.parse(planned.stdout || "{}");
  report(`unlock plan names the ${platform} gesture`, plan.plan?.channel === "phone" && gesture(platform, "Unlock", plan.say ?? "") ? [] : [`channel ${plan.plan?.channel}, say ${JSON.stringify(plan.say)}`]);
  if (!plan.plan?.id) return;
  const run = cli(["apply", "--plan", plan.plan.id]);
  const ask = await question(platform, "Unlock", { unlock: false, urgent: true });
  await result(ask, "Done");
  finished(`unlock confirmed on ${PHONES[platform]}`, await run.done, { out: '"status":"verified"', tap: [platform, "Unlock"] });
}

try {
  await fixture();
  seen = (await (await fetch(`${PUSH}/pushes`)).json()).length;
  await link("ios");
  await confirmUnlock("ios");
  await move("ios", "android");
  await confirmUnlock("android");
  await move("android", "ios");
  await stop("ios");
  await link("android");
  await stop("android");
  await cli(["call", "lock.lock", "lock.front_door"]).done;
  const extra = (await (await fetch(`${PUSH}/pushes`)).json()).slice(seen).filter((p) => p.body.message !== "clear_notification");
  report("no push beyond the asked phones", extra.map((p) => `${p.path}: ${p.body.title}`));
} catch (err) {
  report("phone lane ran to the end", [err.message]);
}
process.exit(failed ? 1 : 0);
