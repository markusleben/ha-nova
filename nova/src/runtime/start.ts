import { accessSync, constants, statSync } from "node:fs";
import { type Server } from "node:http";

import { createConnection, createLongLivedTokenAuth, type HaWebSocket } from "home-assistant-js-websocket";

import { createApp, type App } from "../index.js";
import { buildAppMode } from "./app-mode.js";
import { readAppOptions, type AppOptions } from "../config/app-options.js";
import { resolveFileAccess, type FileAccessConfig, type RootProbe } from "../config/file-access.js";
import { loadEnv, type EnvConfig, type LogLevel } from "../config/env.js";
import { createAuthenticatedHaSocket } from "../ha/socket.js";
import { createHaRestClient, type HaRestClient } from "../ha/rest-client.js";
import { createHaWsClient, type HaWsClient, type HaWsConnection, type HaWsRequest } from "../ha/ws-client.js";
import type { CoreProxyRequest, CoreProxyResponse } from "../types/api.js";
import {
  resolveUpstreamToken,
  type ResolveUpstreamTokenInput,
  type UpstreamTokenResolution
} from "../security/token-resolver.js";

export interface RuntimeBootstrapResult {
  app: App;
  env: EnvConfig;
  appOptions: AppOptions;
  fileAccess: FileAccessConfig;
  upstreamAuth: UpstreamTokenResolution;
}

interface Logger {
  info(message: string, context?: Record<string, unknown>): void;
  warn(message: string, context?: Record<string, unknown>): void;
  error(message: string, context?: Record<string, unknown>): void;
}

export interface RuntimeDependencies {
  loadEnv?: () => EnvConfig;
  readAppOptions?: (path: string) => AppOptions;
  createWsClient?: (input: RuntimeWsClientInput) => HaWsClient;
  createRestClient?: (input: RuntimeRestClientInput) => HaRestClient;
  logger?: Logger;
  listen?: (server: Server, port: number) => Promise<void>;
}

export interface RuntimeWsClientInput {
  env: EnvConfig;
  appOptions: AppOptions;
  upstreamAuth: UpstreamTokenResolution;
}

export interface RuntimeRestClientInput {
  env: EnvConfig;
  appOptions: AppOptions;
  upstreamAuth: UpstreamTokenResolution;
}

export interface StartRelayResult extends RuntimeBootstrapResult {
  port: number;
}

export function bootstrapRuntime(dependencies: RuntimeDependencies = {}): RuntimeBootstrapResult {
  const env = (dependencies.loadEnv ?? loadEnv)();
  const appOptions = (dependencies.readAppOptions ?? readAppOptions)(env.appOptionsPath);

  const upstreamAuth = resolveUpstreamToken(buildTokenResolutionInput(env));
  if (upstreamAuth.capability !== "full" || !upstreamAuth.token) {
    throw new Error("SUPERVISOR_TOKEN or HA_LLAT is required for runtime startup.");
  }

  const wsClient = (dependencies.createWsClient ?? createDefaultWsClient)({
    env,
    appOptions,
    upstreamAuth
  });
  const coreClient = (dependencies.createRestClient ?? createDefaultRestClient)({
    env,
    appOptions,
    upstreamAuth
  });

  // File access is opt-in and defaults to off. The App option (or FILE_ACCESS
  // for the standalone container) is the only way to enable it, and it stays
  // off when no config directory is mounted — reporting a mode the relay cannot
  // serve would be a lie.
  const fileAccess = resolveFileAccess(
    {
      mode: fileAccessOption(appOptions, process.env),
      configRootOverride: process.env.CONFIG_ROOT
    },
    (path: string) => probeConfigRoot(path)
  );
  const serverLogger = dependencies.logger ?? createConsoleLogger(env.logLevel);
  const app = createApp({
    authToken: env.relayAuthToken,
    version: env.relayVersion,
    installerVersion: env.productVersion ?? env.relayVersion,
    requiredRelayVersion: env.minRelayVersion ?? env.relayVersion,
    wsClient,
    fileAccess,
    snapshotRoot: env.snapshotDir,
    logger: serverLogger,
    coreClient: {
      request: async (input: CoreProxyRequest): Promise<CoreProxyResponse> => coreClient.request(input)
    }
  });

  return {
    app,
    env,
    appOptions,
    fileAccess,
    upstreamAuth
  };
}

export async function startRelay(dependencies: RuntimeDependencies = {}): Promise<StartRelayResult> {
  const env = (dependencies.loadEnv ?? loadEnv)();

  // App mode (a real HA App, SUPERVISOR_TOKEN present) runs the three-listener
  // secure-pairing stack. Standalone Container/Core keeps the single-listener
  // path below. The startup pairing-code log is gone in app mode: no code is
  // generated at startup anymore, and codes are never logged.
  if (env.supervisorToken) {
    return await startAppModeRelay(env, dependencies);
  }

  const runtime = bootstrapRuntime(dependencies);
  const logger = dependencies.logger ?? createConsoleLogger(runtime.env.logLevel);

  logStartup(logger, runtime);

  const listen = dependencies.listen ?? listenServer;
  await listen(runtime.app.server, runtime.env.relayPort);

  logger.info("Relay listening", {
    port: runtime.env.relayPort
  });

  return {
    ...runtime,
    port: runtime.env.relayPort
  };
}

async function startAppModeRelay(env: EnvConfig, dependencies: RuntimeDependencies): Promise<StartRelayResult> {
  const logger = dependencies.logger ?? createConsoleLogger(env.logLevel);
  const appOptions = (dependencies.readAppOptions ?? readAppOptions)(env.appOptionsPath);
  const upstreamAuth = resolveUpstreamToken(buildTokenResolutionInput(env));
  if (upstreamAuth.capability !== "full" || !upstreamAuth.token) {
    throw new Error("SUPERVISOR_TOKEN or HA_LLAT is required for runtime startup.");
  }

  const wsClient = (dependencies.createWsClient ?? createDefaultWsClient)({ env, appOptions, upstreamAuth });
  const coreClient = (dependencies.createRestClient ?? createDefaultRestClient)({ env, appOptions, upstreamAuth });
  const fileAccess = resolveFileAccess(
    { mode: fileAccessOption(appOptions, process.env), configRootOverride: process.env.CONFIG_ROOT },
    (path: string) => probeConfigRoot(path)
  );

  logger.info("Relay bootstrap (app mode)", {
    ha_url: env.haUrl,
    auth_source: upstreamAuth.source,
    file_access: fileAccess.mode,
    snapshot_dir: env.snapshotDir,
  });
  for (const warning of fileAccess.warnings) {
    logger.warn(warning);
  }

  const appMode = await buildAppMode({
    supervisorToken: env.supervisorToken!,
    relayVersion: env.relayVersion,
    wsClient: wsClient as never,
    coreClient: { request: (input) => coreClient.request(input) },
    fileAccess,
    snapshotRoot: env.snapshotDir,
    appOptionsPath: env.appOptionsPath,
    startedAtMs: Date.now(),
    now: () => Date.now(),
    logger,
    ...(process.env.NOVA_ICON_PATH ? { iconPath: process.env.NOVA_ICON_PATH } : {}),
  });
  await appMode.listen();

  // The bootstrap port is the liveness/watchdog signal, reported as the port.
  const runtime = bootstrapRuntimeForResult(env, appOptions, fileAccess, upstreamAuth, appMode.servers.bootstrap);
  return { ...runtime, port: env.relayPort };
}

// A minimal RuntimeBootstrapResult for the app-mode return value; the three
// servers live in the app-mode runtime, and callers only read env/options.
function bootstrapRuntimeForResult(
  env: EnvConfig,
  appOptions: AppOptions,
  fileAccess: FileAccessConfig,
  upstreamAuth: UpstreamTokenResolution,
  bootstrapServer: Server
): RuntimeBootstrapResult {
  return {
    app: { version: env.relayVersion, server: bootstrapServer } as unknown as App,
    env,
    appOptions,
    fileAccess,
    upstreamAuth,
  };
}

export function createDefaultWsClient(input: RuntimeWsClientInput): HaWsClient {
  const token = input.upstreamAuth.token;
  if (!token || input.upstreamAuth.capability !== "full") {
    throw new Error("Upstream Home Assistant authentication is required for runtime startup.");
  }

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
          (await createAuthenticatedHaSocket({
            haUrl: input.env.haUrl,
            token
          })) as unknown as HaWebSocket
      });

      const wrapped: HaWsConnection = {
        sendMessagePromise: (message: HaWsRequest) => connection.sendMessagePromise(message),
        subscribeMessage: (callback, message, options) =>
          connection.subscribeMessage(callback, message, options),
        addEventListener: (event, callback) => connection.addEventListener(event, callback)
      };

      return wrapped;
    }
  });
}

export function createDefaultRestClient(input: RuntimeRestClientInput): HaRestClient {
  const token = input.upstreamAuth.token;
  if (!token || input.upstreamAuth.capability !== "full") {
    throw new Error("Upstream Home Assistant authentication is required for runtime startup.");
  }

  return createHaRestClient({
    baseUrl: input.env.haUrl,
    token
  });
}

function buildTokenResolutionInput(env: EnvConfig): ResolveUpstreamTokenInput {
  const input: ResolveUpstreamTokenInput = {};

  if (env.supervisorToken) {
    input.supervisorToken = env.supervisorToken;
  }
  if (env.haLlat) {
    input.envHaLlat = env.haLlat;
  }

  return input;
}

const LOG_LEVEL_ORDER: Record<LogLevel, number> = {
  trace: 0,
  debug: 1,
  info: 2,
  warn: 3,
  error: 4
};

export function levelAtLeast(level: LogLevel, minimum: LogLevel): boolean {
  return LOG_LEVEL_ORDER[level] >= LOG_LEVEL_ORDER[minimum];
}

// LOG_LEVEL finally has a consumer: lines below the configured minimum are
// dropped. Startup/bootstrap errors always surface (error >= every minimum).
export function createConsoleLogger(minimumLevel: LogLevel = "info"): Logger {
  const emit = (level: LogLevel, message: string, context?: Record<string, unknown>) => {
    if (levelAtLeast(level, minimumLevel)) {
      logLine(level, message, context);
    }
  };
  return {
    info(message, context) {
      emit("info", message, context);
    },
    warn(message, context) {
      emit("warn", message, context);
    },
    error(message, context) {
      emit("error", message, context);
    }
  };
}

function logStartup(logger: Logger, runtime: RuntimeBootstrapResult): void {
  logger.info("Relay bootstrap", {
    ha_url: runtime.env.haUrl,
    relay_port: runtime.env.relayPort,
    app_options_path: runtime.env.appOptionsPath,
    auth_source: runtime.upstreamAuth.source,
    auth_capability: runtime.upstreamAuth.capability,
    // Visible in the App log so an operator can always see whether file access
    // is on, and at which level — it is the one capability worth stating.
    file_access: runtime.fileAccess.mode,
    config_root: runtime.fileAccess.configRoot || null,
    snapshot_dir: runtime.env.snapshotDir
  });

  const pairing = runtime.app.pairing.getStatus();
  logger.info("Pairing code ready", {
    pairing_code: pairing.code,
    expires_at: new Date(pairing.expiresAtMs).toISOString()
  });

  for (const warning of runtime.upstreamAuth.warnings) {
    logger.warn(warning);
  }

  // A degraded file-access mode must be visible in the App log: the user set an
  // option and got something less, and they deserve to know why.
  for (const warning of runtime.fileAccess.warnings) {
    logger.warn(warning);
  }
}

function logLine(level: LogLevel | "error", message: string, context?: Record<string, unknown>): void {
  const payload = {
    level,
    message,
    ...(context ? { context } : {})
  };

  console.log(JSON.stringify(payload));
}

async function listenServer(server: Server, port: number): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.listen(port, "0.0.0.0", () => resolve());
    server.on("error", reject);
  });
}

// The App writes its options to /data/options.json; the standalone container
// uses FILE_ACCESS directly. The App option wins when present.
function fileAccessOption(appOptions: AppOptions, env: NodeJS.ProcessEnv): string | undefined {
  const fromApp = appOptions.file_access;
  if (typeof fromApp === "string" && fromApp.trim() !== "") {
    return fromApp;
  }
  return env.FILE_ACCESS;
}

// Probes what the relay can really do with a candidate config root. A mount can
// exist and still be read-only or owned by another UID — the mode must reflect
// reality, not the option.
function probeConfigRoot(path: string): RootProbe {
  try {
    if (!statSync(path).isDirectory()) {
      return { isDirectory: false, readable: false, writable: false };
    }
  } catch {
    return { isDirectory: false, readable: false, writable: false };
  }

  const can = (mode: number): boolean => {
    try {
      accessSync(path, mode);
      return true;
    } catch {
      return false;
    }
  };

  return {
    isDirectory: true,
    readable: can(constants.R_OK),
    writable: can(constants.W_OK)
  };
}
