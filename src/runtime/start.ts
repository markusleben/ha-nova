import { type Server } from "node:http";

import { createConnection, createLongLivedTokenAuth } from "home-assistant-js-websocket";

import { createApp, type App } from "../index.js";
import { readAppOptions, type AppOptions } from "../config/app-options.js";
import { loadEnv, type EnvConfig, type LogLevel } from "../config/env.js";
import { createHaWsClient, type HaWsClient, type HaWsConnection, type HaWsRequest } from "../ha/ws-client.js";
import {
  resolveUpstreamToken,
  type ResolveUpstreamTokenInput,
  type UpstreamTokenResolution
} from "../security/token-resolver.js";
import { createWsAllowlist } from "../security/ws-allowlist.js";

export interface RuntimeBootstrapResult {
  app: App;
  env: EnvConfig;
  appOptions: AppOptions;
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
  logger?: Logger;
  listen?: (server: Server, port: number) => Promise<void>;
}

export interface RuntimeWsClientInput {
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

  const upstreamAuth = resolveUpstreamToken(
    buildTokenResolutionInput(env, normalizeAppOptionToken(appOptions.ha_llat))
  );
  if (upstreamAuth.capability !== "full" || !upstreamAuth.token) {
    throw new Error("HA_LLAT is required for runtime startup.");
  }

  const wsClient = (dependencies.createWsClient ?? createDefaultWsClient)({
    env,
    appOptions,
    upstreamAuth
  });

  const app = createApp({
    authToken: env.relayAuthToken,
    version: env.relayVersion,
    wsClient,
    allowlist: createWsAllowlist()
  });

  return {
    app,
    env,
    appOptions,
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
      if (typeof WebSocket === "undefined") {
        throw new Error(
          "Global WebSocket is unavailable in this Node runtime. Use Node runtime with WebSocket support."
        );
      }

      const auth = createLongLivedTokenAuth(input.env.haUrl, token);
      const connection = await createConnection({ auth });

      const wrapped: HaWsConnection = {
        sendMessagePromise: (message: HaWsRequest) => connection.sendMessagePromise(message)
      };

      return wrapped;
    }
  });
}

function buildTokenResolutionInput(
  env: EnvConfig,
  appOptionHaLlat: string | undefined
): ResolveUpstreamTokenInput {
  const input: ResolveUpstreamTokenInput = {};

  if (env.haLlat) {
    input.envHaLlat = env.haLlat;
  }

  if (appOptionHaLlat) {
    input.appOptionHaLlat = appOptionHaLlat;
  }

  return input;
}

function normalizeAppOptionToken(value: unknown): string | undefined {
  if (typeof value !== "string") {
    return undefined;
  }

  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }

  return trimmed;
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
    auth_capability: runtime.upstreamAuth.capability
  });

  for (const warning of runtime.upstreamAuth.warnings) {
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
