import { spawn as spawnChild, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { chmodSync, existsSync, lstatSync, mkdirSync } from "node:fs";
import { request } from "node:http";
import { join } from "node:path";

import type { GateCheck, GateClient } from "../http/gate.js";
import type { RelayLogger } from "../http/server.js";
import { generateCredential } from "../security/device-credential.js";
import type { DeviceRegistry } from "../security/device-registry.js";
import { GATE_INSTALL_ID } from "../security/pairing-metadata.js";
import { writeFileAtomicSync } from "../storage/atomic-file.js";

// The safety-check worker (docs/work/2026-11-03-relay-typed-gate-spec.md §2):
// `ha-nova gate` on loopback, started and restarted like the chat bridge
// (chat-apps.ts), with the same minimal environment. It reads Home Assistant
// through the Relay with its own credential (loopback only, minted afresh by
// mint() on every start). Everything lives in owner-only /data/gate, its
// Unix socket too: no other listener can answer in the worker's place.
const MAX_ANSWER_BYTES = 192 << 20; // the worker's answer to its largest request (96 MiB, cli/command_gate.go)
const TIMEOUT_MS = 20_000; // above the worker's own 15 s check budget (cli/command_gate.go)
const BASE64 = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;

export interface GateWorker extends GateClient {
  start(): void;
  restart(): void; // after a registry reset: the next start mints a new credential
}

// What the worker reads from relay.json to reach the Relay.
export interface GateRelay {
  relay_url: string;
  spki_pin?: string;
  credential: string;
}

// App mode: the worker is a device under the reserved install id, over the
// pinned TLS listener; activating a new credential drops the previous one.
export function mintGateDevice(registry: DeviceRegistry, now: () => number, relayUrl: string, spkiPin: string): () => GateRelay {
  return () => {
    const { deviceId, secretDigest, credential } = generateCredential();
    registry.createPending({ deviceId, secretDigest, clientInstallId: GATE_INSTALL_ID, name: "Safety check", platform: "NOVA App", client: "ha-nova gate", createdAtMs: now() }, now());
    registry.activate(deviceId, now());
    return { relay_url: relayUrl, spki_pin: spkiPin, credential };
  };
}

export interface GateWorkerDeps {
  dataDir: string; // /data
  binary: string; // the ha-nova CLI in the image
  mint: () => GateRelay; // a fresh credential that replaces the previous one
  logger: RelayLogger;
  spawn?: typeof spawnChild;
  respawnMs?: number;
  timeoutMs?: number;
}

export function createGateWorker(deps: GateWorkerDeps): GateWorker {
  const file = (name: string): string => join(deps.dataDir, "gate", name);
  const socket = file("worker.sock");
  const spawn = deps.spawn ?? spawnChild;
  const secret = randomBytes(32).toString("hex"); // fresh on every Relay start
  let child: ChildProcess | null = null;
  let respawn: NodeJS.Timeout | undefined;
  let failures = 0;

  const launch = (): void => {
    if (child !== null) {
      return;
    }
    if (!existsSync(deps.binary)) {
      deps.logger.error("The safety-check worker is missing; every device request is refused", { binary: deps.binary });
      return;
    }
    const dir = join(deps.dataDir, "gate");
    mkdirSync(dir, { recursive: true, mode: 0o700 });
    const st = lstatSync(dir);
    if (!st.isDirectory() || (process.getuid !== undefined && st.uid !== process.getuid())) {
      throw new Error(`${dir} must be a directory owned by the Relay's user`);
    }
    chmodSync(dir, 0o700);
    writeFileAtomicSync(file("relay.json"), `${JSON.stringify(deps.mint())}\n`);
    writeFileAtomicSync(file("secret"), secret);
    mkdirSync(file("home"), { recursive: true, mode: 0o700 });
    const proc = spawn(
      deps.binary,
      ["gate", "--socket", socket, "--secret-file", file("secret"), "--relay-file", file("relay.json")],
      { stdio: ["ignore", "inherit", "inherit"], env: { PATH: process.env.PATH ?? "/usr/bin:/bin", HOME: file("home"), HA_NOVA_NO_BROWSER: "1", ...(process.env.TZ ? { TZ: process.env.TZ } : {}) } },
    );
    child = proc;
    const started = Date.now();
    let ended = false;
    const end = (why: string): void => {
      if (ended || child !== proc) {
        ended = true;
        return;
      }
      ended = true;
      child = null;
      failures = Date.now() - started > 60_000 ? 1 : failures + 1;
      deps.logger.warn("Safety-check worker stopped; restarting it", { why });
      startLater();
    };
    proc.on("error", (error) => end(error.message));
    proc.once("exit", (code, signal) => end(`exit ${code ?? signal}`));
  };
  const start = (): void => {
    try {
      launch();
    } catch (error) {
      failures += 1;
      deps.logger.error("Safety-check worker could not start", { error: String((error as Error).message) });
      startLater();
    }
  };
  // Restarts back off from 5 s to 5 min while the worker keeps failing.
  const startLater = (): void => {
    clearTimeout(respawn);
    respawn = setTimeout(start, Math.min((deps.respawnMs ?? 5_000) * 2 ** Math.max(0, failures - 1), 300_000));
    respawn.unref?.();
  };

  const post = (path: string, payload: unknown): Promise<Record<string, unknown> | null> => {
    const asked = child;
    if (asked === null) {
      return Promise.resolve(null);
    }
    const body = JSON.stringify(payload);
    return new Promise((resolve) => {
      let settled = false;
      const settle = (answer: Record<string, unknown> | null, warning?: string, context?: Record<string, unknown>): void => {
        if (!settled) {
          settled = true;
          clearTimeout(deadline);
          if (warning !== undefined) {
            req.destroy();
            deps.logger.warn(warning, { path, ...context });
          }
          resolve(answer);
        }
      };
      const req = request(
        { socketPath: socket, path, method: "POST", headers: { authorization: `Bearer ${secret}`, "content-type": "application/json", "content-length": Buffer.byteLength(body) } },
        (response) => {
          const chunks: Buffer[] = [];
          let size = 0;
          response.on("data", (chunk: Buffer) => {
            chunks.push(chunk);
            if ((size += chunk.length) > MAX_ANSWER_BYTES) {
              settle(null, "Safety-check worker answered unusably", { status: "over the size limit" });
            }
          });
          response.on("error", (error) => settle(null, "Safety-check worker did not answer", { error: error.message }));
          response.on("end", () => {
            let answer: unknown;
            try {
              answer = JSON.parse(Buffer.concat(chunks).toString("utf8"));
            } catch {
              answer = null;
            }
            if (child !== asked || response.statusCode !== 200 || typeof answer !== "object" || answer === null || Array.isArray(answer)) {
              settle(null, "Safety-check worker answered unusably", { status: response.statusCode });
              return;
            }
            settle(answer as Record<string, unknown>);
          });
        },
      );
      const deadline = setTimeout(() => settle(null, "Safety-check worker did not answer", { error: "timeout" }), deps.timeoutMs ?? TIMEOUT_MS);
      req.on("error", (error) => settle(null, "Safety-check worker did not answer", { error: error.message }));
      req.end(body);
    });
  };

  return {
    start,
    restart() {
      failures = 0;
      if (child !== null) {
        child.kill("SIGTERM"); // its exit starts it again
      } else {
        startLater();
      }
    },
    async check(principal, endpoint, body) {
      const who = principal.kind === "device" ? { kind: "device", device_id: principal.deviceId } : { kind: "legacy" };
      const answer = await post("/check", { principal: who, endpoint, body_b64: body.toString("base64") });
      const verdict = answer?.verdict;
      const kind = answer?.kind;
      if ((verdict !== "pass" && verdict !== "typed" && verdict !== "refuse") || (kind !== "read" && kind !== "write")) {
        return null;
      }
      const result: GateCheck = { verdict, kind };
      return typeof answer?.why === "string" ? { ...result, why: answer.why } : result;
    },
    async redact(endpoint, request, response) {
      const answer = await post("/redact", { endpoint, request_b64: request.toString("base64"), response_b64: response.toString("base64") });
      const encoded = answer?.response_b64;
      return typeof encoded === "string" && BASE64.test(encoded) ? Buffer.from(encoded, "base64") : null;
    },
  };
}
