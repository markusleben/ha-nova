import { createServer as createHttpServer_, type RequestListener, type Server } from "node:http";
import { createServer as createHttpsServer } from "node:https";
import { readFileSync } from "node:fs";
import { dirname } from "node:path";

import type { FileAccessConfig } from "../config/file-access.js";
import { createBackupsHandler } from "../http/handlers/backups.js";
import { createCoreProxyHandler } from "../http/handlers/core-proxy.js";
import { createFilesHandler } from "../http/handlers/files.js";
import { createHealthHandler } from "../http/handlers/health.js";
import { createNovaActionHandler, createNovaPageHandler, type NovaPageDeps } from "../http/handlers/nova-page.js";
import { createWsProxyHandler } from "../http/handlers/ws-proxy.js";
import { createRouter, type RouteHandler } from "../http/router.js";
import { createRequestListener, DEFAULT_MAX_FORM_BODY_BYTES, type RelayLogger } from "../http/server.js";
import type { CoreProxyRequest, CoreProxyResponse } from "../types/api.js";
import type { HaWsClient } from "../ha/ws-client.js";
import { createSupervisorClient } from "../ha/supervisor-client.js";
import { createCsrfStore } from "../security/csrf.js";
import { archiveCorruptRegistry, openDeviceRegistry, RegistryCorruptError, type DeviceRegistry } from "../security/device-registry.js";
import type { HaAuthUser } from "../security/owner-check.js";
import { createPairingV1Manager, type PairingV1Manager } from "../security/pairing-v1.js";
import { loadOrCreateTlsIdentity } from "../security/tls-identity.js";
import { importLegacyToken } from "./legacy-migration.js";
import { createBootstrapListener, createDeviceListener, type FunctionalHandlers } from "./listeners.js";

// App-mode assembly: three listeners over one shared pipeline. Constructed only
// when a SUPERVISOR_TOKEN is present (a real HA App). Standalone Container/Core
// keeps the single-listener createApp path.

const SECURE_CONTAINER_PORT = "8792/tcp";
export const BOOTSTRAP_PORT = 8791;
export const SECURE_PORT = 8792;
export const INGRESS_PORT = 8793;

export interface AppModeInput {
  supervisorToken: string;
  relayVersion: string;
  wsClient: HaWsClient & {
    sendMessage(message: { type: string }): Promise<unknown>;
    isConnected(): boolean;
  };
  coreClient: { request(input: CoreProxyRequest): Promise<CoreProxyResponse> };
  fileAccess: FileAccessConfig;
  snapshotRoot: string;
  appOptionsPath: string;
  startedAtMs: number;
  now: () => number;
  logger: RelayLogger;
  iconPath?: string;
}

export interface AppModeRuntime {
  servers: { bootstrap: Server; device: Server; ingress: Server };
  registryCorrupt: boolean;
  listen(): Promise<void>;
}

export async function buildAppMode(input: AppModeInput): Promise<AppModeRuntime> {
  const dataDir = dirname(input.appOptionsPath); // /data
  // The Supervisor self-API base is fixed to http://supervisor in production;
  // the disposable-HA e2e overrides it to a mock so app mode runs without a
  // real Supervisor (upstream core/WS still go direct to env.haUrl).
  const supervisorBase = process.env.HA_NOVA_SUPERVISOR_BASE?.trim() || undefined;
  const supervisor = supervisorBase
    ? createSupervisorClient(input.supervisorToken, supervisorBase)
    : createSupervisorClient(input.supervisorToken);

  // Registry: a corrupt file is fail-closed for device auth, but must NOT crash
  // the relay — the owner still needs the NOVA page to reset it. We surface the
  // corrupt state and disable pairing/device auth until the owner triggers a
  // reset. `registry` is a stable proxy so the listeners and pairing manager
  // keep working across a reset that swaps the underlying store.
  let active: DeviceRegistry;
  let registryCorrupt = false;
  try {
    active = openDeviceRegistry(dataDir);
  } catch (error) {
    if (error instanceof RegistryCorruptError) {
      registryCorrupt = true;
      input.logger.error("Device registry is corrupt; device access disabled until reset", { error: error.message });
      active = openInertRegistry();
    } else {
      throw error;
    }
  }
  const registry = swappableRegistry(() => active);
  const resetRegistry = (): void => {
    if (!registryCorrupt) {
      return;
    }
    archiveCorruptRegistry(dataDir, input.now());
    active = openDeviceRegistry(dataDir); // a fresh, empty registry
    registryCorrupt = false;
    input.logger.info?.("Device registry reset by the owner; device pairing re-enabled");
  };

  const tls = await loadOrCreateTlsIdentity(dataDir);

  // One-time legacy migration: import a pre-existing shared token into the
  // registry as a digest, then clear the option. Skipped on a corrupt registry.
  if (!registryCorrupt) {
    await importLegacyToken({ registry, supervisor, dataDir, appOptionsPath: input.appOptionsPath, now: input.now, logger: input.logger });
  }

  const pairing = createPairingV1Manager({
    registry,
    now: input.now,
    secureEndpoint: () => {
      const port = secureHostPort;
      return port === null ? null : { spkiPin: tls.spkiPin, securePort: port };
    },
  });

  // The effective secure host port is read once at startup from self-info; a
  // null (unmapped) value means pairing codes cannot be activated.
  let secureHostPort: number | null = null;
  try {
    secureHostPort = await supervisor.getMappedHostPort(SECURE_CONTAINER_PORT);
  } catch (error) {
    input.logger.warn("Could not read the secure port mapping from Supervisor", { error: String((error as Error).message) });
  }

  const functional = buildFunctionalHandlers(input);
  const listenerDeps = { registry, pairingManager: pairing, functional, relayVersion: input.relayVersion, now: input.now, logger: input.logger };

  const bootstrap = createHttpServer_(createBootstrapListener(listenerDeps));
  const device = createHttpsServer({ key: tls.keyPem, cert: tls.certPem, minVersion: "TLSv1.3" }, createDeviceListener(listenerDeps));
  const ingress = createHttpServer_(buildIngressListener(input, pairing, registry, supervisor, () => registryCorrupt, resetRegistry));

  return {
    servers: { bootstrap, device, ingress },
    registryCorrupt,
    async listen() {
      await Promise.all([
        listen(bootstrap, BOOTSTRAP_PORT),
        listen(device, SECURE_PORT),
        listen(ingress, INGRESS_PORT),
      ]);
      input.logger.info?.("Relay listening (app mode)", {
        bootstrap: BOOTSTRAP_PORT,
        secure: SECURE_PORT,
        secure_host_port: secureHostPort,
        ingress: INGRESS_PORT,
        registry_corrupt: registryCorrupt,
      });
    },
  };
}

function buildFunctionalHandlers(input: AppModeInput): FunctionalHandlers {
  return {
    health: createHealthHandler({
      version: input.relayVersion,
      wsClient: input.wsClient,
      startedAtMs: input.startedAtMs,
      fileAccessMode: input.fileAccess.mode,
      snapshotRoot: input.snapshotRoot,
      now: input.now,
    }),
    ws: createWsProxyHandler({ wsClient: input.wsClient }),
    core: createCoreProxyHandler({ coreClient: input.coreClient }),
    files: createFilesHandler({ fileAccess: input.fileAccess }),
    backups: createBackupsHandler({ snapshotRoot: input.snapshotRoot, now: input.now }),
  };
}

function buildIngressListener(
  input: AppModeInput,
  pairing: PairingV1Manager,
  registry: DeviceRegistry,
  supervisor: ReturnType<typeof createSupervisorClient>,
  registryCorrupt: () => boolean,
  resetRegistry: () => void
): RequestListener {
  const csrf = createCsrfStore();
  const iconBytes = loadIcon(input.iconPath);
  const novaDeps: NovaPageDeps = {
    fetchAuthUsers: async () => {
      const result = await input.wsClient.sendMessage({ type: "config/auth/list" });
      return Array.isArray(result) ? (result as HaAuthUser[]) : [];
    },
    csrf,
    pairing,
    registry,
    registryCorrupt,
    resetRegistry,
    connection: () => ({ haConnected: input.wsClient.isConnected() }),
    update: async () => {
      const info = await supervisor.getSelfInfo();
      return { version: info.version, versionLatest: info.versionLatest, updateAvailable: info.updateAvailable, error: false };
    },
    relayVersion: input.relayVersion,
    now: input.now,
  };

  const page = createNovaPageHandler(novaDeps);
  const action = createNovaActionHandler(novaDeps);
  const icon: RouteHandler = ({ response }) => {
    response.setHeader("content-type", "image/png");
    response.setHeader("cache-control", "no-store");
    response.end(iconBytes ?? Buffer.alloc(0));
  };
  const router = createRouter();
  // Home Assistant forwards the ingress ROOT ("/") to the add-on, not the
  // ingress_entry path, so the console must answer at "/" as well as "/home".
  // Forms and the icon are addressed relative to the ingress path the request
  // arrived on, so both prefixes are registered.
  router.register("GET", "/", page);
  router.register("GET", "/home", page);
  router.register("POST", "/action", action);
  router.register("POST", "/home/action", action);
  if (iconBytes) {
    router.register("GET", "/icon", icon);
    router.register("GET", "/home/icon", icon);
  }

  const formRoutes = new Set(["POST /action", "POST /home/action"]);
  return createRequestListener({
    router,
    version: input.relayVersion,
    logger: input.logger,
    // The owner gate lives inside the handlers (ingress peer + config/auth/list);
    // the listener itself does not resolve a bearer principal.
    authorize: () => ({ ok: true }),
    bodyPolicy: (routeKey) =>
      formRoutes.has(routeKey) ? { type: "form", maxBytes: DEFAULT_MAX_FORM_BODY_BYTES } : { type: "none", maxBytes: 0 },
  });
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

// A no-op registry used only when the on-disk registry is corrupt: every read
// is empty and every mutation throws, so device auth and pairing fail closed
// while the owner page stays reachable to trigger a reset.
function openInertRegistry(): DeviceRegistry {
  const nope = (): never => {
    throw new Error("device registry is corrupt");
  };
  return {
    list: () => [],
    hasLegacy: () => false,
    legacyImportCompleted: () => false,
    resolveDeviceSecret: () => null,
    resolveLegacySecret: () => null,
    createPending: nope,
    activate: nope,
    activatePending: () => null,
    revoke: () => false,
    importLegacy: nope,
    revokeLegacy: nope,
  };
}

// A stable DeviceRegistry facade that always delegates to the current store, so
// a corrupt-registry reset can swap the underlying registry without rebuilding
// the listeners and pairing manager that captured this reference.
function swappableRegistry(get: () => DeviceRegistry): DeviceRegistry {
  return {
    list: () => get().list(),
    hasLegacy: () => get().hasLegacy(),
    legacyImportCompleted: () => get().legacyImportCompleted(),
    resolveDeviceSecret: (deviceId, secretDigest, now) => get().resolveDeviceSecret(deviceId, secretDigest, now),
    resolveLegacySecret: (secretDigest) => get().resolveLegacySecret(secretDigest),
    createPending: (record, now) => get().createPending(record, now),
    activate: (deviceId, now) => get().activate(deviceId, now),
    activatePending: (deviceId, secretDigest, now) => get().activatePending(deviceId, secretDigest, now),
    revoke: (deviceId) => get().revoke(deviceId),
    importLegacy: (secretDigest, now) => get().importLegacy(secretDigest, now),
    revokeLegacy: () => get().revokeLegacy(),
  };
}

async function listen(server: Server, port: number): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.listen(port, "0.0.0.0", () => resolve());
    server.on("error", reject);
  });
}
