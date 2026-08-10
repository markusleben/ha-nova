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
import {
  mkdirSync,
  readFileSync,
  renameSync,
  statSync,
  unlinkSync,
  writeFileSync,
} from "node:fs";
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

// The newest clean verdict, as a timestamp, or null.
//
// Do NOT compare it against the newest commit: by the time the next @codex
// fires there is already a newer commit, so "thumbs-up younger than commit"
// is false exactly when the reset is due — a clean verdict followed by a
// fresh delta. Instead the state remembers WHICH clean verdict was last
// accounted for; a different one means a cycle closed since we last looked.
//
// Only the REVIEW BOT counts. A collaborator's 👍 is encouragement, and
// treating it as a verdict would suppress the reminder when it is due.
const newestCleanVerdict = (pr) => {
  try {
    const reactions = JSON.parse(
      execFileSync(
        "gh",
        ["api", `repos/{owner}/{repo}/issues/${pr}/reactions`],
        { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"], timeout: 5000 },
      ),
    );
    return (
      reactions
        // GitHub reports an App installation's reaction with type "User" and
        // a "[bot]" login suffix — checking type === "Bot" matches nothing.
        // The real login is `chatgpt-codex-connector[bot]`.
        .filter(
          (r) =>
            r.content === "+1" &&
            /codex/i.test(r.user?.login ?? "") &&
            /\[bot\]$/.test(r.user?.login ?? ""),
        )
        .map((r) => r.created_at)
        .sort()
        .pop() ?? null
    );
  } catch {
    return null; // gh unavailable: keep counting rather than reset
  }
};

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

  // A clean verdict ends a review cycle: rounds counted before it belong to
  // that cycle, not to the next delta. Ask GitHub whether the newest thumbs-up
  // is younger than the last commit — if so, this trigger starts a fresh
  // streak.
  const clean = newestCleanVerdict(pr);
  const seen = state[`${pr}:clean`] ?? null;
  const resetStreak = Boolean(clean) && clean !== seen;
  let rounds = (state[pr] ?? 0) + 1;
  try {
    // Two sessions in two worktrees share one state file. An atomic rename
    // stops torn JSON but not a LOST UPDATE: both read 4, both write 5, one
    // increment vanishes and the reminder arrives a round late. Take an
    // exclusive lock, then re-read inside it so the increment is applied to
    // whatever the other session just wrote.
    mkdirSync(dirname(STATE), { recursive: true });
    const lock = `${STATE}.lock`;
    let held = false;
    for (let attempt = 0; attempt < 50 && !held; attempt += 1) {
      try {
        writeFileSync(lock, String(process.pid), { flag: "wx" });
        held = true;
      } catch {
        // a lock older than 10s belonged to a crashed run
        try {
          if (Date.now() - statSync(lock).mtimeMs > 10_000) unlinkSync(lock);
        } catch {
          /* someone else cleaned it up */
        }
        execFileSync("sleep", ["0.05"], { stdio: "ignore" });
      }
    }
    try {
      let fresh = {};
      try {
        fresh = JSON.parse(readFileSync(STATE, "utf8"));
      } catch {
        /* first write */
      }
      if (resetStreak) fresh[pr] = 0;
      if (clean) fresh[`${pr}:clean`] = clean;
      rounds = (fresh[pr] ?? 0) + 1;
      fresh[pr] = rounds;
      const tmp = `${STATE}.${process.pid}.tmp`;
      writeFileSync(tmp, JSON.stringify(fresh, null, 2) + "\n");
      renameSync(tmp, STATE);
    } finally {
      if (held) {
        try {
          unlinkSync(lock);
        } catch {
          /* already gone */
        }
      }
    }
  } catch {
    // a read-only git dir is not a reason to fail the hook
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
  // Same lock as the hook path: a reset racing a concurrent increment would
  // otherwise restore a stale snapshot over it.
  mkdirSync(dirname(STATE), { recursive: true });
  const lock = `${STATE}.lock`;
  let held = false;
  for (let attempt = 0; attempt < 50 && !held; attempt += 1) {
    try {
      writeFileSync(lock, String(process.pid), { flag: "wx" });
      held = true;
    } catch {
      try {
        if (Date.now() - statSync(lock).mtimeMs > 10_000) unlinkSync(lock);
      } catch {
        /* someone else cleaned it up */
      }
      execFileSync("sleep", ["0.05"], { stdio: "ignore" });
    }
  }
  try {
    let state = {};
    try {
      state = JSON.parse(readFileSync(STATE, "utf8"));
    } catch {
      /* nothing to reset */
    }
    if (arg) {
      delete state[arg];
      delete state[`${arg}:clean`];
    } else {
      state = {};
    }
    const tmp = `${STATE}.${process.pid}.tmp`;
    writeFileSync(tmp, JSON.stringify(state, null, 2) + "\n");
    renameSync(tmp, STATE);
  } finally {
    if (held) {
      try {
        unlinkSync(lock);
      } catch {
        /* already gone */
      }
    }
  }
  process.stdout.write(arg ? `reset PR #${arg}\n` : "reset all\n");
} else {
  await main();
}
