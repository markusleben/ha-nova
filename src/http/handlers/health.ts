import type { RouteHandler } from "../router.js";

export interface HealthHandlerOptions {
  version: string;
  wsClient: {
    isConnected(): boolean;
  };
  startedAtMs: number;
  now?: () => number;
}

export interface HealthPayload {
  status: "ok";
  ha_ws_connected: boolean;
  version: string;
  uptime_s: number;
}

export function createHealthHandler(options: HealthHandlerOptions): RouteHandler {
  const now = options.now ?? (() => Date.now());

  return () => {
    const uptimeMs = Math.max(0, now() - options.startedAtMs);
    const uptimeSeconds = Math.floor(uptimeMs / 1000);

    const payload: HealthPayload = {
      status: "ok",
      ha_ws_connected: options.wsClient.isConnected(),
      version: options.version,
      uptime_s: uptimeSeconds
    };

    return payload;
  };
}
