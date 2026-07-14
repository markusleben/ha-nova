export type LogLevel = "trace" | "debug" | "info" | "warn" | "error";

export interface EnvConfig {
  relayAuthToken: string;
  haLlat: string;
  haUrl: string;
  relayVersion: string;
  appOptionsPath: string;
  relayPort: number;
  logLevel: LogLevel;
  snapshotDir: string;
}

const DEFAULT_RELAY_PORT = 8791;
const DEFAULT_LOG_LEVEL: LogLevel = "info";
const DEFAULT_HA_URL = "http://homeassistant:8123";
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
  const relayAuthToken = parseRequiredToken(source.RELAY_AUTH_TOKEN, "RELAY_AUTH_TOKEN is required");
  const haLlat = parseRequiredToken(source.HA_LLAT, "HA_LLAT is required");
  const haUrl = parseRequiredLike(source.HA_URL, DEFAULT_HA_URL);
  const relayVersion = parseRequiredLike(source.RELAY_VERSION, DEFAULT_RELAY_VERSION);
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
    haLlat,
    haUrl,
    relayVersion,
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

function parseRequiredToken(input: string | undefined, errorMessage: string): string {
  const value = parseOptionalToken(input);
  if (!value) {
    throw new Error(errorMessage);
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
