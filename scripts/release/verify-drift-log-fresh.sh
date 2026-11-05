#!/usr/bin/env bash
# Release preflight: fails when the newest screen in the upstream drift log is
# older than 35 days. AGENTS.md (Releases) asks for an upstream drift check
# before each release cycle and on each monthly Home Assistant release. A
# screen is an entry whose "## YYYY-MM-DD" heading says "pre-release screen"
# or "deep-audit screen"; ad hoc checks and corrections don't count.
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG="${HA_NOVA_DRIFT_LOG:-${ROOT_DIR}/docs/work/upstream-drift-log.md}"
TODAY="${HA_NOVA_DRIFT_TODAY:-$(date -u +%Y-%m-%d)}"

node - "${LOG}" "${TODAY}" <<'NODE'
const fs = require("node:fs");

const [log, today] = process.argv.slice(2);
const MAX_AGE_DAYS = 35;
const RULE = "AGENTS.md (Releases) asks for an upstream drift check before each release cycle: screen the Home Assistant release post, the developer blog and HACS releases against the skill-pinned surfaces, append the run to the log, then try again.";

function fail(message) {
  console.error(`[verify-drift-log-fresh] ERROR: ${message}`);
  process.exit(1);
}

function day(raw, label) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(raw);
  const ms = match ? Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3])) : NaN;
  if (Number.isNaN(ms) || new Date(ms).toISOString().slice(0, 10) !== raw) {
    fail(`${label} is not a date (YYYY-MM-DD): ${raw}`);
  }
  return ms;
}

let text;
try {
  text = fs.readFileSync(log, "utf8");
} catch (error) {
  fail(`cannot read ${log}: ${error.message}. ${RULE}`);
}

// Full screens only, outside code fences and HTML comments.
const SCREEN = /^## (\d{4}-\d{2}-\d{2})(?=\s|$).*\b(?:pre-release|deep-audit) screen\b/i;
const dates = [];
let fence = null;
for (const line of text.replace(/<!--[\s\S]*?-->/g, "").split(/\r?\n/)) {
  const marker = /^ {0,3}(`{3,}|~{3,})/.exec(line);
  if (fence) {
    if (marker && marker[1][0] === fence[0] && marker[1].length >= fence.length && line.trim() === marker[1]) fence = null;
    continue;
  }
  if (marker) {
    fence = marker[1];
    continue;
  }
  const match = SCREEN.exec(line);
  if (match) dates.push(match[1]);
}
if (dates.length === 0) {
  fail(`${log} has no screen (a "## YYYY-MM-DD — pre-release screen …" heading). ${RULE}`);
}
const now = day(today, "today");
const newest = dates.reduce((a, b) => (b > a ? b : a));
const age = Math.round((now - day(newest, `the entry heading "## ${newest}"`)) / 86400000);
// One day of slack: entries carry the local date, "today" is UTC.
if (age < -1) {
  fail(`the newest screen in ${log} is dated ${newest}, after today (${today}). Fix the heading.`);
}
if (age > MAX_AGE_DAYS) {
  fail(`the newest screen in ${log} is from ${newest}, ${age} days ago (limit ${MAX_AGE_DAYS}). ${RULE}`);
}
console.log(`[verify-drift-log-fresh] ok: ${log}: newest screen ${newest}, ${Math.max(age, 0)} days old as of ${today} (limit ${MAX_AGE_DAYS})`);
NODE
