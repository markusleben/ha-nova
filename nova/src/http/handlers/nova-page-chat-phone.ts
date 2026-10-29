import type { ChatApps } from "../../runtime/chat-apps.js";
import { ChatPhoneError, type ChatPhoneControl, type ChatPhoneJob, type ChatPhones } from "../../runtime/chat-phone-control.js";

// The NOVA page's chat-app phone (spec docs/work/2026-10-24-chat-app-phone-
// move-spec.md): Link, Change, and Stop, each through the bridge's control
// channel; a move or a stop asks the phone linked now first. Only these
// forms send override, and only for a phone that can never answer again
// (the bridge replaces nothing else with it). While a change runs, the page
// shows it from ?chat_job= and reloads every 5 s.

export type ChatPhoneAction = "chat_phone_move" | "chat_phone_replace" | "chat_phone_stop" | "chat_phone_remove";
export const CHAT_PHONE_ACTIONS = new Set<string>(["chat_phone_move", "chat_phone_replace", "chat_phone_stop", "chat_phone_remove"]);

export interface ChatPhoneModel {
  phones: ChatPhones | "unreadable" | "owner_token";
  pick: boolean; // ?chat_phone=pick: the list of phones to choose from
  stopped: boolean; // ?chat_phone=stopped: a gone phone's Stop took effect at once
  job: { id: string; state: ChatPhoneJob | { status: "unread" } } | null;
}

type CsrfField = (action: ChatPhoneAction) => string;

const JOB = /^[0-9a-f]{32}$/;

export async function readChatPhone(control: ChatPhoneControl, ownerId: string, query: URLSearchParams): Promise<ChatPhoneModel> {
  const id = query.get("chat_job");
  const [phones, job] = await Promise.all([
    control.phones(ownerId).catch((error: unknown) => (error instanceof ChatPhoneError && error.code === "OWNER_TOKEN" ? "owner_token" : "unreadable") as "owner_token" | "unreadable"),
    id !== null && JOB.test(id) ? control.job(ownerId, id).catch(() => ({ status: "unread" as const })) : null,
  ]);
  return { phones, pick: query.get("chat_phone") === "pick", stopped: query.get("chat_phone") === "stopped", job: id !== null && job !== null ? { id, state: job } : null };
}

// The job a move or a stop started (null: a gone phone's stop, done at once).
export async function applyChatPhoneAction(chat: ChatApps, ownerId: string, action: ChatPhoneAction, form: Record<string, unknown>): Promise<string | null> {
  const override = action === "chat_phone_replace" || action === "chat_phone_remove" ? { override: true as const } : {};
  if (action === "chat_phone_stop" || action === "chat_phone_remove") {
    return await chat.phone.start(ownerId, { action: "stop", ...override });
  }
  return await chat.phone.start(ownerId, { action: "move", phone: { device_id: typeof form.device_id === "string" ? form.device_id : "" }, ...override });
}

// The ?err= code of a change the bridge refused before it asked anyone.
export function chatPhoneErrorCode(error: unknown): string {
  if (!(error instanceof ChatPhoneError)) {
    return "chat_phone";
  }
  return error.code === "PHONE_CHANGE_RUNNING" ? "chat_phone_busy"
    : error.code === "CHAT_PHONE_UNKNOWN" ? "chat_phone_unknown"
    : error.status === 409 ? "chat_phone_changed" : "chat_phone";
}

export function renderChatPhoneError(code: string | null): string {
  const text: Record<string, string> = {
    chat_phone_busy: "A phone change is already running. Wait until it ends, then try again.",
    chat_phone_changed: "Nothing changed: the phones or the linked phone changed meanwhile. Check Chat apps below, then try again.",
    chat_phone: "Nothing changed: HA NOVA couldn't start this. Try again in a moment. If it keeps failing, the NOVA App log says why.",
    chat_phone_unknown: "HA NOVA couldn't tell whether this started, so it may still go ahead. If a phone asks, answer it there. Chat apps below shows which phone asks now.",
  };
  const title = code === "chat_phone_unknown" ? "Chat apps: phone change unclear" : "Chat apps: phone not changed";
  return code !== null && text[code] !== undefined ? `<section class="error"><h2>${title}</h2><p>${text[code]}</p></section>` : "";
}

export function chatPhoneWaiting(model: ChatPhoneModel | null): boolean {
  return model?.job?.state.status === "running" || model?.job?.state.status === "unread";
}

const WHY: Record<string, string> = {
  removed: "it is no longer in Home Assistant",
  not_allowed: "its Home Assistant user may no longer confirm changes from chat apps",
  relay_user: "it now belongs to HA NOVA's own Home Assistant user",
  unreachable: "its Home Assistant app is signed out, or its notifications are off",
  shared_name: "another device in Home Assistant has the same name; remove the old one in Settings → Devices &amp; services → Mobile App, or rename one in its Home Assistant app",
};

const LOST = "If that phone is lost, remove it in Home Assistant (Settings → Devices &amp; services → Mobile App). Then you can link a new phone here.";

// Confirmations line of the chat-app section: a link that can't be asked
// refuses, it never falls back to a code.
export function chatPhoneLine(model: ChatPhoneModel | null, ownerPhone: string | null, chatCodes: boolean): string {
  const link = typeof model?.phones === "object" ? model.phones.link : undefined;
  if (link !== undefined && link !== null && link.state !== "ready") {
    return `${named(link.name)} ${link.state === "gone" ? "can't answer anymore" : "can't be asked right now"}, so changes that need your OK are refused`;
  }
  if (ownerPhone === null) {
    return chatCodes ? "no phone linked: changes that need your OK show a code in the chat" : "no phone linked: changes that need your OK are refused";
  }
  return `your phone ${escape(ownerPhone)} asks you`;
}

export function renderChatPhone(model: ChatPhoneModel, chatCodes: boolean, formOpen: string, csrfField: CsrfField, base: string): string {
  const back = `<a class="linkbtn" href="${escape(base)}">`;
  if (model.job !== null) {
    return renderJob(model.job.state, chatCodes, back);
  }
  const stopped = model.stopped ? `<p class="ok">${stoppedLine(chatCodes)}</p>` : "";
  const phones = model.phones;
  if (phones === "unreadable") {
    return `<p class="muted meta">HA NOVA can't read this home's phones right now. Reload this page in a moment.</p>`;
  }
  if (phones === "owner_token") {
    return `<p class="muted meta">No phone can confirm for chat apps here: this NOVA App signs in to Home Assistant as the owner, so a tap can't be told apart from HA NOVA itself.</p>`;
  }
  const link = phones.link;
  const gone = link?.state === "gone";
  if (model.pick && link?.state !== "unavailable") {
    // One form, so one token however many phones: the bridge takes only one of this user's phones that can confirm.
    const action: ChatPhoneAction = gone ? "chat_phone_replace" : "chat_phone_move";
    const rows = phones.phones.map((p) => `<li><label><input type="radio" name="device_id" value="${escape(p.device_id)}" required> ${escape(p.name)}</label></li>`).join("");
    const first = link?.state === "ready" ? `${named(link.name)} asks first, then the phone you pick.` : "The phone you pick asks once.";
    return phones.phones.length === 0
      ? `<p>No other phone can confirm. A phone needs the Home Assistant app, signed in with your own account.</p><p>${back}Back</a></p>`
      : `<p><strong>Which phone should confirm for chat apps?</strong> ${first}</p>${formOpen}${csrfField(action)}<ul>${rows}</ul><button type="submit">Use this phone</button></form><p>${back}Cancel</a></p>`;
  }
  const pick = (label: string): string => `<a class="linkbtn" href="${escape(`${base}?chat_phone=pick`)}">${label}</a>`;
  const stop = (action: ChatPhoneAction): string => `${formOpen}${csrfField(action)}<button class="secondary" type="submit">Stop phone confirmations</button></form>`;
  if (link === null) {
    return stopped + (phones.phones.length === 0 ? `<p class="muted meta">To confirm with a phone, sign in to the Home Assistant app on it with your own account.</p>` : `<p>${pick("Link a phone…")}</p>`);
  }
  if (link.state === "ready") {
    return `<div>${pick("Change phone…")} ${stop("chat_phone_stop")}</div>`;
  }
  if (link.why === "unread") {
    return `<p class="muted meta">HA NOVA can't check ${named(link.name)} just now. Reload this page in a moment.</p>`;
  }
  const why = WHY[link.why] ?? "it can't answer as it is";
  const after = chatCodes ? "show a code in the chat, which your AI assistant can see too" : "stay refused until a phone is linked again";
  return gone
    ? `<p class="error">${named(link.name)} can't answer anymore: ${why}. You can link a new phone here without it.</p><div>${pick("Link a new phone…")} ${stop("chat_phone_remove")}</div><p class="muted meta">Stop takes effect at once, without a question: changes that need your OK then ${after}.</p>`
    : `<p class="error">HA NOVA can't ask ${named(link.name)} right now: ${why}.</p><p class="muted meta">${LOST}</p>`;
}

function stoppedLine(chatCodes: boolean): string {
  return chatCodes ? "Done. Chat apps no longer ask a phone: changes that need your OK show a code in the chat."
    : "Done. Chat apps no longer ask a phone: changes that need your OK are refused until a phone is linked again.";
}

function renderJob(job: ChatPhoneJob | { status: "unread" }, chatCodes: boolean, back: string): string {
  if (job.status === "unread") {
    return `<p class="muted waiting">HA NOVA can't read this phone change right now… this page updates on its own.</p><p>${back}Back</a></p>`;
  }
  const { step, phone, platform, to } = job as ChatPhoneJob;
  if (job.status === "running") {
    const line = step === "new" ? `Check ${named(phone)} and ${tapLine(platform, "Allow")}`
      : to !== undefined ? `Check ${named(phone)} first and ${tapLine(platform, "Move")} Then ${named(to)} asks too.`
      : `Check ${named(phone)} and ${tapLine(platform, "Stop")}`;
    return `<p>${line}</p><p class="muted waiting">Waiting for the phone… this page updates on its own. Nothing on the phone? Open its Home Assistant app; its notifications need to be on.</p>`;
  }
  const lost = step === "old" ? ` ${LOST}` : ""; // the phone linked now didn't answer: it may be lost
  const results: Record<string, string> = {
    done: to !== undefined ? `Done. Chat apps now ask ${named(to)}.` : stoppedLine(chatCodes),
    declined: `Nothing changed. It was cancelled on ${named(phone)}.`,
    timeout: `Nothing changed. No one tapped in time on ${named(phone)}. If it's at hand, open its Home Assistant app and try again.${lost}`,
    rejected: "Nothing changed. The tap came from another Home Assistant user. Sign in to the Home Assistant app on that phone with your own account, then try again.",
    changed: "Nothing changed. The phone, its user, or the chat-app settings changed meanwhile. Choose OK to see them as they are now, then try again.",
    failed: `Nothing changed. HA NOVA couldn't ask ${named(phone)}, or couldn't check the answer. Try again in a moment.${lost}`,
    lost: "HA NOVA lost track of this change: the NOVA App or its chat-app bridge restarted. The Confirmations line above shows which phone asks now.",
  };
  return `<p>${results[job.status] ?? "Something interrupted this. The Confirmations line above shows which phone asks now."}</p><p>${back}OK</a></p>`;
}

// How the buttons show on that phone (as the computer says it, phoneTapLine).
function tapLine(platform: string | undefined, button: string): string {
  return platform === "ios" ? `press and hold the notification, then tap “${button}”.`
    : platform === "android" ? `tap “${button}” in the notification. No button? Pull it down, or tap its arrow.`
    : `tap “${button}” in the notification. To see the buttons, press and hold it on an iPhone, or pull it down on Android.`;
}

function named(name: string | undefined): string {
  return name ? `“${escape(name)}”` : "the linked phone";
}

function escape(value: string): string {
  return value.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c] ?? c);
}
