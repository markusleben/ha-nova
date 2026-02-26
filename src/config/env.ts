export type LogLevel = "trace" | "debug" | "info" | "warn" | "error";

export interface EnvConfig {
  relayAuthToken: string;
  haLlat?: string;
  supervisorToken?: string;
  haUrl: string;
  relayVersion: string;
  appOptionsPath: string;
  relayPort: number;
  logLevel: LogLevel;
  wsAllowlistExtra: string[];
}

const DEFAULT_RELAY_PORT = 8791;
const DEFAULT_LOG_LEVEL: LogLevel = "info";
const DEFAULT_HA_URL = "http://supervisor/core";
const DEFAULT_RELAY_VERSION = "dev";
const DEFAULT_APP_OPTIONS_PATH = "/data/options.json";
const ALLOWED_LOG_LEVELS = new Set<LogLevel>([
  "trace",
  "debug",
  "info",
  "warn",
  "error"
]);

export function loadEnv(source: NodeJS.ProcessEnv = process.env): EnvConfig {
  const relayAuthToken = parseOptionalToken(source.RELAY_AUTH_TOKEN);
  if (!relayAuthToken) {
    throw new Error("RELAY_AUTH_TOKEN is required");
  }
  const haLlat = parseOptionalToken(source.HA_LLAT);
  const supervisorToken = parseOptionalToken(source.SUPERVISOR_TOKEN);
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

  const wsAllowlistExtra = parseCsvList(source.WS_ALLOWLIST_APPEND);

  const config: EnvConfig = {
    relayAuthToken,
    haUrl,
    relayVersion,
    appOptionsPath,
    relayPort,
    logLevel: logRaw as LogLevel,
    wsAllowlistExtra
  };

  if (haLlat) {
    config.haLlat = haLlat;
  }

  if (supervisorToken) {
    config.supervisorToken = supervisorToken;
  }

  return config;
}

function parseCsvList(input: string | undefined): string[] {
  if (!input) {
    return [];
  }

  return input
    .split(",")
    .map((value) => value.trim())
    .filter((value) => value.length > 0);
}

function parseOptionalToken(input: string | undefined): string | undefined {
  const value = input?.trim();
  if (!value) {
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
