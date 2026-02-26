export type LogLevel = "trace" | "debug" | "info" | "warn" | "error";

export interface EnvConfig {
  haToken: string;
  legacyHaToken?: string;
  haLlat?: string;
  supervisorToken?: string;
  haUrl: string;
  bridgeVersion: string;
  addonOptionsPath: string;
  bridgePort: number;
  logLevel: LogLevel;
  wsAllowlistExtra: string[];
}

const DEFAULT_BRIDGE_PORT = 8791;
const DEFAULT_LOG_LEVEL: LogLevel = "info";
const DEFAULT_HA_URL = "http://supervisor/core";
const DEFAULT_BRIDGE_VERSION = "dev";
const DEFAULT_ADDON_OPTIONS_PATH = "/data/options.json";
const ALLOWED_LOG_LEVELS = new Set<LogLevel>([
  "trace",
  "debug",
  "info",
  "warn",
  "error"
]);

export function loadEnv(source: NodeJS.ProcessEnv = process.env): EnvConfig {
  const bridgeAuthToken = parseOptionalToken(source.BRIDGE_AUTH_TOKEN) ?? parseOptionalToken(source.HA_TOKEN);
  if (!bridgeAuthToken) {
    throw new Error("BRIDGE_AUTH_TOKEN or HA_TOKEN is required");
  }
  const legacyHaToken = parseOptionalToken(source.BRIDGE_AUTH_TOKEN)
    ? parseOptionalToken(source.HA_TOKEN)
    : undefined;
  const haLlat = parseOptionalToken(source.HA_LLAT);
  const supervisorToken = parseOptionalToken(source.SUPERVISOR_TOKEN);
  const haUrl = parseRequiredLike(source.HA_URL, DEFAULT_HA_URL);
  const bridgeVersion = parseRequiredLike(source.BRIDGE_VERSION, DEFAULT_BRIDGE_VERSION);
  const addonOptionsPath = parseRequiredLike(
    source.ADDON_OPTIONS_PATH,
    DEFAULT_ADDON_OPTIONS_PATH
  );

  const portRaw = source.BRIDGE_PORT?.trim();
  const bridgePort = portRaw ? Number.parseInt(portRaw, 10) : DEFAULT_BRIDGE_PORT;
  if (!Number.isInteger(bridgePort) || bridgePort <= 0 || bridgePort > 65535) {
    throw new Error("BRIDGE_PORT must be an integer between 1 and 65535");
  }

  const logRaw = source.LOG_LEVEL?.trim() ?? DEFAULT_LOG_LEVEL;
  if (!ALLOWED_LOG_LEVELS.has(logRaw as LogLevel)) {
    throw new Error("LOG_LEVEL must be one of trace|debug|info|warn|error");
  }

  const wsAllowlistExtra = parseCsvList(source.WS_ALLOWLIST_APPEND);

  const config: EnvConfig = {
    haToken: bridgeAuthToken,
    haUrl,
    bridgeVersion,
    addonOptionsPath,
    bridgePort,
    logLevel: logRaw as LogLevel,
    wsAllowlistExtra
  };

  if (legacyHaToken) {
    config.legacyHaToken = legacyHaToken;
  }

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
