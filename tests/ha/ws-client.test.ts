import { describe, expect, it } from "vitest";

import { HaWsClientError, createHaWsClient } from "../../nova/src/ha/ws-client.js";

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

    await expect(client.collectMessageEvents({ type: "system_health/info" })).resolves.toEqual([
      { type: "initial", data: { source: "system_health/info" } },
      { type: "finish" },
    ]);
    expect(unsubscribed).toBe(true);
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
