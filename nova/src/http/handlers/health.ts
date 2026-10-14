import type { HaWsConnectionStatus } from "../../ha/ws-client.js";
import type { RouteHandler } from "../router.js";
import type { RelayLogger } from "../server.js";
import { summarizeSnapshotStore } from "./backups.js";

// /health is polled every few seconds and a broken store does not heal on
// its own: warn once per process, not once per poll.
let snapshotStoreWarned = false;

export interface HealthHandlerOptions {
  version: string;
  wsClient: {
    isConnected(): boolean;
    getConnectionStatus?(): HaWsConnectionStatus;
  };
  startedAtMs: number;
  fileAccessMode: string;
  snapshotRoot: string;
  relayInstanceId?: string;
  now?: () => number;
  logger?: RelayLogger;
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
  relay_instance_id?: string;
}

export function createHealthHandler(
  options: HealthHandlerOptions,
): RouteHandler {
  return async () => await readHealthPayload(options);
}

export async function readHealthPayload(
  options: HealthHandlerOptions,
): Promise<HealthPayload> {
  const now = options.now ?? (() => Date.now());
  const uptimeMs = Math.max(0, now() - options.startedAtMs);
  const uptimeSeconds = Math.floor(uptimeMs / 1000);

  const wsStatus: HaWsConnectionStatus = options.wsClient.getConnectionStatus
    ? options.wsClient.getConnectionStatus()
    : { connected: options.wsClient.isConnected(), disconnect_reason: null };

  // Counting a ≤500-file store is cheap; a broken store must not take either
  // /health or Home Base down with it — report zeros, but never silently: an
  // unreadable store is an I/O fault, not an empty store.
  let snapshots = { files: 0, bytes: 0 };
  try {
    snapshots = await summarizeSnapshotStore(options.snapshotRoot);
  } catch (error) {
    if (!snapshotStoreWarned) {
      snapshotStoreWarned = true;
      options.logger?.warn("snapshot store unreadable; health reports zero snapshots", {
        error: error instanceof Error ? error.message : String(error),
      });
    }
  }

  return {
    status: "ok",
    ha_ws_connected: wsStatus.connected,
    ha_ws_disconnect_reason: wsStatus.connected
      ? null
      : wsStatus.disconnect_reason,
    version: options.version,
    uptime_s: uptimeSeconds,
    file_access: options.fileAccessMode,
    snapshots,
    ...(options.relayInstanceId !== undefined
      ? { relay_instance_id: options.relayInstanceId }
      : {}),
  };
}
