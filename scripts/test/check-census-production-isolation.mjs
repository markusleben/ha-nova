// Static gate (#446): no test, smoke, release, or deployment-verification
// path may call the PRODUCTION census Worker's mutation endpoints. Production
// statistics represent voluntary real participants only; functional census
// checks run exclusively against the isolated test Worker
// (wrangler env "test", scripts/release/verify-census-functional.sh).
//
// The scan is deliberately simple: inside the executable surfaces (scripts/,
// tests/, .github/), a file fails when it (a) combines the production census
// host and a mutation path on ONE line, or (b) assigns the production host to
// a URL variable and later builds a mutation URL from that variable. Local
// wrangler-dev smokes (local_url) and prose stay clean; the read-only
// deployment check may reference the production host, but never build a
// mutation URL from it. cli/ (the product itself) is out of scope.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const PRODUCTION_HOST = "ha-nova-census.markusleben.workers.dev";
const MUTATION_PATTERN = /\/(ping|withdraw)\b/;
const SCAN_ROOTS = ["scripts", "tests", ".github"];
// Files that legitimately QUOTE the forbidden patterns: this gate itself and
// the contract test whose pins are exactly what keeps the two verifier
// scripts honest (production = read-only, functional = test host only).
const ALLOWLIST = new Set([
  "scripts/test/check-census-production-isolation.mjs",
  "tests/onboarding/release-contract.test.ts",
]);

function* walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    const stats = statSync(path);
    if (stats.isDirectory()) {
      if (entry === "node_modules") continue;
      yield* walk(path);
    } else if (stats.isFile()) {
      yield path;
    }
  }
}

const violations = [];
for (const root of SCAN_ROOTS) {
  const rootPath = join(repoRoot, root);
  let rootStat;
  try {
    rootStat = statSync(rootPath);
  } catch {
    continue;
  }
  if (!rootStat.isDirectory()) continue;
  for (const path of walk(rootPath)) {
    const rel = relative(repoRoot, path).split(sep).join("/");
    if (ALLOWLIST.has(rel)) continue;
    let content;
    try {
      content = readFileSync(path, "utf8");
    } catch {
      continue; // binary or unreadable — nothing to scan
    }
    if (!content.includes(PRODUCTION_HOST)) continue;
    // (a) Production host and a mutation path on the same line.
    const sameLine = content
      .split("\n")
      .some((line) => line.includes(PRODUCTION_HOST) && MUTATION_PATTERN.test(line));
    // (b) A URL variable assigned the production host, later used to build a
    // mutation URL (shell `${var}/ping` or template `${var}/withdraw`).
    const assignedVars = [...content.matchAll(
      /(\w+)\s*=\s*["'`]https:\/\/ha-nova-census\.markusleben\.workers\.dev["'`]/g,
    )].map((m) => m[1]);
    const viaVariable = assignedVars.some((name) =>
      new RegExp(`\\$\\{${name}\\}\\/(ping|withdraw)\\b`).test(content),
    );
    if (sameLine || viaVariable) {
      violations.push(rel);
    }
  }
}

if (violations.length > 0) {
  console.error(
    "[census-production-isolation] FAIL: these files reference the production census host together with a mutation path (/ping or /withdraw). Production census statistics must never be mutated by tests, smokes, releases, or deployment verification — use the isolated test worker (wrangler env \"test\") instead:",
  );
  for (const file of violations) {
    console.error(`  - ${file}`);
  }
  process.exit(1);
}

console.log("[census-production-isolation] OK: no executable path mutates the production census worker");
