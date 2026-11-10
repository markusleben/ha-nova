#!/usr/bin/env node
// www media lane (0.30; spec docs/work/2026-11-10-www-media-delete-spec.md)
// on the test bed with file access and the phone lane's fake phones:
//
//   TESTBED_FILES=1 npm run testbed:up
//   TESTBED_PHONE=1 node scripts/e2e/testbed/www-media.mjs
//
// Lists /config/www; deletes a picture through the CLI (plan raw, apply, the
// linked phone's tap), whose copy the App then lists; puts it back through
// the undo item the apply named (the same way); refuses www/community even
// after an approval; fills the copies (four videos just under 25 MiB) and refuses the
// next delete with UNDO_STORE_FULL, the file still there. Prints PASS/FAIL
// lines, WARN when the bed has no file access; exits non-zero on any FAIL.
import { execFileSync } from "node:child_process";
import { rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { answered, cli, failures, fixture, link, noExtraPushes, question, read, relay, report, resetPushes } from "./phone.mjs";

const DIR = "/config/www/wwwlane", CARDS = "/config/www/community/wwwlane-card";
const PICTURE = `${DIR}/door.jpg`, BYTES = "not a real photo";
const inHA = (cmd) => execFileSync("docker", ["exec", "ha-nova-testbed-ha", "sh", "-c", cmd], { encoding: "utf8" });
const PLAN = join(tmpdir(), `ha-nova-wwwlane-${process.pid}.json`);
const there = (path) => inHA(`test -e ${path} && echo yes || echo no`).trim() === "yes";

// A plan raw of these items, applied with the linked phone's tap.
async function applied(name, items) {
  writeFileSync(PLAN, JSON.stringify(items));
  const plan = JSON.parse((await cli(["plan", "raw", "--file", PLAN]).done).stdout || "{}");
  report(`${name}: the plan asks through the Relay`, plan.plan?.channel === "relay" ? [] : [`${JSON.stringify(plan.error ?? plan.plan)}`]);
  if (!plan.plan?.id) return {};
  const run = cli(["apply", "--plan", plan.plan.id]);
  await answered(await question("ios", undefined, { unlock: false, urgent: true }));
  const done = await run.done;
  return JSON.parse(done.stdout || "{}");
}

// Straight to the Relay with a grant the phone's tap gave for exactly body.
async function granted(body) {
  const opened = await relay("/gate/v1/ask", { requests: [{ endpoint: "files", body_b64: Buffer.from(JSON.stringify(body)).toString("base64") }] });
  if (!opened.data?.ask_id) return opened;
  await answered(await question("ios", undefined, { unlock: false, urgent: true }));
  let state = { state: "waiting" };
  for (let i = 0; i < 8 && state?.state === "waiting"; i++) state = (await relay("/gate/v1/ask", { ask_id: opened.data.ask_id, op: "wait" })).data;
  return state?.grant_id ? await relay("/files", body, { "x-ha-nova-grant": state.grant_id }) : { status: 0, error: { code: `ask ${JSON.stringify(state)}` } };
}

for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => process.exit(130));
try {
  await fixture();
  await resetPushes();
  const health = await (await fetch("http://127.0.0.1:18791/health", { headers: { authorization: `Bearer ${read("relay-token")}` } })).json();
  if (health.data?.file_access !== "readwrite") {
    console.log("WARN  www media lane skipped: this bed has no file access (TESTBED_FILES=1 npm run testbed:up)");
  } else {
    await link("ios");
    inHA(`mkdir -p ${DIR} ${CARDS} && printf '${BYTES}' > ${PICTURE} && printf 'png' > ${CARDS}/logo.png && ` +
      `for i in 1 2 3 4; do head -c 26210000 /dev/zero > ${DIR}/clip$i.mp4; done`);

    const listed = await relay("/files", { action: "list_dir", path: DIR });
    const door = listed.data?.entries?.find((e) => e.name === "door.jpg");
    report("www is listed with size and time", listed.status === 200 && door?.size === BYTES.length && typeof door.modified === "string" ? [] : [`HTTP ${listed.status} ${JSON.stringify(listed.error ?? door)}`]);

    const held = await relay("/files", { action: "delete_file", path: PICTURE });
    report("a www delete no one confirmed is held (428), the picture stays", held.status === 428 && there(PICTURE) ? [] : [`HTTP ${held.status} ${held.error?.code}`]);

    const deleted = await applied("delete", [{ endpoint: "files", payload: { action: "delete_file", path: PICTURE } }]);
    const copies = await relay("/files", { action: "list_deleted" });
    const kept = copies.data?.copies?.find((c) => c.path === PICTURE);
    report("the phone's tap deletes the picture, its copy is listed, and the apply names the undo",
      deleted.status === "accepted" && !there(PICTURE) && kept && (deleted.next_step ?? "").includes(`"copy":"${kept.copy}"`) ? [] : [`${deleted.status ?? JSON.stringify(deleted.error)}, there ${there(PICTURE)}, copy ${JSON.stringify(kept)}`]);

    if (kept) {
      const restored = await applied("restore", [{ endpoint: "files", payload: { action: "restore_file", path: PICTURE, copy: kept.copy } }]);
      const back = there(PICTURE) ? inHA(`cat ${PICTURE}`) : "";
      report("the undo puts the same picture back after the phone's tap", restored.status === "accepted" && back === BYTES ? [] : [`${restored.status ?? JSON.stringify(restored.error)}, read back ${JSON.stringify(back)}`]);
    }

    const cardList = await relay("/files", { action: "list_dir", path: "/config/www/community" });
    const card = await granted({ action: "delete_file", path: `${CARDS}/logo.png` });
    report("www/community is refused for list, and for a delete even once approved", cardList.error?.code === "FILE_PATH_DENIED" && card.error?.code === "FILE_PATH_DENIED" && there(`${CARDS}/logo.png`)
      ? [] : [`list ${cardList.status} ${cardList.error?.code}, delete ${card.status} ${card.error?.code}`]);

    const fills = [];
    for (let i = 1; i <= 4; i++) fills.push((await granted({ action: "delete_file", path: `${DIR}/clip${i}.mp4` })).status);
    const full = await granted({ action: "delete_file", path: PICTURE });
    report("four 25 MiB videos fill the copies; the next delete is refused and the picture stays", fills.every((s) => s === 200) && full.status === 409 && full.error?.code === "UNDO_STORE_FULL" && there(PICTURE)
      ? [] : [`fills ${fills.join(",")}, then HTTP ${full.status} ${full.error?.code}, there ${there(PICTURE)}`]);
    await noExtraPushes();
  }
} catch (err) {
  report("www media lane ran to the end", [err.message]);
} finally {
  rmSync(PLAN, { force: true });
  try { inHA(`rm -rf ${DIR} ${CARDS}`); } catch { /* the bed is gone */ }
}
process.exit(failures() ? 1 : 0);
