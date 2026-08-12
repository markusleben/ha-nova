#!/usr/bin/env node

import { execFileSync } from "node:child_process";

const [rootDir, baseCommit, targetCommit] = process.argv.slice(2);

// A stale evidence envelope stays valid when the complete ancestor-to-target
// delta is confined to Markdown under docs/ or skills/, or root-level
// Markdown. Everything else (tests, scripts, workflows, runtime code,
// manifests, version metadata, installers, assets, dotfiles) needs fresh
// evidence. tests/ is deliberately excluded: privileged release workflows
// execute repository tests with production-environment secrets, so test
// content must stay attested. AGENTS.md is deliberately excluded: it is the
// executable policy of agents operating with maintainer credentials.
const allowedPath = /^(docs|skills)\/.+\.md$|^[^/]+\.md$/;
// Agent-policy basenames are denied at every depth, case-folded, and with
// any suffix after the stem: agents load AGENTS.md/CLAUDE.md/GEMINI.md per
// subtree, Codex prefers AGENTS.override.md, and on case-insensitive
// checkouts an added lowercase alias materializes as the policy file.
const deniedBasename = /^(agents?|claude|gemini)([._-][^/]*)?\.md$/i;

// Best-effort guard for the copy-paste surface users and agents execute
// blindly: any changed line that touches a download/install command or a
// raw-script source falls back to the full evidence path. This is a denylist
// and documented as best-effort; PR review remains the semantic control.
// Deliberately NO option grammar: a guarded subcommand appearing anywhere
// after its command on the same line counts. Coarser than parsing options,
// strictly stronger, and it only ever fails closed.
const AFTER = String.raw`\b[^\n]*`;
// Option tokens plus an optional value each. Used where a bare AFTER would
// swallow prose: the group can only ever start at a leading dash, so
// "uv --quiet tool run" matches while "uv is a fast installer" does not.
const OPTS = String.raw`(-\S+(\s+[^-\s]\S*)?\s+)*`;
const installCommand = new RegExp(
  [
    String.raw`curl`,
    String.raw`wget`,
    String.raw`\biwr\b`,
    String.raw`\birm\b`,
    String.raw`invoke-webrequest`,
    String.raw`invoke-restmethod`,
    String.raw`invoke-expression`,
    String.raw`downloadstring`,
    String.raw`downloadfile`,
    String.raw`webclient`,
    String.raw`start-bitstransfer`,
    String.raw`\|\s*(bash|sh|iex)\b`,
    String.raw`\b(ba)?sh${AFTER}\s-c\b`,
    String.raw`\b(powershell|pwsh)(\.exe)?\b[^\n]*\s-e`,
    String.raw`\bcmd(\.exe)?\s+\/[ck]\b`,
    String.raw`\b(npm|pnpm|yarn|bun)${AFTER}\b(inst\w*|isnt\w*|add|exec|dlx|create)\b`,
    // Short aliases tolerate option tokens and their values but never a
    // prose word: "npm i", "npm --silent i", "npm --prefix /tmp in" are
    // commands; "npm packages live in …" is not, because the group only
    // ever starts at a leading dash. Accepted false positive: the English
    // sentence "use npm in the nova directory" is indistinguishable from
    // the alias and fails closed. Zero occurrences in the active guarded
    // docs when measured; the cost is one envelope, the alternative is an
    // open install path.
    // Update verbs stay ADJACENT (via OPTS) on this family: "update" is
    // everyday vocabulary in these docs, and the loose form matched a
    // prose line, while the adjacent form matched none.
    String.raw`\b(npm|pnpm|yarn|bun)\s+${OPTS}(i|in|ins|ci|x|it|ic|cit|sit|up|update|upgrade|udpate|init|innit)\b`,
    String.raw`\b(npx|pnpx|bunx|uvx)\b`,
    // Single-purpose fetch tools are guarded by NAME, not by subcommand:
    // every invocation downloads, so enumerating their verbs only invites
    // the next gap. Each measured at 0 occurrences in the active guarded
    // docs. `pip` is NOT in this group — it appears as the `--pip-image`
    // flag in the demo-recording doc — so it keeps its subcommand list.
    String.raw`\b(pipx|composer|poetry|pdm|helm|terraform|ansible-galaxy|rustup|luarocks|opam|cpanm?)\b`,
    String.raw`\bpip3?${AFTER}\b(install|wheel|download)\b`,
    String.raw`\bdeno${AFTER}\b(run|install)\b`,
    String.raw`\b(cargo|gem)${AFTER}\binstall\b`,
    // Language-ecosystem managers that fetch and may execute install
    // hooks. Ruby's `bundle install` is deliberately absent: this repo
    // says "installer bundle" in prose, so it would fire on our own docs.
    String.raw`\buv\s+${OPTS}(pip|add|tool|run)\b`,
    String.raw`\bdotnet\s+${OPTS}(add|tool|restore)\b`,
    String.raw`\bnuget${AFTER}\binstall\b`,
    String.raw`\b(cabal|stack)${AFTER}\binstall\b`,
    String.raw`\bmix\s+${OPTS}(deps\.get|archive\.install|local\.)`,
    String.raw`\bgit${AFTER}\bclone\b`,
    String.raw`\bgh${AFTER}\brelease${AFTER}\bdownload\b`,
    String.raw`\bbrew${AFTER}\b(install|upgrade)\b`,
    String.raw`\b(apt|apt-get|aptitude|dnf|yum|zypper|snap|flatpak|choco|scoop|winget|apk|pkg|opkg|port|emerge|eopkg|guix|conda|mamba|xbps-install)${AFTER}\b(install|add|upgrade|update)\b`,
    String.raw`\b(pacman|yay|paru)${AFTER}\s-S\b`,
    String.raw`\bnix-shell\b`,
    String.raw`\bnix${AFTER}\brun\b`,
    String.raw`\b(python3?|node|ruby|perl|php)${AFTER}\s-[cer]\b`,
    // build/compose/load/import all accept remote contexts or images.
    String.raw`\b(docker|podman)${AFTER}\b(run|pull|create|build|compose|load|import)\b`,
    String.raw`\bgo\b[^\n]*\b(install|run)\b[^\n]*@`,
    String.raw`install\.(sh|ps1)\b`,
    String.raw`raw\.githubusercontent\.com`,
    String.raw`cdn\.jsdelivr\.net`,
    String.raw`statically\.io`,
    String.raw`githack`,
  ].join("|"),
  "i",
);

const allowedModes = new Set(["000000", "100644"]);

function fail(message) {
  console.error(`[verify-cloud-nonsensitive-source] ERROR: ${message}`);
  process.exit(1);
}

function git(args, encoding = "utf8") {
  try {
    return execFileSync("git", ["-C", rootDir, ...args], {
      encoding,
      stdio: ["ignore", "pipe", "ignore"],
    });
  } catch {
    fail(`git ${args[0]} failed`);
  }
}

function requireCommit(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value ?? "")) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
  git(["rev-parse", "--verify", `${value}^{commit}`]);
}

requireCommit(baseCommit, "base commit");
requireCommit(targetCommit, "target commit");
try {
  execFileSync(
    "git",
    ["-C", rootDir, "merge-base", "--is-ancestor", baseCommit, targetCommit],
    { stdio: "ignore" },
  );
} catch {
  fail("base commit must be an ancestor of the target commit");
}

const records = git([
  "diff",
  "--raw",
  "--no-renames",
  "-z",
  baseCommit,
  targetCommit,
])
  .split("\0")
  .filter(Boolean);

// git diff --raw -z --no-renames alternates metadata and path fields:
// ":<oldmode> <newmode> <oldblob> <newblob> <status>", then the path.
const changedPaths = [];
for (let index = 0; index < records.length; index += 2) {
  const meta =
    /^:([0-7]{6}) ([0-7]{6}) [0-9a-f]+ [0-9a-f]+ ([A-Z])$/.exec(
      records[index],
    );
  const filePath = records[index + 1];
  if (meta === null || filePath === undefined) {
    fail("evidence-to-target delta contains an unsupported diff record");
  }
  const [, oldMode, newMode, status] = meta;
  if (!/^[\x20-\x7e]+$/.test(filePath)) {
    fail("evidence-to-target delta contains a non-ASCII path");
  }
  if (!["A", "M", "D"].includes(status)) {
    fail(`${filePath} uses unsupported change status ${status}`);
  }
  if (!allowedModes.has(oldMode) || !allowedModes.has(newMode)) {
    fail(`${filePath} must stay a regular non-executable file`);
  }
  const baseName = filePath.split("/").pop();
  if (!allowedPath.test(filePath) || deniedBasename.test(baseName)) {
    fail(`${filePath} is outside the non-sensitive source scope`);
  }
  changedPaths.push(filePath);
}

if (changedPaths.length === 0) {
  fail("evidence-to-target delta must not be empty");
}

for (const filePath of changedPaths) {
  // --text and --no-ext-diff override in-tree diff attributes, binary
  // heuristics, and external diff drivers so the guard can never be
  // blinded; :(literal) disables pathspec magic. -U1 keeps one context
  // line per change: editing only the continuation body of a multi-line
  // install command still surfaces the unchanged lead line to the scan.
  const diffLines = git([
    "diff",
    "--no-renames",
    "--no-ext-diff",
    "--text",
    "-U1",
    baseCommit,
    targetCommit,
    "--",
    `:(literal)${filePath}`,
  ]).split(/\r?\n/);
  // Skip only the structural header block before the first hunk; after a
  // "@@" marker every line — changed or context — is scanned, so
  // header-shaped content lines ("+++ b/...") can never dodge the scan.
  let sawHunk = false;
  for (const line of diffLines) {
    if (line.startsWith("@@")) {
      sawHunk = true;
      continue;
    }
    if (!sawHunk) {
      continue;
    }
    // Control characters (UTF-16 NUL padding, separators) would split
    // command words and blind the denylist below — fail closed instead.
    if (/[\0-\b\v-\x1f\x7f]/.test(line)) {
      fail(`${filePath} changes non-text content; full evidence required`);
    }
    // A trailing backtick is a PowerShell continuation only when it is
    // unpaired: Markdown inline code spans ("Use `ha-nova setup`") close
    // their own backtick and fence lines are backticks only. Measured
    // against the active docs, this separates 37 real shell continuations
    // and 2 real PowerShell ones from 1700 ordinary code-span lines.
    const content = line.slice(1);
    const backticks = (content.match(/`/g) ?? []).length;
    const continuation =
      /[\\^]$/.test(content) ||
      (content.endsWith("`") &&
        backticks % 2 === 1 &&
        !/^\s*`+\s*$/.test(content));
    if (installCommand.test(line) || continuation) {
      fail(
        `${filePath} changes an install-command or continuation line; full evidence required`,
      );
    }
  }
  if (!sawHunk) {
    fail(
      `${filePath} produced no scannable textual delta; full evidence required`,
    );
  }
}

console.log(
  `[verify-cloud-nonsensitive-source] OK: ${changedPaths.length} non-sensitive file(s)`,
);
