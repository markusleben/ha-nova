import { closeSync, constants, fstatSync, openSync, readFileSync } from "node:fs";

import type { PhoneLink } from "../http/gate-asks.js";
import { writeFileAtomicSync } from "../storage/atomic-file.js";

// The standalone container's phone link (spec §3): one link for its one
// principal, the shared Relay token, in the Relay's own data folder, which
// /files never reaches (files-paths.ts assertOutsideData). Read fresh on
// every call, never through a link: anything but a plain file with one name,
// or unreadable, or invalid, is no link, so an ask has no channel and
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
    let fd: number | undefined;
    try {
      fd = openSync(file, constants.O_RDONLY | constants.O_NOFOLLOW);
      const info = fstatSync(fd);
      return info.isFile() && info.nlink === 1 ? parsed(JSON.parse(readFileSync(fd, "utf8"))) : undefined;
    } catch {
      return undefined; // missing, a link, or unreadable: no link
    } finally {
      if (fd !== undefined) {
        closeSync(fd);
      }
    }
  };
  return {
    link,
    setLink(next, current) {
      if (JSON.stringify(link()) !== JSON.stringify(current)) {
        return false;
      }
      writeFileAtomicSync(file, JSON.stringify(next)); // a new file, never through a link, synced
      return true;
    },
  };
}
