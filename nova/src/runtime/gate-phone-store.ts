import { readFileSync, renameSync, writeFileSync } from "node:fs";

import type { PhoneLink } from "../http/gate-asks.js";

// The standalone container's phone link (spec §3): one link for its one
// principal, the shared Relay token, in the Relay's own data folder, which
// /files never reaches (resolveFileAccess keeps file access off when the
// configuration folder overlaps it). Read fresh on every call; a missing,
// unreadable, or invalid file is no link, so an ask has no channel and
// nothing runs. Two containers sharing one data folder don't coordinate.

export interface PhoneLinkStore {
  link(): PhoneLink | undefined;
  // Sets the link while the file still holds current; false when it doesn't.
  setLink(link: PhoneLink, current: PhoneLink | undefined): boolean;
}

const id = (v: unknown): v is string => typeof v === "string" && v !== "" && v.length <= 128;

function parsed(raw: unknown): PhoneLink | undefined {
  const { service, device_id, user_id, platform } = (typeof raw === "object" && raw !== null ? raw : {}) as Record<string, unknown>;
  if (!id(service) || !/^mobile_app_[a-z0-9_]+$/.test(service) || !id(device_id) || !id(user_id) || (platform !== undefined && platform !== "ios" && platform !== "android")) {
    return undefined;
  }
  return { service, device_id, user_id, ...(platform !== undefined ? { platform } : {}) };
}

export function createPhoneLinkStore(file: string): PhoneLinkStore {
  const link = (): PhoneLink | undefined => {
    try {
      return parsed(JSON.parse(readFileSync(file, "utf8")));
    } catch {
      return undefined; // missing or unreadable: no link
    }
  };
  return {
    link,
    setLink(next, current) {
      if (JSON.stringify(link()) !== JSON.stringify(current)) {
        return false;
      }
      writeFileSync(file + ".tmp", JSON.stringify(next), { mode: 0o600 });
      renameSync(file + ".tmp", file);
      return true;
    },
  };
}
