#!/usr/bin/env node
// Marketplace lane (0.31; spec docs/work/2026-11-12-marketplace-lifecycle-spec.md)
// on a running bed, with the phone lane's fixture (the Relay signs in as the
// admin "Testbed Relay", a person's account, and gates):
//
//   npm run testbed:up
//   node scripts/e2e/testbed/marketplace.mjs
//
// Reads marketplace/info through the Relay (the Relay's user hasn't accepted
// the warning); the CLI refuses accepting it for that account (live account
// reads; only the NOVA App's Supervisor user is accepted for) and plans an
// install typed; straight to the Relay, the gate holds an install (428) and
// refuses the acceptance and every other Marketplace write (422). Then
// Home Assistant itself, as the owner: marketplace/warning/accept flips the
// owner's marketplace/info and not the Relay's, so a change upstream to the
// command, the flag, or its per-user scope fails here. Sends no install and
// approves nothing; the owner's acceptance stays on this throwaway bed.
// Prints PASS/FAIL lines; exits non-zero on any FAIL.
import { rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { cli, failures, fixture, read, relay, report } from "./phone.mjs";

const PLAN = join(tmpdir(), `ha-nova-marketplace-lane-${process.pid}.json`);
const ACCEPT = { type: "marketplace/warning/accept" };
const INSTALL = { type: "marketplace/repository/install", repository: "123456" };

// Home Assistant's own WebSocket API with the owner's token: one command.
async function owner(message) {
  const ws = new WebSocket("ws://127.0.0.1:18123/api/websocket");
  try {
    return await new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`${message.type}: no answer`)), 15_000);
      ws.onerror = () => reject(new Error(`${message.type}: connection failed`));
      ws.onmessage = ({ data }) => {
        const m = JSON.parse(data);
        if (m.type === "auth_required") ws.send(JSON.stringify({ type: "auth", access_token: read("ha-llat") }));
        if (m.type === "auth_ok") ws.send(JSON.stringify({ id: 1, ...message }));
        if (m.type === "auth_invalid") reject(new Error("the owner's token was refused"));
        if (m.type === "result") clearTimeout(timer), resolve(m);
      };
    });
  } finally {
    ws.close();
  }
}

async function plan(items) {
  writeFileSync(PLAN, JSON.stringify(items.map((payload) => ({ endpoint: "ws", payload }))));
  const run = await cli(["plan", "raw", "--file", PLAN]).done;
  return { code: run.code, out: JSON.parse(run.stdout || "{}") };
}

for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => process.exit(130));
try {
  await fixture();
  const info = await relay("/ws", { type: "marketplace/info" });
  report("marketplace/info through the Relay: the Relay's user hasn't accepted the warning", info.status === 200 && info.data?.warning_accepted === false
    ? [] : [`HTTP ${info.status} ${JSON.stringify(info.error ?? info.data)}`]);

  const accept = await plan([ACCEPT]);
  report("the CLI refuses accepting the warning for a person's account (live account reads)", accept.code === 4 && !accept.out.plan && /a person's account/.test(accept.out.error?.message ?? "")
    ? [] : [`exit ${accept.code} ${JSON.stringify(accept.out.error ?? accept.out.plan)}`]);
  const install = await plan([INSTALL]);
  report("a Marketplace install plans typed, asked through the Relay", install.code === 3 && install.out.tier === "typed" && install.out.plan?.channel === "relay" && /Marketplace/.test(install.out.result?.items?.[0]?.change ?? "")
    ? [] : [`exit ${install.code} tier ${install.out.tier} ${JSON.stringify(install.out.error ?? install.out.plan)}`]);

  const held = await relay("/ws", INSTALL);
  report("the gate holds a Marketplace install no one approved (428)", held.status === 428 && held.error?.code === "GATE_APPROVAL_REQUIRED" ? [] : [`HTTP ${held.status} ${held.error?.code}`]);
  for (const [name, body] of [["the acceptance for a person's account", ACCEPT], ["a version pick", { type: "marketplace/repository/version", repository: "123456", version: "v1" }],
    ["a custom repository", { type: "marketplace/repositories/add", repository: "someone/thing", category: "integration" }]]) {
    const sent = await relay("/ws", body);
    report(`the gate refuses ${name} (422)`, sent.status === 422 && sent.error?.code === "GATE_REFUSED" ? [] : [`HTTP ${sent.status} ${sent.error?.code}`]);
  }
  const still = await relay("/ws", { type: "marketplace/info" });
  report("nothing reached Home Assistant: the Relay's user still hasn't accepted", still.data?.warning_accepted === false ? [] : [JSON.stringify(still.data ?? still.error)]);

  const accepted = await owner(ACCEPT);
  const mine = await owner({ type: "marketplace/info" });
  const relays = await relay("/ws", { type: "marketplace/info" });
  report("upstream: marketplace/warning/accept stores it for its own user only", accepted.success && mine.result?.warning_accepted === true && relays.data?.warning_accepted === false
    ? [] : [`accept ${JSON.stringify(accepted.error ?? accepted.success)}, owner ${mine.result?.warning_accepted}, Relay ${relays.data?.warning_accepted}`]);
} catch (err) {
  report("Marketplace lane ran to the end", [err.message]);
} finally {
  rmSync(PLAN, { force: true });
}
process.exit(failures() ? 1 : 0);
