import { escapeHtml } from "./handlers/nova-page-view.js";
import type { RelayLogger } from "./server.js";

// The NOVA approval page's words (gate spec §3, item 2), from
// cli/confirm_strings.*.json in Home Assistant's language, as the worker
// renders them (POST /texts, cli/gate_texts.go): the Relay words nothing
// itself. The approver's name is already in page.signed_in (sayText);
// {minutes} and {count} take a number only. No words, no page.

export const PAGE_KEYS = [
  "page.title", "page.heading", "page.done_approved", "page.done_cancelled", "page.done_gone", "page.ends_in", "page.approve", "window.cancel",
  "page.empty", "page.more", "page.signed_in", "page.back", "page.cross_site", "page.expired", "page.open_again",
  "page.approver_unknown", "page.approver_unavailable", "page.approver_not_admin", "page.approver_relay_user",
  "page.waiting_one", "page.waiting_many", "page.open", "page.paused", "page.revoke_device", "page.link",
  "page.paused_heading", "page.paused_left", "page.resume",
] as const;
export type PageKey = (typeof PAGE_KEYS)[number];

const LANG = /^[a-z]{2}(-[A-Z]{2})?$/;
const KEEP_MS = 60_000; // a changed Home Assistant language shows within a minute
const NAMES = 16; // the names kept at once; the oldest goes first

// lang goes into <html lang>; say(key) is the text, HTML-escaped, with
// each number filled in.
export interface PageTexts {
  lang: string;
  say(key: PageKey, numbers?: Record<string, number>): string;
}

// The page's words for name ("" before anyone is known), or null.
export type PageTextSource = (name: string) => Promise<PageTexts | null>;

// pageTexts: the worker's words, checked, kept a minute per name, one load
// shared while it runs; a load that fails is not kept.
export function pageTexts(load: (name: string) => Promise<unknown>, now: () => number, logger?: Pick<RelayLogger, "warn">): PageTextSource {
  const kept = new Map<string, { at: number; texts: Promise<PageTexts | null> }>();
  return (name) => {
    const hit = kept.get(name);
    if (hit !== undefined && now() - hit.at < KEEP_MS) {
      return hit.texts;
    }
    kept.delete(name);
    const texts = load(name).then((answer) => checked(answer, logger), () => null);
    kept.set(name, { at: now(), texts });
    const oldest = kept.keys().next();
    if (kept.size > NAMES && !oldest.done) {
      kept.delete(oldest.value);
    }
    void texts.then((t) => {
      if (t === null && kept.get(name)?.texts === texts) {
        kept.delete(name);
      }
    });
    return texts;
  };
}

// checked: the worker's answer as page texts, or null (logged: the worker
// logs its own failures, this the words it sent).
function checked(answer: unknown, logger?: Pick<RelayLogger, "warn">): PageTexts | null {
  if (answer === null) {
    return null;
  }
  const { lang, texts } = answer as { lang?: unknown; texts?: unknown };
  if (typeof lang !== "string" || !LANG.test(lang)) {
    logger?.warn("The safety-check worker sent the approval page no usable language", { lang: typeof lang === "string" ? lang.slice(0, 40) : typeof lang });
    return null;
  }
  const table = (typeof texts === "object" && texts !== null ? texts : {}) as Record<string, unknown>;
  const missing = PAGE_KEYS.find((key) => typeof table[key] !== "string" || table[key] === "");
  if (missing !== undefined) {
    logger?.warn("The safety-check worker sent the approval page no text for a key", { key: missing });
    return null;
  }
  return {
    lang,
    say: (key, numbers = {}) => Object.entries(numbers).reduce((text, [placeholder, n]) => text.replaceAll(`{${placeholder}}`, String(n)), escapeHtml(table[key] as string)),
  };
}
