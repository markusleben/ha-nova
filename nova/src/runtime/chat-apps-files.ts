import { join } from "node:path";

import type { RelayLogger } from "../http/server.js";
import { readPrivateFileSync, writeFileAtomicSync } from "../storage/atomic-file.js";
import { ChatAppsError } from "./chat-apps-ha.js";
import { CHAT_LEVELS, type ChatLevel, type ChatUsers } from "./chat-apps-household.js";
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
  // Each user's link by its phone's name, a broken entry left out; null:
  // the bridge's file can't be read, which never blocks taking someone off.
  const links = (): Record<string, { name: string; deviceId: string | null }> | null => {
    let byUser: unknown;
    try {
      byUser = read<unknown>("phone-links.json", {});
    } catch (error) {
      byUser = error;
    }
    if (typeof byUser !== "object" || byUser === null || Array.isArray(byUser) || byUser instanceof Error) {
      logger.warn("Chat apps: phone-links.json can't be read", { error: String(byUser instanceof Error ? byUser.message : "not an object") });
      return null;
    }
    const out: Record<string, { name: string; deviceId: string | null }> = {};
    for (const [user, link] of Object.entries(byUser as Record<string, { name?: unknown; device_id?: unknown } | null>)) {
      if (typeof link?.name === "string") {
        out[user] = { name: link.name, deviceId: typeof link.device_id === "string" ? link.device_id : null };
      }
    }
    return out;
  };
  return {
    choices(ownerId) {
      const current = settings();
      const linked = links() ?? {};
      const ids: unknown = current.allowed_users;
      const levels: unknown = current.levels ?? {};
      // A hand-edited file the bridge refuses (it fails closed) is never shown as a list.
      if (ids !== undefined && (!Array.isArray(ids) || !ids.every((id) => typeof id === "string"))) {
        throw new Error("chat-apps.json: allowed_users is not a list of user ids");
      }
      if (typeof levels !== "object" || levels === null || Array.isArray(levels) || !Object.values(levels).every((l) => CHAT_LEVELS.includes(l as ChatLevel))) {
        throw new Error("chat-apps.json: levels is not a map of known levels");
      }
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
    // link before the write, and the save waits for that (else the old phone
    // would confirm again without a new tap); one removed loses theirs after
    // it. A failed drop after a removal is harmless: the bridge never uses
    // the link of a user not on the list, and the next save drops it again.
    async setUsers(ownerId, users) {
      const allowed = (s: { allowed_users?: string[] }): string[] => s.allowed_users ?? [ownerId];
      const before = allowed(settings());
      const after = allowed(users);
      const readable = links();
      const linked = readable === null ? null : Object.keys(readable);
      for (const user of after.filter((u) => !before.includes(u) && (linked === null || linked.includes(u)))) {
        await phone.start(user, { action: "drop" }).catch((error: unknown) => {
          throw new ChatAppsError(503, "OLD_LINK_STAYS", `a re-added user's old phone link couldn't be removed: ${String((error as Error).message)}`);
        });
      }
      const next: ChatAppsSettings = { ...settings(), ...users };
      if (users.allowed_users === undefined) {
        delete next.allowed_users;
        delete next.levels;
        delete next.roles;
      }
      write(next);
      for (const user of (linked ?? before).filter((u) => !after.includes(u))) {
        await drop(user);
      }
    },
  };
}
