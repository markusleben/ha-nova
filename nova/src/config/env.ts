import { resolveRelayAuthToken } from "./relay-token.js";

export type LogLevel = "trace" | "debug" | "info" | "warn" | "error";

export interface EnvConfig {
  relayAuthToken: string;
  supervisorToken?: string;
  haLlat?: string;
  haUrl: string;
  relayVersion: string;
  minRelayVersion?: string;
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
// Lives inside the App data dir so Supervised full backups sweep it up; the
// standalone container overrides via SNAPSHOT_DIR (mount a volume to persist).
const DEFAULT_SNAPSHOT_DIR = "/data/ha_nova_snapshots";
const ALLOWED_LOG_LEVELS = new Set<LogLevel>([
  "trace",
  "debug",
  "info",
  "warn",
  "error"
]);

export function loadEnv(source: NodeJS.ProcessEnv = process.env): EnvConfig {
  const relayAuthToken = resolveRelayAuthToken(source);
  const supervisorToken = parseOptionalToken(source.SUPERVISOR_TOKEN);
  const haLlat = parseOptionalToken(source.HA_LLAT);
  if (!supervisorToken && !haLlat) {
    throw new Error("SUPERVISOR_TOKEN or HA_LLAT is required");
  }
  const haUrl = parseRequiredLike(
    source.HA_URL,
    supervisorToken ? DEFAULT_SUPERVISOR_HA_URL : DEFAULT_HA_URL
  );
  const relayVersion = parseRequiredLike(source.RELAY_VERSION, DEFAULT_RELAY_VERSION);
  const minRelayVersion = parseRequiredLike(source.MIN_RELAY_VERSION, relayVersion);
  const appOptionsPath = parseRequiredLike(source.APP_OPTIONS_PATH, DEFAULT_APP_OPTIONS_PATH);

  const portRaw = source.RELAY_PORT?.trim();
  const relayPort = portRaw ? Number.parseInt(portRaw, 10) : DEFAULT_RELAY_PORT;
  if (!Number.isInteger(relayPort) || relayPort <= 0 || relayPort > 65535) {
    throw new Error("RELAY_PORT must be an integer between 1 and 65535");
  }

  const logRaw = source.LOG_LEVEL?.trim() ?? DEFAULT_LOG_LEVEL;
  if (!ALLOWED_LOG_LEVELS.has(logRaw as LogLevel)) {
    throw new Error("LOG_LEVEL must be one of trace|debug|info|warn|error");
  }

  const snapshotDir = parseRequiredLike(source.SNAPSHOT_DIR, DEFAULT_SNAPSHOT_DIR);

  return {
    relayAuthToken,
    ...(supervisorToken ? { supervisorToken } : {}),
    ...(haLlat ? { haLlat } : {}),
    haUrl,
    relayVersion,
    minRelayVersion,
    appOptionsPath,
    relayPort,
    logLevel: logRaw as LogLevel,
    snapshotDir
  };
}

function parseOptionalToken(input: string | undefined): string | undefined {
  const value = input?.trim();
  if (!value || value === "null") {
    return undefined;
  }

  return value;
}

function parseRequiredLike(input: string | undefined, fallback: string): string {
  const value = input?.trim();
  if (!value) {
    return fallback;
  }

  return value;
}
