import type { IncomingMessage } from "node:http";

import { digestSecret, parseCredential } from "./device-credential.js";
import type { DeviceRegistry } from "./device-registry.js";
import { BRIDGE_INSTALL_ID, GATE_INSTALL_ID } from "./pairing-metadata.js";

// Resolves an inbound bearer credential to a principal. The NOVA App accepts
// one credential kind: the per-device token minted by pairing, and ONLY over
// the pinned TLS listener; presenting it on plain HTTP returns a distinct
// "secure transport required" so the client knows to use the secure port. Any
// other bearer is the old shared access, which the App no longer accepts: it
// gets PAIRING_REQUIRED, so the client can say how to pair again. The
// standalone container's own token is checked in http/server.ts, never here.

export type Transport = "secure" | "plain";

// peer: the request's socket address. internal: a credential the App minted
// for its own chat bridge or safety-check worker (reserved install ids),
// accepted only from a loopback peer.
// shared: the standalone container's own token (http/server.ts); the App never
// produces it.
export type Principal =
  | { kind: "device"; deviceId: string; peer?: string; internal?: "chat-bridge" | "gate" }
  | { kind: "shared"; peer?: string };

export type PrincipalResult =
  | { ok: true; principal: Principal }
  | { ok: false; status: 401; code: "UNAUTHORIZED" | "PAIRING_REQUIRED"; message: string }
  | {
      ok: false;
      status: 403;
      code: "SECURE_TRANSPORT_REQUIRED";
      message: string;
    };

export interface PrincipalResolverDeps {
  registry: DeviceRegistry;
  now: () => number;
}

export const PAIRING_REQUIRED_MESSAGE =
  "The NOVA App no longer accepts the old shared access. Pair this computer again: run ha-nova setup.";

export const CLOUD_DEVICE_UNAUTHORIZED_MESSAGE =
  "Unknown or inactive device credential";

export function resolvePrincipal(
  authorizationHeader: string | undefined,
  transport: Transport,
  deps: PrincipalResolverDeps,
  peer?: string,
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
        message:
          "Device credentials are only accepted over the secure (TLS) port",
      };
    }
    const principal = deps.registry.resolveDeviceSecret(
      device.deviceId,
      digestSecret(device.secret),
      deps.now(),
    );
    if (principal === null) {
      return unauthorized("Unknown or revoked device credential");
    }
    return devicePrincipal(deps.registry, device.deviceId, peer);
  }

  return { ok: false, status: 401, code: "PAIRING_REQUIRED", message: PAIRING_REQUIRED_MESSAGE };
}

// Cloud ingress accepts device credentials only. The exact Supervisor
// user identity is an additional required factor and must match the binding on
// the active device record.
export function resolveCloudPrincipal(
  request: IncomingMessage,
  userId: string,
  relayInstanceId: string,
  deps: PrincipalResolverDeps,
): PrincipalResult {
  const token = bearerToken(singleAuthorizationHeader(request));
  const device = token === null ? null : parseCredential(token);
  if (device === null) {
    return unauthorized(CLOUD_DEVICE_UNAUTHORIZED_MESSAGE);
  }
  const principal = deps.registry.resolveCloudDeviceSecret(
    device.deviceId,
    digestSecret(device.secret),
    userId,
    relayInstanceId,
    deps.now(),
  );
  if (principal === null) {
    return unauthorized(CLOUD_DEVICE_UNAUTHORIZED_MESSAGE);
  }
  return devicePrincipal(deps.registry, device.deviceId, request.socket?.remoteAddress);
}

export function isLoopback(peer: string | undefined): peer is string {
  return peer === "127.0.0.1" || peer === "::1";
}

// The App's own credentials work only from inside the App: from any other
// peer the bridge's would skip the safety check, the worker's its limits.
function devicePrincipal(registry: DeviceRegistry, deviceId: string, peer: string | undefined): PrincipalResult {
  const installId = registry.list().find((d) => d.deviceId === deviceId)?.clientInstallId;
  if (installId === undefined) {
    return unauthorized("Unknown or revoked device credential");
  }
  const base = { kind: "device" as const, deviceId, ...(peer !== undefined ? { peer } : {}) };
  const internal = installId === BRIDGE_INSTALL_ID ? "chat-bridge" : installId === GATE_INSTALL_ID ? "gate" : null;
  if (internal === null) {
    return { ok: true, principal: base };
  }
  if (!isLoopback(peer)) {
    return unauthorized("This credential is accepted only from inside the NOVA App");
  }
  return { ok: true, principal: { ...base, internal } };
}

export function bearerToken(header: string | undefined): string | null {
  if (!header) {
    return null;
  }
  const match = /^Bearer ([^ ]+)$/.exec(header);
  if (!match?.[1]) {
    return null;
  }
  return match[1];
}

export function singleAuthorizationHeader(
  request: IncomingMessage,
): string | undefined {
  const rawHeaders = request.rawHeaders;
  let count = 0;
  if (Array.isArray(rawHeaders)) {
    for (let index = 0; index < rawHeaders.length; index += 2) {
      if (rawHeaders[index]?.toLowerCase() === "authorization") {
        count += 1;
      }
    }
  }
  if (count !== 1) {
    return undefined;
  }
  const value = request.headers.authorization;
  return typeof value === "string" && !value.includes(",") ? value : undefined;
}

function unauthorized(message: string): PrincipalResult {
  return { ok: false, status: 401, code: "UNAUTHORIZED", message };
}
