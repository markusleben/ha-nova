import { readFileSync, unlinkSync } from "node:fs";
import { join } from "node:path";

import { createSupervisorClient } from "../ha/supervisor-client.js";
import type { RelayLogger } from "../http/server.js";
import { digestSecret } from "../security/device-credential.js";
import type { DeviceRegistry } from "../security/device-registry.js";

// One-time migration of the pre-pairing shared relay token into the registry as
// a single "legacy shared access" digest, so existing clients keep working until
// the owner revokes legacy. Order is safety-first: import the digest (atomic) and
// stamp the tombstone, THEN delete the plaintext file. The registry record or the
// tombstone always wins, so this never re-imports — including on a restored
// backup — and a corrupt registry never reaches the import. Clearing the plaintext
// OPTIONS is a separate single write (clearLegacyOptions) to avoid clobbering a
// stale options.json.

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
  // Import the digest exactly once (a registry record or tombstone wins), then
  // remove the plaintext FILE on every boot (idempotent): a prior boot may have
  // stamped the tombstone yet failed to remove the file, and that must self-heal.
  // The plaintext OPTION is cleared separately in clearLegacyOptions, in one write
  // shared with the ha_llat clear.
  if (!deps.registry.legacyImportCompleted() && !deps.registry.hasLegacy()) {
    const token = findEffectiveLegacyToken(readOptions(deps.appOptionsPath), deps.dataDir);
    if (token === null) {
      return; // nothing to migrate (a fresh install)
    }
    deps.registry.importLegacy(digestSecret(token), deps.now());
    deps.logger.info?.("Migrated the legacy shared token into the device registry as one legacy record");
  }

  try {
    unlinkSync(join(deps.dataDir, LEGACY_TOKEN_FILE));
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") {
      deps.logger.warn("Could not remove the legacy plaintext token file", { error: String((error as Error).message) });
    }
  }
}

// Clears the legacy plaintext OPTIONS in a SINGLE write. /data/options.json is a
// start-time snapshot Supervisor does not rewrite at runtime, so two separate
// read-modify-writes would each spread the same stale file and the second would
// resurrect what the first cleared. `clearRelayToken` is false on a corrupt
// registry: the shared token was NOT migrated there, so it must stay recoverable
// until the owner resets. A `ha_llat` is always cleared — App mode's upstream is
// the Supervisor token, so it is unused dead weight at rest, and this is only
// reached with a Supervisor token present, so clearing can never drop the upstream.
export async function clearLegacyOptions(
  deps: Pick<LegacyMigrationDeps, "supervisor" | "appOptionsPath" | "logger">,
  clearRelayToken: boolean,
): Promise<void> {
  const options = readOptions(deps.appOptionsPath);
  const patch: Record<string, unknown> = { ...options };
  let changed = false;
  if (clearRelayToken && typeof options.relay_auth_token === "string" && options.relay_auth_token.length > 0) {
    patch.relay_auth_token = "";
    changed = true;
  }
  if (typeof options.ha_llat === "string" && options.ha_llat.length > 0) {
    patch.ha_llat = "";
    changed = true;
  }
  if (!changed) {
    return;
  }
  try {
    await deps.supervisor.setOptions(patch);
    deps.logger.info?.("Cleared legacy plaintext tokens from App options");
  } catch (error) {
    deps.logger.warn("Could not clear legacy tokens from App options", { error: String((error as Error).message) });
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
