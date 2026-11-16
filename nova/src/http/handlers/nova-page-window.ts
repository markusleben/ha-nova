import type { DeviceRecord } from "../../security/device-registry-types.js";
import { isOwner, type HaAuthUser } from "../../security/owner-check.js";
import { windowStatus } from "../../security/window-confirm.js";
import type { WindowLogEntry } from "../../storage/window-log.js";
import type { PageKey, PageTexts } from "../page-texts.js";
import { escapeHtml, formatWhen } from "./nova-page-view.js";

// A computer's "Confirm in its window" line on the NOVA page (spec §3), and
// the owner's buttons for it; every word comes from the worker's catalog.

export type WindowAction = "window_mine" | "window_not_mine" | "window_on" | "window_off";
export const WINDOW_ACTIONS = new Set<string>(["window_mine", "window_not_mine", "window_on", "window_off"]);

const REASONS: Record<string, PageKey> = {
  not_yours: "page.window_not_yours", file: "page.window_file", pair_again: "page.window_pair_again",
  legacy_cleanup_pending: "page.window_cleanup", owner: "page.window_owner",
};

// form: an owner's device-bound POST button.
export function windowRow(d: DeviceRecord, users: HaAuthUser[], viewer: { isOwner: boolean; userId: string }, t: PageTexts, form: (action: WindowAction, deviceId: string, label: string) => string): string {
  const status = windowStatus({ kind: "device", deviceId: d.deviceId }, d);
  const declared = users.some((u) => u.id === d.ownComputer?.userId && isOwner(u)); // the Home Assistant owner today
  const why = (status.why === "on" || status.why === "off") && !declared ? "not_yours" : status.why;
  const key = why === "on" ? "page.window_on" : why === "off" ? "page.window_off" : REASONS[why];
  if (key === undefined) return "";
  const line = `<span class="muted meta">${t.say(key)}</span>`;
  if (!viewer.isOwner) return line;
  const mine = d.ownComputer?.userId === viewer.userId;
  const buttons = [
    ...(why === "on" && mine ? [form("window_off", d.deviceId, t.say("page.window_turn_off"))] : []),
    ...(why === "off" && mine ? [form("window_on", d.deviceId, t.say("page.window_turn_on"))] : []),
    ...(why === "not_yours" ? [form("window_mine", d.deviceId, t.say("page.window_mine"))] : []),
    ...(d.ownComputer !== undefined ? [form("window_not_mine", d.deviceId, t.say("page.window_not_mine"))] : []),
  ];
  const warning = why === "on" || why === "off" || why === "not_yours" ? `<span class="muted meta">${t.say("page.window_warning")}</span>` : "";
  return `${line}${warning}${buttons.length === 0 ? "" : `<span class="actions">${buttons.join("")}</span>`}`;
}

export function recentWindowSection(entries: WindowLogEntry[], t: PageTexts): string {
  if (entries.length === 0) return "";
  const rows = [...entries].reverse().map((e) => `<li><span>${escapeHtml(formatWhen(e.atMs))} · <strong>${escapeHtml(e.computer.name)}</strong> · ${escapeHtml(e.title)}</span></li>`).join("");
  return `<section><h2>${t.say("page.window_recent")}</h2><ul class="facts">${rows}</ul></section>`;
}
