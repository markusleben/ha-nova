import { createServer, request as httpRequest, type Server } from "node:http";

import { afterEach, describe, expect, it } from "vitest";

import { createRouter } from "../../nova/src/http/router.js";
import { createRequestListener } from "../../nova/src/http/server.js";

const servers: Server[] = [];

afterEach(async () => {
  await Promise.all(
    servers
      .splice(0)
      .map(
        (server) =>
          new Promise<void>((resolve) => server.close(() => resolve())),
      ),
  );
});

describe("request listener authorization lifetime", () => {
  it("rejects a credential revoked while its request body is still arriving", async () => {
    const router = createRouter();
    let dispatched = 0;
    router.register("POST", "/mutate", () => {
      dispatched += 1;
      return { changed: true };
    });

    let authorized = true;
    let authCalls = 0;
    let releaseFirstAuth!: () => void;
    const firstAuth = new Promise<void>((resolve) => {
      releaseFirstAuth = resolve;
    });
    const listener = createRequestListener({
      router,
      authorize: () => {
        authCalls += 1;
        if (authCalls === 1) {
          releaseFirstAuth();
        }
        return authorized
          ? { ok: true, principal: { kind: "device", deviceId: "device-1" } }
          : {
              ok: false,
              status: 401,
              code: "UNAUTHORIZED",
              message: "Revoked",
            };
      },
    });
    const server = createServer(listener);
    servers.push(server);
    const port = await new Promise<number>((resolve) => {
      server.listen(0, "127.0.0.1", () =>
        resolve((server.address() as { port: number }).port),
      );
    });

    const response = new Promise<{ status: number; body: string }>(
      (resolve, reject) => {
        const outgoing = httpRequest(
          {
            host: "127.0.0.1",
            port,
            method: "POST",
            path: "/mutate",
            headers: { "content-type": "application/json" },
          },
          (incoming) => {
            const chunks: Buffer[] = [];
            incoming.on("data", (chunk: Buffer) => chunks.push(chunk));
            incoming.on("end", () => {
              resolve({
                status: incoming.statusCode ?? 0,
                body: Buffer.concat(chunks).toString("utf8"),
              });
            });
          },
        );
        outgoing.on("error", reject);
        outgoing.write('{"value":');
        void firstAuth.then(() => {
          authorized = false;
          outgoing.end("1}");
        });
      },
    );

    await expect(response).resolves.toMatchObject({
      status: 401,
      body: expect.stringContaining('"code":"UNAUTHORIZED"'),
    });
    expect(authCalls).toBe(2);
    expect(dispatched).toBe(0);
  });
});
