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

let seen = 0;
async function nextPush() {
  let list = [];
  await waitFor("a push", async () => (list = await (await fetch(`${PUSH}/pushes`)).json()).length > seen, 30_000);
  const push = list[seen++];
  return { ...push, platform: push.path.split("/").pop(), data: push.body.data ?? {} };
}

async function tap(push) {
  const res = await fetch(`${HA}/api/webhook/${push.body.registration_info.webhook_id}`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ type: "fire_event", data: { event_type: "mobile_app_notification_action", event_data: { action: push.data.actions[0].action } } }),
  });
  if (!res.ok) throw new Error(`tap -> HTTP ${res.status}`);
}

// How to answer on each phone (cli/confirm_phone_text.go phoneTapLine).
const tapLine = (platform, button) => platform === "ios"
  ? `Press and hold the notification, then tap “${button}”.`
  : `Pull the notification down, or tap its arrow, then tap “${button}”.`;

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
  want(platform === "ios" ? b.message?.endsWith("Press and hold to answer.") : b.message?.startsWith("Pull down to answer."), `message ${JSON.stringify(b.message)}`);
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

function finished(name, run, { code = 0, out = "", err = "" }) {
  const p = [];
  if (run.code !== code) p.push(`exit ${run.code}, want ${code}`);
  if (out && !run.stdout.includes(out)) p.push(`stdout lacks ${JSON.stringify(out)}: ${run.stdout.slice(0, 300)}`);
  if (err && !run.stderr.includes(err)) p.push(`stderr lacks ${JSON.stringify(err)}: ${run.stderr.slice(0, 300)}`);
  report(name, p);
}

async function link(platform) {
  const run = cli(["setup", "confirm-phone", PHONES[platform]]);
  const ask = await question(platform, "Allow", { unlock: true });
  await result(ask, "Linked");
  finished(`link ${PHONES[platform]}`, await run.done, { out: `Linked “${PHONES[platform]}”`, err: tapLine(platform, "Allow") });
}

async function move(from, to) {
  const run = cli(["setup", "confirm-phone", PHONES[to]]);
  const ask = await question(from, "Move", { unlock: true });
  await result(ask, "Approved");
  const linkAsk = await question(to, "Allow", { unlock: true });
  await result(linkAsk, "Linked");
  finished(`move ${PHONES[from]} -> ${PHONES[to]}`, await run.done, { out: `Linked “${PHONES[to]}”`, err: tapLine(from, "Move") });
}

async function stop(platform) {
  const run = cli(["setup", "confirm-phone", "off"]);
  const ask = await question(platform, "Stop", { unlock: true });
  await result(ask, "Approved");
  finished(`stop on ${PHONES[platform]}`, await run.done, { out: "Phone confirmation is off" });
}

async function confirmUnlock(platform) {
  await cli(["call", "lock.lock", "lock.front_door"]).done; // locked again (ordinary, no confirmation)
  const planned = await cli(["plan", "call", "lock.unlock", "lock.front_door"]).done;
  const plan = JSON.parse(planned.stdout || "{}");
  report(`unlock plan names the ${platform} gesture`, plan.plan?.channel === "phone" && plan.say?.includes(tapLine(platform, "Unlock")) ? [] : [`channel ${plan.plan?.channel}, say ${JSON.stringify(plan.say)}`]);
  if (!plan.plan?.id) return;
  const run = cli(["apply", "--plan", plan.plan.id]);
  const ask = await question(platform, "Unlock", { unlock: false, urgent: true });
  await result(ask, "Done");
  finished(`unlock confirmed on ${PHONES[platform]}`, await run.done, { out: '"status":"verified"', err: tapLine(platform, "Unlock") });
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
} catch (err) {
  report("phone lane ran to the end", [err.message]);
}
for (const child of children) child.kill(); // an unanswered ask must not outlive the lane
process.exit(failed ? 1 : 0);
