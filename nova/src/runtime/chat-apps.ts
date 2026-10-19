import { spawn as spawnChild, type ChildProcess } from "node:child_process";
import { randomBytes } from "node:crypto";
import { existsSync, lstatSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import type { SupervisorClient } from "../ha/supervisor-client.js";
import type { RelayLogger } from "../http/server.js";
import { digestSecret, generateCredential, parseCredential } from "../security/device-credential.js";
import type { DeviceRegistry } from "../security/device-registry.js";
import { BRIDGE_INSTALL_ID } from "../security/pairing-metadata.js"; // re-minting replaces the old record
import { readPrivateFileSync, writeFileAtomicSync } from "../storage/atomic-file.js";
import type { CoreProxyRequest, CoreProxyResponse } from "../types/api.js";

// Chat apps (spec docs/work/2026-10-18-onboarding-0.27-spec.md §11; contract
// docs/work/2026-10-19-chat-bridge-contract.md, "App side"): the owner's
// switch, the bridge secret, the internal device credential, the Supervisor
// discovery message, the integration in /config/custom_components/ha_nova,
// and the `ha-nova serve` child. Built only when HA_NOVA_CHAT_APPS=1.
// Everything lives in /data/chat-apps; the bridge's own --data-dir is bridge/.

export const BRIDGE_PORT = 8794;
const SERVICE = "ha_nova";
const SECRET = /^[0-9a-f]{64}$/;

// The connector address: Home Assistant Link's while its remote access is on
// (cloud/status), else Home Assistant's external URL when it is public HTTPS
// (get_config); null when neither. Never http://. Same rules as
// cli/chat_apps.go chatAppsBase. Add to Claude is Claude's documented
// install link: claude.ai only, the address only, never a token.
export function chatConnectorUrl(status: unknown, config: unknown): string | null {
  const s = status as { logged_in?: unknown; remote_connected?: unknown; remote_domain?: unknown; prefs?: { remote_enabled?: unknown } } | null;
  const link = s?.logged_in === true && s.remote_connected === true && s.prefs?.remote_enabled === true && typeof s.remote_domain === "string";
  const external = /^https:\/\/([^/?#@\s]+?)\/?$/i.exec(String((config as { external_url?: unknown } | null)?.external_url ?? ""));
  const host = (link ? String(s.remote_domain) : external?.[1] ?? "").trim().toLowerCase().replace(/\.(?=$|:)/, "");
  const name = /^([a-z0-9-]{1,63}(?:\.[a-z0-9-]{1,63})+)(:\d{1,5})?$/.exec(host)?.[1] ?? "";
  const local = /\.(local|lan|home|internal|localdomain|localhost|home\.arpa)$|^[\d.]+$/.test(name) || (!link && name.endsWith(".nabu.casa"));
  return name === "" || local ? null : `https://${host}/api/ha_nova/mcp`;
}
export const claudeConnectorLink = (url: string): string =>
  `https://claude.ai/customize/connectors?modal=add-custom-connector&connectorName=HA%20NOVA&connectorUrl=${encodeURIComponent(url)}`;

export class ChatAppsError extends Error {
  constructor(readonly status: number, readonly code: string, message: string) {
    super(message);
  }
}

export interface PhoneLinkInput {
  service: string;
  device_id: string;
  name: string;
}

export interface ChatAppsStatus {
  available: boolean;
  enabled: boolean;
  unfinished: boolean; // off, but the discovery message is still published
  readOnly: boolean;
  chatCodes: boolean;
  allowedUsers: string[] | null; // null: the owner only
  ownerPhone: string | null; // the owner's linked phone's name
}

export interface ChatApps {
  status(ownerId: string): ChatAppsStatus;
  turnOn(allowRestart: boolean): Promise<{ restart: boolean }>;
  turnOff(): Promise<void>;
  setSwitches(change: { readOnly?: boolean; chatCodes?: boolean }): void;
  setOwnerPhone(ownerId: string, link: PhoneLinkInput | null): void;
  restartSoon(): void;
  boot(): Promise<void>;
  restartBridge(): void;
  bridgeDeviceId(): string | null;
}

export interface ChatAppsDeps {
  dataDir: string; // /data
  configRoot: string; // the mounted HA config, "" when none
  binary: string; // the ha-nova CLI in the image
  integrationSource: string; // the integration folder in the image (PR B3)
  securePort: number;
  spkiPin: string;
  registry: DeviceRegistry;
  supervisor: SupervisorClient;
  core: { request(input: CoreProxyRequest): Promise<CoreProxyResponse> };
  now: () => number;
  logger: RelayLogger;
  spawn?: typeof spawnChild;
  respawnMs?: number;
}

interface State {
  enabled: boolean;
  discovery_uuid?: string;
}

export function createChatApps(deps: ChatAppsDeps): ChatApps {
  const dir = join(deps.dataDir, "chat-apps");
  const file = (name: string): string => join(dir, name);
  const bridgeDir = file("bridge");
  const spawn = deps.spawn ?? spawnChild;
  let child: ChildProcess | null = null;
  let wanted = false;
  let respawn: NodeJS.Timeout | undefined;
  let failures = 0;
  let restartTimer: NodeJS.Timeout | undefined; // turning off cancels a restart not yet sent
  // On and off never interleave (the NOVA page and the CLI may both ask).
  let queue: Promise<unknown> = Promise.resolve();
  const serial = <T>(task: () => Promise<T>): Promise<T> => {
    const run = queue.then(task, task);
    queue = run.catch(() => {});
    return run;
  };

  const readJson = <T>(path: string, fallback: T): T => {
    const raw = readPrivateFileSync(path, 256 * 1024);
    return raw === null ? fallback : (JSON.parse(raw.toString("utf8")) as T);
  };
  const writeJson = (path: string, value: unknown): void => writeFileAtomicSync(path, `${JSON.stringify(value)}\n`);
  const load = (): State => readJson<State>(file("state.json"), { enabled: false });
  const save = (state: State): void => writeJson(file("state.json"), state);
  const settings = (): { allowed_users?: string[]; read_only?: boolean; chat_codes?: boolean } =>
    readJson(join(bridgeDir, "chat-apps.json"), {});
  const available = (): boolean =>
    existsSync(deps.binary) && existsSync(join(deps.integrationSource, "manifest.json"));

  // The 256-bit secret the integration sends: made once, kept across restarts
  // and updates; only turning chat apps off deletes it.
  const ensureSecret = (): string => {
    const current = readPrivateFileSync(file("secret"), 128)?.toString("utf8") ?? "";
    if (SECRET.test(current)) {
      return current;
    }
    const secret = randomBytes(32).toString("hex");
    writeFileAtomicSync(file("secret"), secret);
    return secret;
  };

  // The internal device credential: an ordinary device record in the Relay's
  // own registry, minted here instead of by pairing (no code: the Relay and the
  // bridge share this container), reused while it still resolves.
  // Found by its reserved install id (pairing refuses it), never via a file.
  const bridgeDeviceId = (): string | null =>
    deps.registry.list().find((d) => d.clientInstallId === BRIDGE_INSTALL_ID)?.deviceId ?? null;
  const ensureCredential = (): void => {
    const relay = readJson<{ credential?: string; relay_url?: string; spki_pin?: string } | null>(file("relay.json"), null);
    const url = `https://127.0.0.1:${deps.securePort}`;
    const current = parseCredential(relay?.credential);
    if (current && deps.registry.resolveDeviceSecret(current.deviceId, digestSecret(current.secret), deps.now())) {
      if (relay?.relay_url !== url || relay.spki_pin !== deps.spkiPin) { // a new TLS identity keeps the credential
        writeJson(file("relay.json"), { relay_url: url, spki_pin: deps.spkiPin, credential: relay?.credential });
      }
      return;
    }
    const minted = generateCredential();
    const { deviceId, secretDigest } = minted;
    const record = { deviceId, secretDigest, clientInstallId: BRIDGE_INSTALL_ID, name: "Chat apps", platform: "NOVA App", client: "ha-nova serve" };
    deps.registry.createPending({ ...record, createdAtMs: deps.now() }, deps.now());
    deps.registry.activate(minted.deviceId, deps.now());
    writeJson(file("relay.json"), { relay_url: url, spki_pin: deps.spkiPin, credential: minted.credential });
  };

  const startChild = (): void => {
    wanted = true;
    if (child !== null) {
      return;
    }
    ensureCredential();
    mkdirSync(file("home"), { recursive: true, mode: 0o700 });
    // A minimal environment: the App's Supervisor and Home Assistant tokens
    // never reach the bridge; it talks to Home Assistant only through the Relay.
    const proc = spawn(
      deps.binary,
      ["serve", "--listen", `0.0.0.0:${BRIDGE_PORT}`, "--data-dir", bridgeDir,
        "--secret-file", file("secret"), "--relay-file", file("relay.json")],
      { stdio: ["ignore", "inherit", "inherit"], env: { PATH: process.env.PATH ?? "/usr/bin:/bin", HOME: file("home"), HA_NOVA_NO_BROWSER: "1", ...(process.env.TZ ? { TZ: process.env.TZ } : {}) } },
    );
    child = proc;
    clearTimeout(respawn);
    const started = Date.now();
    let ended = false;
    const end = (why: string): void => {
      if (ended) {
        return;
      }
      ended = true;
      if (child !== proc) {
        return; // a stopped process: its end changes nothing
      }
      child = null;
      failures = Date.now() - started > 60_000 ? 1 : failures + 1;
      if (wanted) {
        deps.logger.warn("Chat-app bridge stopped; restarting it", { why });
        startLater();
      }
    };
    proc.on("error", (error) => end(error.message));
    proc.once("exit", (code, signal) => end(`exit ${code ?? signal}`));
  };
  // Restarts back off from 5 s to 5 min while serve keeps failing.
  const startLater = (): void => {
    clearTimeout(respawn);
    respawn = setTimeout(() => {
      try {
        startChild();
      } catch (error) {
        failures += 1;
        deps.logger.error("Chat-app bridge could not start", { error: String((error as Error).message) });
        startLater();
      }
    }, Math.min((deps.respawnMs ?? 5_000) * 2 ** Math.max(0, failures - 1), 300_000));
  };
  // serve finishes running calls for up to 5 s; a hung one must not keep the port.
  const kill = (proc: ChildProcess | null): void => {
    proc?.kill("SIGTERM");
    setTimeout(() => proc?.exitCode === null && proc.signalCode === null && proc.kill("SIGKILL"), 7_000).unref();
  };
  const stopChild = (): void => {
    wanted = false;
    clearTimeout(respawn);
    const proc = child;
    child = null;
    kill(proc);
  };
  // Turning off, in this order: serve stops and its credential is revoked
  // before any file can fail (so a request still finishing can no longer
  // reach the Relay); the state says off before the revoke can fail (so a
  // start-up never resumes); the secret goes after the message that carries it.
  const off = async (): Promise<void> => {
    stopChild();
    clearTimeout(restartTimer);
    let state: State = { enabled: false };
    let unreadable: unknown = null;
    try {
      state = load();
      save({ ...state, enabled: false });
    } catch (error) {
      unreadable = error;
    }
    for (const device of deps.registry.list()) {
      if (device.clientInstallId === BRIDGE_INSTALL_ID) {
        deps.registry.revoke(device.deviceId);
      }
    }
    rmSync(file("relay.json"), { force: true });
    if (unreadable !== null) {
      throw unreadable;
    }
    if (state.discovery_uuid !== undefined) {
      await deps.supervisor.deleteDiscovery(state.discovery_uuid);
    }
    rmSync(file("secret"), { force: true });
    removeIntegration();
    save({ enabled: false });
  };

  // custom_components/ha_nova is the ONLY path written in /config: the parent
  // must be a real directory (never a symlink), the old folder is removed (a
  // symlink there is unlinked, never followed), and every file is created
  // exclusively inside the fresh folder.
  const customComponents = (create: boolean): string | null => {
    if (deps.configRoot === "" || !existsSync(join(deps.configRoot, "configuration.yaml"))) {
      if (!create) {
        return null;
      }
      throw new ChatAppsError(503, "CONFIG_UNAVAILABLE", "Home Assistant's configuration folder is not available to the App.");
    }
    const parent = join(deps.configRoot, "custom_components");
    const st = lstatSync(parent, { throwIfNoEntry: false });
    if (st === undefined && create) {
      mkdirSync(parent, { mode: 0o755 });
    } else if (st === undefined) {
      return null;
    } else if (!st.isDirectory()) {
      throw new ChatAppsError(409, "CONFIG_UNSAFE", "custom_components in Home Assistant's configuration is not a plain folder.");
    }
    return parent;
  };
  const removeIntegration = (): void => {
    const parent = customComponents(false);
    if (parent !== null) {
      rmSync(join(parent, "ha_nova"), { recursive: true, force: true });
    }
  };
  const copyTree = (from: string, to: string): void => {
    mkdirSync(to, { mode: 0o755 });
    for (const entry of readdirSync(from, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        copyTree(join(from, entry.name), join(to, entry.name));
      } else if (entry.isFile()) {
        writeFileSync(join(to, entry.name), readFileSync(join(from, entry.name)), { mode: 0o644, flag: "wx" });
      }
    }
  };

  const core = async (method: "GET" | "POST", path: string): Promise<unknown> => {
    const response = await deps.core.request({ method, path, ...(method === "POST" ? { body: {} } : {}) });
    if (response.status !== 200) {
      throw new Error(`Home Assistant ${path} answered ${response.status}`);
    }
    return response.body;
  };

  const publish = async (): Promise<void> => {
    const { hostname } = await deps.supervisor.getSelfInfo();
    if (hostname === "") {
      throw new Error("Supervisor reported no App hostname");
    }
    const uuid = await deps.supervisor.sendDiscovery(SERVICE, { host: hostname, port: BRIDGE_PORT, secret: ensureSecret() });
    save({ ...load(), discovery_uuid: uuid });
  };

  return {
    status(ownerId) {
      const state = load();
      const current = settings();
      const name = readJson<Record<string, { name?: unknown } | null>>(join(bridgeDir, "phone-links.json"), {})[ownerId]?.name;
      return {
        available: available(),
        enabled: state.enabled,
        unfinished: !state.enabled && state.discovery_uuid !== undefined,
        readOnly: current.read_only === true,
        chatCodes: current.chat_codes !== false,
        allowedUsers: current.allowed_users ?? null,
        ownerPhone: typeof name === "string" ? name : null,
      };
    },

    // Every step is idempotent: a failure leaves a state that the same action
    // ([Try again]) completes. The restart itself is restartSoon(), after the
    // caller answered: Home Assistant serves the ingress connection.
    turnOn: (allowRestart) => serial(async () => {
      if (!available()) {
        throw new ChatAppsError(503, "CHAT_APPS_UNAVAILABLE", "Chat apps are not available in this NOVA App version.");
      }
      const components = (await core("GET", "/api/config")) as { components?: unknown };
      if (!Array.isArray(components?.components)) {
        throw new Error("Home Assistant /api/config has no component list");
      }
      const [year = 0, month = 0] = String((components as { version?: unknown }).version ?? "").split(".").map(Number);
      if (year * 100 + month < 202610) {
        throw new ChatAppsError(409, "HA_TOO_OLD", "Chat apps need Home Assistant 2026.10 or newer. Update Home Assistant first.");
      }
      const restart = !components.components.includes(SERVICE);
      if (restart && !allowRestart) {
        throw new ChatAppsError(409, "RESTART_CONSENT_REQUIRED", "Home Assistant restarts once (about a minute). Ask the owner first.");
      }
      if (restart && ((await core("POST", "/api/config/core/check_config")) as { result?: unknown })?.result !== "valid") {
        throw new ChatAppsError(409, "HA_CONFIG_INVALID", "Home Assistant's configuration has an error, so it can't restart. Fix it in Home Assistant, then try again.");
      }
      const wasOn = load().enabled;
      failures = 0;
      try {
        ensureSecret();
        const parent = customComponents(true)!;
        removeIntegration();
        copyTree(deps.integrationSource, join(parent, "ha_nova"));
        startChild();
        await publish();
        save({ ...load(), enabled: true });
      } catch (error) {
        if (!wasOn) {
          // Leave nothing behind: off is what the page shows, and [Turn on] the retry.
          await off().catch((cleanup: unknown) => deps.logger.error("Chat apps: cleanup after a failed turn-on failed", { error: String((cleanup as Error).message) }));
        } else if (child === null && wanted) {
          startLater();
        }
        throw error;
      }
      return { restart };
    }),

    turnOff: () => serial(off),

    setSwitches(change) {
      const next = { ...settings(), ...(change.readOnly !== undefined ? { read_only: change.readOnly } : {}), ...(change.chatCodes !== undefined ? { chat_codes: change.chatCodes } : {}) };
      writeJson(join(bridgeDir, "chat-apps.json"), next);
    },

    setOwnerPhone(ownerId, link) {
      const links = readJson<Record<string, unknown>>(join(bridgeDir, "phone-links.json"), {});
      if (link === null) {
        delete links[ownerId];
      } else {
        links[ownerId] = { service: link.service, device_id: link.device_id, user_id: ownerId, name: link.name };
      }
      writeJson(join(bridgeDir, "phone-links.json"), links);
    },

    // homeassistant.restart checks the configuration again before it stops.
    // The call itself is cut when Home Assistant goes down, so only an answer
    // from Home Assistant that refuses counts as a failure (logged: the owner's
    // request was answered already).
    restartSoon() {
      const refused = (response: CoreProxyResponse): void => {
        if (response.status >= 400 && response.status <= 500) {
          deps.logger.error("Home Assistant refused the restart for chat apps", { status: response.status });
        }
      };
      clearTimeout(restartTimer);
      restartTimer = setTimeout(() => {
        if (!wanted) {
          return; // turned off meanwhile
        }
        deps.core.request({ method: "POST", path: "/api/services/homeassistant/restart", body: {} })
          .then(refused, () => deps.logger.info?.("Home Assistant is restarting for chat apps"));
      }, 1_000);
    },

    // Never fatal: a damaged chat-app file must not take the Relay down.
    boot: () => serial(async () => {
      try {
        if (!load().enabled) {
          return;
        }
        if (!available()) {
          deps.logger.error("Chat apps are on, but this App version has no bridge; turn them off on the NOVA page");
          return;
        }
        startChild();
        await publish(); // Supervisor keeps one message per App and service: a repeat is a no-op
      } catch (error) {
        deps.logger.error("Chat apps could not resume", { error: String((error as Error).message) });
        if (wanted && child === null) {
          startLater(); // e.g. the credential could not be minted yet
        }
      }
    }),

    // After a registry reset: the (re)start mints a fresh credential.
    restartBridge() {
      failures = 0;
      if (child !== null) {
        kill(child); // its exit restarts it
      } else if (wanted) {
        startLater();
      }
    },

    bridgeDeviceId,
  };
}
