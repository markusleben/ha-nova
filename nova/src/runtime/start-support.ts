import { accessSync, constants, statSync } from "node:fs";
import type { Server } from "node:http";

import type { AppOptions } from "../config/app-options.js";
import type { RootProbe } from "../config/file-access.js";
import type { LogLevel } from "../config/env.js";

export interface Logger {
  info(message: string, context?: Record<string, unknown>): void;
  warn(message: string, context?: Record<string, unknown>): void;
  error(message: string, context?: Record<string, unknown>): void;
}

const LOG_LEVEL_ORDER: Record<LogLevel, number> = {
  trace: 0,
  debug: 1,
  info: 2,
  warn: 3,
  error: 4,
};

export function levelAtLeast(level: LogLevel, minimum: LogLevel): boolean {
  return LOG_LEVEL_ORDER[level] >= LOG_LEVEL_ORDER[minimum];
}

// LOG_LEVEL finally has a consumer: lines below the configured minimum are
// dropped. Startup/bootstrap errors always surface (error >= every minimum).
export function createConsoleLogger(minimumLevel: LogLevel = "info"): Logger {
  const emit = (
    level: LogLevel,
    message: string,
    context?: Record<string, unknown>,
  ) => {
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
    },
  };
}

function logLine(
  level: LogLevel | "error",
  message: string,
  context?: Record<string, unknown>,
): void {
  const payload = {
    level,
    message,
    ...(context ? { context } : {}),
  };
  console.log(JSON.stringify(payload));
}

export async function listenServer(
  server: Server,
  port: number,
): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.listen(port, "0.0.0.0", () => resolve());
    server.on("error", reject);
  });
}

// The App writes its options to /data/options.json; the standalone container
// uses FILE_ACCESS directly. The App option wins when present.
export function fileAccessOption(
  appOptions: AppOptions,
  env: NodeJS.ProcessEnv,
): string | undefined {
  const fromApp = appOptions.file_access;
  if (typeof fromApp === "string" && fromApp.trim() !== "") {
    return fromApp;
  }
  return env.FILE_ACCESS;
}

// Probes what the relay can really do with a candidate config root. A mount can
// exist and still be read-only or owned by another UID — the mode must reflect
// reality, not the option.
export function probeConfigRoot(path: string): RootProbe {
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
    writable: can(constants.W_OK),
  };
}
