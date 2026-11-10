import { createServer as createHttpServer_, type Server } from "node:http";
import { createServer as createHttpsServer } from "node:https";
import { dirname } from "node:path";

import type { FileAccessConfig } from "../config/file-access.js";
import { createBackupsHandler } from "../http/handlers/backups.js";
import { createCoreForwarder, parseCoreRequest } from "../http/handlers/core-proxy.js";
import { createFilesHandler } from "../http/handlers/files.js";
import { createHealthHandler } from "../http/handlers/health.js";
import { createSupervisorProxyHandler } from "../http/handlers/supervisor-proxy.js";
import { createWsProxyHandler } from "../http/handlers/ws-proxy.js";
import { gated } from "../http/gate.js";
import { gateWithAsks, type GatePage, type PhoneLink } from "../http/gate-asks.js";
import { pageChannel } from "../http/handlers/nova-approve.js";
import { pageTexts } from "../http/page-texts.js";
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
import type { Principal } from "../security/principal.js";
import { createFileResponseStore } from "../security/pairing-response-store.js";
import { createPairingV1Manager } from "../security/pairing-v1.js";
import { loadOrCreateTlsIdentity } from "../security/tls-identity.js";
import { loadOrCreateRelayInstanceId } from "../storage/relay-instance.js";
import { openInertRegistry, swappableRegistry } from "./app-registry.js";
import { chatAppFilesPresent, createChatApps, type ChatApps } from "./chat-apps.js";
import { createGateWorker, mintGateDevice, type GateWorker } from "./gate-worker.js";
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
    active = openDeviceRegistry(dataDir, input.logger);
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
    active = openDeviceRegistry(dataDir, input.logger); // a fresh, empty registry
    // A reset is a full fresh start ("every computer will need to pair again"),
    // so cut pre-pairing legacy access too: tombstone the migration so a lingering
    // plaintext relay_auth_token is NOT re-imported into the fresh registry on the
    // next boot. The next boot's importLegacyToken + clearLegacyOptions then remove
    // the plaintext file and option.
    active.markLegacyMigrated();
    registryCorrupt = false;
    chatApps?.restartBridge(); // its credential went with the old registry
    gate?.restart(); // so did the safety-check worker's
    input.logger.info?.(
      "Device registry reset by the owner; device pairing re-enabled",
    );
  };
  let chatApps: ChatApps | undefined;
  let gate: GateWorker | undefined;

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
    logger: input.logger,
  });

  // Prime the port at startup; a failure here recovers via the lazy retry above.
  await ensureSecurePort();

  // Chat apps exist only in images built with HA_NOVA_CHAT_APPS=1 (nova/Dockerfile).
  if (process.env.HA_NOVA_CHAT_APPS === "1") {
    chatApps = createChatApps({
      dataDir,
      configRoot: input.fileAccess.configRoot,
      binary: "/opt/ha-nova/ha-nova",
      integrationSource: "/app/integration/ha_nova",
      securePort: SECURE_PORT,
      spkiPin: tls.spkiPin,
      registry,
      supervisor,
      core: input.coreClient,
      now: input.now,
      logger: input.logger,
    });
  }

  // The safety check fronts every device-facing handler on all three listeners.
  gate = createGateWorker({
    dataDir,
    binary: "/opt/ha-nova/ha-nova",
    mint: mintGateDevice(registry, input.now, `https://127.0.0.1:${SECURE_PORT}`, tls.spkiPin),
    logger: input.logger,
  });
  const { functional, page: gatePage } = buildFunctionalHandlers(input, relayInstanceId, supervisorBase, gate, registry);
  const listenerDeps = {
    registry,
    pairingManager: pairing,
    functional,
    relayVersion: input.relayVersion,
    now: input.now,
    logger: input.logger,
    chatAppFiles: () => chatAppFilesPresent(input.fileAccess.configRoot),
    // Unreadable: counted as another device, so uninstall keeps the App.
    bridgeDevice: () => {
      try {
        return chatApps?.bridgeDeviceId() ?? null;
      } catch {
        return null;
      }
    },
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
      gatePage,
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
      ...(chatApps !== undefined ? { chatApps } : {}),
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
      gate?.start(); // the worker reaches the secure listener just started
      await chatApps?.boot(); // so does the bridge
    },
  };
}

// phoneLinkOf: an active device's linked phone from the registry, on the
// worker's wire; none for a pending device or the shared token.
export function phoneLinkOf(registry: Pick<DeviceRegistry, "list">): (principal: Principal) => PhoneLink | undefined {
  return (p) => {
    const phone = p.kind === "device" ? registry.list().find((d) => d.deviceId === p.deviceId && d.state === "active")?.confirmPhone : undefined;
    return phone && { service: phone.service, device_id: phone.deviceId, user_id: phone.userId, ...(phone.platform !== undefined ? { platform: phone.platform } : {}) };
  };
}

// setPhoneLinkOf: the link route's registry write, from the worker's wire.
export function setPhoneLinkOf(registry: Pick<DeviceRegistry, "setConfirmPhone">): (principal: Principal, link: PhoneLink, current: PhoneLink | undefined) => boolean {
  const record = (l: PhoneLink) => ({ service: l.service, deviceId: l.device_id, userId: l.user_id, ...(l.platform !== undefined ? { platform: l.platform } : {}) });
  return (p, link, current) => p.kind === "device" && registry.setConfirmPhone(p.deviceId, record(link), current && record(current));
}

function buildFunctionalHandlers(
  input: AppModeInput,
  relayInstanceId: string,
  supervisorBase: string | undefined,
  worker: GateWorker,
  registry: DeviceRegistry,
): { functional: FunctionalHandlers; page: GatePage } {
  const forwardCore = createCoreForwarder({ coreClient: input.coreClient });
  const { gate, ask, asks } = gateWithAsks(worker, {
    prepare: { core: parseCoreRequest },
    deviceName: (p) => (p.kind === "device" ? registry.list().find((d) => d.deviceId === p.deviceId)?.name : undefined) ?? "the shared Relay token",
    channels: () => pageChannel(input.wsClient), // the approval view, while someone may approve there
    link: phoneLinkOf(registry),
    setLink: setPhoneLinkOf(registry),
    now: input.now,
    logger: input.logger,
  });
  const functional: FunctionalHandlers = {
    ask,
    health: createHealthHandler({
      version: input.relayVersion,
      wsClient: input.wsClient,
      startedAtMs: input.startedAtMs,
      fileAccessMode: input.fileAccess.mode,
      snapshotRoot: input.snapshotRoot,
      relayInstanceId,
      gate: true,
      now: input.now,
      logger: input.logger,
    }),
    ws: gated("ws", createWsProxyHandler({ wsClient: input.wsClient }), gate),
    core: gated("core", async ({ body }) => await forwardCore(body as CoreProxyRequest), gate, parseCoreRequest),
    files: gated("files", createFilesHandler({ fileAccess: input.fileAccess }), gate),
    backups: gated("backups", createBackupsHandler({ snapshotRoot: input.snapshotRoot, now: input.now }), gate),
    supervisor: gated(
      "supervisor",
      createSupervisorProxyHandler(supervisorBase ? { token: input.supervisorToken, base: supervisorBase } : { token: input.supervisorToken }),
      gate,
    ),
  };
  return { functional, page: { ...asks, texts: pageTexts((name) => worker.texts(name), input.now, input.logger) } };
}

async function listen(server: Server, port: number): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.listen(port, "0.0.0.0", () => resolve());
    server.on("error", reject);
  });
}
