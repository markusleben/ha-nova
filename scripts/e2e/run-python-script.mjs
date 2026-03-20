import { spawnSync } from "node:child_process";

const [, , scriptPath, ...scriptArgs] = process.argv;

if (!scriptPath) {
  console.error("Usage: node scripts/e2e/run-python-script.mjs <script> [args...]");
  process.exit(1);
}

const candidates = process.platform === "win32"
  ? [["py", ["-3"]], ["python", []], ["python3", []]]
  : [["python3", []], ["python", []], ["py", ["-3"]]];

for (const [command, prefixArgs] of candidates) {
  const result = spawnSync(command, [...prefixArgs, scriptPath, ...scriptArgs], {
    stdio: "inherit",
  });

  if (result.error && "code" in result.error && result.error.code === "ENOENT") {
    continue;
  }

  if (result.error) {
    throw result.error;
  }

  process.exit(result.status ?? 1);
}

console.error("Python 3 runtime not found. Install python3, python, or py -3.");
process.exit(1);
