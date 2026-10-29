import { join } from "node:path";

import type { RelayLogger } from "../http/server.js";
import { readPrivateFileSync, writeFileAtomicSync } from "../storage/atomic-file.js";
import type { ChatLevel, ChatUsers } from "./chat-apps-household.js";
import type { ChatPhoneControl } from "./chat-phone-control.js";

// The bridge's two files the App reads (spec docs/work/2026-10-24-chat-
// apps-household-spec.md): chat-apps.json, the owner's switches and list,
// which only the App writes and the bridge re-reads on every request; and
// phone-links.json, each user's chat-app phone, which only the bridge
// writes (its control channel).

export interface ChatAppsSettings {
  allowed_users?: string[]; // absent: the owner only
  levels?: Record<string, ChatLevel>;
  roles?: number;
  read_only?: boolean;
  chat_codes?: boolean;
}

// What the owner chose, as the NOVA page and the machine API show it.
export interface ChatAppsChoices {
  readOnly: boolean;
  chatCodes: boolean;
  allowedUsers: string[] | null; // null: the owner only
  levels: Record<string, ChatLevel>; // as the owner picked them, before the role's cap
  phones: Record<string, string>; // each user's linked phone's name
  ownerPhone: string | null; // the owner's linked phone's name
  ownerPhoneId: string | null; // and its Home Assistant device id
}

export interface ChatAppsFiles {
  choices(ownerId: string): ChatAppsChoices;
  setSwitches(change: { readOnly?: boolean; chatCodes?: boolean }): void;
  setUsers(ownerId: string, users: ChatUsers): Promise<void>;
}

export function chatAppsFiles(bridgeDir: string, phone: ChatPhoneControl, logger: RelayLogger): ChatAppsFiles {
  const read = <T>(name: string, fallback: T): T => {
    const raw = readPrivateFileSync(join(bridgeDir, name), 256 * 1024);
    return raw === null ? fallback : (JSON.parse(raw.toString("utf8")) as T);
  };
  const write = (settings: ChatAppsSettings): void => writeFileAtomicSync(join(bridgeDir, "chat-apps.json"), `${JSON.stringify(settings)}\n`);
  const settings = (): ChatAppsSettings => read("chat-apps.json", {});
  const drop = (user: string): Promise<void> => phone.start(user, { action: "drop" }).then(
    () => {},
    (error: unknown) => logger.warn("Chat apps: a removed user's phone link stays until the next save", { error: String((error as Error).message) }),
  );
  // Each user's link by its phone's name; a broken entry is left out.
  const links = (): Record<string, { name: string; deviceId: string | null }> => {
    const out: Record<string, { name: string; deviceId: string | null }> = {};
    for (const [user, link] of Object.entries(read<Record<string, { name?: unknown; device_id?: unknown } | null>>("phone-links.json", {}))) {
      if (typeof link?.name === "string") {
        out[user] = { name: link.name, deviceId: typeof link.device_id === "string" ? link.device_id : null };
      }
    }
    return out;
  };
  return {
    choices(ownerId) {
      const current = settings();
      const linked = links();
      return {
        readOnly: current.read_only === true,
        chatCodes: current.chat_codes !== false,
        allowedUsers: current.allowed_users ?? null,
        levels: current.levels ?? {},
        phones: Object.fromEntries(Object.entries(linked).map(([user, link]) => [user, link.name])),
        ownerPhone: linked[ownerId]?.name ?? null,
        ownerPhoneId: linked[ownerId]?.deviceId ?? null,
      };
    },
    setSwitches(change) {
      write({ ...settings(), ...(change.readOnly !== undefined ? { read_only: change.readOnly } : {}), ...(change.chatCodes !== undefined ? { chat_codes: change.chatCodes } : {}) });
    },
    // The owner's list, with the switches as they are now (read again after
    // the caller checked Home Assistant's users). The bridge drops a user's
    // phone link only while they aren't allowed: one re-added loses an old
    // link before the write, so they link a phone anew; one removed loses
    // theirs after it. A failed drop is harmless: the bridge never uses the
    // link of a user not on the list, and the next save drops it again.
    async setUsers(ownerId, users) {
      const allowed = (s: { allowed_users?: string[] }): string[] => s.allowed_users ?? [ownerId];
      const before = allowed(settings());
      const linked = Object.keys(links());
      for (const user of linked.filter((u) => allowed(users).includes(u) && !before.includes(u))) {
        await drop(user);
      }
      const next: ChatAppsSettings = { ...settings(), ...users };
      if (users.allowed_users === undefined) {
        delete next.allowed_users;
        delete next.levels;
        delete next.roles;
      }
      write(next);
      for (const user of linked.filter((u) => !allowed(users).includes(u))) {
        await drop(user);
      }
    },
  };
}
