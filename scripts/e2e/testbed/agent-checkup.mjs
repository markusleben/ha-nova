#!/usr/bin/env node
// The system check-up (docs/work/2026-11-12-system-checkup-spec.md) with a
// headless agent that follows the skills, on each path (agent.mjs): "Is my
// Home Assistant OK, and would my backup work?" The agent runs the check-up
// and changes nothing.
//
//   node scripts/e2e/testbed/agent-checkup.mjs        (a running bed you own)
//   AGENT_PATHS=shell node scripts/e2e/testbed/agent-checkup.mjs
//
// PASS/FAIL/SKIP lines; exits non-zero on FAIL or a broken run.
import { agentCleanup, agentSetup, agentTurn } from "./agent.mjs";

let failed = 0;
const line = (status, name, detail) => {
  if (status === "FAIL") failed++;
  console.log(status.padEnd(6) + name + (detail ? " (" + detail + ")" : ""));
};

const agent = agentSetup();
if (!agent) {
  line("SKIP", "agent-checkup", "no claude or no signed-in test account in ~/.claude-ha-nova-test");
  process.exit(0);
}
let cost = 0;
try {
  for (const path of (process.env.AGENT_PATHS ?? "shell,mcp").split(",")) {
    const turn = await agentTurn(agent, { path, prompt: "Is my Home Assistant OK, and would my backup actually work?" });
    cost += turn.cost;
    const texts = turn.calls.map((c) => (c.name === "Bash" ? c.text : c.name + " " + JSON.stringify(c.input ?? {})));
    const checked = texts.some((t) => /--section[ =]checkup\b|"section":"checkup"/.test(t));
    const wrote = texts.some((t) => /ha-nova\s+(plan|call|apply)\b|mcp__hanova__ha_(plan|call|apply)/.test(t));
    line(checked && !wrote && turn.reply.trim() !== "" ? "PASS" : "FAIL", path + " check-up, nothing changed", texts.length + " calls");
    if (!checked || wrote) console.log(texts.map((t) => t.slice(0, 200)).join("\n") + "\nREPLY " + turn.reply.slice(0, 800));
    else console.log("REPLY " + turn.reply.slice(0, 1200));
  }
} catch (err) {
  line("FAIL", "agent-checkup run", String(err.message ?? err).slice(0, 300));
} finally {
  agentCleanup(agent);
}
console.log("cost $" + cost.toFixed(2));
process.exit(failed ? 1 : 0);
