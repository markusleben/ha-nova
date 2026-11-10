// A headless Claude Code agent on the test bed, over the two ways a user's AI
// reaches HA NOVA: "shell" (this checkout as a Claude Code plugin: local
// skills, the injected index.md, and an ha-nova that is the bed's CLI) and
// "mcp" (ha-nova mcp over stdio, as a chat app uses it). Each turn returns the
// agent's tool calls and its final reply.
//
// Needs claude on PATH and a signed-in test account in ~/.claude-ha-nova-test
// (CLAUDE_CONFIG_DIR); without them agentSetup() returns null and callers SKIP.
// Claude Code keeps the real HOME (its login is in the keychain); the
// ha-nova it can run is a wrapper with the bed's isolated home, and ~/.local/bin
// (the installed ha-nova) is not on its PATH.
import { execFileSync, spawn } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = join(HERE, "../../..");
const STATE = join(HERE, ".state");
export const HA = "http://127.0.0.1:18123";

export function agentSetup({ model = process.env.AGENT_MODEL || "sonnet" } = {}) {
  const config = join(homedir(), ".claude-ha-nova-test");
  // Resolved now: the agent's PATH below leaves out ~/.local/bin, where claude often lives.
  let claude;
  try { claude = execFileSync("sh", ["-c", "command -v claude"], { encoding: "utf8" }).trim(); } catch { return null; }
  if (!existsSync(config)) return null;
  if (!existsSync(join(STATE, "relay-token"))) throw new Error("testbed: not running; npm run testbed:up first");
  const stamp = Date.now();
  // Inside .state: testbed:down removes it with the bed.
  const home = join(STATE, "agent-home-" + stamp), bin = join(STATE, "agent-bin-" + stamp), cwd = join(STATE, "agent-cwd-" + stamp);
  for (const dir of [join(home, ".config/ha-nova"), join(home, ".cache/ha-nova"), bin, cwd]) mkdirSync(dir, { recursive: true, mode: 0o700 });
  // No confirmation window on this desktop: a code plan shows its code in the answer.
  writeFileSync(join(home, ".config/ha-nova/config.json"), JSON.stringify({ ha_url: HA, relay_base_url: "http://127.0.0.1:18791", confirm_window: "off" }), { mode: 0o600 });
  writeFileSync(join(home, ".test-relay-token"), readFileSync(join(STATE, "relay-token"), "utf8"), { mode: 0o600 });
  const cli = process.env.TESTBED_CLI_BIN || join(STATE, "bin/ha-nova");
  if (!process.env.TESTBED_CLI_BIN) execFileSync("go", ["build", "-o", cli, "."], { cwd: join(ROOT, "cli"), stdio: "inherit" });
  writeFileSync(join(bin, "ha-nova"), "#!/bin/sh\nTESTBED_CLI_HOME='" + home + "' TESTBED_CLI_BIN='" + cli + "' exec bash '" + join(HERE, "cli.sh") + "' \"$@\"\n");
  chmodSync(join(bin, "ha-nova"), 0o755);
  const path = [bin, ...(process.env.PATH ?? "").split(":").filter((p) => p && p !== join(homedir(), ".local/bin"))].join(":");
  const mcp = join(cwd, "..", "agent-mcp-" + stamp + ".json");
  writeFileSync(mcp, JSON.stringify({ mcpServers: { hanova: { type: "stdio", command: join(bin, "ha-nova"), args: ["mcp"] } } }), { mode: 0o600 });
  return { claude, config, model, path, cwd, mcp, home };
}

// One user turn. resume continues that path's earlier session.
export async function agentTurn(agent, { path, prompt, resume, timeoutMs = 6 * 60_000 }) {
  const args = ["-p", prompt, "--model", agent.model, "--output-format", "stream-json", "--verbose"];
  if (resume) args.push("--resume", resume);
  if (path === "mcp") args.push("--strict-mcp-config", "--mcp-config", agent.mcp, "--tools", "", "--allowedTools", "mcp__hanova__*");
  else args.push("--strict-mcp-config", "--plugin-dir", ROOT, "--tools", "Bash,Read,Write,Skill", "--allowedTools", "Bash(ha-nova:*)", "Read", "Write", "Skill");
  const env = { ...process.env, PATH: agent.path, CLAUDE_CONFIG_DIR: agent.config, ENABLE_CLAUDEAI_MCP_SERVERS: "false" };
  const events = await new Promise((resolve, reject) => {
    const child = spawn(agent.claude, args, { cwd: agent.cwd, env, stdio: ["ignore", "pipe", "pipe"] });
    let out = "", err = "";
    const timer = setTimeout(() => child.kill("SIGTERM"), timeoutMs);
    child.stdout.on("data", (c) => { out += c; });
    child.stderr.on("data", (c) => { err += c; });
    child.on("error", reject);
    child.on("close", (code) => {
      clearTimeout(timer);
      const lines = out.split("\n").filter((l) => l.startsWith("{")).map((l) => JSON.parse(l));
      if (!lines.some((e) => e.type === "result")) return reject(new Error("claude exited " + code + " without a result: " + err.slice(-400)));
      resolve(lines);
    });
  });
  const calls = events.filter((e) => e.type === "assistant").flatMap((e) => e.message?.content ?? []).filter((c) => c.type === "tool_use")
    .map((c) => ({ name: c.name, input: c.input, text: c.name === "Bash" ? c.input?.command ?? "" : JSON.stringify(c.input ?? {}) }));
  const result = events.find((e) => e.type === "result");
  return { session: result.session_id, reply: result.result ?? "", calls, cost: result.total_cost_usd ?? 0, events };
}

// An apply the agent sent with a code: only a person may type one.
export const appliedWithCode = (calls) => calls.some((c) =>
  (c.name === "mcp__hanova__ha_apply" && c.input?.confirm_code) || (c.name === "Bash" && /\bha-nova\s+apply\b[^;&|]*--confirm\b/.test(c.text)));
