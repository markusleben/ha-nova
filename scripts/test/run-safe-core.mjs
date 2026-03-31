import { existsSync, readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const filesPath = resolve(scriptDir, "safe-core-files.json");
const files = JSON.parse(readFileSync(filesPath, "utf8"));

if (!Array.isArray(files) || files.length === 0) {
  console.error("safe-core-files.json must contain at least one test file");
  process.exit(1);
}

const invalidFiles = files.filter((file) => typeof file !== "string" || !file.startsWith("tests/"));
if (invalidFiles.length > 0) {
  console.error(`safe-core-files.json contains invalid entries: ${invalidFiles.join(", ")}`);
  process.exit(1);
}

const duplicates = files.filter((file, index) => files.indexOf(file) !== index);
if (duplicates.length > 0) {
  console.error(`safe-core-files.json contains duplicate entries: ${[...new Set(duplicates)].join(", ")}`);
  process.exit(1);
}

const missingFiles = files.filter((file) => !existsSync(resolve(process.cwd(), file)));
if (missingFiles.length > 0) {
  console.error(`safe-core-files.json references missing files: ${missingFiles.join(", ")}`);
  process.exit(1);
}

const result = spawnSync("vitest", ["run", ...files], { stdio: "inherit" });
process.exit(result.status ?? 1);
