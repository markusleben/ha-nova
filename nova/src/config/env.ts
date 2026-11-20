import { readFileSync } from "node:fs";

import { resolveRelayAuthToken } from "./relay-token.js";

export type LogLevel = "trace" | "debug" | "info" | "warn" | "error";

export interface EnvConfig {
  relayAuthToken: string;
  supervisorToken?: string;
  haLlat?: string;
  haUrl: string;
  relayVersion: string;
  cloudRemoteEnabled: boolean;
  appOptionsPath: string;
  relayPort: number;
  logLevel: LogLevel;
  snapshotDir: string;
}

const DEFAULT_RELAY_PORT = 8791;
const DEFAULT_LOG_LEVEL: LogLevel = "info";
const DEFAULT_HA_URL = "http://homeassistant:8123";
const DEFAULT_SUPERVISOR_HA_URL = "http://supervisor/core";
const DEFAULT_RELAY_VERSION = "dev";
const DEFAULT_APP_OPTIONS_PATH = "/data/options.json";
// Only the App image carries it; the standalone image has none (Cloud off).
const APP_VERSION_FILE = "/app/version.json";
// Lives inside the App data dir so Supervised full backups sweep it up; the
// standalone container overrides via SNAPSHOT_DIR (mount a volume to persist).
const DEFAULT_SNAPSHOT_DIR = "/data/ha_nova_snapshots";
const ALLOWED_LOG_LEVELS = new Set<LogLevel>([
  "trace",
  "debug",
  "info",
  "warn",
  "error",
]);

export function loadEnv(
  source: NodeJS.ProcessEnv = process.env,
  versionFile = APP_VERSION_FILE,
): EnvConfig {
  const relayAuthToken = resolveRelayAuthToken(source);
  const supervisorToken = parseOptionalToken(source.SUPERVISOR_TOKEN);
  const haLlat = parseOptionalToken(source.HA_LLAT);
  if (!supervisorToken && !haLlat) {
    throw new Error("SUPERVISOR_TOKEN or HA_LLAT is required");
  }
  const haUrl = parseRequiredLike(
    source.HA_URL,
    supervisorToken ? DEFAULT_SUPERVISOR_HA_URL : DEFAULT_HA_URL,
  );
  const relayVersion = parseRequiredLike(
    source.RELAY_VERSION,
    DEFAULT_RELAY_VERSION,
  );
  const cloudRemoteEnabled =
    source.CLOUD_REMOTE_ENABLED === undefined
      ? cloudRemoteEnabledFromImage(versionFile)
      : parseCloudRemoteEnabled(source.CLOUD_REMOTE_ENABLED);
  const appOptionsPath = parseRequiredLike(
    source.APP_OPTIONS_PATH,
    DEFAULT_APP_OPTIONS_PATH,
  );

  const portRaw = source.RELAY_PORT?.trim();
  // Number() (not the prefix-lenient parseInt): "8791oops" and "1.5" must
  // fail the integer guard below instead of starting on a different port.
  const relayPort = portRaw ? Number(portRaw) : DEFAULT_RELAY_PORT;
  if (!Number.isInteger(relayPort) || relayPort <= 0 || relayPort > 65535) {
    throw new Error("RELAY_PORT must be an integer between 1 and 65535");
  }

  const logRaw = source.LOG_LEVEL?.trim() ?? DEFAULT_LOG_LEVEL;
  if (!ALLOWED_LOG_LEVELS.has(logRaw as LogLevel)) {
    throw new Error("LOG_LEVEL must be one of trace|debug|info|warn|error");
  }

  const snapshotDir = parseRequiredLike(
    source.SNAPSHOT_DIR,
    DEFAULT_SNAPSHOT_DIR,
  );

  return {
    relayAuthToken,
    ...(supervisorToken ? { supervisorToken } : {}),
    ...(haLlat ? { haLlat } : {}),
    haUrl,
    relayVersion,
    cloudRemoteEnabled,
    appOptionsPath,
    relayPort,
    logLevel: logRaw as LogLevel,
    snapshotDir,
  };
}

function parseOptionalToken(input: string | undefined): string | undefined {
  const value = input?.trim();
  if (!value || value === "null") {
    return undefined;
  }

  return value;
}

function parseRequiredLike(
  input: string | undefined,
  fallback: string,
): string {
  const value = input?.trim();
  if (!value) {
    return fallback;
  }

  return value;
}

function cloudRemoteEnabledFromImage(path: string): boolean {
  let raw: string;
  try {
    raw = readFileSync(path, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") {
      return false;
    }
    throw error;
  }
  const value = (JSON.parse(raw) as { cloud_remote_enabled?: unknown }).cloud_remote_enabled;
  if (typeof value !== "boolean") {
    throw new Error(`cloud_remote_enabled must be a boolean in ${path}`);
  }
  return value;
}

function parseCloudRemoteEnabled(input: string): boolean {
  if (input === "true") {
    return true;
  }
  if (input === "false") {
    return false;
  }
  throw new Error("CLOUD_REMOTE_ENABLED must be exactly true or false");
}
