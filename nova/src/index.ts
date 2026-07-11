import type { Server } from "node:http";

import type { FileAccessConfig } from "./config/file-access.js";
import type { CoreProxyRequest, CoreProxyResponse } from "./types/api.js";
import type {
  HaWsEventCollection,
  HaWsEventCollectionOptions,
  HaWsRequest,
} from "./ha/ws-client.js";
import { createCoreProxyHandler } from "./http/handlers/core-proxy.js";
import { createFilesHandler } from "./http/handlers/files.js";
import { createHealthHandler } from "./http/handlers/health.js";
import { createWsProxyHandler } from "./http/handlers/ws-proxy.js";
import { createRouter, type Router } from "./http/router.js";
import { createHttpServer } from "./http/server.js";

export interface AppOptions {
  authToken: string;
  version: string;
  wsClient: {
    isConnected(): boolean;
    sendMessage(message: HaWsRequest): Promise<unknown>;
    collectMessageEvents(
      message: HaWsRequest,
      options?: HaWsEventCollectionOptions
    ): Promise<HaWsEventCollection<unknown>>;
  };
  fileAccess: FileAccessConfig;
  coreClient: {
    request(input: CoreProxyRequest): Promise<CoreProxyResponse>;
  };
  startedAtMs?: number;
  now?: () => number;
}

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
    startedAtMs
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

  const server = createHttpServer({
    authToken: options.authToken,
    router,
    version: options.version
  });

  return {
    version: options.version,
    router,
    server
  };
}
