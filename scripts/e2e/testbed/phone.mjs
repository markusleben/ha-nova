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
// The bed's Relay gates (0.30): it holds the phone link and its safety-check
// worker asks the phone; after the answer it withdraws the question. A fresh
// Relay every run, so no link, ask, or grant carries over. gate.mjs and
// rc-lane.mjs reuse these helpers.
// Prints PASS/FAIL lines; exits non-zero on any FAIL.
import { execFileSync, spawn } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "../../..");
const STATE = join(HERE, ".state");
const HOME = join(STATE, "phone-home");
const HA = "http://127.0.0.1:18123";
const PUSH = "http://127.0.0.1:18792";
const PHONES = { ios: "Testbed iPhone", android: "Testbed Pixel" };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
export const read = (name) => readFileSync(join(STATE, name), "utf8").trim();
let failed = 0;
export const failures = () => failed;

export function report(name, problems) {
  if (problems.length) failed++;
  console.log(problems.length ? `FAIL  ${name}: ${problems.join("; ")}` : `PASS  ${name}`);
}

const skip = (name, why) => console.log(`SKIP  ${name}: ${why}`);

async function waitFor(what, check, ms = 60_000) {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(500)) {
    if (await check().catch(() => false)) return;
  }
  throw new Error(`timed out waiting for ${what}`);
}

export async function fixture() {
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
  // A fresh Relay (its link, asks, and grants live in it) signed in as
  // Testbed Relay; it keeps file access where up.sh gave it (TESTBED_FILES=1
  // leaves .state/files).
  const files = existsSync(join(STATE, "files")) ? ["-f", join(HERE, "docker-compose.files.yml")] : [];
  execFileSync("docker", ["compose", "-f", join(HERE, "docker-compose.yml"), ...files, "up", "-d", "--no-deps", "--no-build", "--force-recreate", "relay", "push"], {
    env: { ...process.env, RELAY_AUTH_TOKEN: read("relay-token"), HA_LLAT: fx.relay_llat }, stdio: ["ignore", "ignore", "pipe"], // its error says why
  });
  const health = async () => (await (await fetch("http://127.0.0.1:18791/health", { headers: { authorization: `Bearer ${read("relay-token")}` } })).json()).data?.status === "ok"; // it connects to Home Assistant on first use
  await waitFor("the Relay as Testbed Relay", health);
  // Docker's own health check too (30 s interval): a client matrix after this lane needs "healthy".
  await waitFor("the Relay container healthy", async () => execFileSync("docker", ["inspect", "-f", "{{.State.Health.Status}}", "ha-nova-testbed-relay"], { encoding: "utf8" }).trim() === "healthy", 90_000);
  await waitFor("the push service", async () => (await fetch(`${PUSH}/pushes`)).ok);
  // Its safety-check worker starts after the listener; until it answers, every request gets 503.
  await waitFor("the Relay's safety check", async () => (await relay("/core", { method: "GET", path: "/api/config" })).status === 200);
  // A fresh home every run: a link left by an aborted run never carries over.
  rmSync(HOME, { recursive: true, force: true });
  mkdirSync(HOME, { recursive: true });
  await cli(["version"]).done;
  const config = join(HOME, ".config/ha-nova/config.json");
  writeFileSync(config, JSON.stringify({ ...JSON.parse(readFileSync(config, "utf8")), confirm_window: "off" }), { mode: 0o600 });
}

// A request to the Relay as its own client (the shared token), with any extra headers.
export async function relay(path, body, headers = {}) {
  const res = await fetch(`http://127.0.0.1:18791${path}`, { method: "POST", headers: { authorization: `Bearer ${read("relay-token")}`, "content-type": "application/json", ...headers }, body: JSON.stringify(body) });
  const answer = await res.json().catch(() => null);
  return { status: res.status, data: answer?.data, error: answer?.error };
}

const children = new Set();
export function cli(args) {
  const child = spawn("bash", [join(HERE, "cli.sh"), ...args], { env: { ...process.env, TESTBED_CLI_HOME: HOME }, stdio: ["ignore", "pipe", "pipe"] });
  let stdout = "", stderr = "";
  child.stdout.on("data", (d) => (stdout += d));
  child.stderr.on("data", (d) => (stderr += d));
  children.add(child);
  child.on("close", () => children.delete(child));
  return { child, done: new Promise((resolve) => child.on("close", (code) => resolve({ code, stdout, stderr }))) };
}

const boxes = new Set(); // names of the case's containers
// An unanswered ask must not outlive the lane, however it ends.
process.on("exit", () => { for (const child of children) child.kill(); });

let seen = 0;
const lastResult = {}; // per platform: the tag of the last result shown, or of the question withdrawn last
// From now on, only pushes sent after this call count.
export async function resetPushes() {
  seen = (await (await fetch(`${PUSH}/pushes`)).json()).length;
}

// The next push; raw: a clear too. Since 0.27.3 a phone's last result is
// cleared before the next question: a clear may remove only that result (or
// a question already withdrawn), never an open question.
async function nextPush({ raw = false, ms = 30_000 } = {}) {
  let list = [];
  await waitFor("a push", async () => (list = await (await fetch(`${PUSH}/pushes`)).json()).length > seen, ms);
  const push = { ...list[seen++] };
  Object.assign(push, { platform: push.path.split("/").pop(), data: push.body.data ?? {} });
  if (push.body.message === "clear_notification" && !raw) {
    const tag = push.data.tag;
    report(`clear on ${PHONES[push.platform]} removes only its last result`, tag && tag === lastResult[push.platform] ? [] : [`cleared ${tag}, last result ${lastResult[push.platform]}`]);
    return nextPush({ ms });
  }
  return push;
}

// answer 0 taps the question's own button, 1 its Cancel.
async function tap(push, answer = 0) {
  const res = await fetch(`${HA}/api/webhook/${push.body.registration_info.webhook_id}`, {
    method: "POST", headers: { "content-type": "application/json" },
    body: JSON.stringify({ type: "fire_event", data: { event_type: "mobile_app_notification_action", event_data: { action: push.data.actions[answer].action } } }),
  });
  if (!res.ok) throw new Error(`tap -> HTTP ${res.status}`);
}

// The line naming button says this phone's gesture (phoneTapLine, keys
// phone.tap.* in cli/confirm_strings.en.json): press and hold on iOS, pull
// down / its arrow on Android.
// Matched loosely, so a copy edit doesn't break the lane; the wrong phone's
// gesture or the both-phones line does.
const gesture = (platform, button, text) => text.split("\n").filter((l) => l.includes(`“${button}”`)).some((l) => platform === "ios"
  ? l.includes(`Press and hold the notification, then tap “${button}”`)
  : l.includes("tap its arrow") && !l.includes("Press and hold"));

const tags = new Set();
// One question push against the contract, then the tap (answer: tap).
// button: its expected title; none: any. Returns the push.
export async function question(platform, button, { unlock, urgent, answer = 0 }) {
  const push = await nextPush();
  const { body: b, data: d } = push, a = d.actions ?? [], p = [];
  const want = (ok, why) => ok || p.push(why);
  button ??= a[0]?.title;
  want(push.platform === platform, `went to ${push.platform}`);
  want(a.length === 2 && button && a[0].title === button && a[1].title === "Cancel", `buttons ${JSON.stringify(a.map((x) => x.title))}`);
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
  want(/^ha-nova-(link|setting|gate|p_[0-9a-f]+)-[0-9a-f]{8}$/.test(d.tag) && !tags.has(d.tag), `tag ${d.tag} is not its own`);
  tags.add(d.tag);
  report(`${button} question on ${PHONES[platform]}: ${unlock ? (platform === "ios" ? "bool" : "string") + " authenticationRequired" : "no unlock"}, ${platform} hint`, p);
  await tap(push, answer);
  return push;
}

// The Relay's worker withdraws its question once answered (gatePhoneTap): a
// clear of exactly its tag on that phone.
// TODO(MAR-143): with #1090 in the base the withdraw replaces the question
// instead (same tag, no buttons): the answer itself when answered on the
// phone, "Approved on the NOVA page" (no page on this bed), "Not confirmed"
// on a timeout. Then check that result here, as result() did before 0.30.
export async function withdrawn(ask) {
  const push = await nextPush({ raw: true });
  report(`question on ${PHONES[ask.platform]} withdrawn after the answer`, push.platform === ask.platform && push.body.message === "clear_notification" && push.data.tag === ask.data.tag
    ? [] : [`next push on ${push.platform}: ${push.body.message === "clear_notification" ? `clear of ${push.data.tag}` : JSON.stringify(push.body.title)}, question had ${ask.data.tag}`]);
  lastResult[ask.platform] = ask.data.tag;
}

// Taps the next question, wherever the Relay asks (rc-lane's slow check).
export async function approve(ms) {
  const push = await nextPush({ ms });
  await tap(push);
  return push;
}

function finished(name, run, { code = 0, out = "", tap }) {
  const p = [];
  if (run.code !== code) p.push(`exit ${run.code}, want ${code}`);
  if (out && !run.stdout.includes(out)) p.push(`stdout lacks ${JSON.stringify(out)}: ${run.stdout.slice(0, 300)}`);
  if (tap && !gesture(...tap, `${run.stdout}\n${run.stderr}`)) p.push(`output lacks the ${tap[0]} way to tap “${tap[1]}”: ${run.stdout.slice(0, 300)}`);
  report(name, p);
}

// On a gated App the Relay links (setup confirm-phone, linkPhoneAtRelay):
// the phone linked there now confirms the move first, then the new one.
export async function link(platform, from) {
  const run = cli(["setup", "confirm-phone", PHONES[platform]]);
  if (from) await withdrawn(await question(from, "Move", { unlock: true }));
  await withdrawn(await question(platform, "Allow", { unlock: true }));
  finished(from ? `move ${PHONES[from]} -> ${PHONES[platform]}` : `link ${PHONES[platform]}`, await run.done, { out: `Linked “${PHONES[platform]}”`, tap: [platform, "Allow"] });
}

const lockState = async () => (await (await fetch(`${HA}/api/states/lock.front_door`, { headers: { authorization: `Bearer ${read("ha-llat")}` } })).json()).state;

// Locks the door again through Home Assistant itself: the lane's own setup,
// not a step under test (HA NOVA rates locking this registry-less lock typed).
export async function relock() {
  await fetch(`${HA}/api/services/lock/lock`, { method: "POST", headers: { authorization: `Bearer ${read("ha-llat")}`, "content-type": "application/json" }, body: JSON.stringify({ entity_id: "lock.front_door" }) });
  let state = await lockState();
  for (let i = 0; i < 20 && state !== "locked"; i++) state = await new Promise((r) => setTimeout(r, 500)).then(lockState);
  return state;
}

// An unlock plan of the demo lock, through the Relay (channel relay, no
// code), applied: the linked phone's tap (answer 0) or Cancel (answer 1).
export async function confirmUnlock(platform, answer = 0) {
  const before = await relock();
  report("the door is locked before the unlock", before === "locked" ? [] : [`lock ${before}`]);
  const planned = await cli(["plan", "call", "lock.unlock", "lock.front_door"]).done;
  const plan = JSON.parse(planned.stdout || "{}");
  report("unlock plan asks through the Relay, no code", plan.plan?.channel === "relay" && plan.plan.code === undefined && (plan.say ?? "").includes("Confirm on your linked phone or in Home Assistant (NOVA).")
    ? [] : [`channel ${plan.plan?.channel}, say ${JSON.stringify(plan.say)}`]);
  if (!plan.plan?.id) return;
  const run = cli(["apply", "--plan", plan.plan.id]);
  await withdrawn(await question(platform, "Unlock", { unlock: false, urgent: true, answer }));
  const done = await run.done, state = await lockState();
  if (answer === 0) finished(`unlock confirmed on ${PHONES[platform]}`, done, { out: '"status":"verified"' });
  else report(`unlock cancelled on ${PHONES[platform]} sends nothing`, done.code !== 0 && state === "locked" ? [] : [`exit ${done.code}, lock ${state}: ${(done.stdout + done.stderr).slice(0, 300)}`]);
}

// Pushes past the lane's own questions and their withdraws.
export async function noExtraPushes() {
  const extra = (await (await fetch(`${PUSH}/pushes`)).json()).slice(seen).filter((p) => p.body.message !== "clear_notification");
  report("no push beyond the asked phones", extra.map((p) => `${p.path}: ${p.body.title}`));
}

// Off on a gated App needs a confirmation window (allowPhoneChange): no stop here.
if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => process.exit(130));
  try {
    await fixture();
    await resetPushes();
    await link("ios");
    await confirmUnlock("ios");
    await link("android", "ios");
    await confirmUnlock("android");
    await link("ios", "android");
    await relock();
    await noExtraPushes();
  } catch (err) {
    report("phone lane ran to the end", [err.message]);
  }
  process.exit(failed ? 1 : 0);
}
