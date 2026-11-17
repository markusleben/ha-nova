// The bed's Relay image started as a NOVA App (app mode), for ask-count.mjs:
// device keys, the NOVA page, and window confirmation exist only there. The
// bed has no Supervisor, so this stands in for the two parts of it the App
// needs: the self-API (the secure port's host mapping) and ingress. Ingress is
// the Relay's own ingress listener, given the Supervisor's peer address and
// the user the lane names in x-lane-user (the client's own identity headers
// are dropped), as scripts/e2e/pairing-harness.mjs does. Everything else is
// the real App: start.js, the safety-check worker, the registry. Runs inside
// the container only (ask-count.mjs starts it, ports on 127.0.0.1).
import { mkdirSync, writeFileSync } from "node:fs";
import { createServer, Server } from "node:http";

mkdirSync("/data/ha_nova_snapshots", { recursive: true });
writeFileSync("/data/options.json", "{}\n");

const SECURE_HOST_PORT = Number(process.env.LANE_SECURE_HOST_PORT);
const supervisor = createServer((req, res) => {
  res.setHeader("content-type", "application/json");
  res.end(JSON.stringify({ result: "ok", data: { version: "lane", version_latest: "lane", update_available: false, hostname: "", network: { "8792/tcp": SECURE_HOST_PORT } } }));
});
await new Promise((resolve) => supervisor.listen(8790, "127.0.0.1", resolve));
process.env.HA_NOVA_SUPERVISOR_BASE = "http://127.0.0.1:8790";

let ingress;
const listen = Server.prototype.listen;
Server.prototype.listen = function (...args) {
  if (args[0] === 8793) ingress = this;
  return listen.apply(this, args);
};
const { startRelay } = await import("/app/dist/src/runtime/start.js");
await startRelay();
if (!ingress) throw new Error("the App's ingress listener never started");

createServer((req, res) => {
  const user = req.headers["x-lane-user"];
  const raw = [];
  for (let i = 0; i < req.rawHeaders.length; i += 2) {
    if (!["x-lane-user", "x-remote-user-id", "x-ingress-path"].includes(req.rawHeaders[i].toLowerCase())) raw.push(req.rawHeaders[i], req.rawHeaders[i + 1]);
  }
  for (const name of ["x-lane-user", "x-remote-user-id", "x-ingress-path"]) delete req.headers[name];
  if (typeof user !== "string" || user === "") {
    res.statusCode = 401;
    res.end();
    return;
  }
  req.headers["x-remote-user-id"] = user;
  req.headers["x-ingress-path"] = "/api/hassio_ingress/lane";
  req.rawHeaders = [...raw, "x-remote-user-id", user, "x-ingress-path", "/api/hassio_ingress/lane"];
  Object.defineProperty(req.socket, "remoteAddress", { configurable: true, value: "172.30.32.2" });
  ingress.emit("request", req, res);
}).listen(8794, "0.0.0.0");
