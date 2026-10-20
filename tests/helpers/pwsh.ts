/**
 * pwsh for installer tests, without telemetry. On a fresh CI runner the
 * first pwsh starts of two test files, at the same moment, hung for 35 s
 * (run 39223271065) or died at startup (run 39347184646); later starts never
 * failed. Telemetry setup is the shared first-start step: it creates the
 * per-user telemetry UUID file under a named mutex, and pwsh 7.6.x (the
 * runner's) can hold that mutex until exit (fixed upstream in
 * PowerShell#28026, not in any 7.6 release). Opting out skips that step.
 */
import { spawnSync } from "node:child_process";

export const pwshEnv: NodeJS.ProcessEnv = { ...process.env, POWERSHELL_TELEMETRY_OPTOUT: "1" };

let available: boolean | undefined;

/** One probe spawn per test file, not one per test. */
export function hasPowerShell(): boolean {
  available ??= spawnSync("pwsh", ["-NoProfile", "-Command", "exit 0"], { env: pwshEnv }).status === 0;
  return available;
}
