import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const scriptNames = process.argv.slice(2);

if (scriptNames.length === 0) {
  console.error("Usage: node scripts/test/assert-vitest-files-exist.mjs <npm-script> [...]");
  process.exit(1);
}

const packageJson = JSON.parse(readFileSync(resolve(process.cwd(), "package.json"), "utf8"));
const scripts = packageJson.scripts ?? {};
let failed = false;

for (const scriptName of scriptNames) {
  const script = scripts[scriptName];
  if (typeof script !== "string") {
    console.error(`npm script "${scriptName}" does not exist`);
    failed = true;
    continue;
  }

  const referencedFiles = [...new Set(script.match(/\btests\/[^\s"'`]+\.test\.[cm]?[jt]s\b/g) ?? [])];
  if (referencedFiles.length === 0) {
    console.error(`npm script "${scriptName}" does not reference explicit Vitest test files`);
    failed = true;
    continue;
  }

  const missingFiles = referencedFiles.filter((file) => !existsSync(resolve(process.cwd(), file)));
  if (missingFiles.length > 0) {
    console.error(`npm script "${scriptName}" references missing test files: ${missingFiles.join(", ")}`);
    failed = true;
  }
}

if (failed) {
  process.exit(1);
}
