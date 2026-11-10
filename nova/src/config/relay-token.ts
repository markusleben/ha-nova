import { randomBytes } from "node:crypto";
import { chmodSync, lstatSync, readFileSync, writeFileSync } from "node:fs";

const OWNER_READ_WRITE = 0o600;
const MAX_TOKEN_FILE_BYTES = 4_096;

export function resolveRelayAuthToken(source: NodeJS.ProcessEnv): string {
  // App mode (SUPERVISOR_TOKEN present) authenticates per device via pairing
  // only: it has no shared token, configured or generated.
  if (parseOptionalToken(source.SUPERVISOR_TOKEN)) {
    return "";
  }

  const configuredToken = parseOptionalToken(source.RELAY_AUTH_TOKEN);
  if (configuredToken) {
    return configuredToken;
  }

  const tokenFile = source.RELAY_AUTH_TOKEN_FILE?.trim();
  if (!tokenFile) {
    throw new Error("RELAY_AUTH_TOKEN is required");
  }

  return loadOrCreateRelayAuthToken(tokenFile);
}

export function loadOrCreateRelayAuthToken(tokenFile: string): string {
  try {
    return readPersistedToken(tokenFile);
  } catch (error) {
    if (!isNodeError(error, "ENOENT")) {
      throw error;
    }
  }

  const token = randomBytes(32).toString("hex");
  try {
    writeFileSync(tokenFile, `${token}\n`, {
      encoding: "utf8",
      flag: "wx",
      mode: OWNER_READ_WRITE
    });
    return token;
  } catch (error) {
    // A concurrent startup may have won the exclusive create. Reuse only the
    // complete token it persisted; every other filesystem failure stays loud.
    if (isNodeError(error, "EEXIST")) {
      return readPersistedToken(tokenFile);
    }
    throw error;
  }
}

function readPersistedToken(tokenFile: string): string {
  const stats = lstatSync(tokenFile);
  if (!stats.isFile() || stats.size > MAX_TOKEN_FILE_BYTES) {
    throw new Error(`Relay auth token path must be a small regular file: ${tokenFile}`);
  }

  const token = parseOptionalToken(readFileSync(tokenFile, "utf8"));
  if (!token) {
    throw new Error(`Relay auth token file is empty: ${tokenFile}`);
  }

  chmodSync(tokenFile, OWNER_READ_WRITE);
  return token;
}

function parseOptionalToken(input: string | undefined): string | undefined {
  const value = input?.trim();
  if (!value || value === "null") {
    return undefined;
  }
  return value;
}

function isNodeError(error: unknown, code: string): error is NodeJS.ErrnoException {
  return error instanceof Error && "code" in error && error.code === code;
}
