import { escapeHtml } from "./handlers/nova-page-view.js";

// The NOVA approval page's words (gate spec §3, item 2), from
// cli/confirm_strings.*.json in Home Assistant's language, as the worker
// renders them on each view (POST /texts, cli/gate_texts.go): the Relay
// words nothing itself. The approver's name is already in page.signed_in
// (sayText); {minutes} and {count} take a number only. No words, no page.

export const PAGE_KEYS = [
  "page.title", "page.heading", "page.done_approved", "page.done_cancelled", "page.done_gone", "page.ends_in", "page.approve", "window.cancel",
  "page.empty", "page.more", "page.signed_in", "page.back", "page.cross_site", "page.expired", "page.open_again",
  "page.approver_unknown", "page.approver_unavailable", "page.approver_not_admin", "page.approver_relay_user",
  "page.waiting_one", "page.waiting_many", "page.open", "page.paused", "page.revoke_device", "page.link",
] as const;
export type PageKey = (typeof PAGE_KEYS)[number];

const LANG = /^[a-z]{2}(-[A-Z]{2})?$/;

// lang goes into <html lang>; say(key) is the text, HTML-escaped, with
// each number filled in.
export interface PageTexts {
  lang: string;
  say(key: PageKey, numbers?: Record<string, number>): string;
}

// The worker's answer for name ("" before anyone is known), or null when
// it fails or lacks a text.
export async function loadPageTexts(source: { texts(name: string): Promise<unknown> }, name: string): Promise<PageTexts | null> {
  const answer = await source.texts(name).catch(() => null);
  const { lang, texts: words } = (answer ?? {}) as { lang?: unknown; texts?: unknown };
  if (typeof lang !== "string" || !LANG.test(lang) || typeof words !== "object" || words === null) {
    return null;
  }
  const table = words as Record<string, unknown>;
  if (!PAGE_KEYS.every((key) => typeof table[key] === "string")) {
    return null;
  }
  return {
    lang,
    say: (key, numbers = {}) => Object.entries(numbers).reduce((text, [placeholder, n]) => text.replaceAll(`{${placeholder}}`, String(n)), escapeHtml(table[key] as string)),
  };
}
