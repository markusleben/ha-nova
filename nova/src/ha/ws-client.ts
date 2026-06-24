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
  subscribeMessage?(
    callback: (event: unknown) => void,
    message: HaWsRequest,
    options?: { resubscribe?: boolean }
  ): Promise<() => void | Promise<void>>;
}

export interface HaWsClient {
  sendMessage<T>(message: HaWsRequest): Promise<T>;
  collectMessageEvents<T>(message: HaWsRequest, options?: HaWsEventCollectionOptions): Promise<T[]>;
  isConnected(): boolean;
}

export interface HaWsEventCollectionOptions {
  finishEventType?: string;
  maxEvents?: number;
  timeoutMs?: number;
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
    async collectMessageEvents<T>(
      message: HaWsRequest,
      collectionOptions: HaWsEventCollectionOptions = {}
    ): Promise<T[]> {
      const upstream = await getOrCreateConnection();
      const subscribeMessage = upstream.subscribeMessage;
      if (!subscribeMessage) {
        throw new HaWsClientError(
          "UPSTREAM_WS_ERROR",
          "WS event collection is not supported by this connection"
        );
      }

      const finishEventType = collectionOptions.finishEventType ?? "finish";
      const maxEvents = collectionOptions.maxEvents ?? 100;
      const timeoutMs = collectionOptions.timeoutMs ?? requestTimeoutMs;
      const events: T[] = [];
      let unsubscribe: (() => void | Promise<void>) | undefined;

      try {
        return await withTimeout(
          new Promise<T[]>((resolve, reject) => {
            let settled = false;
            const settleResolve = (value: T[]) => {
              if (!settled) {
                settled = true;
                resolve(value);
              }
            };
            const settleReject = (error: unknown) => {
              if (!settled) {
                settled = true;
                reject(error);
              }
            };

            subscribeMessage(
              (event: unknown) => {
                if (settled) {
                  return;
                }

                events.push(event as T);
                if (isEventType(event, finishEventType)) {
                  settleResolve([...events]);
                  return;
                }

                if (events.length >= maxEvents) {
                  settleReject(
                    new HaWsClientError(
                      "UPSTREAM_WS_ERROR",
                      `WS event collection exceeded ${maxEvents} events`
                    )
                  );
                }
              },
              message,
              { resubscribe: false }
            )
              .then((cancel) => {
                unsubscribe = cancel;
              })
              .catch(settleReject);
          }),
          timeoutMs
        );
      } catch (error) {
        resetConnection();
        if (error instanceof TimeoutError) {
          throw new HaWsClientError(
            "UPSTREAM_WS_TIMEOUT",
            `WS event collection timed out after ${timeoutMs}ms`,
            error
          );
        }

        if (error instanceof HaWsClientError) {
          throw error;
        }

        const errorMessage =
          error instanceof Error && error.message
            ? error.message
            : "WS event collection failed";
        throw new HaWsClientError("UPSTREAM_WS_ERROR", errorMessage, error);
      } finally {
        if (unsubscribe) {
          await unsubscribe();
        }
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

function isEventType(event: unknown, type: string): boolean {
  return (
    !!event &&
    typeof event === "object" &&
    (event as { type?: unknown }).type === type
  );
}
