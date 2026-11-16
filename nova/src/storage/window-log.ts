import { readFileSync } from "node:fs";

import type { RelayLogger } from "../http/server.js";
import { writeFileAtomicSync } from "./atomic-file.js";

// The NOVA page's "Recent window confirmations" (spec §3): the last 50 asks a
// computer's own window approved, kept in /data/gate/window-log.json. A
// failed write is logged and never blocks the approval.

export interface WindowLogEntry {
  atMs: number;
  computer: { id: string; name: string };
  title: string;
}

export interface WindowLog {
  append(entry: WindowLogEntry): void;
  list(): WindowLogEntry[];
}

const KEEP = 50;

function isEntry(value: unknown): value is WindowLogEntry {
  const e = value as Partial<WindowLogEntry> | null;
  return typeof e === "object" && e !== null && typeof e.atMs === "number" && Number.isFinite(e.atMs) && typeof e.title === "string" && e.title.length <= 1024
    && typeof e.computer?.id === "string" && e.computer.id.length <= 256 && typeof e.computer.name === "string" && e.computer.name.length <= 256;
}

export function openWindowLog(file: string, logger: Pick<RelayLogger, "warn">): WindowLog {
  let entries: WindowLogEntry[] = [];
  try {
    const raw: unknown = JSON.parse(readFileSync(file, "utf8"));
    if (!Array.isArray(raw) || !raw.every(isEntry)) throw new Error("not a window confirmation log");
    entries = raw.slice(-KEEP);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") logger.warn("The window confirmation log is unreadable; it starts again", { error: String(error) });
  }
  return {
    append(entry) {
      entries = [...entries, entry].slice(-KEEP);
      try {
        writeFileAtomicSync(file, `${JSON.stringify(entries)}\n`);
      } catch (error) {
        logger.warn("The window confirmation log could not be saved; the approval stands", { error: String(error) });
      }
    },
    list: () => [...entries],
  };
}
