import { TimeoutError, withTimeout } from "../shared/timeout.js";

export type HaWsClientErrorCode =
  | "UPSTREAM_WS_CONNECT_ERROR"
  | "UPSTREAM_WS_TIMEOUT"
  | "UPSTREAM_WS_ERROR";

export interface HaWsRequest {
  type: string;
  [key: string]: unknown;
}

export interface HaWsConnection {
  sendMessagePromise(message: HaWsRequest): Promise<unknown>;
}

export interface HaWsClient {
  sendMessage<T>(message: HaWsRequest): Promise<T>;
  isConnected(): boolean;
}

export interface HaWsClientOptions {
  createConnection: () => Promise<HaWsConnection>;
  requestTimeoutMs?: number;
}

const DEFAULT_REQUEST_TIMEOUT_MS = 10_000;

export class HaWsClientError extends Error {
  public readonly code: HaWsClientErrorCode;

  public constructor(code: HaWsClientErrorCode, message: string, cause?: unknown) {
    super(message, cause === undefined ? undefined : { cause });
    this.code = code;
  }
}

export function createHaWsClient(options: HaWsClientOptions): HaWsClient {
  const requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
  let connection: HaWsConnection | undefined;
  let connectingPromise: Promise<HaWsConnection> | undefined;

  return {
    async sendMessage<T>(message: HaWsRequest): Promise<T> {
      const upstream = await getOrCreateConnection();
      try {
        const result = await withTimeout(upstream.sendMessagePromise(message), requestTimeoutMs);
        return result as T;
      } catch (error) {
        resetConnection();
        if (error instanceof TimeoutError) {
          throw new HaWsClientError(
            "UPSTREAM_WS_TIMEOUT",
            `WS request timed out after ${requestTimeoutMs}ms`,
            error
          );
        }

        if (error instanceof HaWsClientError) {
          throw error;
        }

        const message =
          error instanceof Error && error.message
            ? error.message
            : "WS request failed";
        throw new HaWsClientError("UPSTREAM_WS_ERROR", message, error);
      }
    },
    isConnected(): boolean {
      return connection !== undefined;
    }
  };

  async function getOrCreateConnection(): Promise<HaWsConnection> {
    if (connection) {
      return connection;
    }

    if (!connectingPromise) {
      connectingPromise = options.createConnection();
    }

    try {
      connection = await connectingPromise;
      return connection;
    } catch (error) {
      throw new HaWsClientError(
        "UPSTREAM_WS_CONNECT_ERROR",
        "Failed to connect to Home Assistant WebSocket",
        error
      );
    } finally {
      connectingPromise = undefined;
    }
  }

  function resetConnection(): void {
    connection = undefined;
  }
}
