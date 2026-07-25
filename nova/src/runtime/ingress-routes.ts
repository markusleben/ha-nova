import { readFileSync } from "node:fs";

import {
  createCloudDeviceActivateHandler,
  createCloudDeviceBindHandler,
  createCloudDeviceRevokeSelfHandler,
  createCloudInfoHandler,
} from "../http/handlers/cloud.js";
import {
  createNovaActionHandler,
  createNovaPageHandler,
  type NovaPageDeps,
} from "../http/handlers/nova-page.js";
import {
  createPairV2FinishHandler,
  createPairV2InfoHandler,
  createPairV2StartHandler,
} from "../http/handlers/pair-v2.js";
import { createRouter, type RouteHandler } from "../http/router.js";
import { createCsrfStore } from "../security/csrf.js";
import type { HaAuthUser } from "../security/owner-check.js";
import type { IngressListenerDeps } from "./ingress-listener.js";
import type { FunctionalHandlers } from "./listeners.js";

type IngressAuthClass = "identity" | "functional";
type IngressBodyClass = "none" | "form" | "json";

export interface IngressRoutePolicy {
  auth: IngressAuthClass;
  body: IngressBodyClass;
  noStore?: true;
}

type IngressRoutePolicies = Map<string, IngressRoutePolicy>;

export function createIngressRoutes(deps: IngressListenerDeps): {
  router: ReturnType<typeof createRouter>;
  policies: IngressRoutePolicies;
} {
  const router = createRouter();
  const policies: IngressRoutePolicies = new Map();
  registerOwnerRoutes(router, policies, deps);
  registerCloudCleanupRoutes(router, policies, deps);
  if (deps.cloudRemoteEnabled) {
    registerCloudSetupRoutes(router, policies, deps);
    registerFunctionalRoutes(router, policies, deps.functional);
  }
  return { router, policies };
}

function registerCloudCleanupRoutes(
  router: ReturnType<typeof createRouter>,
  policies: IngressRoutePolicies,
  deps: IngressListenerDeps,
): void {
  const cloudInfo = {
    relayInstanceId: deps.relayInstanceId,
    relayVersion: deps.relayVersion,
    cloudRemoteEnabled: deps.cloudRemoteEnabled,
  };
  const cloudDevice = {
    registry: deps.registry,
    relayInstanceId: deps.relayInstanceId,
    now: deps.now,
  };
  registerIngressRoute(
    router,
    policies,
    "GET",
    "/cloud/v1/info",
    machine("none"),
    createCloudInfoHandler(cloudInfo),
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/cloud/v1/device/revoke-self",
    machine("json"),
    createCloudDeviceRevokeSelfHandler(cloudDevice),
  );
}

function registerCloudSetupRoutes(
  router: ReturnType<typeof createRouter>,
  policies: IngressRoutePolicies,
  deps: IngressListenerDeps,
): void {
  const cloudDevice = {
    registry: deps.registry,
    relayInstanceId: deps.relayInstanceId,
    now: deps.now,
  };
  const pairV2 = {
    manager: deps.pairing,
    relayInstanceId: deps.relayInstanceId,
    relayVersion: deps.relayVersion,
  };
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/cloud/v1/device/bind",
    machine("json"),
    createCloudDeviceBindHandler(cloudDevice),
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/cloud/v1/device/activate",
    machine("json"),
    createCloudDeviceActivateHandler(cloudDevice),
  );
  registerIngressRoute(
    router,
    policies,
    "GET",
    "/pair/v2/info",
    machine("none"),
    createPairV2InfoHandler(pairV2),
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/pair/v2/start",
    machine("json"),
    createPairV2StartHandler(pairV2),
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/pair/v2/finish",
    machine("json"),
    createPairV2FinishHandler(pairV2),
  );
}

function registerFunctionalRoutes(
  router: ReturnType<typeof createRouter>,
  policies: IngressRoutePolicies,
  handlers: FunctionalHandlers,
): void {
  registerIngressRoute(
    router,
    policies,
    "GET",
    "/health",
    functional("none"),
    handlers.health,
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/ws",
    functional("json"),
    handlers.ws,
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/core",
    functional("json"),
    handlers.core,
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/files",
    functional("json"),
    handlers.files,
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/backups",
    functional("json"),
    handlers.backups,
  );
}

function registerOwnerRoutes(
  router: ReturnType<typeof createRouter>,
  policies: IngressRoutePolicies,
  deps: IngressListenerDeps,
): void {
  const csrf = createCsrfStore();
  const novaDeps: NovaPageDeps = {
    fetchAuthUsers: async () => {
      const result = await deps.wsClient.sendMessage({
        type: "config/auth/list",
      });
      return Array.isArray(result) ? (result as HaAuthUser[]) : [];
    },
    csrf,
    pairing: deps.pairing,
    registry: deps.registry,
    registryCorrupt: deps.registryCorrupt,
    resetRegistry: deps.resetRegistry,
    connection: () => ({ haConnected: deps.wsClient.isConnected() }),
    update: async () => {
      const info = await deps.supervisor.getSelfInfo();
      return {
        version: info.version,
        versionLatest: info.versionLatest,
        updateAvailable: info.updateAvailable,
        error: false,
      };
    },
    relayVersion: deps.relayVersion,
    now: deps.now,
  };
  const page = createNovaPageHandler(novaDeps);
  const action = createNovaActionHandler(novaDeps);
  // These handlers perform the fresh Home Assistant owner lookup after the
  // shared listener establishes genuine Ingress identity.
  registerIngressRoute(
    router,
    policies,
    "GET",
    "/",
    { auth: "identity", body: "none" },
    page,
  );
  registerIngressRoute(
    router,
    policies,
    "GET",
    "/home",
    { auth: "identity", body: "none" },
    page,
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/action",
    { auth: "identity", body: "form" },
    action,
  );
  registerIngressRoute(
    router,
    policies,
    "POST",
    "/home/action",
    { auth: "identity", body: "form" },
    action,
  );

  const iconBytes = loadIcon(deps.iconPath);
  if (iconBytes === null) {
    return;
  }
  const icon: RouteHandler = ({ response }) => {
    response.setHeader("content-type", "image/png");
    response.setHeader("cache-control", "no-store");
    response.end(iconBytes);
  };
  registerIngressRoute(
    router,
    policies,
    "GET",
    "/icon",
    { auth: "identity", body: "none" },
    icon,
  );
  registerIngressRoute(
    router,
    policies,
    "GET",
    "/home/icon",
    { auth: "identity", body: "none" },
    icon,
  );
}

function registerIngressRoute(
  router: ReturnType<typeof createRouter>,
  policies: IngressRoutePolicies,
  method: string,
  path: string,
  policy: IngressRoutePolicy,
  handler: RouteHandler,
): void {
  const routeKey = `${method.toUpperCase()} ${path}`;
  if (policies.has(routeKey)) {
    throw new Error(`duplicate ingress route policy: ${routeKey}`);
  }
  policies.set(routeKey, policy);
  router.register(method, path, handler);
}

function machine(body: IngressBodyClass): IngressRoutePolicy {
  return { auth: "identity", body, noStore: true };
}

function functional(body: IngressBodyClass): IngressRoutePolicy {
  return { auth: "functional", body, noStore: true };
}

function loadIcon(iconPath: string | undefined): Buffer | null {
  if (!iconPath) {
    return null;
  }
  try {
    return readFileSync(iconPath);
  } catch {
    return null;
  }
}
