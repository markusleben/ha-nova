import { readFileSync, unlinkSync } from "node:fs";
import { join } from "node:path";

import { createSupervisorClient } from "../ha/supervisor-client.js";
import type { RelayLogger } from "../http/server.js";
import { digestSecret } from "../security/device-credential.js";
import type { DeviceRegistry } from "../security/device-registry.js";

// One-time migration of the pre-pairing shared relay token into the registry as
// a single "legacy shared access" digest, so existing clients keep working until
// the owner revokes legacy. Order is safety-first: import the digest (atomic) and
// stamp the tombstone, THEN delete the plaintext file, THEN clear the option.
// The registry record or the tombstone always wins, so this never re-imports —
// including on a restored backup — and a corrupt registry never reaches here.

const LEGACY_TOKEN_FILE = "relay_auth_token";

export interface LegacyMigrationDeps {
  registry: DeviceRegistry;
  supervisor: ReturnType<typeof createSupervisorClient>;
  dataDir: string;
  appOptionsPath: string;
  now: () => number;
  logger: RelayLogger;
}

export async function importLegacyToken(deps: LegacyMigrationDeps): Promise<void> {
  const options = readOptions(deps.appOptionsPath);

  // Import the digest exactly once (a registry record or tombstone wins), but
  // ALWAYS retry the plaintext cleanup below: a prior boot may have persisted the
  // digest yet failed to remove the file or clear the option, and that residue
  // must not linger just because the one-shot import guard now short-circuits.
  if (!deps.registry.legacyImportCompleted() && !deps.registry.hasLegacy()) {
    const token = findEffectiveLegacyToken(options, deps.dataDir);
    if (token === null) {
      return; // nothing to migrate (a fresh install)
    }
    // Import the digest + stamp the tombstone atomically, THEN clean up plaintext.
    deps.registry.importLegacy(digestSecret(token), deps.now());
    deps.logger.info?.("Migrated the legacy shared token into the device registry as one legacy record");
  }

  await removeResidualLegacyPlaintext(deps, options);
}

// Removes any leftover plaintext shared token — the /data file and the App option
// — once the digest is safely in the registry. Idempotent and retried on every
// boot, so a migration that stamped the tombstone but failed to finish cleanup on
// an earlier boot still self-heals (the runtime ignores the option regardless).
async function removeResidualLegacyPlaintext(
  deps: LegacyMigrationDeps,
  options: Record<string, unknown>,
): Promise<void> {
  try {
    unlinkSync(join(deps.dataDir, LEGACY_TOKEN_FILE));
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") {
      deps.logger.warn("Could not remove the legacy plaintext token file", { error: String((error as Error).message) });
    }
  }
  if (typeof options.relay_auth_token === "string" && options.relay_auth_token.length > 0) {
    try {
      await deps.supervisor.setOptions({ ...options, relay_auth_token: "" });
    } catch (error) {
      deps.logger.warn("Could not clear the legacy shared token from App options", { error: String((error as Error).message) });
    }
  }
}

// App mode's upstream is the Supervisor token, so a `ha_llat` left in
// options.json by a pre-pairing install is an unused full Home Assistant access
// token sitting at rest. Clear it. This is independent of the shared-token
// migration above (which is gated by a one-shot tombstone), so a value that
// lingers when there is no shared token to migrate — or after a transient
// setOptions failure — is still cleaned up on a later boot. Idempotent: once the
// option is empty this is a no-op. Only reached with a Supervisor token present,
// where `ha_llat` is never consulted, so clearing it can never drop the upstream.
export async function clearLegacyUpstreamOption(
  deps: Pick<LegacyMigrationDeps, "supervisor" | "appOptionsPath" | "logger">,
): Promise<void> {
  const options = readOptions(deps.appOptionsPath);
  const llat = options.ha_llat;
  if (typeof llat !== "string" || llat.length === 0) {
    return;
  }
  try {
    await deps.supervisor.setOptions({ ...options, ha_llat: "" });
    deps.logger.info?.("Cleared the unused legacy Home Assistant token from App options (upstream is the Supervisor token)");
  } catch (error) {
    deps.logger.warn("Could not clear the legacy Home Assistant token from App options", { error: String((error as Error).message) });
  }
}

// Precedence: a non-empty configured option wins; otherwise the /data file. The
// residual plaintext (file + option) is always cleaned up afterwards regardless
// of which source supplied the effective value.
function findEffectiveLegacyToken(options: Record<string, unknown>, dataDir: string): string | null {
  const optionToken = options.relay_auth_token;
  if (typeof optionToken === "string" && optionToken.trim().length > 0) {
    return optionToken.trim();
  }
  try {
    const fileToken = readFileSync(join(dataDir, LEGACY_TOKEN_FILE), "utf8").trim();
    if (fileToken.length > 0) {
      return fileToken;
    }
  } catch {
    // no file
  }
  return null;
}

function readOptions(appOptionsPath: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(readFileSync(appOptionsPath, "utf8")) as unknown;
    return typeof parsed === "object" && parsed !== null ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}
