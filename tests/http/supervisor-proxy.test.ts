import { createServer, type IncomingMessage, type Server } from "node:http";
import type { AddressInfo } from "node:net";

import { afterEach, describe, expect, it } from "vitest";

import { HttpError } from "../../nova/src/http/errors.js";
import { HIDDEN_VALUE, SUPERVISOR_ROUTES } from "../../nova/src/http/handlers/supervisor-allowlist.js";
import { createSupervisorProxyHandler } from "../../nova/src/http/handlers/supervisor-proxy.js";
import type { RouteContext } from "../../nova/src/http/router.js";

interface Seen {
  method: string;
  url: string;
  accept: string | undefined;
  authorization: string | undefined;
  body: string;
}

const servers: Server[] = [];

afterEach(async () => {
  await Promise.all(servers.splice(0).map((s) => new Promise<void>((resolve) => s.close(() => resolve()))));
});

const STORED = { log_level: "info", password: "hunter2", logins: [{ username: "a", password: "b" }], broker: "core-mosquitto", mqtt_pass: "x9" };

async function fakeSupervisor(reply?: (req: IncomingMessage, seen: Seen) => { status?: number; body: string; type?: string } | undefined) {
  const seen: Seen[] = [];
  const server = createServer((req, res) => {
    let body = "";
    req.on("data", (c) => (body += c));
    req.on("end", () => {
      const entry = { method: req.method!, url: req.url!, accept: req.headers.accept, authorization: req.headers.authorization, body };
      seen.push(entry);
      const custom = reply?.(req, entry);
      if (custom) {
        res.writeHead(custom.status ?? 200, { "content-type": custom.type ?? "application/json" });
        res.end(custom.body);
        return;
      }
      if (req.url === "/addons/self/info") {
        res.end(JSON.stringify({ result: "ok", data: { slug: "local_my_relay_copy" } }));
        return;
      }
      if (/^\/addons\/[^/]+\/info$/.test(req.url!)) {
        res.end(JSON.stringify({ result: "ok", data: { slug: "core_mosquitto", options: STORED, schema: [{ name: "mqtt_pass", type: "string", format: "password" }, { name: "broker", type: "string" }] } }));
        return;
      }
      if (req.url!.includes("?lines=")) {
        res.writeHead(200, { "content-type": "text/plain" });
        res.end("line one\nline two\n");
        return;
      }
      res.end(JSON.stringify({ result: "ok", data: { url: req.url } }));
    });
  });
  servers.push(server);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", () => resolve()));
  const base = "http://127.0.0.1:" + (server.address() as AddressInfo).port;
  const handler = createSupervisorProxyHandler({ token: "sup-token", base });
  const call = (body: unknown) => handler({ body } as RouteContext) as Promise<{ status: number; body: unknown; truncated?: true }>;
  return { seen, call };
}

async function refusal(promise: Promise<unknown>): Promise<HttpError> {
  try {
    await promise;
  } catch (error) {
    expect(error).toBeInstanceOf(HttpError);
    return error as HttpError;
  }
  throw new Error("expected a refusal");
}

describe("POST /supervisor", () => {
  it("forwards every listed read with the Supervisor token", async () => {
    const { seen, call } = await fakeSupervisor();
    for (const route of SUPERVISOR_ROUTES.filter((r) => r.kind === "read")) {
      const path = route.path.replace("{slug}", "core_mosquitto");
      const reply = await call({ method: "GET", path });
      expect(reply.status).toBe(200);
      expect(seen.at(-1)).toMatchObject({ method: "GET", url: path, authorization: "Bearer sup-token" });
    }
  });

  it("hides App options marked as passwords or named like secrets", async () => {
    const { call } = await fakeSupervisor();
    const reply = await call({ method: "GET", path: "/addons/core_mosquitto/info" });
    const options = (reply.body as { data: { options: Record<string, unknown> } }).data.options;
    expect(options).toEqual({ log_level: "info", password: HIDDEN_VALUE, logins: [{ username: "a", password: HIDDEN_VALUE }], broker: "core-mosquitto", mqtt_pass: HIDDEN_VALUE });
    expect(JSON.stringify(reply.body)).not.toMatch(/hunter2|x9|"b"/);
  });

  it("hides secret-like keys in every reply but keeps booleans", async () => {
    const { call } = await fakeSupervisor(() => ({ body: JSON.stringify({ result: "ok", data: { wifi: { ssid: "home", psk: "wlanpass", psk_set: true, auth: "wpa-psk" }, ha_llat: "abc", refresh_token: null } }) }));
    const reply = await call({ method: "GET", path: "/network/info" });
    expect(reply.body).toEqual({ result: "ok", data: { wifi: { ssid: "home", psk: HIDDEN_VALUE, psk_set: true, auth: HIDDEN_VALUE }, ha_llat: HIDDEN_VALUE, refresh_token: null } });
  });

  it("reads logs as bounded plain text", async () => {
    const { seen, call } = await fakeSupervisor();
    expect(await call({ method: "GET", path: "/addons/core_mosquitto/logs", lines: 50 })).toEqual({ status: 200, body: "line one\nline two\n" });
    expect(seen.at(-1)).toMatchObject({ url: "/addons/core_mosquitto/logs?lines=50&no_colors", accept: "text/plain" });
    await call({ method: "GET", path: "/host/logs/identifiers/kernel" });
    expect(seen.at(-1)!.url).toBe("/host/logs/identifiers/kernel?lines=100&no_colors");
    const huge = await fakeSupervisor(() => ({ body: "x".repeat(600 * 1024), type: "text/plain" }));
    const cut = await huge.call({ method: "GET", path: "/core/logs" });
    expect(cut.truncated).toBe(true);
    expect((cut.body as string).length).toBe(512 * 1024);
    for (const lines of [0, 1001, 2.5, "10"]) {
      expect((await refusal(call({ method: "GET", path: "/core/logs", lines }))).code).toBe("VALIDATION_ERROR");
    }
  });

  it("refuses every request that is not listed exactly, before sending", async () => {
    const { seen, call } = await fakeSupervisor();
    const refused = [
      ["POST", "/dns/options"], ["GET", "/dns/info"], ["POST", "/security/options"], ["POST", "/supervisor/options"],
      ["POST", "/supervisor/update"], ["GET", "/auth/list"], ["POST", "/auth"], ["GET", "/backups"], ["POST", "/backups/x/restore/full"],
      ["POST", "/addons/core_mosquitto/stdin"], ["POST", "/addons/core_mosquitto/security"], ["POST", "/addons/core_mosquitto/start"],
      ["POST", "/addons/core_mosquitto/update"], ["POST", "/addons/core_mosquitto/sys_options"], ["POST", "/host/reboot"],
      ["POST", "/host/shutdown"], ["POST", "/os/update"], ["POST", "/os/datadisk/wipe"], ["POST", "/network/interface/eth0/update"],
      ["GET", "/mounts"], ["GET", "/docker/info"], ["GET", "/jobs/info"], ["POST", "/store/repositories"], ["GET", "/store/repositories"],
      ["POST", "/refresh_updates"], ["POST", "/core/restart"], ["GET", "/core/api/states"], ["GET", "/addons/self/info"],
      ["POST", "/addons/self/options"], ["GET", "/addons/core_mosquitto/logs/follow"], ["GET", "/host/logs/boots/-1"],
      ["GET", "/v2/apps"], ["GET", "/info/"], ["GET", "/info?x=1"], ["GET", "//info"], ["GET", "/addons/../dns/info"],
      ["GET", "/addons/%2e%2e/info"], ["GET", "/addons/.x/info"], ["POST", "/info"], ["GET", "/addons/core_mosquitto/install"],
      ["GET", "/host/logs/identifiers/../../dns"], ["GET", "http://evil/info"],
    ];
    for (const [method, path] of refused) {
      expect((await refusal(call({ method, path }))).code, method + " " + path).toBe("SUPERVISOR_REQUEST_REFUSED");
    }
    expect(seen).toEqual([]);
    for (const body of [null, [], { method: "PUT", path: "/info" }, { method: "GET", path: 1 }, { method: "GET", path: "/info", extra: 1 }, { method: "GET", path: "/info", data: {} }]) {
      expect((await refusal(call(body))).code).toBe("VALIDATION_ERROR");
    }
  });

  it("never writes to HA NOVA's own App", async () => {
    const { seen, call } = await fakeSupervisor();
    for (const path of ["/addons/2368fcfa_ha_nova_relay/options", "/addons/local_ha_nova_relay_b4/uninstall", "/store/addons/2368fcfa_ha_nova_relay/install", "/addons/local_my_relay_copy/uninstall"]) {
      const data = path.endsWith("options") ? { auto_update: false } : undefined;
      expect((await refusal(call({ method: "POST", path, ...(data ? { data } : {}) }))).code, path).toBe("SUPERVISOR_REQUEST_REFUSED");
    }
    expect(seen.filter((s) => s.method === "POST")).toEqual([]);
    expect((await call({ method: "GET", path: "/addons/2368fcfa_ha_nova_relay/logs" })).status).toBe(200);
  });

  it("fails closed when the Relay's own slug is unreadable", async () => {
    const { seen, call } = await fakeSupervisor((req) => (req.url === "/addons/self/info" ? { status: 500, body: "{}" } : undefined));
    expect((await refusal(call({ method: "POST", path: "/addons/core_mosquitto/uninstall" }))).code).toBe("SUPERVISOR_SELF_UNREADABLE");
    expect(seen.filter((s) => s.method === "POST")).toEqual([]);
  });

  it("installs in the background and uninstalls with only remove_config", async () => {
    const { seen, call } = await fakeSupervisor();
    await call({ method: "POST", path: "/store/addons/core_mosquitto/install" });
    expect(seen.at(-1)).toMatchObject({ method: "POST", url: "/store/addons/core_mosquitto/install", body: '{"background":true}' });
    await call({ method: "POST", path: "/addons/core_mosquitto/uninstall", data: { remove_config: true } });
    expect(seen.at(-1)).toMatchObject({ url: "/addons/core_mosquitto/uninstall", body: '{"remove_config":true}' });
    for (const [path, data] of [["/store/addons/core_mosquitto/install", { background: false }], ["/addons/core_mosquitto/uninstall", { remove_config: "yes" }], ["/addons/core_mosquitto/uninstall", { force: true }]] as const) {
      expect((await refusal(call({ method: "POST", path, data }))).code).toBe("VALIDATION_ERROR");
    }
  });

  it("merges an options change onto the stored options and toggles auto-update", async () => {
    const { seen, call } = await fakeSupervisor();
    await call({ method: "POST", path: "/addons/core_mosquitto/options", data: { options: { log_level: "debug" } } });
    expect(JSON.parse(seen.at(-1)!.body)).toEqual({ options: { ...STORED, log_level: "debug" } });
    await call({ method: "POST", path: "/addons/core_mosquitto/options", data: { auto_update: true } });
    expect(seen.at(-1)!.body).toBe('{"auto_update":true}');
    const before = seen.length;
    for (const data of [{ options: { password: HIDDEN_VALUE } }, { boot: "manual" }, { options: {}, auto_update: true }, { options: [] }, { auto_update: "yes" }, { network: { "80/tcp": 80 } }, { watchdog: false }]) {
      const error = await refusal(call({ method: "POST", path: "/addons/core_mosquitto/options", data }));
      expect(["VALIDATION_ERROR", "SUPERVISOR_REQUEST_REFUSED"]).toContain(error.code);
    }
    expect(seen.slice(before).filter((s) => s.method === "POST")).toEqual([]);
  });
});

describe("POST /supervisor redaction hardening", () => {
  it("cuts stored values out of a refused options write", async () => {
    const { call } = await fakeSupervisor((req, seen) =>
      req.method === "POST" && req.url === "/addons/core_mosquitto/options"
        ? { status: 400, body: JSON.stringify({ result: "error", message: "bad value for port. Got {'password': 'hunter2', 'mqtt_pass': 'x9'}", extra_fields: { validation_error: "hunter2" } }) }
        : undefined);
    const reply = await call({ method: "POST", path: "/addons/core_mosquitto/options", data: { options: { log_level: "nope" } } });
    expect(reply.status).toBe(400);
    expect(JSON.stringify(reply.body)).not.toMatch(/hunter2|x9|extra_fields|Got/);
    expect((reply.body as { message: string }).message).toContain("bad value for port");
  });

  it("hides nested password options, environment values, URL user info, and compound keys", async () => {
    const info = { result: "ok", data: { slug: "x_app", schema: [{ name: "server", type: "schema", schema: [{ name: "wpa", type: "string", format: "password" }] }],
      options: { server: { wpa: "s3cret-wpa", host: "h" }, env_vars: [{ name: "GF_ADMIN_PASSWORD", value: "graf" }, { name: "TZ", value: "Europe/Berlin" }],
        adminPin: "1234", dbpass: "db1", access_code: "4711", url: "https://user:ghp_x@github.com/me/apps", odd: "https://name@example.com:pw_odd@host/p" }, error_key: "app_x" } };
    const { call } = await fakeSupervisor((req) => (req.url === "/addons/x_app/info" ? { body: JSON.stringify(info) } : undefined));
    const text = JSON.stringify((await call({ method: "GET", path: "/addons/x_app/info" })).body);
    expect(text).not.toMatch(/s3cret-wpa|graf"|1234|db1|4711|ghp_x|pw_odd/);
    expect(text).toContain("Europe/Berlin");
    expect(text).toContain('"host":"h"');
    expect(text).toContain('"error_key":"app_x"');
  });

  it("refuses !secret references and changes to an option holding a hidden value", async () => {
    const { seen, call } = await fakeSupervisor();
    for (const options of [{ broker: "!secret wifi_password" }, { logins: [{ username: "a", password: "new" }] }, { broker: "mqtt://(hidden: a stored code, password, or key)@core-mosquitto" }]) {
      expect((await refusal(call({ method: "POST", path: "/addons/core_mosquitto/options", data: { options } }))).code).toBe("SUPERVISOR_REQUEST_REFUSED");
    }
    expect(seen.filter((s) => s.method === "POST")).toEqual([]);
  });

  it("cuts an App's options out of a Supervisor log line and keeps a plain-text refusal", async () => {
    const log = await fakeSupervisor(() => ({ body: "ok line\nApp core_x has invalid options: Got {'password': 'hunter2'}\n", type: "text/plain" }));
    const body = (await log.call({ method: "GET", path: "/supervisor/logs" })).body as string;
    expect(body).toContain("ok line");
    expect(body).not.toContain("hunter2");
    const denied = await fakeSupervisor(() => ({ status: 403, body: "403: Forbidden", type: "text/plain" }));
    expect(await denied.call({ method: "GET", path: "/addons" })).toEqual({ status: 403, body: "403: Forbidden" });
  });
});
