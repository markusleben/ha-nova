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
import { createCoreForwarder, parseCoreRequest } from "./http/handlers/core-proxy.js";
import { createFilesHandler } from "./http/handlers/files.js";
import { createHealthHandler } from "./http/handlers/health.js";
import { createWsProxyHandler, prepareWsBody } from "./http/handlers/ws-proxy.js";
import { gated, type GateClient } from "./http/gate.js";
import { gateWithAsks } from "./http/gate-asks.js";
import { createRouter, type Router } from "./http/router.js";
import { createHttpServer } from "./http/server.js";
import type { PhoneLinkStore } from "./runtime/gate-phone-store.js";


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
  // The safety check (nova/src/http/gate.ts); without one every device-facing
  // request is refused as unavailable.
  gate?: GateClient;
  gateToken?: () => string | null;
  // The shared token's linked phone (runtime/gate-phone-store.ts); none: no link route.
  phoneLink?: PhoneLinkStore;
}

const NO_GATE: GateClient = { check: async () => null, redact: async () => null };

export interface App {
  version: string;
  router: Router;
  server: Server;
}

export function createApp(options: AppOptions): App {
  const router = createRouter();
  const startedAtMs = options.startedAtMs ?? Date.now();

  const healthOptions = {
    version: options.version,
    wsClient: options.wsClient,
    startedAtMs,
    fileAccessMode: options.fileAccess.mode,
    snapshotRoot: options.snapshotRoot,
    gate: options.gate !== undefined,
    ...(options.logger ? { logger: options.logger } : {})
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

  // No Supervisor ingress, so no NOVA page: the linked phone is the only channel.
  const phoneLink = options.phoneLink;
  const { gate, ask } = gateWithAsks(options.gate ?? NO_GATE, {
    prepare: { core: parseCoreRequest, ws: prepareWsBody },
    deviceName: () => "your HA NOVA container",
    channels: () => [],
    link: (p) => (p.kind === "shared" ? phoneLink?.link() : undefined),
    ...(phoneLink ? { setLink: (p, link, current) => p.kind === "shared" && phoneLink.setLink(link, current), linkable: (p) => p.kind === "shared" } : {}),
    now: options.now ?? Date.now,
    ...(options.logger ? { logger: options.logger } : {}),
  });
  router.register("POST", "/gate/v1/ask", ask);
  router.register("POST", "/gate/v1/phone", ask);
  const forwardCore = createCoreForwarder({ coreClient: options.coreClient });
  router.register("POST", "/ws", gated("ws", createWsProxyHandler({ wsClient: options.wsClient }), gate, prepareWsBody));
  router.register("POST", "/core", gated("core", async ({ body }) => await forwardCore(body as CoreProxyRequest), gate, parseCoreRequest));
  router.register("POST", "/files", gated("files", createFilesHandler({ fileAccess: options.fileAccess }), gate));
  router.register(
    "POST",
    "/backups",
    gated("backups", createBackupsHandler(options.now ? { snapshotRoot: options.snapshotRoot, now: options.now } : { snapshotRoot: options.snapshotRoot }), gate)
  );

  const server = createHttpServer({
    authToken: options.authToken,
    router,
    version: options.version,
    ...(options.gateToken ? { gateToken: options.gateToken } : {}),
    ...(options.logger ? { logger: options.logger } : {})
  });

  return {
    version: options.version,
    router,
    server
  };
}
