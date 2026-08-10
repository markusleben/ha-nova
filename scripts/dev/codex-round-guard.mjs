#!/usr/bin/env node
// PostToolUse hook: counts `@codex` review rounds per PR and, from the fifth
// round on, tells the agent to stop answering findings one at a time.
//
// Why this exists: on #513/#527 the serial loop ran ~40 rounds over eight
// hours. Three adversarial subagents in parallel returned 30+ findings in one
// pass, including defects the serial loop had not reached. The rule is in
// AGENTS.md ("Post-PR batch trigger"); this hook is what makes it fire, since
// the rule was already written once and got followed only by luck.
//
// Wired from .claude/settings.json as a PostToolUse hook on Bash.

import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";

const THRESHOLD = 5;

// In a linked worktree `.git` is a FILE, so joining it as a directory fails
// with ENOTDIR and the counter silently restarts at 1 on every call — dead
// exactly where this repo does its parallel work. Ask git for the shared dir,
// which also makes the count span all worktrees of one clone, as it should:
// the rounds belong to the PR, not to the checkout.
const gitDir = () => {
  try {
    return execFileSync("git", ["rev-parse", "--git-common-dir"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return ".git";
  }
};
const STATE = join(gitDir(), "codex-rounds.json");

const read = (stream) =>
  new Promise((resolve) => {
    let buf = "";
    stream.on("data", (chunk) => (buf += chunk));
    stream.on("end", () => resolve(buf));
  });

const emit = (text) => {
  process.stdout.write(
    JSON.stringify({
      hookSpecificOutput: { hookEventName: "PostToolUse", additionalContext: text },
    }),
  );
};

const main = async () => {
  let payload;
  try {
    payload = JSON.parse((await read(process.stdin)) || "{}");
  } catch {
    return; // never break the tool call on a malformed payload
  }

  const command = payload?.tool_input?.command ?? "";
  // Only the trigger itself counts — not the reply comment that precedes it,
  // and not a `gh pr view` that happens to contain the word.
  const match = /gh\s+pr\s+comment\s+(\d+)[^\n]*@codex/.exec(command);
  if (!match) return;

  const pr = match[1];
  let state = {};
  try {
    state = JSON.parse(readFileSync(STATE, "utf8"));
  } catch {
    // first round in this clone
  }

  const rounds = (state[pr] ?? 0) + 1;
  state[pr] = rounds;
  try {
    mkdirSync(dirname(STATE), { recursive: true });
    writeFileSync(STATE, JSON.stringify(state, null, 2) + "\n");
  } catch {
    // a read-only .git is not a reason to fail the hook
  }

  if (rounds < THRESHOLD) return;

  emit(
    `AGENTS.md → Post-PR batch trigger: this is Codex round ${rounds} on PR #${pr}. ` +
      `Do not answer the next findings one at a time. Before the next @codex, run 2-3 ` +
      `adversarial subagents IN PARALLEL over this PR's full current diff — distinct lenses ` +
      `(correctness/domain vocabulary, races and ordering, cross-file contract consistency), ` +
      `each briefed with what is already fixed so they do not re-tread it — fix everything ` +
      `they return in ONE commit, then trigger Codex once. This is mandatory, not a judgement ` +
      `call. Reset the counter for this PR with: node scripts/dev/codex-round-guard.mjs --reset ${pr}`,
  );
};

const [, , flag, arg] = process.argv;
if (flag === "--reset") {
  let state = {};
  try {
    state = JSON.parse(readFileSync(STATE, "utf8"));
  } catch {
    /* nothing to reset */
  }
  if (arg) delete state[arg];
  else state = {};
  mkdirSync(dirname(STATE), { recursive: true });
  writeFileSync(STATE, JSON.stringify(state, null, 2) + "\n");
  process.stdout.write(arg ? `reset PR #${arg}\n` : "reset all\n");
} else {
  await main();
}
