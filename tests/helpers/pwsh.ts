/**
 * pwsh for installer tests, without telemetry. On a fresh CI runner the
 * first pwsh starts of two test files, at the same moment, hung for 35 s
 * (run 39223271065) or died at startup with exit 0 (run 39347184646); later
 * starts never failed. Suspected cause (not reproduced locally): telemetry
 * setup, the shared first-start step. It creates the per-user telemetry UUID
 * file under a named mutex that pwsh 7.6.x (the runner's) can hold until
 * exit, which explains the linked hang (fixed upstream in PowerShell#28026,
 * not in any 7.6 release); the exit-0 crash fits a telemetry background
 * thread failing. Opting out skips all telemetry setup.
 */
import { spawnSync } from "node:child_process";

export const pwshEnv: NodeJS.ProcessEnv = { ...process.env, POWERSHELL_TELEMETRY_OPTOUT: "1" };

let available: boolean | undefined;

/**
 * One probe spawn per test file, not one per test. Only a missing pwsh skips
 * (never in CI, which installs it); a pwsh that is there but fails throws.
 */
export function hasPowerShell(): boolean {
  if (available !== undefined) return available;
  const probe = spawnSync("pwsh", ["-NoProfile", "-Command", "exit 0"], { encoding: "utf8", env: pwshEnv });
  const missing = (probe.error as NodeJS.ErrnoException | undefined)?.code === "ENOENT";
  if (missing && process.env.CI) throw new Error("pwsh is missing on CI; the installer tests need it");
  if (!missing && probe.status !== 0) {
    throw new Error(`pwsh probe failed (status ${probe.status}, signal ${probe.signal}): ${probe.error ?? ""}${probe.stdout}${probe.stderr}`);
  }
  available = !missing;
  return available;
}
