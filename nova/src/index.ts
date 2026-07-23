import type { Server } from "node:http";

import type { FileAccessConfig } from "./config/file-access.js";
import type { CoreProxyRequest, CoreProxyResponse } from "./types/api.js";
import type {
  HaWsConnectionStatus,
  HaWsEventCollection,
  HaWsEventCollectionOptions,
  HaWsRequest,
} from "./ha/ws-client.js";
import { createBackupsHandler } from "./http/handlers/backups.js";
import { createCoreProxyHandler } from "./http/handlers/core-proxy.js";
import { createFilesHandler } from "./http/handlers/files.js";
import { createHealthHandler } from "./http/handlers/health.js";
import { createHomeHandler } from "./http/handlers/home.js";
import { createPairHandler } from "./http/handlers/pair.js";
import { createWsProxyHandler } from "./http/handlers/ws-proxy.js";
import { createRouter, type Router } from "./http/router.js";
import { createHttpServer } from "./http/server.js";
import { createPairingManager, type PairingManager } from "./security/pairing.js";

const PAIR_PATH = "/pair";
const PAIR_ROUTE = `POST ${PAIR_PATH}`;
const HOME_PATH = "/home";
const HOME_ROUTE = `GET ${HOME_PATH}`;

export interface AppOptions {
  authToken: string;
  version: string;
  wsClient: {
    isConnected(): boolean;
    getConnectionStatus?(): HaWsConnectionStatus;
    sendMessage(message: HaWsRequest): Promise<unknown>;
    collectMessageEvents(
      message: HaWsRequest,
      options?: HaWsEventCollectionOptions
    ): Promise<HaWsEventCollection<unknown>>;
  };
  logger?: {
    warn(message: string, context?: Record<string, unknown>): void;
    error(message: string, context?: Record<string, unknown>): void;
  };
  fileAccess: FileAccessConfig;
  snapshotRoot: string;
  coreClient: {
    request(input: CoreProxyRequest): Promise<CoreProxyResponse>;
  };
  startedAtMs?: number;
  now?: () => number;
  pairingManager?: PairingManager;
  requiredRelayVersion?: string;
}

export interface App {
  version: string;
  router: Router;
  server: Server;
  pairing: PairingManager;
}

export function createApp(options: AppOptions): App {
  const router = createRouter();
  const startedAtMs = options.startedAtMs ?? Date.now();
  const pairing =
    options.pairingManager
    ?? createPairingManager({
      relayToken: options.authToken,
      ...(options.now ? { now: options.now } : {})
    });

  const healthOptions = {
    version: options.version,
    wsClient: options.wsClient,
    startedAtMs,
    fileAccessMode: options.fileAccess.mode,
    snapshotRoot: options.snapshotRoot
  } as const;

  router.register(
    "GET",
    "/health",
    createHealthHandler(
      options.now
        ? {
            ...healthOptions,
            now: options.now
          }
        : healthOptions
    )
  );

  router.register("POST", PAIR_PATH, createPairHandler(pairing));

  router.register(
    "GET",
    HOME_PATH,
    createHomeHandler({
      health: options.now ? { ...healthOptions, now: options.now } : healthOptions,
      pairing,
      requiredRelayVersion: options.requiredRelayVersion ?? options.version,
      ...(options.now ? { now: options.now } : {})
    })
  );

  router.register(
    "POST",
    "/ws",
    createWsProxyHandler({
      wsClient: options.wsClient
    })
  );

  router.register(
    "POST",
    "/core",
    createCoreProxyHandler({
      coreClient: options.coreClient
    })
  );

  router.register(
    "POST",
    "/files",
    createFilesHandler({
      fileAccess: options.fileAccess
    })
  );

  router.register(
    "POST",
    "/backups",
    createBackupsHandler(
      options.now
        ? { snapshotRoot: options.snapshotRoot, now: options.now }
        : { snapshotRoot: options.snapshotRoot }
    )
  );

  const server = createHttpServer({
    authToken: options.authToken,
    router,
    version: options.version,
    bearerExemptRoutes: new Set([PAIR_ROUTE, HOME_ROUTE]),
    noStorePaths: new Set([PAIR_PATH, HOME_PATH]),
    ...(options.logger ? { logger: options.logger } : {})
  });

  return {
    version: options.version,
    router,
    server,
    pairing
  };
}
