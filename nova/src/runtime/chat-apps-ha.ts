import { existsSync, lstatSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import type { SupervisorClient } from "../ha/supervisor-client.js";
import type { CoreProxyRequest, CoreProxyResponse } from "../types/api.js";

// What chat apps (chat-apps.ts) put into Home Assistant: the integration in
// /config/custom_components/ha_nova, the Supervisor discovery message, and
// the ha_nova config entries Home Assistant makes from that message.

export const SERVICE = "ha_nova";

export class ChatAppsError extends Error {
  constructor(readonly status: number, readonly code: string, message: string) {
    super(message);
  }
}

// Whether the chat-app integration directory exists in Home Assistant's config
// (existence only); null without a mounted config or when lstat fails.
export function chatAppFilesPresent(configRoot: string): boolean | null {
  const exists = (...path: string[]) => lstatSync(join(configRoot, ...path), { throwIfNoEntry: false }) !== undefined;
  try {
    return configRoot === "" || !exists("configuration.yaml") ? null : exists("custom_components", SERVICE);
  } catch {
    return null;
  }
}

// custom_components/ha_nova is the ONLY path written in /config: the parent
// must be a real directory (never a symlink), the old folder is removed (a
// symlink there is unlinked, never followed), and every file is created
// exclusively inside the fresh folder.
function customComponents(configRoot: string, create: boolean): string | null {
  if (configRoot === "" || !existsSync(join(configRoot, "configuration.yaml"))) {
    if (!create) {
      return null;
    }
    throw new ChatAppsError(503, "CONFIG_UNAVAILABLE", "Home Assistant's configuration folder is not available to the App.");
  }
  const parent = join(configRoot, "custom_components");
  const st = lstatSync(parent, { throwIfNoEntry: false });
  if (st === undefined && create) {
    mkdirSync(parent, { mode: 0o755 });
  } else if (st === undefined) {
    return null;
  } else if (!st.isDirectory()) {
    throw new ChatAppsError(409, "CONFIG_UNSAFE", "custom_components in Home Assistant's configuration is not a plain folder.");
  }
  return parent;
}

export function removeIntegration(configRoot: string): void {
  const parent = customComponents(configRoot, false);
  if (parent !== null) {
    rmSync(join(parent, SERVICE), { recursive: true, force: true });
  }
}

export function installIntegration(configRoot: string, source: string): void {
  const parent = customComponents(configRoot, true)!;
  removeIntegration(configRoot);
  copyTree(source, join(parent, SERVICE));
}

function copyTree(from: string, to: string): void {
  mkdirSync(to, { mode: 0o755 });
  for (const entry of readdirSync(from, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      copyTree(join(from, entry.name), join(to, entry.name));
    } else if (entry.isFile()) {
      writeFileSync(join(to, entry.name), readFileSync(join(from, entry.name)), { mode: 0o644, flag: "wx" });
    }
  }
}

export interface ClearDeps {
  configRoot: string;
  supervisor: SupervisorClient;
  core: { request(input: CoreProxyRequest): Promise<CoreProxyResponse> };
}

// Removes the discovery message and every ha_nova entry, found in Home
// Assistant and the Supervisor, never only through /data: a backup restore
// rewinds /data, not them. saved is the message id /data remembers. The
// message goes first, so Home Assistant can't set up an entry from it again.
// The integration folder stays for the caller (it goes last).
export async function clearChatAppsInHA(deps: ClearDeps, saved: string | undefined): Promise<void> {
  const entries = await haEntries(deps);
  const dropped = saved !== undefined && (await deps.supervisor.deleteDiscovery(saved));
  if (!dropped && entries.length > 0) {
    // An entry, but no message /data knows: the Supervisor keeps one message
    // per App and service, so sending one names the message this App left.
    // The integration refuses the empty config: Home Assistant sets up
    // nothing from it.
    await deps.supervisor.deleteDiscovery(await deps.supervisor.sendDiscovery(SERVICE, {}));
  }
  for (const id of await haEntries(deps)) {
    const response = await deps.core.request({ method: "DELETE", path: `/api/config/config_entries/entry/${id}` });
    if (response.status !== 200 && response.status !== 404) { // 404: Home Assistant removed it with the message
      throw new Error(`Home Assistant answered ${response.status} when removing the ha_nova entry`);
    }
  }
}

async function haEntries(deps: ClearDeps): Promise<string[]> {
  const response = await deps.core.request({ method: "GET", path: `/api/config/config_entries/entry?domain=${SERVICE}` });
  if (response.status !== 200 || !Array.isArray(response.body)) {
    throw new Error(`Home Assistant's config entries answered ${response.status}`);
  }
  return response.body.map((entry) => {
    const id = (entry as { entry_id?: unknown } | null)?.entry_id;
    if (typeof id !== "string" || !/^[0-9A-Za-z_-]{1,64}$/.test(id)) {
      throw new Error("Home Assistant listed a ha_nova entry without a plain id");
    }
    return id;
  });
}
