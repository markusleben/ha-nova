import { randomBytes } from "node:crypto";
import { type Server } from "node:http";
import { dirname, join } from "node:path";

import {
  createConnection,
  createLongLivedTokenAuth,
  type HaWebSocket,
} from "home-assistant-js-websocket";

import { createApp, type App } from "../index.js";
import { buildAppMode } from "./app-mode.js";
import { createGateWorker, type GateWorker, type GateWorkerDeps } from "./gate-worker.js";
import { createPhoneLinkStore } from "./gate-phone-store.js";
import { readAppOptions, type AppOptions } from "../config/app-options.js";
import {
  resolveFileAccess,
  type FileAccessConfig,
} from "../config/file-access.js";
import { loadEnv, type EnvConfig } from "../config/env.js";
import { createHawsSocket } from "../ha/socket.js";
import { createHaRestClient, type HaRestClient } from "../ha/rest-client.js";
import {
  createHaWsClient,
  type HaWsClient,
  type HaWsConnection,
  type HaWsRequest,
} from "../ha/ws-client.js";
import type { CoreProxyRequest, CoreProxyResponse } from "../types/api.js";
import {
  createConsoleLogger,
  fileAccessOption,
  levelAtLeast,
  listenServer,
  probeConfigRoot,
  type Logger,
} from "./start-support.js";

export { createConsoleLogger, levelAtLeast } from "./start-support.js";

export interface UpstreamAuth {
  token: string;
  source: "supervisor_token" | "env_ha_llat";
}

export interface RuntimeBootstrapResult {
  app: App;
  gate: GateWorker; // started once the Relay listens
  env: EnvConfig;
  appOptions: AppOptions;
  fileAccess: FileAccessConfig;
  upstreamAuth: UpstreamAuth;
}

export interface RuntimeDependencies {
  loadEnv?: () => EnvConfig;
  readAppOptions?: (path: string) => AppOptions;
  createWsClient?: (input: RuntimeClientInput) => HaWsClient;
  createRestClient?: (input: RuntimeClientInput) => HaRestClient;
  createGate?: (deps: GateWorkerDeps) => GateWorker;
  logger?: Logger;
  listen?: (server: Server, port: number) => Promise<void>;
}

export interface RuntimeClientInput {
  env: EnvConfig;
  appOptions: AppOptions;
  upstreamAuth: UpstreamAuth;
}

// What both modes share: options, the upstream credential, the HA clients,
// and file access.
interface RuntimePrelude extends RuntimeClientInput {
  wsClient: HaWsClient;
  coreClient: HaRestClient;
  fileAccess: FileAccessConfig;
  logger: Logger;
}

function prepareRuntime(env: EnvConfig, dependencies: RuntimeDependencies): RuntimePrelude {
  const appOptions = (dependencies.readAppOptions ?? readAppOptions)(env.appOptionsPath);
  const input = { env, appOptions, upstreamAuth: resolveUpstreamAuth(env) };
  return {
    ...input,
    wsClient: (dependencies.createWsClient ?? createDefaultWsClient)(input),
    coreClient: (dependencies.createRestClient ?? createDefaultRestClient)(input),
    // File access is opt-in and defaults to off. The App option (or FILE_ACCESS
    // for the standalone container) is the only way to enable it, and it stays
    // off when no config directory is mounted — reporting a mode the relay
    // cannot serve would be a lie.
    fileAccess: resolveFileAccess(
      {
        mode: fileAccessOption(appOptions, process.env),
        configRootOverride: process.env.CONFIG_ROOT,
        dataDir: dirname(env.appOptionsPath),
      },
      (path: string) => probeConfigRoot(path),
    ),
    logger: dependencies.logger ?? createConsoleLogger(env.logLevel),
  };
}

function resolveUpstreamAuth(env: EnvConfig): UpstreamAuth {
  if (env.supervisorToken) {
    return { token: env.supervisorToken, source: "supervisor_token" };
  }
  const haLlat = env.haLlat?.trim();
  if (haLlat) {
    return { token: haLlat, source: "env_ha_llat" };
  }
  throw new Error("SUPERVISOR_TOKEN or HA_LLAT is required for runtime startup.");
}

// The standalone container: one listener, the shared relay token, and the
// safety-check worker.
export function bootstrapRuntime(
  dependencies: RuntimeDependencies = {},
  env: EnvConfig = (dependencies.loadEnv ?? loadEnv)(),
): RuntimeBootstrapResult {
  const { appOptions, upstreamAuth, wsClient, coreClient, fileAccess, logger } =
    prepareRuntime(env, dependencies);
  // The safety-check worker reaches this one listener over loopback with its
  // own random token, replaced on every start.
  let gateToken: string | null = null;
  const gate = (dependencies.createGate ?? createGateWorker)({
    dataDir: dirname(env.appOptionsPath),
    binary: "/opt/ha-nova/ha-nova",
    mint: () => {
      gateToken = randomBytes(32).toString("base64url");
      return { relay_url: `http://127.0.0.1:${env.relayPort}`, credential: gateToken };
    },
    logger,
  });
  const app = createApp({
    gate,
    gateToken: () => gateToken,
    phoneLink: createPhoneLinkStore(join(dirname(env.appOptionsPath), "gate-phone.json")),
    authToken: env.relayAuthToken,
    version: env.relayVersion,
    wsClient,
    fileAccess,
    snapshotRoot: env.snapshotDir,
    logger,
    coreClient: {
      request: async (input: CoreProxyRequest): Promise<CoreProxyResponse> =>
        coreClient.request(input),
    },
  });

  return { app, gate, env, appOptions, fileAccess, upstreamAuth };
}

export async function startRelay(dependencies: RuntimeDependencies = {}): Promise<void> {
  const env = (dependencies.loadEnv ?? loadEnv)();

  // App mode (a real HA App, SUPERVISOR_TOKEN present) runs the three-listener
  // secure-pairing stack; the standalone container runs one listener.
  if (env.supervisorToken) {
    await startAppModeRelay(env, dependencies);
    return;
  }

  const runtime = bootstrapRuntime(dependencies, env);
  const logger = dependencies.logger ?? createConsoleLogger(env.logLevel);
  logger.info("Relay bootstrap", {
    ha_url: env.haUrl,
    relay_port: env.relayPort,
    app_options_path: env.appOptionsPath,
    auth_source: runtime.upstreamAuth.source,
    // Visible in the log so an operator can always see whether file access
    // is on, and at which level — it is the one capability worth stating.
    file_access: runtime.fileAccess.mode,
    config_root: runtime.fileAccess.configRoot || null,
    snapshot_dir: env.snapshotDir,
  });
  logFileAccessWarnings(logger, runtime.fileAccess);

  await (dependencies.listen ?? listenServer)(runtime.app.server, env.relayPort);
  logger.info("Relay listening", { port: env.relayPort });
  runtime.gate.start(); // the worker reaches the listener just started
}

async function startAppModeRelay(env: EnvConfig, dependencies: RuntimeDependencies): Promise<void> {
  const { upstreamAuth, wsClient, coreClient, fileAccess, logger } = prepareRuntime(env, dependencies);
  logger.info("Relay bootstrap (app mode)", {
    ha_url: env.haUrl,
    auth_source: upstreamAuth.source,
    file_access: fileAccess.mode,
    snapshot_dir: env.snapshotDir,
  });
  logFileAccessWarnings(logger, fileAccess);

  const appMode = await buildAppMode({
    supervisorToken: upstreamAuth.token,
    relayVersion: env.relayVersion,
    cloudRemoteEnabled: env.cloudRemoteEnabled,
    wsClient: wsClient as never,
    coreClient: { request: (input) => coreClient.request(input) },
    fileAccess,
    snapshotRoot: env.snapshotDir,
    appOptionsPath: env.appOptionsPath,
    startedAtMs: Date.now(),
    now: () => Date.now(),
    logger,
  });
  await appMode.listen();
}

// A degraded file-access mode must be visible in the log: the user set an
// option and got something less, and they deserve to know why.
function logFileAccessWarnings(logger: Logger, fileAccess: FileAccessConfig): void {
  for (const warning of fileAccess.warnings) {
    logger.warn(warning);
  }
}

export function createDefaultWsClient(input: RuntimeClientInput): HaWsClient {
  const token = input.upstreamAuth.token;

  return createHaWsClient({
    createConnection: async () => {
      const auth = createLongLivedTokenAuth(input.env.haUrl, token);
      // Use the `ws` client instead of the Node global WebSocket (undici):
      // undici's permessage-deflate path enforces a max decompressed message
      // size and drops the connection on large command results such as
      // `config/entity_registry/list` on big instances (see nova/src/ha/socket.ts).
      const connection = await createConnection({
        auth,
        // The ws socket is API-compatible with the browser WebSocket surface
        // home-assistant-js-websocket uses (addEventListener/send/close), but
        // the TS types differ structurally — hence the unknown-cast.
        createSocket: async () =>
          (await createHawsSocket({
            haUrl: input.env.haUrl,
            token,
          })) as unknown as HaWebSocket,
      });

      const wrapped: HaWsConnection = {
        sendMessagePromise: (message: HaWsRequest) =>
          connection.sendMessagePromise(message),
        subscribeMessage: (callback, message, options) =>
          connection.subscribeMessage(callback, message, options),
        addEventListener: (event, callback) =>
          connection.addEventListener(event, callback),
        close: () => {
          connection.close();
          // haws checks closeRequested only BEFORE awaiting createSocket: a
          // close that lands mid-reconnect still installs the new socket. A
          // `ready` after close() can only be that leaked socket — close it.
          connection.addEventListener("ready", () => connection.close());
        },
      };

      return wrapped;
    },
  });
}

export function createDefaultRestClient(input: RuntimeClientInput): HaRestClient {
  return createHaRestClient({ baseUrl: input.env.haUrl, token: input.upstreamAuth.token });
}
