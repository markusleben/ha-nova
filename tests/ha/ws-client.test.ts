import { describe, expect, it } from "vitest";

import { HaWsClientError, createHaWsClient } from "../../nova/src/ha/ws-client.js";
import { HaSocketAuthError } from "../../nova/src/ha/socket.js";

describe("ha ws client", () => {
  it("connects once and reuses the same connection for multiple requests", async () => {
    let connectCalls = 0;
    const sentTypes: string[] = [];

    const client = createHaWsClient({
      createConnection: async () => {
        connectCalls += 1;
        return {
          sendMessagePromise: async (message: { type: string }) => {
            sentTypes.push(message.type);
            return { echoed: message.type };
          }
        };
      }
    });

    const first = await client.sendMessage<{ echoed: string }>({ type: "ping" });
    const second = await client.sendMessage<{ echoed: string }>({ type: "get_states" });

    expect(first).toEqual({ echoed: "ping" });
    expect(second).toEqual({ echoed: "get_states" });
    expect(connectCalls).toBe(1);
    expect(sentTypes).toEqual(["ping", "get_states"]);
    expect(client.isConnected()).toBe(true);
  });

  it("tracks upstream disconnected/ready events for isConnected", async () => {
    const listeners: Record<string, () => void> = {};

    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => ({ ok: true }),
        addEventListener: (event: "ready" | "disconnected", callback: () => void) => {
          listeners[event] = callback;
        }
      })
    });

    await client.sendMessage({ type: "ping" });
    expect(client.isConnected()).toBe(true);

    // The underlying connection auto-reconnects and keeps its object alive
    // through an HA outage — only the events may flip the health signal.
    listeners["disconnected"]?.();
    expect(client.isConnected()).toBe(false);

    listeners["ready"]?.();
    expect(client.isConnected()).toBe(true);
  });

  it("ignores events from a stale connection after reset", async () => {
    const listenersByConnection: Array<Record<string, () => void>> = [];
    let failNext = false;

    const client = createHaWsClient({
      createConnection: async () => {
        const listeners: Record<string, () => void> = {};
        listenersByConnection.push(listeners);
        return {
          sendMessagePromise: async () => {
            if (failNext) {
              failNext = false;
              throw new Error("transport gone");
            }
            return { ok: true };
          },
          addEventListener: (event: "ready" | "disconnected", callback: () => void) => {
            listeners[event] = callback;
          }
        };
      }
    });

    await client.sendMessage({ type: "ping" });
    failNext = true;
    await expect(client.sendMessage({ type: "ping" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_ERROR"
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(false);

    // Second connection becomes current.
    await client.sendMessage({ type: "ping" });
    expect(client.isConnected()).toBe(true);

    // The abandoned first connection auto-reconnects in the background and
    // keeps firing events — those must not flip the active signal.
    listenersByConnection[0]?.["disconnected"]?.();
    expect(client.isConnected()).toBe(true);
    listenersByConnection[0]?.["ready"]?.();
    expect(client.isConnected()).toBe(true);

    // Events from the current connection still work.
    listenersByConnection[1]?.["disconnected"]?.();
    expect(client.isConnected()).toBe(false);
    listenersByConnection[1]?.["ready"]?.();
    expect(client.isConnected()).toBe(true);
  });

  it("maps connection failures to UPSTREAM_WS_CONNECT_ERROR", async () => {
    const client = createHaWsClient({
      createConnection: async () => {
        throw new Error("connection refused");
      }
    });

    await expect(client.sendMessage({ type: "ping" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_CONNECT_ERROR"
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(false);
  });

  it("maps request timeout to UPSTREAM_WS_TIMEOUT", async () => {
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () =>
          await new Promise((resolve) => {
            setTimeout(() => resolve({ ok: true }), 50);
          })
      }),
      requestTimeoutMs: 10
    });

    await expect(client.sendMessage({ type: "ping" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_TIMEOUT"
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(false);
  });

  it("drops a stale connection after request failure and reconnects on the next request", async () => {
    let connectCalls = 0;

    const client = createHaWsClient({
      createConnection: async () => {
        connectCalls += 1;
        if (connectCalls === 1) {
          return {
            sendMessagePromise: async (message: { type: string }) => {
              if (message.type === "broken") {
                throw new Error("socket closed");
              }
              return { echoed: message.type };
            }
          };
        }

        return {
          sendMessagePromise: async (message: { type: string }) => ({ echoed: `retry:${message.type}` })
        };
      }
    });

    await expect(client.sendMessage({ type: "ping" })).resolves.toEqual({ echoed: "ping" });
    await expect(client.sendMessage({ type: "broken" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_ERROR",
      message: "socket closed"
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(false);
    await expect(client.sendMessage({ type: "recover" })).resolves.toEqual({ echoed: "retry:recover" });
    expect(connectCalls).toBe(2);
  });

  it("collects subscription events until finish and unsubscribes", async () => {
    let unsubscribed = false;
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => ({ ok: true }),
        subscribeMessage: async (callback, message) => {
          callback({ type: "initial", data: { source: message.type } });
          callback({ type: "finish" });
          return () => {
            unsubscribed = true;
          };
        }
      })
    });

    await expect(client.collectMessageEvents({ type: "system_health/info" })).resolves.toEqual({
      events: [
        { type: "initial", data: { source: "system_health/info" } },
        { type: "finish" },
      ],
      truncated: false,
    });
    expect(unsubscribed).toBe(true);
  });

  // Envelope v2 window mode: a stream that never emits a finish event (mqtt
  // topics, event buses) must end at max_events / timeout with the events seen
  // so far, marked truncated — instead of the strict mode's hard error.
  it("returns partial events at max_events in window mode", async () => {
    let unsubscribed = false;
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => ({ ok: true }),
        subscribeMessage: async (callback) => {
          callback({ type: "one" });
          callback({ type: "two" });
          callback({ type: "three" });
          return () => {
            unsubscribed = true;
          };
        }
      })
    });

    await expect(
      client.collectMessageEvents(
        { type: "mqtt/subscribe", topic: "zigbee2mqtt/#" },
        { maxEvents: 2, onLimit: "return" }
      )
    ).resolves.toEqual({
      events: [{ type: "one" }, { type: "two" }],
      truncated: true,
    });
    expect(unsubscribed).toBe(true);
  });

  it("returns what it saw when the window times out in window mode", async () => {
    let unsubscribed = false;
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => ({ ok: true }),
        subscribeMessage: async (callback) => {
          callback({ type: "only" });
          return () => {
            unsubscribed = true;
          };
        }
      })
    });

    await expect(
      client.collectMessageEvents(
        { type: "mqtt/subscribe", topic: "zigbee2mqtt/#" },
        { timeoutMs: 30, onLimit: "return" }
      )
    ).resolves.toEqual({
      events: [{ type: "only" }],
      truncated: true,
    });
    expect(unsubscribed).toBe(true);
  });

  // A subscription that never acks must FAIL in window mode, not resolve as an
  // empty-but-successful sniff window — otherwise "nothing seen" is
  // indistinguishable from "never subscribed".
  it("fails in window mode when the subscription is never acknowledged", async () => {
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => ({ ok: true }),
        subscribeMessage: () => new Promise(() => undefined)
      })
    });

    await expect(
      client.collectMessageEvents(
        { type: "mqtt/subscribe", topic: "zigbee2mqtt/#" },
        { timeoutMs: 50, onLimit: "return" }
      )
    ).rejects.toThrow(/timed out/i);
  });

  it("still errors at max_events in the default strict mode", async () => {
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => ({ ok: true }),
        subscribeMessage: async (callback) => {
          callback({ type: "one" });
          callback({ type: "two" });
          return () => undefined;
        }
      })
    });

    await expect(
      client.collectMessageEvents({ type: "system_health/info" }, { maxEvents: 1 })
    ).rejects.toThrow(/exceeded 1 events/);
  });

  it("unsubscribes when event collection times out before subscription ack", async () => {
    let unsubscribed = false;
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => ({ ok: true }),
        subscribeMessage: async () => {
          await new Promise((resolve) => setTimeout(resolve, 30));
          return () => {
            unsubscribed = true;
          };
        }
      }),
      requestTimeoutMs: 10
    });

    await expect(client.collectMessageEvents({ type: "system_health/info" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_TIMEOUT"
    } satisfies Partial<HaWsClientError>);
    await new Promise((resolve) => setTimeout(resolve, 40));
    expect(unsubscribed).toBe(true);
  });
});

describe("ha ws client upstream error transparency", () => {
  it("surfaces structured HA command errors with code and message, keeping the connection", async () => {
    let connectCalls = 0;
    let calls = 0;

    const client = createHaWsClient({
      createConnection: async () => {
        connectCalls += 1;
        return {
          sendMessagePromise: async (message: { type: string }) => {
            calls += 1;
            if (calls === 1) {
              // home-assistant-js-websocket rejects command failures with
              // HA's raw error payload, not an Error instance.
              // eslint-disable-next-line @typescript-eslint/no-throw-literal
              throw { code: "unknown_command", message: "Unknown command." };
            }
            return { echoed: message.type };
          }
        };
      }
    });

    await expect(client.sendMessage({ type: "config/entity_registry/list" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_COMMAND_ERROR",
      message: "HA rejected 'config/entity_registry/list': unknown_command: Unknown command."
    } satisfies Partial<HaWsClientError>);

    // Command-level rejection must not tear down the healthy connection.
    expect(client.isConnected()).toBe(true);
    const second = await client.sendMessage<{ echoed: string }>({ type: "ping" });
    expect(second).toEqual({ echoed: "ping" });
    expect(connectCalls).toBe(1);
  });

  it("surfaces a structured command error that only carries a message", async () => {
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => {
          // eslint-disable-next-line @typescript-eslint/no-throw-literal
          throw { message: "Invalid format." };
        }
      })
    });

    await expect(client.sendMessage({ type: "ping" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_COMMAND_ERROR",
      message: "HA rejected 'ping': Invalid format."
    } satisfies Partial<HaWsClientError>);
  });

  it("maps numeric transport rejections to readable UPSTREAM_WS_ERROR messages and resets", async () => {
    let connectCalls = 0;

    const client = createHaWsClient({
      createConnection: async () => {
        connectCalls += 1;
        return {
          sendMessagePromise: async () => {
            // ERR_CONNECTION_LOST from home-assistant-js-websocket
            // eslint-disable-next-line @typescript-eslint/no-throw-literal
            throw 3;
          }
        };
      }
    });

    await expect(client.sendMessage({ type: "ping" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_ERROR",
      message: "Home Assistant WebSocket connection lost"
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(false);
    expect(connectCalls).toBe(1);
  });

  it("surfaces structured command errors from event collection, keeping the connection", async () => {
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => ({ ok: true }),
        subscribeMessage: async () => {
          // eslint-disable-next-line @typescript-eslint/no-throw-literal
          throw { code: "unknown_command", message: "Unknown command." };
        }
      })
    });

    await expect(
      client.collectMessageEvents({ type: "system_health/info" }, { timeoutMs: 100 })
    ).rejects.toMatchObject({
      code: "UPSTREAM_WS_COMMAND_ERROR",
      message: "HA rejected 'system_health/info': unknown_command: Unknown command."
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(true);
  });
});

describe("ha ws client wrapped connection-loss rejections", () => {
  it("maps the wrapped in-flight connection-loss shape to a readable transport error and resets", async () => {
    let connectCalls = 0;

    const client = createHaWsClient({
      createConnection: async () => {
        connectCalls += 1;
        return {
          sendMessagePromise: async () => {
            // _handleClose rejects in-flight commands with
            // messages.error(ERR_CONNECTION_LOST, "Connection lost").
            // eslint-disable-next-line @typescript-eslint/no-throw-literal
            throw {
              type: "result",
              success: false,
              error: { code: 3, message: "Connection lost" }
            };
          }
        };
      }
    });

    await expect(client.sendMessage({ type: "ping" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_ERROR",
      message: "Home Assistant WebSocket connection lost"
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(false);
    expect(connectCalls).toBe(1);
  });

  it("keeps wrapped string-code payloads classified as command errors", async () => {
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => {
          // eslint-disable-next-line @typescript-eslint/no-throw-literal
          throw {
            type: "result",
            success: false,
            error: { code: "not_allowed", message: "Not allowed." }
          };
        }
      })
    });

    await expect(client.sendMessage({ type: "config/x" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_COMMAND_ERROR",
      message: "HA rejected 'config/x': not_allowed: Not allowed."
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(true);
  });

  it("falls back to the wrapped message for unknown numeric transport codes", async () => {
    const client = createHaWsClient({
      createConnection: async () => ({
        sendMessagePromise: async () => {
          // eslint-disable-next-line @typescript-eslint/no-throw-literal
          throw { error: { code: 42, message: "Something odd" } };
        }
      })
    });

    await expect(client.sendMessage({ type: "ping" })).rejects.toMatchObject({
      code: "UPSTREAM_WS_ERROR",
      message: "Something odd"
    } satisfies Partial<HaWsClientError>);
    expect(client.isConnected()).toBe(false);
  });
});

describe("connection status classification", () => {
  it("reports never_connected before any attempt succeeds or fails", () => {
    const client = createHaWsClient({
      createConnection: async () => {
        throw new Error("unused");
      }
    });
    expect(client.getConnectionStatus()).toEqual({
      connected: false,
      disconnect_reason: "never_connected"
    });
  });

  it("classifies HaSocketAuthError (the real socket path) as auth", async () => {
    const client = createHaWsClient({
      createConnection: async () => {
        throw new HaSocketAuthError("HA rejected the long-lived access token");
      }
    });
    await expect(client.sendMessage({ type: "ping" })).rejects.toBeInstanceOf(HaWsClientError);
    expect(client.getConnectionStatus()).toEqual({ connected: false, disconnect_reason: "auth" });
  });

  it("classifies wrapped haws code 2 as auth and other failures as network", async () => {
    const authClient = createHaWsClient({
      createConnection: async () => {
        throw Object.assign(new Error("wrapped"), { cause: 2 });
      }
    });
    await expect(authClient.sendMessage({ type: "ping" })).rejects.toBeInstanceOf(HaWsClientError);
    expect(authClient.getConnectionStatus().disconnect_reason).toBe("auth");

    const networkClient = createHaWsClient({
      createConnection: async () => {
        throw new Error("ECONNREFUSED");
      }
    });
    await expect(networkClient.sendMessage({ type: "ping" })).rejects.toBeInstanceOf(HaWsClientError);
    expect(networkClient.getConnectionStatus()).toEqual({
      connected: false,
      disconnect_reason: "network"
    });
  });
});
