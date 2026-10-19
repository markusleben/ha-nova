import { digestSecret, parseCredential } from "../../security/device-credential.js";
import type { DeviceRegistry } from "../../security/device-registry.js";
import { bearerToken } from "../../security/principal.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";

// Device credential lifecycle over the pinned TLS port only. Both endpoints
// authenticate themselves from the presented credential rather than the shared
// principal resolver: activate must accept a PENDING credential (which the
// resolver rejects), and revoke-self must stay idempotent (a second call after
// the credential is gone returns 401, which the CLI treats as success).

export interface DeviceAuthDeps {
  registry: DeviceRegistry;
  now: () => number;
}

// POST /auth/device/activate — promote the presented provisional credential to
// active. Idempotent: activating an already-active credential succeeds.
export function createDeviceActivateHandler(deps: DeviceAuthDeps): RouteHandler {
  return ({ request }: RouteContext) => {
    const cred = presentedCredential(request);
    const record = deps.registry.activatePending(cred.deviceId, digestSecret(cred.secret), deps.now());
    if (record === null) {
      throw new HttpError(401, "UNAUTHORIZED", "Unknown or expired provisional credential");
    }
    return { device_id: record.deviceId, activated: true };
  };
}

// POST /auth/device/revoke-self — the active device revokes its own credential.
export function createDeviceRevokeSelfHandler(deps: DeviceAuthDeps): RouteHandler {
  return ({ request }: RouteContext) => {
    const cred = presentedCredential(request);
    const principal = deps.registry.resolveDeviceSecret(cred.deviceId, digestSecret(cred.secret), deps.now());
    if (principal === null) {
      // Already revoked (or never valid): the CLI treats this 401 as success.
      throw new HttpError(401, "UNAUTHORIZED", "Unknown or revoked device credential");
    }
    deps.registry.revoke(cred.deviceId);
    return { device_id: cred.deviceId, revoked: true };
  };
}

// GET /auth/device/peers — whether uninstall may remove the App: other device
// records (pending included), an open pairing window, legacy shared access, and
// chat-app files (null: unknown). Paired devices learn nothing new.
// The chat-app bridge's own device (bridgeDevice) is covered by chat_app_files.
export function createDevicePeersHandler(deps: { registry: DeviceRegistry; pairingOpen: () => boolean; chatAppFiles: () => boolean | null; bridgeDevice?: () => string | null }): RouteHandler {
  return ({ principal }: RouteContext) => {
    if (principal?.kind !== "device") {
      throw new HttpError(403, "DEVICE_REQUIRED", "Only a paired device can read this");
    }
    const bridge = deps.bridgeDevice?.() ?? null;
    const others = deps.registry.list().filter((d) => d.deviceId !== principal.deviceId && d.deviceId !== bridge);
    return { other_devices: others.length, pairing_open: deps.pairingOpen(), legacy_access: deps.registry.hasLegacy(), chat_app_files: deps.chatAppFiles() };
  };
}

function presentedCredential(request: RouteContext["request"]): { deviceId: string; secret: string } {
  const token = bearerToken(request.headers.authorization);
  const cred = token !== null ? parseCredential(token) : null;
  if (cred === null) {
    throw new HttpError(401, "UNAUTHORIZED", "Missing or malformed device credential");
  }
  return cred;
}
