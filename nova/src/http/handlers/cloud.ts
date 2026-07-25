import {
  digestSecret,
  parseCredential,
} from "../../security/device-credential.js";
import type { DeviceRegistry } from "../../security/device-registry.js";
import { resolveIngressIdentity } from "../../security/ingress-identity.js";
import {
  CLOUD_DEVICE_UNAUTHORIZED_MESSAGE,
  bearerToken,
  singleAuthorizationHeader,
} from "../../security/principal.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";

export interface CloudInfoDeps {
  relayInstanceId: string;
  relayVersion: string;
  cloudRemoteEnabled: boolean;
}

export function createCloudInfoHandler(deps: CloudInfoDeps): RouteHandler {
  return ({ request }) => {
    requireIngressUser(request);
    return {
      protocol_version: "v1",
      relay_instance_id: deps.relayInstanceId,
      relay_version: deps.relayVersion,
      capabilities: {
        device_user_binding: deps.cloudRemoteEnabled,
        pairing_v2: deps.cloudRemoteEnabled,
        functional_routes: deps.cloudRemoteEnabled
          ? ["health", "ws", "core", "files", "backups"]
          : [],
        cleanup_routes: ["device_revoke_self"],
      },
    };
  };
}

export interface CloudDeviceDeps {
  registry: DeviceRegistry;
  relayInstanceId: string;
  now: () => number;
}

// Existing local devices add Cloud access without re-pairing. Repeating the
// bind for the same HA user is safe; silently moving it to another user is not.
export function createCloudDeviceBindHandler(
  deps: CloudDeviceDeps,
): RouteHandler {
  return ({ body, request }) => {
    const userId = requireIngressUser(request);
    requireExpectedRelayInstance(body, deps.relayInstanceId);
    const credential = presentedDeviceCredential(request);
    const result = deps.registry.bindCloudUser(
      credential.deviceId,
      digestSecret(credential.secret),
      userId,
      deps.relayInstanceId,
    );
    if (!result.ok) {
      // Binding conflicts stay indistinguishable from unknown credentials.
      throw new HttpError(
        401,
        "UNAUTHORIZED",
        CLOUD_DEVICE_UNAUTHORIZED_MESSAGE,
      );
    }
    return {
      device_id: result.record.deviceId,
      bound: true,
      changed: result.changed,
    };
  };
}

// Cloud pairing activation promotes and binds in one registry write. There is
// no active-but-unbound crash window and retries by the same user are idempotent.
export function createCloudDeviceActivateHandler(
  deps: CloudDeviceDeps,
): RouteHandler {
  return ({ body, request }) => {
    const userId = requireIngressUser(request);
    requireExpectedRelayInstance(body, deps.relayInstanceId);
    const credential = presentedDeviceCredential(request);
    const result = deps.registry.activatePendingForCloud(
      credential.deviceId,
      digestSecret(credential.secret),
      userId,
      deps.relayInstanceId,
      deps.now(),
    );
    if (!result.ok) {
      // Pairing provenance mismatches stay indistinguishable from unknown credentials.
      throw new HttpError(
        401,
        "UNAUTHORIZED",
        CLOUD_DEVICE_UNAUTHORIZED_MESSAGE,
      );
    }
    return {
      device_id: result.record.deviceId,
      activated: true,
      bound: true,
      changed: result.changed,
    };
  };
}

// The device proves the exact credential, HA user, and Relay instance before
// removing itself. A persisted registry tombstone makes a lost-response retry
// safe without allowing another identity to discover that the device existed.
export function createCloudDeviceRevokeSelfHandler(
  deps: CloudDeviceDeps,
): RouteHandler {
  return ({ body, request }) => {
    const userId = requireIngressUser(request);
    requireExpectedRelayInstance(body, deps.relayInstanceId);
    const credential = presentedDeviceCredential(request);
    const result = deps.registry.revokeCloudDevice(
      credential.deviceId,
      digestSecret(credential.secret),
      userId,
      deps.relayInstanceId,
      deps.now(),
    );
    if (!result.ok) {
      throw new HttpError(
        401,
        "UNAUTHORIZED",
        CLOUD_DEVICE_UNAUTHORIZED_MESSAGE,
      );
    }
    return {
      device_id: result.deviceId,
      revoked: true,
      changed: result.changed,
    };
  };
}

function requireExpectedRelayInstance(body: unknown, actual: string): void {
  if (typeof body !== "object" || body === null || Array.isArray(body)) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "Request body must be a JSON object",
    );
  }
  const expected = (body as Record<string, unknown>).relay_instance_id;
  if (typeof expected !== "string" || expected.length === 0) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "Missing field: relay_instance_id",
    );
  }
  if (expected !== actual) {
    // A stale Relay identity is deliberately indistinguishable from an
    // unknown, revoked, or differently bound device.
    throw new HttpError(401, "UNAUTHORIZED", CLOUD_DEVICE_UNAUTHORIZED_MESSAGE);
  }
}

export function requireIngressUser(request: RouteContext["request"]): string {
  const identity = resolveIngressIdentity(request);
  if (!identity.ok) {
    throw new HttpError(
      403,
      "INGRESS_REQUIRED",
      "Authenticated Supervisor ingress is required",
    );
  }
  return identity.userId;
}

function presentedDeviceCredential(request: RouteContext["request"]): {
  deviceId: string;
  secret: string;
} {
  const header = singleAuthorizationHeader(request);
  const token = bearerToken(header);
  const credential = token === null ? null : parseCredential(token);
  if (credential === null) {
    throw new HttpError(401, "UNAUTHORIZED", CLOUD_DEVICE_UNAUTHORIZED_MESSAGE);
  }
  return credential;
}
