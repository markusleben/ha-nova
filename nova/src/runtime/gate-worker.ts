import { spawn as spawnChild, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { existsSync, mkdirSync } from "node:fs";
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
// through the Relay as its own internal device (reserved install id, loopback
// only, minted afresh on every start). Everything lives in /data/gate.

export const GATE_PORT = 8796;
const TIMEOUT_MS = 5_000;
const BASE64 = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;

export interface GateWorker extends GateClient {
  start(): void;
  restart(): void; // after a registry reset: the next start mints a new credential
}

export interface GateWorkerDeps {
  dataDir: string; // /data
  binary: string; // the ha-nova CLI in the image
  securePort: number;
  spkiPin: string;
  registry: DeviceRegistry;
  now: () => number;
  logger: RelayLogger;
  port?: number;
  spawn?: typeof spawnChild;
  respawnMs?: number;
  timeoutMs?: number;
}

export function createGateWorker(deps: GateWorkerDeps): GateWorker {
  const file = (name: string): string => join(deps.dataDir, "gate", name);
  const port = deps.port ?? GATE_PORT;
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
    // Activating a credential under the reserved id drops the previous one.
    const minted = generateCredential();
    deps.registry.createPending({ deviceId: minted.deviceId, secretDigest: minted.secretDigest, clientInstallId: GATE_INSTALL_ID, name: "Safety check", platform: "NOVA App", client: "ha-nova gate", createdAtMs: deps.now() }, deps.now());
    deps.registry.activate(minted.deviceId, deps.now());
    writeFileAtomicSync(file("relay.json"), `${JSON.stringify({ relay_url: `https://127.0.0.1:${deps.securePort}`, spki_pin: deps.spkiPin, credential: minted.credential })}\n`);
    writeFileAtomicSync(file("secret"), secret);
    mkdirSync(file("home"), { recursive: true, mode: 0o700 });
    const proc = spawn(
      deps.binary,
      ["gate", "--listen", `127.0.0.1:${port}`, "--secret-file", file("secret"), "--relay-file", file("relay.json")],
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

  const post = async (path: string, payload: unknown): Promise<Record<string, unknown> | null> => {
    const asked = child;
    if (asked === null) {
      return null;
    }
    try {
      const response = await fetch(`http://127.0.0.1:${port}${path}`, {
        method: "POST",
        headers: { authorization: `Bearer ${secret}`, "content-type": "application/json" },
        body: JSON.stringify(payload),
        signal: AbortSignal.timeout(deps.timeoutMs ?? TIMEOUT_MS),
      });
      const answer: unknown = await response.json();
      if (child !== asked || response.status !== 200 || typeof answer !== "object" || answer === null || Array.isArray(answer)) {
        deps.logger.warn("Safety-check worker answered unusably", { path, status: response.status });
        return null;
      }
      return answer as Record<string, unknown>;
    } catch (error) {
      deps.logger.warn("Safety-check worker did not answer", { path, error: String((error as Error).message) });
      return null;
    }
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
