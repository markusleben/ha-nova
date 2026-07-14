import type { HaWsConnectionStatus } from "../../ha/ws-client.js";
import type { RouteHandler } from "../router.js";
import { summarizeSnapshotStore } from "./backups.js";

export interface HealthHandlerOptions {
  version: string;
  wsClient: {
    isConnected(): boolean;
    getConnectionStatus?(): HaWsConnectionStatus;
  };
  startedAtMs: number;
  fileAccessMode: string;
  snapshotRoot: string;
  now?: () => number;
}

export interface HealthPayload {
  status: "ok";
  ha_ws_connected: boolean;
  // Why the WS is down — "auth" (fix the token), "network" (HA unreachable /
  // restarting), "never_connected" (no successful handshake since relay
  // start), or null while connected. The onboarding skill keys its diagnosis
  // on this instead of guessing.
  ha_ws_disconnect_reason: "auth" | "network" | "never_connected" | null;
  version: string;
  uptime_s: number;
  file_access: string;
  snapshots: { files: number; bytes: number };
}

export function createHealthHandler(options: HealthHandlerOptions): RouteHandler {
  const now = options.now ?? (() => Date.now());

  return async () => {
    const uptimeMs = Math.max(0, now() - options.startedAtMs);
    const uptimeSeconds = Math.floor(uptimeMs / 1000);

    const wsStatus: HaWsConnectionStatus = options.wsClient.getConnectionStatus
      ? options.wsClient.getConnectionStatus()
      : { connected: options.wsClient.isConnected(), disconnect_reason: null };

    // Counting a ≤500-file store is cheap; a broken store must not take
    // /health down with it — report zeros instead.
    let snapshots = { files: 0, bytes: 0 };
    try {
      snapshots = await summarizeSnapshotStore(options.snapshotRoot);
    } catch {
      // deliberately swallowed: /health stays available
    }

    const payload: HealthPayload = {
      status: "ok",
      ha_ws_connected: wsStatus.connected,
      ha_ws_disconnect_reason: wsStatus.connected ? null : wsStatus.disconnect_reason,
      version: options.version,
      uptime_s: uptimeSeconds,
      file_access: options.fileAccessMode,
      snapshots
    };

    return payload;
  };
}
