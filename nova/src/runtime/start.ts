import { accessSync, constants, statSync } from "node:fs";
import { type Server } from "node:http";

import { createConnection, createLongLivedTokenAuth, type HaWebSocket } from "home-assistant-js-websocket";

import { createApp, type App } from "../index.js";
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
    throw new Error("HA_LLAT is required for runtime startup.");
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
  const app = createApp({
    authToken: env.relayAuthToken,
    version: env.relayVersion,
    wsClient,
    fileAccess,
    snapshotRoot: env.snapshotDir,
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
  const logger = dependencies.logger ?? createConsoleLogger();
  const runtime = bootstrapRuntime(dependencies);

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

export function createDefaultWsClient(input: RuntimeWsClientInput): HaWsClient {
  const token = input.upstreamAuth.token;
  if (!token || input.upstreamAuth.capability !== "full") {
    throw new Error("HA_LLAT is required for runtime startup.");
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
    throw new Error("HA_LLAT is required for runtime startup.");
  }

  return createHaRestClient({
    baseUrl: input.env.haUrl,
    token
  });
}

function buildTokenResolutionInput(env: EnvConfig): ResolveUpstreamTokenInput {
  const input: ResolveUpstreamTokenInput = {};

  if (env.haLlat) {
    input.envHaLlat = env.haLlat;
  }

  return input;
}

function createConsoleLogger(): Logger {
  return {
    info(message, context) {
      logLine("info", message, context);
    },
    warn(message, context) {
      logLine("warn", message, context);
    },
    error(message, context) {
      logLine("error", message, context);
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
