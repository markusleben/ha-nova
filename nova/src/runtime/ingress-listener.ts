import type { RequestListener } from "node:http";

import type { HaWsClient } from "../ha/ws-client.js";
import type { SupervisorClient } from "../ha/supervisor-client.js";
import {
  createRequestListener,
  DEFAULT_MAX_FORM_BODY_BYTES,
  DEFAULT_MAX_JSON_BODY_BYTES,
  type AuthOutcome,
  type RelayLogger,
} from "../http/server.js";
import type { DeviceRegistry } from "../security/device-registry.js";
import { resolveIngressIdentity } from "../security/ingress-identity.js";
import type { PairingV1Manager } from "../security/pairing-v1.js";
import { resolveCloudPrincipal } from "../security/principal.js";
import type { FunctionalHandlers } from "./listeners.js";
import {
  createIngressRoutes,
  type IngressRoutePolicy,
} from "./ingress-routes.js";

export interface IngressListenerDeps {
  registry: DeviceRegistry;
  pairing: PairingV1Manager;
  functional: FunctionalHandlers;
  relayInstanceId: string;
  relayVersion: string;
  cloudRemoteEnabled: boolean;
  wsClient: HaWsClient & {
    sendMessage(message: { type: string }): Promise<unknown>;
    isConnected(): boolean;
  };
  supervisor: SupervisorClient;
  registryCorrupt: () => boolean;
  resetRegistry: () => void;
  now: () => number;
  logger: RelayLogger;
  iconPath?: string;
}

export function createIngressListener(
  deps: IngressListenerDeps,
): RequestListener {
  const { router, policies } = createIngressRoutes(deps);

  return createRequestListener({
    router,
    version: deps.relayVersion,
    logger: deps.logger,
    noStorePaths: new Set(
      [...policies.entries()]
        .filter(([, policy]) => policy.noStore === true)
        .map(([routeKey]) => routeKey.slice(routeKey.indexOf(" ") + 1)),
    ),
    authorize: (request, routeKey) =>
      authorizeIngressRoute(request, routeKey, policies.get(routeKey), deps),
    bodyPolicy: (routeKey) => {
      const body = policies.get(routeKey)?.body;
      if (body === "form") {
        return { type: body, maxBytes: DEFAULT_MAX_FORM_BODY_BYTES };
      }
      return body === "json"
        ? { type: body, maxBytes: DEFAULT_MAX_JSON_BODY_BYTES }
        : { type: "none", maxBytes: 0 };
    },
  });
}

function authorizeIngressRoute(
  request: Parameters<RequestListener>[0],
  routeKey: string,
  policy: IngressRoutePolicy | undefined,
  deps: IngressListenerDeps,
): AuthOutcome {
  if (policy === undefined) {
    return {
      ok: false,
      status: 404,
      code: "NOT_FOUND",
      message: `Route not found: ${routeKey}`,
    };
  }
  const identity = resolveIngressIdentity(request);
  if (!identity.ok) {
    return {
      ok: false,
      status: 403,
      code: "INGRESS_REQUIRED",
      message: "Authenticated Supervisor ingress is required",
    };
  }
  if (policy.auth !== "functional") {
    // Owner handlers perform the fresh Home Assistant owner lookup themselves.
    return { ok: true };
  }
  const principal = resolveCloudPrincipal(
    request,
    identity.userId,
    deps.relayInstanceId,
    {
      registry: deps.registry,
      now: deps.now,
    },
  );
  return principal.ok
    ? { ok: true, principal: principal.principal }
    : {
        ok: false,
        status: principal.status,
        code: principal.code,
        message: principal.message,
      };
}
