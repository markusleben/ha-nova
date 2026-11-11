import type { IncomingMessage, RequestListener } from "node:http";

import { createRequestListener, DEFAULT_MAX_JSON_BODY_BYTES, type AuthOutcome, type RelayLogger } from "../http/server.js";
import { createRouter, type RouteHandler, type Router } from "../http/router.js";
import { createDeviceActivateHandler, createDevicePeersHandler, createDeviceRevokeSelfHandler } from "../http/handlers/device-auth.js";
import { createPairV1FinishHandler, createPairV1InfoHandler, createPairV1StartHandler } from "../http/handlers/pair-v1.js";
import type { DeviceRegistry } from "../security/device-registry.js";
import type { PairingV1Manager } from "../security/pairing-v1.js";
import { resolvePrincipal, type Transport } from "../security/principal.js";

// App-mode listener assembly. The relay runs three listeners over one shared
// request pipeline (createRequestListener), differing only in their route set,
// transport, and auth hook:
//   - bootstrap (8791, plain HTTP): OPAQUE pairing (bearer-exempt) only. Every
//     other request is refused: a device credential with
//     SECURE_TRANSPORT_REQUIRED, the old shared token with PAIRING_REQUIRED.
//   - device (8792, pinned TLS): functional routes + device credential lifecycle.
//   - ingress (8793): the NOVA owner page (wired separately; not here).
// Standalone Container/Core keeps the single-listener createApp path.

export interface FunctionalHandlers {
  health: RouteHandler;
  ws: RouteHandler;
  core: RouteHandler;
  files: RouteHandler;
  backups: RouteHandler;
  // Device listener only: not a Home Assistant Link functional route (docs/work/2026-10-26-supervisor-role-spec.md → Decisions).
  supervisor: RouteHandler;
  ask: RouteHandler; // POST /gate/v1/ask and /gate/v1/phone (gate-asks.ts)
}

export interface PairingListenerDeps {
  registry: DeviceRegistry;
  pairingManager: PairingV1Manager;
  functional: FunctionalHandlers;
  relayVersion: string;
  now: () => number;
  logger?: RelayLogger;
  // Chat-app integration files in Home Assistant's config; null: unknown.
  chatAppFiles?: () => boolean | null;
  bridgeDevice?: () => string | null; // the chat-app bridge's internal device
}

const PAIR_INFO = "GET /pair/v1/info";
const PAIR_START = "POST /pair/v1/start";
const PAIR_FINISH = "POST /pair/v1/finish";
const DEVICE_ACTIVATE = "POST /auth/device/activate";
const DEVICE_REVOKE = "POST /auth/device/revoke-self";

// Routes whose credential is not a bearer principal: pairing (the code is the
// credential) and device-auth (it authenticates the presented device secret,
// including a pending one the resolver would reject).
const PAIRING = new Set([PAIR_INFO, PAIR_START, PAIR_FINISH]);
const SELF_AUTHENTICATED = new Set([...PAIRING, DEVICE_ACTIVATE, DEVICE_REVOKE]);

function registerFunctional(router: Router, fns: FunctionalHandlers): void {
  router.register("GET", "/health", fns.health);
  router.register("POST", "/ws", fns.ws);
  router.register("POST", "/core", fns.core);
  router.register("POST", "/files", fns.files);
  router.register("POST", "/backups", fns.backups);
  router.register("POST", "/supervisor", fns.supervisor);
  router.register("POST", "/gate/v1/ask", fns.ask);
  router.register("POST", "/gate/v1/phone", fns.ask);
}

// The auth hook for functional routes on a given transport: resolve a device
// principal, mapping the resolver's outcome to the listener contract. On the
// plain transport it never succeeds (resolvePrincipal), so it only words the
// refusal.
function functionalAuthorize(deps: PairingListenerDeps, transport: Transport) {
  return (request: IncomingMessage): AuthOutcome => {
    const r = resolvePrincipal(request.headers.authorization, transport, { registry: deps.registry, now: deps.now }, request.socket.remoteAddress);
    if (r.ok) {
      return { ok: true, principal: r.principal };
    }
    // The CLI tells this refusal apart by its header, before reading a body.
    const headers = r.code === "PAIRING_REQUIRED" ? { headers: { "x-ha-nova-auth": "pairing-required" } } : {};
    return { ok: false, status: r.status, code: r.code, message: r.message, ...headers };
  };
}

// Plain HTTP bootstrap listener: pairing only. No functional route: the old
// shared access it once served is gone, so every other request is refused.
export function createBootstrapListener(deps: PairingListenerDeps): RequestListener {
  const router = createRouter();
  router.register("GET", "/pair/v1/info", createPairV1InfoHandler({ manager: deps.pairingManager, relayVersion: deps.relayVersion, signIn: true }));
  router.register("POST", "/pair/v1/start", createPairV1StartHandler({ manager: deps.pairingManager, relayVersion: deps.relayVersion }));
  router.register("POST", "/pair/v1/finish", createPairV1FinishHandler({ manager: deps.pairingManager, relayVersion: deps.relayVersion }));

  const functionalAuth = functionalAuthorize(deps, "plain");
  return createRequestListener({
    router,
    version: deps.relayVersion,
    ...(deps.logger ? { logger: deps.logger } : {}),
    noStorePaths: new Set(["/pair/v1/info", "/pair/v1/start", "/pair/v1/finish"]),
    authorize: (request, routeKey) => (PAIRING.has(routeKey) ? { ok: true } : functionalAuth(request)),
    bodyPolicy: () => ({ type: "json", maxBytes: DEFAULT_MAX_JSON_BODY_BYTES }),
  });
}

// Pinned-TLS device listener: functional routes + device credential lifecycle.
export function createDeviceListener(deps: PairingListenerDeps): RequestListener {
  const router = createRouter();
  registerFunctional(router, deps.functional);
  router.register("POST", "/auth/device/activate", createDeviceActivateHandler({ registry: deps.registry, now: deps.now }));
  router.register("POST", "/auth/device/revoke-self", createDeviceRevokeSelfHandler({ registry: deps.registry, now: deps.now }));
  router.register("GET", "/auth/device/peers", createDevicePeersHandler({ registry: deps.registry, pairingOpen: () => !["inactive", "consumed"].includes(deps.pairingManager.getStatus().phase), chatAppFiles: deps.chatAppFiles ?? (() => null), ...(deps.bridgeDevice ? { bridgeDevice: deps.bridgeDevice } : {}) }));

  const functionalAuth = functionalAuthorize(deps, "secure");
  return createRequestListener({
    router,
    version: deps.relayVersion,
    ...(deps.logger ? { logger: deps.logger } : {}),
    authorize: (request, routeKey) => (SELF_AUTHENTICATED.has(routeKey) ? { ok: true } : functionalAuth(request)),
    bodyPolicy: () => ({ type: "json", maxBytes: DEFAULT_MAX_JSON_BODY_BYTES }),
  });
}
