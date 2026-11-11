#!/usr/bin/env node
// Gate lane: the positive proof of HA NOVA's safety check (0.30; spec
// docs/work/2026-11-03-relay-typed-gate-spec.md §3, §4) on the test bed, with
// the phone lane's fake phones (phone.mjs). A person approves on the linked
// phone, and only then does a typed request run. The disposable-HA run
// proves the refusals only; this lane is the approval. On a running bed with
// file access:
//
//   TESTBED_FILES=1 npm run testbed:up
//   TESTBED_PHONE=1 node scripts/e2e/testbed/gate.mjs
//
// As the Relay's own client (the shared token): a typed file write is held
// (428) and nothing changes; its ask goes to the linked phone; the tap grants
// exactly those bytes: sent with the grant it lands once (the readwrite
// roundtrip), and the same grant again is held. A delete the same way.
// Through the CLI (ha-nova apply): an unlock cancelled on the phone sends
// nothing; an approved one unlocks. Each answered question is replaced by
// its result (answered() in phone.mjs): same tag, no buttons, "Approved: …"
// or "Cancelled: …".
// Prints PASS/FAIL lines, and WARN for a part this bed can't run (no file
// access); exits non-zero on any FAIL. The release workflow fails on WARN too.
import { answered, confirmUnlock, failures, fixture, link, noExtraPushes, question, read, relay, relock, report, resetPushes } from "./phone.mjs";

const FILE = "/config/ha_nova/gate-lane.yaml";
const CONTENT = "gate_lane: testbed\n";

const readBack = async () => {
  const r = await relay("/files", { action: "read_file", path: FILE });
  return { status: r.status, content: r.data?.content };
};

// Until a person approves, the request is held and nothing changes. Asked
// for, the linked phone's tap grants exactly these bytes, once.
async function held(kind, body, landed) {
  const before = await readBack();
  const sent = await relay("/files", body);
  const after = await readBack();
  report(`a ${kind} no one confirmed is held (428) and nothing changes`, sent.status === 428 && sent.error?.code === "GATE_APPROVAL_REQUIRED" && after.status === before.status && after.content === before.content
    ? [] : [`HTTP ${sent.status} ${sent.error?.code}, file HTTP ${before.status} -> ${after.status}`]);
  const opened = await relay("/gate/v1/ask", { requests: [{ endpoint: "files", body_b64: Buffer.from(JSON.stringify(body)).toString("base64") }] });
  report(`the ${kind}'s ask goes to the linked phone`, opened.status === 200 && opened.data?.needed === true && opened.data.channels?.includes("phone")
    ? [] : [`HTTP ${opened.status} ${JSON.stringify(opened.error ?? opened.data)}`]);
  if (!opened.data?.ask_id) return;
  await answered(await question("ios", undefined, { unlock: false, urgent: true }));
  let state = { state: "waiting" };
  for (let i = 0; i < 8 && state?.state === "waiting"; i++) state = (await relay("/gate/v1/ask", { ask_id: opened.data.ask_id, op: "wait" })).data;
  report(`the tap grants the ${kind}`, state?.state === "approved" && state.grant_id ? [] : [`ask ${JSON.stringify(state)}`]);
  if (!state?.grant_id) return;
  const grant = { "x-ha-nova-grant": state.grant_id };
  const done = await relay("/files", body, grant);
  const read = await readBack();
  report(`the granted ${kind} is sent once and lands (readwrite roundtrip)`, done.status === 200 && landed(read) ? [] : [`HTTP ${done.status} ${done.error?.code ?? ""}, read back HTTP ${read.status}`]);
  const again = await relay("/files", body, grant);
  report(`the same grant again is held (428): it counts once`, again.status === 428 ? [] : [`HTTP ${again.status}`]);
}

for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => process.exit(130));
try {
  await fixture();
  await resetPushes();
  const health = await (await fetch("http://127.0.0.1:18791/health", { headers: { authorization: `Bearer ${read("relay-token")}` } })).json();
  report("the bed's Relay gates (health gate 1)", health.data?.gate === 1 ? [] : [`health ${JSON.stringify(health.data)}`]);
  await link("ios");
  if (health.data?.file_access === "readwrite") {
    await held("write", { action: "write_file", path: FILE, content: CONTENT }, (read) => read.status === 200 && read.content === CONTENT);
    await held("delete", { action: "delete_file", path: FILE }, (read) => read.status === 404);
  } else {
    console.log("WARN  file write and delete skipped: this bed has no file access (TESTBED_FILES=1 npm run testbed:up)");
  }
  await confirmUnlock("ios", 1);
  await confirmUnlock("ios");
  await relock();
  await noExtraPushes();
} catch (err) {
  report("gate lane ran to the end", [err.message]);
}
process.exit(failures() ? 1 : 0);
