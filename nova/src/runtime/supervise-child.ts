import type { ChildProcess } from "node:child_process";

import type { RelayLogger } from "../http/server.js";

// One `ha-nova` child of the Relay (the safety-check worker, the chat-app
// bridge): started again while it is wanted, backing off from 5 s to 5 min
// while it keeps failing.
export interface SupervisedChild {
  start(): void; // throws what launch throws
  tryStart(): void; // a failure is logged and retried later
  startLater(): void;
  stop(): void;
  restart(): void; // its exit starts it again
  resetFailures(): void;
  current(): ChildProcess | null;
  wanted(): boolean;
}

export interface SuperviseDeps {
  name: string; // the log lines' subject: "Safety-check worker"
  logger: RelayLogger;
  launch: () => ChildProcess | null; // null: nothing to run (launch logged why)
  respawnMs?: number | undefined;
}

// A minimal environment: the App's Supervisor and Home Assistant tokens never
// reach a child; it talks to Home Assistant only through the Relay.
export function childEnv(home: string): NodeJS.ProcessEnv {
  return { PATH: process.env.PATH ?? "/usr/bin:/bin", HOME: home, HA_NOVA_NO_BROWSER: "1", ...(process.env.TZ ? { TZ: process.env.TZ } : {}) };
}

export function superviseChild(deps: SuperviseDeps): SupervisedChild {
  let child: ChildProcess | null = null;
  let wanted = false;
  let respawn: NodeJS.Timeout | undefined;
  let failures = 0;

  const start = (): void => {
    wanted = true;
    if (child !== null) {
      return;
    }
    const proc = deps.launch();
    if (proc === null) {
      return;
    }
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
        deps.logger.warn(`${deps.name} stopped; restarting it`, { why });
        startLater();
      }
    };
    proc.on("error", (error) => end(error.message));
    proc.once("exit", (code, signal) => end(`exit ${code ?? signal}`));
  };
  const tryStart = (): void => {
    try {
      start();
    } catch (error) {
      failures += 1;
      deps.logger.error(`${deps.name} could not start`, { error: String((error as Error).message) });
      startLater();
    }
  };
  const startLater = (): void => {
    clearTimeout(respawn);
    respawn = setTimeout(tryStart, Math.min((deps.respawnMs ?? 5_000) * 2 ** Math.max(0, failures - 1), 300_000));
    respawn.unref?.();
  };
  // serve finishes running calls for up to 5 s; a hung child must not keep its port.
  const kill = (proc: ChildProcess): void => {
    proc.kill("SIGTERM");
    setTimeout(() => proc.exitCode === null && proc.signalCode === null && proc.kill("SIGKILL"), 7_000).unref();
  };

  return {
    start,
    tryStart,
    startLater,
    stop() {
      wanted = false;
      clearTimeout(respawn);
      const proc = child;
      child = null;
      if (proc !== null) {
        kill(proc);
      }
    },
    restart() {
      failures = 0;
      if (child !== null) {
        kill(child);
      } else if (wanted) {
        startLater();
      }
    },
    resetFailures() {
      failures = 0;
    },
    current: () => child,
    wanted: () => wanted,
  };
}
