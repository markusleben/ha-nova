// A headless Claude Code agent on the test bed, over the two ways a user's AI
// reaches HA NOVA: "shell" (this checkout as a Claude Code plugin: local
// skills, the injected index.md, and an ha-nova that is the bed's CLI) and
// "mcp" (ha-nova mcp over stdio, as a chat app uses it). Each turn returns the
// agent's tool calls, the tool results, and its final reply.
//
// Needs claude on PATH and a signed-in test account in ~/.claude-ha-nova-test
// (CLAUDE_CONFIG_DIR=~/.claude-ha-nova-test claude, then sign in); without
// them agentSetup() returns null and callers SKIP. Claude Code keeps the real
// HOME (its login is in the keychain). Isolation from a real home, twice over:
// the ha-nova it may run is a wrapper with the bed's isolated home (every call
// logged, and a turn whose ha-nova commands miss the log fails), and the claude
// process itself carries the bed's config dir and test keyring, so even the
// installed ha-nova would only see the bed.
import { execFileSync, spawn } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { homedir, tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "../../..");
const STATE = join(HERE, ".state");
export const HA = "http://127.0.0.1:18123";
// Read verbs plus plan, call, and apply: never setup, pair, cloud, serve, or
// uninstall, which reach the macOS keychain even under another HOME.
const VERBS = ["check-update", "version", "find", "get", "history", "trace", "map", "read", "relay", "diff", "plan", "call", "apply"];

export function agentSetup({ model = process.env.AGENT_MODEL || "sonnet" } = {}) {
  const config = join(homedir(), ".claude-ha-nova-test");
  // Resolved now: the agent's PATH below leaves out ~/.local/bin, where claude often lives.
  let claude;
  try { claude = execFileSync("sh", ["-c", "command -v claude"], { encoding: "utf8" }).trim(); } catch { return null; }
  if (!claude || !existsSync(config)) return null;
  if (!existsSync(join(STATE, "relay-token"))) throw new Error("testbed: not running; npm run testbed:up first");
  const stamp = Date.now();
  // Inside .state (testbed:down removes it with the bed) and removed by agentCleanup.
  const home = join(STATE, "agent-home-" + stamp), bin = join(STATE, "agent-bin-" + stamp), mcp = join(STATE, "agent-mcp-" + stamp + ".json");
  for (const dir of [join(home, ".config/ha-nova"), join(home, ".cache/ha-nova"), bin]) mkdirSync(dir, { recursive: true, mode: 0o700 });
  // Outside the repo: Claude Code would load this checkout's CLAUDE.md and answer as a coding agent.
  // Its real path: on macOS /var is /private/var, and the Write rule below must name the path the agent writes to.
  const cwd = realpathSync(mkdtempSync(join(tmpdir(), "ha-nova-agent-")));
  // No confirmation window on this desktop: a code plan shows its code in the answer.
  writeFileSync(join(home, ".config/ha-nova/config.json"), JSON.stringify({ ha_url: HA, relay_base_url: "http://127.0.0.1:18791", confirm_window: "off" }), { mode: 0o600 });
  writeFileSync(join(home, ".test-relay-token"), readFileSync(join(STATE, "relay-token"), "utf8"), { mode: 0o600 });
  const cli = process.env.TESTBED_CLI_BIN || join(STATE, "bin/ha-nova");
  if (!process.env.TESTBED_CLI_BIN) execFileSync("go", ["build", "-o", cli, "."], { cwd: join(ROOT, "cli"), stdio: "inherit" });
  const log = join(bin, "calls.log");
  writeFileSync(join(bin, "ha-nova"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + log + "'\nTESTBED_CLI_HOME='" + home + "' TESTBED_CLI_BIN='" + cli + "' exec bash '" + join(HERE, "cli.sh") + "' \"$@\"\n");
  chmodSync(join(bin, "ha-nova"), 0o755);
  writeFileSync(log, "");
  const path = [bin, ...(process.env.PATH ?? "").split(":").filter((p) => p && p !== join(homedir(), ".local/bin"))].join(":");
  writeFileSync(mcp, JSON.stringify({ mcpServers: { hanova: { type: "stdio", command: join(bin, "ha-nova"), args: ["mcp"] } } }), { mode: 0o600 });
  const env = {
    ...process.env, PATH: path, TMPDIR: cwd, CLAUDE_CONFIG_DIR: config, ENABLE_CLAUDEAI_MCP_SERVERS: "false",
    HA_NOVA_CONFIG_DIR: join(home, ".config/ha-nova"), XDG_CONFIG_HOME: join(home, ".config"), XDG_CACHE_HOME: join(home, ".cache"),
    HA_NOVA_ALLOW_INSECURE_TEST_KEYRING: "1", HA_NOVA_TEST_KEYRING_FILE: join(home, ".test-relay-token"), HA_NOVA_NO_BROWSER: "1",
  };
  return { claude, model, env, cwd, mcp, home, bin, log };
}

export function agentCleanup(agent) {
  if (!agent) return;
  for (const p of [agent.cwd, agent.home, agent.bin, agent.mcp]) rmSync(p, { recursive: true, force: true });
}

// The ha-nova command lines the wrapper ran, in order.
export const wrapperCalls = (agent) => readFileSync(agent.log, "utf8").split("\n").filter(Boolean);

// One user turn. resume continues that path's earlier session.
export async function agentTurn(agent, { path, prompt, resume, timeoutMs = 6 * 60_000 }) {
  if (!["mcp", "shell"].includes(path)) throw new Error("unknown agent path " + path + " (mcp or shell)");
  const args = ["-p", prompt, "--model", agent.model, "--output-format", "stream-json", "--verbose", "--max-budget-usd", "1", "--strict-mcp-config"];
  if (resume) args.push("--resume", resume);
  if (path === "mcp") args.push("--mcp-config", agent.mcp, "--tools", "", "--allowedTools", "mcp__hanova__*");
  else args.push("--plugin-dir", ROOT, "--tools", "Bash,Read,Write,Skill", "--allowedTools", ...VERBS.map((v) => "Bash(ha-nova " + v + ":*)"),
    "Read(/" + join(ROOT, "skills") + "/**)", "Read(/" + agent.cwd + "/**)", "Edit(/" + agent.cwd + "/**)", "Write(/" + agent.cwd + "/**)", "Skill");
  const logged = wrapperCalls(agent).length;
  const events = await new Promise((resolveRun, reject) => {
    const child = spawn(agent.claude, args, { cwd: agent.cwd, env: agent.env, stdio: ["ignore", "pipe", "pipe"] });
    let out = "", err = "";
    const timer = setTimeout(() => child.kill("SIGTERM"), timeoutMs);
    child.stdout.on("data", (c) => { out += c; });
    child.stderr.on("data", (c) => { err += c; });
    child.on("error", reject);
    child.on("close", (code) => {
      clearTimeout(timer);
      const lines = out.split("\n").filter((l) => l.startsWith("{")).map((l) => JSON.parse(l));
      if (!lines.some((e) => e.type === "result")) return reject(new Error("claude exited " + code + " without a result: " + err.slice(-400)));
      resolveRun(lines);
    });
  });
  const result = events.find((e) => e.type === "result");
  if (result.is_error) throw new Error("claude failed (" + (result.subtype ?? "error") + "): " + String(result.result ?? "").slice(0, 300));
  const init = events.find((e) => e.type === "system" && e.subtype === "init");
  if (path === "mcp" && !init?.mcp_servers?.some((s) => s.name === "hanova" && s.status === "connected")) throw new Error("the ha-nova MCP server did not connect: " + JSON.stringify(init?.mcp_servers));
  const blocks = (type) => events.filter((e) => e.type === type).flatMap((e) => e.message?.content ?? []);
  const calls = blocks("assistant").filter((c) => c.type === "tool_use")
    .map((c) => ({ id: c.id, name: c.name, input: c.input, text: c.name === "Bash" ? c.input?.command ?? "" : JSON.stringify(c.input ?? {}) }));
  const results = blocks("user").filter((c) => c.type === "tool_result")
    .map((c) => (Array.isArray(c.content) ? c.content.map((x) => x.text ?? "").join("\n") : String(c.content ?? "")));
  const ran = wrapperCalls(agent).slice(logged);
  // Every ha-nova command the agent ran must have gone through the wrapper;
  // one Claude Code refused to run (a permission rule) never ran at all.
  const refused = new Set((result.permission_denials ?? []).map((d) => d.tool_use_id));
  const asked = calls.filter((c) => c.name === "Bash" && !refused.has(c.id)).reduce((n, c) => n + (c.text.match(/(^|[\s;&|("'])ha-nova\s+[a-z]/g) ?? []).length, 0);
  if (path === "shell" && ran.length < asked) throw new Error("ha-nova ran outside the bed's wrapper: " + asked + " commands, " + ran.length + " logged");
  return { session: result.session_id, reply: result.result ?? "", calls, results, ran, cost: result.total_cost_usd ?? 0 };
}

// An apply the agent sent with a code: only a person may type one.
export const appliedWithCode = (turn) =>
  turn.calls.some((c) => c.name === "mcp__hanova__ha_apply" && c.input?.confirm_code) || turn.ran.some((l) => /(^|\s)apply\b.*\s--confirm\b/.test(l));
