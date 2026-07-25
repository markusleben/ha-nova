import { randomBytes } from "node:crypto";
import { join } from "node:path";

import { readPrivateFileSync, writeFileAtomicSync } from "./atomic-file.js";

const FILE_NAME = "relay-instance-id";
const PREFIX = "hanova-relay-v1";
const RANDOM_BYTES = 16;
const RANDOM_LENGTH = 22;
const MAX_FILE_BYTES = 128;

export class RelayInstanceIdentityError extends Error {}

// The relay identity is a stable, non-secret collision-resistant handle. Never
// silently rotate a present but invalid file: clients use it to prevent sending
// a device credential to the wrong Home Assistant instance.
export function loadOrCreateRelayInstanceId(dataDir: string): string {
  const path = join(dataDir, FILE_NAME);
  const stored = readPrivateFileSync(path, MAX_FILE_BYTES);
  if (stored !== null) {
    const value = stored.toString("utf8");
    if (!isRelayInstanceId(value)) {
      throw new RelayInstanceIdentityError(
        "relay instance identity is invalid",
      );
    }
    return value;
  }

  const value = `${PREFIX}.${randomBytes(RANDOM_BYTES).toString("base64url")}`;
  writeFileAtomicSync(path, value);
  return value;
}

export function isRelayInstanceId(value: unknown): value is string {
  if (typeof value !== "string") {
    return false;
  }
  const [prefix, random, extra] = value.split(".");
  return (
    extra === undefined &&
    prefix === PREFIX &&
    random?.length === RANDOM_LENGTH &&
    /^[A-Za-z0-9_-]+$/.test(random)
  );
}
