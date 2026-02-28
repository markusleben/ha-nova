import type { Server } from "node:http";

import type { HaWsRequest } from "./ha/ws-client.js";
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

  const server = createHttpServer({
    authToken: options.authToken,
    router
  });

  return {
    version: options.version,
    router,
    server
  };
}
