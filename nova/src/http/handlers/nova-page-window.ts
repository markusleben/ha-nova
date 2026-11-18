import type { DeviceRecord } from "../../security/device-registry-types.js";
import { isOwner, type HaAuthUser } from "../../security/owner-check.js";
import { codeState, windowBlocked, windowStatus } from "../../security/window-confirm.js";
import type { WindowLogEntry } from "../../storage/window-log.js";
import type { PageKey, PageTexts } from "../page-texts.js";
import { escapeHtml, formatWhen } from "./nova-page-view.js";

// A computer's "Confirm with one click" line on the NOVA page (spec §3), and
// the owner's one on/off button for it; every word comes from the worker's
// catalog. Turning on an undeclared computer declares it the clicking
// owner's (window_mine); turning off one that can't confirm anyway clears
// that (window_not_mine).

export type WindowAction = "window_mine" | "window_not_mine" | "window_on" | "window_off" | "code_on" | "code_off";
export const WINDOW_ACTIONS = new Set<string>(["window_mine", "window_not_mine", "window_on", "window_off", "code_on", "code_off"]);

const REASONS: Record<string, PageKey> = {
  on: "page.window_on", off: "page.window_off", not_yours: "page.window_off", file: "page.window_file", pair_again: "page.window_pair_again",
  legacy_cleanup_pending: "page.window_cleanup", owner: "page.window_owner",
};

// form: an owner's device-bound POST button.
export function windowRow(d: DeviceRecord, users: HaAuthUser[], viewer: { isOwner: boolean; userId: string }, t: PageTexts, form: (action: WindowAction, deviceId: string, label: string) => string): string {
  const status = windowStatus({ kind: "device", deviceId: d.deviceId }, d);
  const declared = users.some((u) => u.id === d.ownComputer?.userId && isOwner(u)); // the Home Assistant owner today
  const owner = users.find(isOwner)?.id;
  const open = status.why === "on" || status.why === "off" || status.why === "not_yours";
  const why = open && owner !== undefined && windowBlocked(d, owner) === "owner" ? "owner"
    : (status.why === "on" || status.why === "off") && !declared ? "not_yours" : status.why;
  const key = REASONS[why];
  if (key === undefined) return "";
  const line = `<span class="muted meta">${t.say(key)}</span>`;
  if (!viewer.isOwner) return line;
  const mine = d.ownComputer?.userId === viewer.userId;
  const button = why === "on" && mine ? form("window_off", d.deviceId, t.say("page.window_turn_off"))
    : why === "off" && mine ? form("window_on", d.deviceId, t.say("page.window_turn_on"))
      : why === "not_yours" ? form("window_mine", d.deviceId, t.say("page.window_turn_on"))
        : why !== "on" && why !== "off" && d.ownComputer !== undefined ? form("window_not_mine", d.deviceId, t.say("page.window_turn_off")) : "";
  const warning = why === "on" || why === "off" || why === "not_yours" ? `<span class="muted meta">${t.say("page.window_hint")} ${t.say("page.window_warning")}</span>` : "";
  return `${line}${button === "" ? "" : `<span class="actions">${button}</span>`}${warning}`;
}

// Its chat code (MAR-244): only the owner turns it on, for a computer nobody
// sits at; turning it on turns the window off, and a linked phone ends it.
export function codeRow(d: DeviceRecord, users: HaAuthUser[], viewer: { isOwner: boolean; userId: string }, t: PageTexts, form: (action: WindowAction, deviceId: string, label: string) => string): string {
  const on = codeState({ kind: "device", deviceId: d.deviceId }, d).mode === "on" && users.some((u) => u.id === d.codeConfirm?.userId && isOwner(u));
  if (!on && (!viewer.isOwner || d.confirmPhone !== undefined || windowBlocked(d, viewer.userId, true) !== null)) return "";
  const line = on ? `<span class="muted meta">${t.say("page.code_on")}</span>` : "";
  if (!viewer.isOwner) return line;
  return `${line}<span class="actions">${form(on ? "code_off" : "code_on", d.deviceId, t.say(on ? "page.code_turn_off" : "page.code_turn_on"))}</span>${on ? "" : `<span class="muted meta">${t.say("page.code_warning")}</span>`}`;
}

export function recentWindowSection(entries: WindowLogEntry[], t: PageTexts): string {
  if (entries.length === 0) return "";
  const rows = [...entries].reverse().map((e) => `<li><span>${escapeHtml(formatWhen(e.atMs))} · <strong>${escapeHtml(e.computer.name)}</strong> · ${escapeHtml(e.title)}${e.code ? ` · ${t.say("page.code_recent")}` : ""}</span></li>`).join("");
  return `<section><h2>${t.say("page.window_recent")}</h2><ul class="facts">${rows}</ul></section>`;
}
