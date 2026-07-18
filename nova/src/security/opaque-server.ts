import * as opaque from "@serenity-kit/opaque";

// Server half of the OPAQUE (RFC 9807) device-pairing handshake, wrapping
// @serenity-kit/opaque. The relay knows the one-time 6-digit code and runs the
// full registration locally, so only the server setup + registration record for
// the CURRENT code exist in memory. The Go CLI is the OPAQUE client.
//
// The suite and KSF are pinned to the exact values that interoperate with the
// Go client (github.com/bytemare/opaque): ristretto255/SHA-512, OPAQUE context
// None, Argon2id(t=3, m=65536 KiB, p=4), KSF salt = 16 zero bytes, output = 64.
// These were validated by a cross-language known-answer handshake; do not change
// them without re-proving Go interop.

export const OPAQUE_KSF = { iterations: 3, memory: 65536, parallelism: 4 } as const;

// Fixed, non-secret protocol identities bound into the AKE transcript. The Go
// client passes the same byte strings as clientIdentity/serverIdentity.
export const OPAQUE_CLIENT_ID = "ha-nova-device";
export const OPAQUE_SERVER_ID = "ha-nova-relay";

const IDENTIFIERS = { client: OPAQUE_CLIENT_ID, server: OPAQUE_SERVER_ID } as const;
const KEY_STRETCHING = { "argon2id-custom": OPAQUE_KSF } as const;

export interface OpaqueRegistration {
  // Opaque server secret for THIS code only (base64url as the library emits it).
  serverSetup: string;
  registrationRecord: string;
}

export interface OpaqueLoginStart {
  serverLoginState: string;
  loginResponse: string;
}

// Must be awaited once before any other call (WASM init).
export async function opaqueReady(): Promise<void> {
  await opaque.ready;
}

// Locally register the given code, producing the server setup + record the login
// half will verify against. userIdentifier scopes the record to one handshake
// generation; the caller passes a fresh random value per code.
export function registerCode(code: string, userIdentifier: string): OpaqueRegistration {
  const serverSetup = opaque.server.createSetup();
  const { clientRegistrationState, registrationRequest } = opaque.client.startRegistration({
    password: code,
  });
  const { registrationResponse } = opaque.server.createRegistrationResponse({
    serverSetup,
    userIdentifier,
    registrationRequest,
  });
  const { registrationRecord } = opaque.client.finishRegistration({
    password: code,
    registrationResponse,
    clientRegistrationState,
    identifiers: IDENTIFIERS,
    keyStretching: KEY_STRETCHING,
  });
  return { serverSetup, registrationRecord };
}

// Server side of login step 1: consume the client's KE1 and produce KE2, or
// null when the KE1 is malformed. startLoginRequest is attacker-controlled
// network input; @serenity-kit throws on bad base64/length/protocol bytes, so
// this module (the crypto boundary) returns null symmetrically with finishLogin
// and the endpoint treats null as a generic auth failure + a rate-limit hit.
export function startLogin(
  registration: OpaqueRegistration,
  userIdentifier: string,
  startLoginRequest: string
): OpaqueLoginStart | null {
  try {
    const { serverLoginState, loginResponse } = opaque.server.startLogin({
      serverSetup: registration.serverSetup,
      registrationRecord: registration.registrationRecord,
      startLoginRequest,
      userIdentifier,
      identifiers: IDENTIFIERS,
    });
    return { serverLoginState, loginResponse };
  } catch {
    return null;
  }
}

// Server side of login step 2: consume KE3 and return the shared session key,
// or null when authentication fails (wrong code / tampered messages).
export function finishLogin(serverLoginState: string, finishLoginRequest: string): string | null {
  try {
    const { sessionKey } = opaque.server.finishLogin({
      serverLoginState,
      finishLoginRequest,
      identifiers: IDENTIFIERS,
    });
    return sessionKey;
  } catch {
    return null;
  }
}
