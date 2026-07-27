import { createServer as createHttpServer_, type Server } from "node:http";
import { createServer as createHttpsServer } from "node:https";
import { dirname } from "node:path";

import type { FileAccessConfig } from "../config/file-access.js";
import { createBackupsHandler } from "../http/handlers/backups.js";
import { createCoreProxyHandler } from "../http/handlers/core-proxy.js";
import { createFilesHandler } from "../http/handlers/files.js";
import { createHealthHandler } from "../http/handlers/health.js";
import { createWsProxyHandler } from "../http/handlers/ws-proxy.js";
import { applyServerTimeouts, type RelayLogger } from "../http/server.js";
import type { CoreProxyRequest, CoreProxyResponse } from "../types/api.js";
import type { HaWsClient } from "../ha/ws-client.js";
import { createSupervisorClient } from "../ha/supervisor-client.js";
import {
  archiveCorruptRegistry,
  openDeviceRegistry,
  RegistryCorruptError,
  type DeviceRegistry,
} from "../security/device-registry.js";
import { opaqueReady } from "../security/opaque-server.js";
import { createFileResponseStore } from "../security/pairing-response-store.js";
import { createPairingV1Manager } from "../security/pairing-v1.js";
import { loadOrCreateTlsIdentity } from "../security/tls-identity.js";
import { loadOrCreateRelayInstanceId } from "../storage/relay-instance.js";
import { createIngressListener } from "./ingress-listener.js";
import { clearLegacyOptions, importLegacyToken } from "./legacy-migration.js";
import { ensureSidebarPanel } from "./sidebar-panel.js";
import {
  createBootstrapListener,
  createDeviceListener,
  type FunctionalHandlers,
} from "./listeners.js";

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
  cloudRemoteEnabled: boolean;
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

export async function buildAppMode(
  input: AppModeInput,
): Promise<AppModeRuntime> {
  const dataDir = dirname(input.appOptionsPath); // /data
  const relayInstanceId = loadOrCreateRelayInstanceId(dataDir);
  // The Supervisor self-API base is fixed to http://supervisor in production;
  // the disposable-HA e2e overrides it to a mock so app mode runs without a
  // real Supervisor (upstream core/WS still go direct to env.haUrl).
  const supervisorBase =
    process.env.HA_NOVA_SUPERVISOR_BASE?.trim() || undefined;
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
      input.logger.error(
        "Device registry is corrupt; device access disabled until reset",
        { error: error.message },
      );
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
    // A reset is a full fresh start ("every computer will need to pair again"),
    // so cut pre-pairing legacy access too: tombstone the migration so a lingering
    // plaintext relay_auth_token is NOT re-imported into the fresh registry on the
    // next boot. The next boot's importLegacyToken + clearLegacyOptions then remove
    // the plaintext file and option.
    active.markLegacyMigrated();
    registryCorrupt = false;
    input.logger.info?.(
      "Device registry reset by the owner; device pairing re-enabled",
    );
  };

  const tls = await loadOrCreateTlsIdentity(dataDir);

  // One-time legacy migration: import a pre-existing shared token into the
  // registry as a digest, then clear the option. Skipped on a corrupt registry.
  if (!registryCorrupt) {
    await importLegacyToken({
      registry,
      supervisor,
      dataDir,
      appOptionsPath: input.appOptionsPath,
      now: input.now,
      logger: input.logger,
    });
  }
  // Clear the plaintext options in ONE write (relay token only when the registry
  // is healthy enough to have migrated it; ha_llat always). A corrupt registry
  // keeps the shared token recoverable until the owner resets.
  await clearLegacyOptions(
    { supervisor, appOptionsPath: input.appOptionsPath, logger: input.logger },
    !registryCorrupt,
  );
  // First-boot default: make the NOVA sidebar entry appear without a manual
  // "Add to sidebar" toggle. Once-ever via marker; never overrides an owner
  // who hides the panel later.
  await ensureSidebarPanel({ supervisor, dataDir, logger: input.logger });

  // OPAQUE runs on WASM that must finish initializing before the first pairing
  // operation. Await it once here, before any listener accepts traffic, so the
  // owner's first "Connect a device" never races the WASM load.
  await opaqueReady();

  // The secure host port comes from Supervisor self-info. A transient failure at
  // startup must not disable pairing for the whole process lifetime, so it is
  // re-queried lazily (deduped) whenever it is still unknown and the owner pairs.
  let secureHostPort: number | null = null;
  let refreshingSecurePort = false;
  const ensureSecurePort = async (): Promise<void> => {
    if (secureHostPort !== null || refreshingSecurePort) {
      return;
    }
    refreshingSecurePort = true;
    try {
      secureHostPort = await supervisor.getMappedHostPort(
        SECURE_CONTAINER_PORT,
      );
    } catch (error) {
      input.logger.warn(
        "Could not read the secure port mapping from Supervisor",
        { error: String((error as Error).message) },
      );
    } finally {
      refreshingSecurePort = false;
    }
  };

  const pairing = createPairingV1Manager({
    registry,
    now: input.now,
    secureEndpoint: () => {
      if (secureHostPort === null) {
        void ensureSecurePort(); // transient startup failure: refresh for the next attempt
        return null;
      }
      return { spkiPin: tls.spkiPin, securePort: secureHostPort };
    },
    // Upgrade-only fallback for finish responses written by older versions.
    // New responses commit atomically with their pending registry credential.
    legacyResponseStore: createFileResponseStore(
      dataDir,
      input.now,
      input.logger,
    ),
    cloudPairing: input.cloudRemoteEnabled,
  });

  // Prime the port at startup; a failure here recovers via the lazy retry above.
  await ensureSecurePort();

  const functional = buildFunctionalHandlers(input, relayInstanceId);
  const listenerDeps = {
    registry,
    pairingManager: pairing,
    functional,
    relayVersion: input.relayVersion,
    now: input.now,
    logger: input.logger,
  };

  const bootstrap = createHttpServer_(createBootstrapListener(listenerDeps));
  const device = createHttpsServer(
    { key: tls.keyPem, cert: tls.certPem, minVersion: "TLSv1.3" },
    createDeviceListener(listenerDeps),
  );
  const ingress = createHttpServer_(
    createIngressListener({
      registry,
      pairing,
      functional,
      relayInstanceId,
      relayVersion: input.relayVersion,
      cloudRemoteEnabled: input.cloudRemoteEnabled,
      wsClient: input.wsClient,
      supervisor,
      registryCorrupt: () => registryCorrupt,
      resetRegistry,
      now: input.now,
      logger: input.logger,
      ...(input.iconPath !== undefined ? { iconPath: input.iconPath } : {}),
    }),
  );
  // These servers bypass createHttpServer, so apply the same stalled-client
  // request/header timeout guards the single-listener path gets.
  for (const server of [bootstrap, device, ingress]) {
    applyServerTimeouts(server);
  }

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

function buildFunctionalHandlers(
  input: AppModeInput,
  relayInstanceId: string,
): FunctionalHandlers {
  return {
    health: createHealthHandler({
      version: input.relayVersion,
      wsClient: input.wsClient,
      startedAtMs: input.startedAtMs,
      fileAccessMode: input.fileAccess.mode,
      snapshotRoot: input.snapshotRoot,
      relayInstanceId,
      now: input.now,
    }),
    ws: createWsProxyHandler({ wsClient: input.wsClient }),
    core: createCoreProxyHandler({ coreClient: input.coreClient }),
    files: createFilesHandler({ fileAccess: input.fileAccess }),
    backups: createBackupsHandler({
      snapshotRoot: input.snapshotRoot,
      now: input.now,
    }),
  };
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
    resolveCloudDeviceSecret: () => null,
    resolveLegacySecret: () => null,
    getPairingResponse: () => null,
    createPending: nope,
    createPendingWithResponse: nope,
    activate: nope,
    activatePending: () => null,
    bindCloudUser: () => ({ ok: false, reason: "unknown" }),
    activatePendingForCloud: () => ({ ok: false, reason: "unknown" }),
    revokeCloudDevice: () => ({ ok: false, reason: "unknown" }),
    revoke: () => false,
    importLegacy: nope,
    markLegacyMigrated: nope,
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
    resolveDeviceSecret: (deviceId, secretDigest, now) =>
      get().resolveDeviceSecret(deviceId, secretDigest, now),
    resolveCloudDeviceSecret: (
      deviceId,
      secretDigest,
      userId,
      relayInstanceId,
      now,
    ) =>
      get().resolveCloudDeviceSecret(
        deviceId,
        secretDigest,
        userId,
        relayInstanceId,
        now,
      ),
    resolveLegacySecret: (secretDigest) =>
      get().resolveLegacySecret(secretDigest),
    getPairingResponse: (handshakeId, contextKey, now) =>
      get().getPairingResponse(handshakeId, contextKey, now),
    createPending: (record, now) => get().createPending(record, now),
    createPendingWithResponse: (record, response, now) =>
      get().createPendingWithResponse(record, response, now),
    activate: (deviceId, now) => get().activate(deviceId, now),
    activatePending: (deviceId, secretDigest, now) =>
      get().activatePending(deviceId, secretDigest, now),
    bindCloudUser: (deviceId, secretDigest, userId, relayInstanceId) =>
      get().bindCloudUser(deviceId, secretDigest, userId, relayInstanceId),
    activatePendingForCloud: (
      deviceId,
      secretDigest,
      userId,
      relayInstanceId,
      now,
    ) =>
      get().activatePendingForCloud(
        deviceId,
        secretDigest,
        userId,
        relayInstanceId,
        now,
      ),
    revokeCloudDevice: (deviceId, secretDigest, userId, relayInstanceId, now) =>
      get().revokeCloudDevice(
        deviceId,
        secretDigest,
        userId,
        relayInstanceId,
        now,
      ),
    revoke: (deviceId) => get().revoke(deviceId),
    importLegacy: (secretDigest, now) => get().importLegacy(secretDigest, now),
    markLegacyMigrated: () => get().markLegacyMigrated(),
    revokeLegacy: () => get().revokeLegacy(),
  };
}

async function listen(server: Server, port: number): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.listen(port, "0.0.0.0", () => resolve());
    server.on("error", reject);
  });
}
