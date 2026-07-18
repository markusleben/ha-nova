import { digestSecret, parseCredential } from "./device-credential.js";
import type { DeviceRegistry } from "./device-registry.js";

// Resolves an inbound bearer credential to a principal, replacing the single
// shared-token model. Two credential kinds exist:
//   - device: the per-device token minted by pairing. Accepted ONLY over the
//     pinned TLS listener; presenting it on plain HTTP returns a distinct
//     "secure transport required" so the client knows to use the secure port.
//   - legacy: the one global "legacy shared access" record migrated from the
//     old relay token. Accepted on either transport WHILE it exists, so
//     pre-migration clients keep working until the owner revokes it.
// Anything else is unauthorized.

export type Transport = "secure" | "plain";

export type Principal =
  | { kind: "device"; deviceId: string }
  | { kind: "legacy" };

export type PrincipalResult =
  | { ok: true; principal: Principal }
  | { ok: false; status: 401; code: "UNAUTHORIZED"; message: string }
  | { ok: false; status: 403; code: "SECURE_TRANSPORT_REQUIRED"; message: string };

export interface PrincipalResolverDeps {
  registry: DeviceRegistry;
  now: () => number;
}

export function resolvePrincipal(
  authorizationHeader: string | undefined,
  transport: Transport,
  deps: PrincipalResolverDeps
): PrincipalResult {
  const token = bearerToken(authorizationHeader);
  if (token === null) {
    return unauthorized("Missing or malformed bearer token");
  }

  const device = parseCredential(token);
  if (device !== null) {
    // A well-formed device credential over plain HTTP is never accepted — the
    // secret would travel unencrypted. Point the client at the secure port.
    if (transport !== "secure") {
      return {
        ok: false,
        status: 403,
        code: "SECURE_TRANSPORT_REQUIRED",
        message: "Device credentials are only accepted over the secure (TLS) port",
      };
    }
    const principal = deps.registry.resolveDeviceSecret(device.deviceId, digestSecret(device.secret), deps.now());
    if (principal === null) {
      return unauthorized("Unknown or revoked device credential");
    }
    return { ok: true, principal: { kind: "device", deviceId: device.deviceId } };
  }

  // Not a device credential: the only other accepted shape is the legacy shared
  // token (an opaque string). It works on either transport while it exists.
  const legacy = deps.registry.resolveLegacySecret(digestSecret(token));
  if (legacy !== null) {
    return { ok: true, principal: { kind: "legacy" } };
  }

  return unauthorized("Invalid credential");
}

export function bearerToken(header: string | undefined): string | null {
  if (!header) {
    return null;
  }
  const [scheme, token] = header.split(" ", 2);
  if (scheme !== "Bearer" || !token) {
    return null;
  }
  return token;
}

function unauthorized(message: string): PrincipalResult {
  return { ok: false, status: 401, code: "UNAUTHORIZED", message };
}
