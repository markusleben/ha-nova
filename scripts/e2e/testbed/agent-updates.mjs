#!/usr/bin/env node
// The update advisor (docs/work/2026-11-12-update-advisor-spec.md) with a
// headless agent that follows the skills, on each path (agent.mjs):
//   check    "Can I install the pending updates?": the agent runs the
//            advisor's check and installs nothing.
//   install  "Install <demo update>": the check runs before the install is
//            planned, nothing is applied before the user's reply; after
//            "yes" the install runs and the after-update check follows.
// The bed's demo update entities are the updates (no Supervisor: its checks
// say not applicable). Installs a demo update per path: run it alone.
//
//   node scripts/e2e/testbed/agent-updates.mjs        (a running bed you own)
//   AGENT_PATHS=shell node scripts/e2e/testbed/agent-updates.mjs
//
// PASS/FAIL/SKIP lines; exits non-zero on FAIL or a broken run.
import { agentCleanup, agentSetup, agentTurn } from "./agent.mjs";

const TARGETS = { shell: ["Demo Update with Progress", "update.demo_update_with_progress"], mcp: ["Demo Update with Decimal Progress", "update.demo_update_with_decimal_progress"] };
let failed = 0;
const line = (status, name, detail) => {
  if (status === "FAIL") failed++;
  console.log(status.padEnd(6) + name + (detail ? " (" + detail + ")" : ""));
};

// Each step the agent took, in order, as one word: check, after, plan, apply.
function steps(turn) {
  const out = [];
  for (const c of turn.calls) {
    const t = c.name === "Bash" ? c.text : c.name + " " + JSON.stringify(c.input ?? {});
    if (/--section[ =]updates\b|"section":"updates"/.test(t)) out.push("check");
    if (/--section[ =]after_update\b|"section":"after_update"/.test(t)) out.push("after");
    if (/ha-nova\s+call\s+update\.install|ha_call.*update\.install|ha_plan.*update\.install/.test(t)) out.push("plan");
    if (/ha-nova\s+apply\b|mcp__hanova__ha_apply/.test(t)) out.push("apply");
  }
  return out;
}

async function installed(entity) {
  const { execFileSync } = await import("node:child_process");
  const raw = execFileSync("bash", ["scripts/e2e/testbed/cli.sh", "get", entity], { encoding: "utf8" });
  const state = JSON.parse(raw).result?.state;
  return state?.attributes?.installed_version === state?.attributes?.latest_version;
}

const agent = agentSetup();
if (!agent) {
  line("SKIP", "agent-updates", "no claude or no signed-in test account in ~/.claude-ha-nova-test");
  process.exit(0);
}
let cost = 0;
try {
  for (const path of (process.env.AGENT_PATHS ?? "shell,mcp").split(",")) {
    const check = await agentTurn(agent, { path, prompt: "Can I install the pending Home Assistant updates? Don't install anything yet." });
    cost += check.cost;
    const s1 = steps(check);
    line(s1.includes("check") && !s1.includes("apply") && !s1.includes("plan") ? "PASS" : "FAIL", path + " check", s1.join(",") || "no steps");
    const [name, entity] = TARGETS[path];
    const ask = await agentTurn(agent, { path, prompt: "Install the " + name + " update." });
    cost += ask.cost;
    const s2 = steps(ask);
    const ordered = s2.indexOf("check") >= 0 && (s2.indexOf("plan") < 0 || s2.indexOf("check") < s2.indexOf("plan"));
    line(ordered && !s2.includes("apply") ? "PASS" : "FAIL", path + " install asks first", s2.join(",") || "no steps");
    const yes = await agentTurn(agent, { path, prompt: "Yes, go ahead.", resume: ask.session });
    cost += yes.cost;
    const s3 = steps(yes);
    const after = s3.indexOf("after") > s3.indexOf("apply") && s3.includes("apply");
    line(after && (await installed(entity)) ? "PASS" : "FAIL", path + " install, then the after-update check", s3.join(",") || "no steps");
  }
} catch (err) {
  line("FAIL", "agent-updates run", String(err.message ?? err).slice(0, 300));
} finally {
  agentCleanup(agent);
}
console.log("cost $" + cost.toFixed(2));
process.exit(failed ? 1 : 0);
