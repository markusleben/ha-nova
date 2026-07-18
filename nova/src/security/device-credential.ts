import { createHash, randomBytes, timingSafeEqual } from "node:crypto";

// Versioned device credential. Format:  hanova-dev-v1.<device_id>.<secret>
// - device_id: 16 random bytes, base64url (a stable, non-secret handle)
// - secret:    32 random bytes (256-bit), base64url (the actual bearer secret)
// The relay stores ONLY sha256(secret) per device, never the plaintext. On auth
// the presented credential is parsed, the device looked up by device_id, and
// sha256(secret) compared to the stored digest in constant time.

const PREFIX = "hanova-dev-v1";
const DEVICE_ID_BYTES = 16;
const SECRET_BYTES = 32;
// base64url lengths (no padding): ceil(n*4/3)
const DEVICE_ID_LEN = 22;
const SECRET_LEN = 43;

export interface ParsedCredential {
  deviceId: string;
  secret: string;
}

export interface GeneratedCredential {
  credential: string;
  deviceId: string;
  secretDigest: string;
}

export function generateCredential(): GeneratedCredential {
  const deviceId = randomBytes(DEVICE_ID_BYTES).toString("base64url");
  const secret = randomBytes(SECRET_BYTES).toString("base64url");
  return {
    credential: `${PREFIX}.${deviceId}.${secret}`,
    deviceId,
    secretDigest: digestSecret(secret),
  };
}

// Strict parser: exact prefix, exact segment count, exact base64url lengths and
// alphabet. Anything off returns null (the caller fails auth generically).
export function parseCredential(input: unknown): ParsedCredential | null {
  if (typeof input !== "string" || input.length > 128) {
    return null;
  }
  const parts = input.split(".");
  if (parts.length !== 3) {
    return null;
  }
  const [prefix, deviceId, secret] = parts;
  if (prefix !== PREFIX || deviceId === undefined || secret === undefined) {
    return null;
  }
  if (!isB64Url(deviceId, DEVICE_ID_LEN) || !isB64Url(secret, SECRET_LEN)) {
    return null;
  }
  return { deviceId, secret };
}

export function digestSecret(secret: string): string {
  return createHash("sha256").update(secret, "utf8").digest("base64url");
}

// Constant-time compare of two base64url digests of equal expected length.
export function digestsEqual(a: string, b: string): boolean {
  const ab = Buffer.from(a, "utf8");
  const bb = Buffer.from(b, "utf8");
  if (ab.length !== bb.length) {
    return false;
  }
  return timingSafeEqual(ab, bb);
}

function isB64Url(value: string, len: number): boolean {
  return value.length === len && /^[A-Za-z0-9_-]+$/.test(value);
}
