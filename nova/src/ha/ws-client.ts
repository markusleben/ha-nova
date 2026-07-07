import { TimeoutError, withTimeout } from "../shared/timeout.js";

export type HaWsClientErrorCode =
  | "UPSTREAM_WS_CONNECT_ERROR"
  | "UPSTREAM_WS_TIMEOUT"
  | "UPSTREAM_WS_COMMAND_ERROR"
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
        const commandError = describeUpstreamCommandError(error);
        if (commandError !== undefined) {
          // HA answered the command with a structured error. The connection
          // itself is healthy — keep it, and surface HA's code/message
          // instead of a generic transport failure.
          throw new HaWsClientError(
            "UPSTREAM_WS_COMMAND_ERROR",
            `HA rejected '${message.type}': ${commandError}`,
            error
          );
        }

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

        throw new HaWsClientError("UPSTREAM_WS_ERROR", describeTransportError(error), error);
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
      let unsubscribeOnAck = false;

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
                if (unsubscribeOnAck) {
                  void Promise.resolve(cancel()).catch(() => undefined);
                }
              })
              .catch(settleReject);
          }),
          timeoutMs
        );
      } catch (error) {
        const commandError = describeUpstreamCommandError(error);
        if (commandError !== undefined) {
          // Structured command rejection (e.g. unknown command on older HA):
          // the connection stays healthy, surface HA's error details.
          throw new HaWsClientError(
            "UPSTREAM_WS_COMMAND_ERROR",
            `HA rejected '${message.type}': ${commandError}`,
            error
          );
        }

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

        throw new HaWsClientError("UPSTREAM_WS_ERROR", describeTransportError(error), error);
      } finally {
        if (unsubscribe) {
          await unsubscribe();
        } else {
          unsubscribeOnAck = true;
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

// home-assistant-js-websocket rejects command failures with HA's raw error
// payload ({code, message}), and in-flight connection loss with a wrapped
// result shape ({type:"result", success:false, error:{code:3, message}}).
// Neither is an Error instance. Unwrap first, then classify by code type:
// string code = HA command error, numeric code = transport-level failure.
function unwrapRejectionPayload(error: unknown): { code?: unknown; message?: unknown } | undefined {
  if (!error || typeof error !== "object" || error instanceof Error) {
    return undefined;
  }
  const inner = (error as { error?: unknown }).error;
  if (inner && typeof inner === "object") {
    return inner as { code?: unknown; message?: unknown };
  }
  return error as { code?: unknown; message?: unknown };
}

function describeUpstreamCommandError(error: unknown): string | undefined {
  const payload = unwrapRejectionPayload(error);
  if (payload === undefined) {
    return undefined;
  }
  if (typeof payload.code === "number") {
    // Numeric codes are the library's transport enum, not an HA command error.
    return undefined;
  }
  const codeText =
    typeof payload.code === "string" && payload.code.trim() !== "" ? payload.code : undefined;
  const messageText =
    typeof payload.message === "string" && payload.message.trim() !== "" ? payload.message : undefined;
  if (codeText === undefined && messageText === undefined) {
    return undefined;
  }
  if (codeText !== undefined && messageText !== undefined) {
    return `${codeText}: ${messageText}`;
  }
  return codeText ?? messageText;
}

// Connection-level rejections arrive as bare numeric codes or wrapped
// numeric error payloads from home-assistant-js-websocket; map them to
// readable transport messages.
const HAWS_TRANSPORT_ERRORS: Record<number, string> = {
  1: "cannot connect to Home Assistant WebSocket",
  2: "invalid Home Assistant authentication",
  3: "Home Assistant WebSocket connection lost",
  4: "Home Assistant host required",
  5: "invalid HTTPS-to-HTTP WebSocket upgrade",
  6: "invalid authentication callback"
};

function describeTransportError(error: unknown): string {
  if (typeof error === "number" && HAWS_TRANSPORT_ERRORS[error] !== undefined) {
    return HAWS_TRANSPORT_ERRORS[error];
  }
  const payload = unwrapRejectionPayload(error);
  if (payload !== undefined && typeof payload.code === "number") {
    const mapped = HAWS_TRANSPORT_ERRORS[payload.code];
    if (mapped !== undefined) {
      return mapped;
    }
    if (typeof payload.message === "string" && payload.message.trim() !== "") {
      return payload.message;
    }
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return "WS request failed";
}
