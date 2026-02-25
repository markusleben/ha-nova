export type LogLevel = "trace" | "debug" | "info" | "warn" | "error";

export interface EnvConfig {
  haToken: string;
  bridgePort: number;
  logLevel: LogLevel;
  wsAllowlistExtra: string[];
}

const DEFAULT_BRIDGE_PORT = 8791;
const DEFAULT_LOG_LEVEL: LogLevel = "info";
const ALLOWED_LOG_LEVELS = new Set<LogLevel>([
  "trace",
  "debug",
  "info",
  "warn",
  "error"
]);

export function loadEnv(source: NodeJS.ProcessEnv = process.env): EnvConfig {
  const haToken = source.HA_TOKEN?.trim();
  if (!haToken) {
    throw new Error("HA_TOKEN is required");
  }

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

  return {
    haToken,
    bridgePort,
    logLevel: logRaw as LogLevel,
    wsAllowlistExtra
  };
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
