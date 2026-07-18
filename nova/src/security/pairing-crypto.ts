import { createCipheriv, createDecipheriv, hkdfSync, randomBytes } from "node:crypto";

// Application-layer AEAD over the OPAQUE session key, protecting the pairing
// FINISH exchange. The OPAQUE bootstrap runs over plain HTTP on port 8791, so
// the device credential in the response — and the client metadata in the
// request — must be encrypted with keys only the two handshake parties hold.
// HKDF-SHA-512 derives SEPARATE directional keys so a client->server frame can
// never be replayed as a server->client frame, and every frame's AAD binds the
// protocol name, version, handshake id, direction, and the fixed identities.
//
// Both sides (JS relay, Go CLI) must derive identically; this is a second
// cross-language interop surface after OPAQUE, but over standard primitives
// (HKDF-SHA-512, AES-256-GCM) that Node and Go both implement natively.

const PROTOCOL = "ha-nova-pair-v1";
const KEY_BYTES = 32; // AES-256
const NONCE_BYTES = 12; // GCM standard
const TAG_BYTES = 16;

export type Direction = "c2s" | "s2c";

export interface DirectionKeys {
  c2s: Buffer;
  s2c: Buffer;
}

// Derive both directional keys from the OPAQUE session key and handshake id.
// The session key is the HKDF input keying material; the handshake id is the
// salt so keys are unique per handshake even if a session key ever repeated.
export function deriveDirectionKeys(sessionKey: Buffer, handshakeId: Buffer): DirectionKeys {
  return {
    c2s: hkdf(sessionKey, handshakeId, `${PROTOCOL}:c2s`),
    s2c: hkdf(sessionKey, handshakeId, `${PROTOCOL}:s2c`),
  };
}

// AAD binds each frame to the protocol/version/handshake/direction/identities so
// a frame cannot be replayed cross-direction, cross-handshake, or cross-protocol.
function aad(handshakeId: Buffer, direction: Direction): Buffer {
  return Buffer.from(`${PROTOCOL}|${direction}|${handshakeId.toString("base64url")}`, "utf8");
}

// Encrypt plaintext for the given direction. Output = nonce || ciphertext || tag.
export function seal(key: Buffer, handshakeId: Buffer, direction: Direction, plaintext: Buffer): Buffer {
  const nonce = randomBytes(NONCE_BYTES);
  const cipher = createCipheriv("aes-256-gcm", key, nonce);
  cipher.setAAD(aad(handshakeId, direction));
  const ciphertext = Buffer.concat([cipher.update(plaintext), cipher.final()]);
  const tag = cipher.getAuthTag();
  return Buffer.concat([nonce, ciphertext, tag]);
}

// Decrypt a nonce||ciphertext||tag frame, returning null on any tampering or
// malformed input (the caller treats null as a generic failure).
export function open(key: Buffer, handshakeId: Buffer, direction: Direction, blob: Buffer): Buffer | null {
  if (blob.length < NONCE_BYTES + TAG_BYTES) {
    return null;
  }
  const nonce = blob.subarray(0, NONCE_BYTES);
  const tag = blob.subarray(blob.length - TAG_BYTES);
  const ciphertext = blob.subarray(NONCE_BYTES, blob.length - TAG_BYTES);
  try {
    const decipher = createDecipheriv("aes-256-gcm", key, nonce);
    decipher.setAAD(aad(handshakeId, direction));
    decipher.setAuthTag(tag);
    return Buffer.concat([decipher.update(ciphertext), decipher.final()]);
  } catch {
    return null;
  }
}

function hkdf(ikm: Buffer, salt: Buffer, info: string): Buffer {
  // hkdfSync returns an ArrayBuffer; wrap without copying.
  const derived = hkdfSync("sha512", ikm, salt, Buffer.from(info, "utf8"), KEY_BYTES);
  return Buffer.from(derived);
}
