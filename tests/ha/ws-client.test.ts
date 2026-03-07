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
  });
});
