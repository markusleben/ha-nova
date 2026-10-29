import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createChatPhoneControl } from "../../nova/src/runtime/chat-phone-control.js";

// The App's client for the bridge's control channel (contract
// docs/work/2026-10-19-chat-bridge-contract.md, "Control channel (0.28)"),
// against a fake bridge on loopback.

const SECRET = "c".repeat(64);

let server: Server;
let port: number;
let seen: { method: string; url: string; auth: string | undefined; type: string | undefined; body: string }[];
let reply: (request: IncomingMessage, response: ServerResponse) => void;

beforeEach(async () => {
  seen = [];
  reply = (_request, response) => {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ phones: [], link: null }));
  };
  server = createServer((request, response) => {
    const chunks: Buffer[] = [];
    request.on("data", (chunk: Buffer) => chunks.push(chunk));
    request.on("end", () => {
      seen.push({ method: request.method ?? "", url: request.url ?? "", auth: request.headers.authorization, type: request.headers["content-type"], body: Buffer.concat(chunks).toString("utf8") });
      reply(request, response);
    });
  });
  port = await new Promise<number>((resolve) => server.listen(0, "127.0.0.1", () => resolve((server.address() as { port: number }).port)));
});

afterEach(async () => {
  await new Promise<void>((resolve) => server.close(() => resolve()));
});

const answer = (status: number, body: unknown) => (_request: IncomingMessage, response: ServerResponse) => {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(JSON.stringify(body));
};

describe("chat-app phone control client", () => {
  it("calls the control channel on loopback with the control secret only", async () => {
    const control = createChatPhoneControl(() => SECRET, port);
    expect(await control.phones("user 1")).toEqual({ phones: [], link: null });
    reply = answer(202, { job: "a".repeat(32), status: "running" });
    expect(await control.start("u1", { action: "move", phone: { device_id: "d".repeat(32) } })).toBe("a".repeat(32));
    reply = answer(200, { status: "done" });
    expect(await control.start("u1", { action: "adopt", phone: { service: "mobile_app_p", device_id: "d".repeat(32), name: "P" } })).toBeNull();
    reply = answer(200, { status: "declined", step: "old", phone: "iPhone", platform: "ios" });
    expect(await control.job("a".repeat(32))).toEqual({ status: "declined", step: "old", phone: "iPhone", platform: "ios" });
    expect(seen.map((s) => `${s.method} ${s.url}`)).toEqual(["GET /control/v1/phones?user=user%201", "POST /control/v1/phone", "POST /control/v1/phone", `GET /control/v1/phone/${"a".repeat(32)}`]);
    expect(seen.every((s) => s.auth === `Bearer ${SECRET}`)).toBe(true);
    expect(JSON.parse(seen[1]!.body)).toEqual({ user: "u1", action: "move", phone: { device_id: "d".repeat(32) } });
    expect(seen[1]!.type).toBe("application/json");
  });

  it("passes the bridge's refusals through with their code", async () => {
    reply = answer(409, { error: { code: "PHONE_EXISTS", message: "this user has a chat-app phone already" } });
    await expect(createChatPhoneControl(() => SECRET, port).start("u1", { action: "stop" })).rejects.toMatchObject({ status: 409, code: "PHONE_EXISTS", message: "this user has a chat-app phone already" });
    reply = (_request, response) => {
      response.writeHead(500);
      response.end("not json");
    };
    await expect(createChatPhoneControl(() => SECRET, port).phones("u1")).rejects.toMatchObject({ status: 500, code: "CHAT_PHONE_FAILED" });
  });

  it("asks nothing while chat apps are off, and says when the bridge can't be reached", async () => {
    await expect(createChatPhoneControl(() => null, port).phones("u1")).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" });
    expect(seen).toEqual([]);
    await new Promise<void>((resolve) => server.close(() => resolve()));
    await expect(createChatPhoneControl(() => SECRET, port).phones("u1")).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" });
    // nothing went out, so a change didn't happen either
    await expect(createChatPhoneControl(() => SECRET, port).start("u1", { action: "stop" })).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" });
    server = createServer(); // afterEach closes it
  });

  it("says a change may have happened when its answer is lost after it went out", async () => {
    const control = createChatPhoneControl(() => SECRET, port);
    reply = (_request, response) => response.socket?.destroy();
    await expect(control.start("u1", { action: "stop" })).rejects.toMatchObject({ status: 504, code: "CHAT_PHONE_UNKNOWN" });
    await expect(control.phones("u1")).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" }); // a read changed nothing
    reply = answer(202, { job: "../../x", status: "running" });
    await expect(control.start("u1", { action: "move", phone: { device_id: "d".repeat(32) } })).rejects.toMatchObject({ status: 504, code: "CHAT_PHONE_UNKNOWN" });
  });

  it("treats an answer it can't read as a bridge it can't reach", async () => {
    const control = createChatPhoneControl(() => SECRET, port);
    for (const body of [{ phones: "none", link: null }, { phones: [{ device_id: 1, name: "P" }], link: null }, { phones: [], link: { state: "ready" } }, { phones: [] }]) {
      reply = answer(200, body);
      await expect(control.phones("u1")).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" });
    }
    reply = answer(200, {});
    await expect(control.job("a".repeat(32))).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" });
  });

  it("says the bridge can't be reached when its answer is cut off", async () => {
    reply = (_request, response) => {
      response.writeHead(200, { "content-type": "application/json", "content-length": "100" });
      response.write('{"phones": [');
      setTimeout(() => response.socket?.destroy(), 10);
    };
    await expect(createChatPhoneControl(() => SECRET, port).phones("u1")).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" });
  });

  it("refuses an answer that is too large", async () => {
    reply = (_request, response) => {
      response.writeHead(200, { "content-type": "application/json" });
      response.end("x".repeat(70 * 1024));
    };
    await expect(createChatPhoneControl(() => SECRET, port).phones("u1")).rejects.toMatchObject({ status: 503, code: "CHAT_PHONE_UNAVAILABLE" });
  });
});
