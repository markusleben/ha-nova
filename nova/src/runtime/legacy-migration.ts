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
  // Registry record or tombstone wins: never re-import.
  if (deps.registry.legacyImportCompleted() || deps.registry.hasLegacy()) {
    return;
  }

  const options = readOptions(deps.appOptionsPath);
  const source = findEffectiveLegacyToken(options, deps.dataDir);
  if (source === null) {
    return; // nothing to migrate (a fresh install)
  }

  // Import the digest + stamp the tombstone atomically.
  deps.registry.importLegacy(digestSecret(source.token), deps.now());

  // Only after the digest is persisted do we remove the plaintext file.
  if (source.filePath !== null) {
    try {
      unlinkSync(source.filePath);
    } catch (error) {
      deps.logger.warn("Migrated legacy token but could not remove the plaintext file", { error: String((error as Error).message) });
    }
  }

  // Clear the option so the plaintext no longer sits in options.json. A failure
  // here is non-fatal: the tombstone already prevents re-import and the runtime
  // ignores the option, so we only warn.
  if (typeof options.relay_auth_token === "string" && options.relay_auth_token.length > 0) {
    try {
      await deps.supervisor.setOptions({ ...options, relay_auth_token: "" });
    } catch (error) {
      deps.logger.warn("Migrated legacy token but could not clear the App option", { error: String((error as Error).message) });
    }
  }

  deps.logger.info?.("Migrated the legacy shared token into the device registry as one legacy record");
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

interface LegacySource {
  token: string;
  filePath: string | null; // set when the source was the /data file
}

// Precedence: a non-empty configured option wins; otherwise the /data file. The
// "effective" value is imported (the option path does not always write a file).
function findEffectiveLegacyToken(options: Record<string, unknown>, dataDir: string): LegacySource | null {
  const optionToken = options.relay_auth_token;
  if (typeof optionToken === "string" && optionToken.trim().length > 0) {
    return { token: optionToken.trim(), filePath: null };
  }
  const filePath = join(dataDir, LEGACY_TOKEN_FILE);
  try {
    const fileToken = readFileSync(filePath, "utf8").trim();
    if (fileToken.length > 0) {
      return { token: fileToken, filePath };
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
