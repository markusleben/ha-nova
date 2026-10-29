import type { ChatApps, ChatAppsStatus } from "../../runtime/chat-apps.js";
import { CHAT_LEVELS, effectiveLevel, pickable, roleCap, usersToSave, type ChatLevel } from "../../runtime/chat-apps-household.js";
import type { ChatActivity } from "../../runtime/chat-phone-control.js";
import type { HaAuthUser } from "../../security/owner-check.js";

// The NOVA page's household list (spec docs/work/2026-10-24-chat-apps-
// household-spec.md, "UX copy"): who may use chat apps and how much, each
// one's phone, how they join, and the last changes chat apps made. One form
// saves the whole list, from the owner's session only (checkOwner), checked
// against Home Assistant's users as read for that request. Names come from
// Home Assistant and the bridge: every one is escaped.

export interface ChatUsersModel {
  users: HaAuthUser[]; // the people listed: the owner first, then every other active login
  seen: Record<string, string> | null; // each user's last request; null: the bridge can't say now
  activity: ChatActivity[] | null;
  saved: boolean;
  removed: string[]; // the names of the people the last save removed
}

export interface ChatPhoneUser {
  id: string;
  name: string;
  codes: boolean; // a chat code may stand in for their phone: only at Manage everything, with codes on
}

const HA_USER = /^[0-9a-f]{32}$/;

const LEVEL: Record<ChatLevel, string> = { ask: "Ask only", use: "Use the home", manage: "Manage everything" };

export async function readChatUsers(chat: ChatApps, ownerId: string, users: HaAuthUser[], query: URLSearchParams): Promise<ChatUsersModel> {
  const [seen, activity] = await Promise.all([chat.phone.seen().catch(() => null), chat.phone.activity().catch(() => null)]);
  const removed = (query.get("chat_removed") ?? "").split(",").filter((id) => HA_USER.test(id));
  return {
    users: [...users.filter((u) => u.id === ownerId), ...users.filter((u) => u.id !== ownerId && pickable(u))],
    seen,
    activity,
    saved: query.get("chat_users") === "saved",
    removed: removed.map((id) => nameOf(users.find((u) => u.id === id))),
  };
}

// The user whose phone the page shows (?chat_user=): one on the list at Use
// the home or more; null: the owner.
export function chatPhoneUser(id: string | null, status: ChatAppsStatus, ownerId: string, users: HaAuthUser[]): ChatPhoneUser | null {
  const user = users.find((u) => u.id === id && u.id !== ownerId);
  const level = user === undefined ? "ask" : effectiveLevel(user, status.levels[user.id]);
  return user !== undefined && pickable(user) && status.allowedUsers?.includes(user.id) === true && level !== "ask"
    ? { id: user.id, name: nameOf(user), codes: status.chatCodes && level === "manage" }
    : null;
}

// Saves the ticked users (on_<id>=1) at their picked levels (level_<id>);
// the owner always stays. Returns the ids the save removed.
export async function applyChatUsers(chat: ChatApps, ownerId: string, users: HaAuthUser[], form: Record<string, unknown>): Promise<string[]> {
  const picked = new Map<string, string>();
  for (const [key, value] of Object.entries(form)) {
    if (key.startsWith("on_") && value === "1") {
      const level = form[`level_${key.slice(3)}`];
      picked.set(key.slice(3), typeof level === "string" ? level : "");
    }
  }
  const next = usersToSave(ownerId, picked, users);
  const before = chat.status(ownerId).allowedUsers ?? [ownerId];
  await chat.setUsers(ownerId, next);
  return before.filter((id) => !(next.allowed_users ?? [ownerId]).includes(id));
}

export interface ChatUsersView {
  model: ChatUsersModel;
  status: ChatAppsStatus;
  ownerId: string;
  ownerName: string;
  url: string | null; // the address chat apps add
  form: string; // the save form's opening tag and CSRF fields
  base: string;
}

export function renderChatUsers(v: ChatUsersView): string {
  const { model, status } = v;
  const listed = (u: HaAuthUser): boolean => status.allowedUsers?.includes(u.id) === true;
  const others = model.users.filter((u) => u.id !== v.ownerId);
  const owner = model.users.find((u) => u.id === v.ownerId);
  const ownerRow = `<li><span class="person"><label><input type="checkbox" checked disabled> <strong>${escape(nameOf(owner))}</strong></label><span class="muted meta">Owner · Manage everything</span></span></li>`;
  const row = (u: HaAuthUser): string => {
    const name = escape(nameOf(u));
    const cap = roleCap(u);
    const level = effectiveLevel(u, status.levels[u.id]);
    const options = CHAT_LEVELS.slice(0, CHAT_LEVELS.indexOf(cap) + 1)
      .map((l) => `<option value="${l}"${l === level ? " selected" : ""}>${LEVEL[l]}</option>`).join("");
    const role = cap === "manage" ? "Administrator" : "User";
    return `<li><span class="person"><label><input type="checkbox" name="on_${u.id}" value="1"${listed(u) ? " checked" : ""}> <strong>${name}</strong></label><span class="muted meta">${role}${listed(u) ? ` · ${rowStatus(v, u, level)}` : ""}</span>${listed(u) ? joins(v, u, level) : ""}</span><select name="level_${u.id}" aria-label="${name}: level">${options}</select></li>`;
  };
  const saved = model.saved ? `<p class="ok">Saved.</p>` : "";
  const removed = model.removed.map((n) => `<p class="ok">${escape(n)} can't use HA NOVA in chat apps anymore. To sign ${escape(n)}'s chat app out of Home Assistant too, ${escape(n)} removes it under Profile → Security.</p>`).join("");
  const missing = `<p class="muted meta">Someone missing? Each person needs their own Home Assistant login. Add one under Settings → People.</p>`;
  const list = others.length === 0
    ? `<ul>${ownerRow}</ul>${missing}`
    : `${v.form}<ul>${ownerRow}${others.map(row).join("")}</ul><button type="submit">Save</button></form>${missing}
<p class="muted meta"><strong>Ask only:</strong> questions about the home. Changes nothing.<br><strong>Use the home:</strong> controls devices (lights, switches, climate, covers, locks, the alarm, media players, and more) and runs scenes, scripts, and automations. Doors, gates, and the alarm need a tap on their own phone first. Can't change automations, scripts, or device settings.<br><strong>Manage everything:</strong> what you can do. Only for Home Assistant administrators.</p>
<p class="muted meta">With anyone besides you on this list, going back to an older NOVA App refuses every chat request until you update it again.</p>`;
  return `<h3>Who can use it</h3>${saved}${removed}<p>Pick who may use HA NOVA in Claude and ChatGPT. Each person signs in with their own Home Assistant account.</p>${list}`;
}

// A listed user's row: whether they signed in yet, and their phone.
function rowStatus(v: ChatUsersView, u: HaAuthUser, level: ChatLevel): string {
  const name = escape(nameOf(u));
  const seen = v.model.seen === null ? "" : v.model.seen[u.id] === undefined ? "Not signed in yet" : "Signed in";
  if (level === "ask") {
    return seen;
  }
  const phone = v.status.phones[u.id];
  const link = (label: string, pick: boolean): string =>
    `<a href="${escape(`${v.base}?chat_user=${u.id}${pick ? "&chat_phone=pick" : ""}#chat-phone`)}" aria-label="${label}: ${name}">${label}</a>`;
  return phone !== undefined
    ? `Phone: “${escape(phone)}” · ${link("Change or stop…", false)}`
    : `${seen === "" ? "" : `${seen} · `}no phone · ${link("Link phone…", true)}`;
}

function joins(v: ChatUsersView, u: HaAuthUser, level: ChatLevel): string {
  const name = escape(nameOf(u));
  const add = v.url === null
    ? "In Claude or ChatGPT, add HA NOVA with the address above, once this page shows one."
    : `In Claude or ChatGPT, add HA NOVA with this link: <span class="address">${escape(v.url)}</span>`;
  const phone = level === "ask" ? "" : `<li>Ask ${escape(v.ownerName) || "the owner"} to link your phone here.</li>`;
  return `<details><summary>How ${name} joins</summary><ol><li>${add}</li><li>Sign in with your own Home Assistant login.</li>${phone}</ol></details>`;
}

const VERB: Record<string, string> = {
  turn_on: "Turned on", turn_off: "Turned off", toggle: "Switched", lock: "Locked", unlock: "Unlocked", open: "Opened",
  open_cover: "Opened", close_cover: "Closed", stop_cover: "Stopped", press: "Pressed", alarm_disarm: "Disarmed",
};

const RESULT: Record<string, string> = {
  done: "", phone: "confirmed on their phone", code: "confirmed with a chat code", declined: "cancelled on the phone",
  refused: "refused", failed: "failed", unknown: "may have happened",
};

// "Recent changes from chat apps": the bridge's last 20 lines, newest first.
export function renderChatActivity(activity: ChatActivity[] | null, now: number): string {
  if (activity === null || activity.length === 0) {
    return "";
  }
  const rows = activity.map((a) => {
    const [domain = "", service = ""] = a.what.split(".");
    const targets = (a.targets ?? []).join(", ");
    const verb = ["script", "scene", "automation"].includes(domain) ? "Ran" : service.startsWith("alarm_arm") ? "Armed" : VERB[service];
    const what = verb !== undefined && targets !== "" ? `${verb} ${escape(targets)}` : `<code>${escape(a.what)}</code>${targets === "" ? "" : ` ${escape(targets)}`}`;
    const result = RESULT[a.result] ?? escape(a.result);
    return `<li><span>${clock(a.at, now)} · ${escape(a.name)} · ${what}${result === "" ? "" : ` · ${result}`}</span></li>`;
  });
  return `<section><h2>Recent changes from chat apps</h2><ul>${rows.join("")}</ul></section>\n`;
}

// Server-local time (as the device list): the hour and minute today, else with the date.
function clock(at: string, now: number): string {
  const d = new Date(at);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  const pad = (n: number): string => String(n).padStart(2, "0");
  const day = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  const today = new Date(now);
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  return day === `${today.getFullYear()}-${pad(today.getMonth() + 1)}-${pad(today.getDate())}` ? time : `${day} ${time}`;
}

function nameOf(user: HaAuthUser | undefined): string {
  return typeof user?.name === "string" && user.name.trim() !== "" ? user.name.trim() : "Someone";
}

function escape(value: string): string {
  return value.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] ?? c);
}
