/**
 * pwsh for installer tests. Every pwsh exit rewrites the .NET startup JIT
 * profile (StartupProfileData-NonInteractive) in place in its cache dir. Two
 * pwsh processes that share that dir and exit together can tear the file, and
 * the next pwsh that reads it dies in the profile player before it runs
 * anything (ArgumentException or FileLoadException from
 * AssemblyName.ParseAsAssemblySpec). The host then prints only "An error has
 * occurred that was not properly handled" and can still exit 0 (run
 * 41056840500). vitest runs both pwsh test files in parallel, so each file
 * gets its own cache dir; inside a file the starts are sequential. Telemetry
 * stays off: its first-start mutex can block a parallel start until exit
 * (PowerShell#28026).
 */
import { spawnSync } from "node:child_process";

import { makeTempDir } from "./temp-dir.js";

export const pwshEnv: NodeJS.ProcessEnv = {
  ...process.env,
  POWERSHELL_TELEMETRY_OPTOUT: "1",
  XDG_CACHE_HOME: makeTempDir("ha-nova-pwsh-cache-"),
};

const PROBE_MARKER = "ha-nova-pwsh-probe-ok";

let available: boolean | undefined;

/**
 * One probe spawn per test file, not one per test. Only a missing pwsh skips
 * (never in CI, which installs it). A pwsh that is there but fails throws,
 * including one that exits 0 without running the probe.
 */
export function hasPowerShell(): boolean {
  if (available !== undefined) return available;
  const probe = spawnSync("pwsh", ["-NoProfile", "-Command", `'${PROBE_MARKER}'`], { encoding: "utf8", env: pwshEnv });
  const missing = (probe.error as NodeJS.ErrnoException | undefined)?.code === "ENOENT";
  if (missing && process.env.CI) throw new Error("pwsh is missing on CI; the installer tests need it");
  if (!missing && (probe.status !== 0 || !probe.stdout?.includes(PROBE_MARKER))) {
    throw new Error(`pwsh probe failed (status ${probe.status}, signal ${probe.signal}): ${probe.error ?? ""}${probe.stdout}${probe.stderr}`);
  }
  available = !missing;
  return available;
}
