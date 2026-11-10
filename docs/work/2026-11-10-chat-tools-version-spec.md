# Chat apps: tools version and the ChatGPT refresh hint (#923)

Status: active

## Problem

ChatGPT keeps HA NOVA's tool list and instructions from when the plugin was created or last refreshed. After a NOVA App update that changed them, it keeps working from the old text until the user opens chatgpt.com/plugins, chooses HA NOVA, and taps Refresh. Claude reads the tool list again on its own. The bridge is stateless and can't tell which text a client holds. Since 0.29 the terminal shows a refresh line after every App update while chat apps are on; users who never touch the terminal don't see it.

## Design (conservative: on every tools change, per #923's plan of 2026-11-10)

**Tools version.** `ha-nova serve` computes it once at start: the first 12 hex digits of sha256 over the JSON of its `tools/list` result, a NUL byte, and the instructions. It changes only when the tool text or the instructions change, not on every App update. It is not written into any tool description, so the tool text (and claude.ai's Always allow, `TestMCPToolTextStable`) stays as it is. #923's earlier idea, a version in every description, would let a model compare versions; the conservative design doesn't need that, so it is left out.

**Bridge state** in its `--data-dir` (0600, the bridge's own):
- `tools-version.json`, `{"version": "...", "changed_at": "RFC 3339" | ""}`. At start: no file and no user folder yet: `changed_at` is "" (a fresh setup has nothing to refresh). No file but user folders exist (the first App with this feature): `changed_at` is now. A different version: version and now. Same version: unchanged.
- `users/<id>/tools-version`: the version that user last had. A user folder the bridge creates (a first request, or the owner linking that user's phone) starts with the current version: nothing to refresh. A record that differs, or none (a folder an older App made), makes the user's next `tools/call` carry the hint; then the current version is recorded. Only `tools/call` carries it, one request at a time per user.

**In the chat.** The hint follows the result's own `next_step`, in the result's own rendering; a result without an envelope (a skill's text) gets it as a text of its own: "Tell the user once, above any one-time code: HA NOVA was updated. If they use HA NOVA in ChatGPT, they open chatgpt.com/plugins, choose HA NOVA, then Refresh, so ChatGPT gets its new tools and instructions; Claude needs nothing." The bridge can't tell Claude from ChatGPT (the integration forwards neither client name nor user agent) and one Home Assistant user may use both, so every known user hears it once per change, in whichever app calls first. Delivery is best effort: a record that can't be written only logs, and a reply lost after the record was written isn't sent again; the NOVA page box covers both.

**On the NOVA page.** A new control route `GET /control/v1/tools` answers `{version, changed_at}`. While chat apps are on and `changed_at` is within the last 30 days, the chat-apps section shows: "HA NOVA's tools changed on <date>. Using HA NOVA in ChatGPT? Open chatgpt.com/plugins, choose HA NOVA, then Refresh. Claude needs nothing." The bridge unreachable, or an older bridge (404): no box.

## Known limits

- A ChatGPT session whose tool names changed gets a JSON-RPC error for an unknown tool, which carries no hint; the box covers it.
- A user who refreshed ChatGPT while the App was rolled back, then updated again to a version they already had, hears nothing.
- A folder an older App made for a user who never chatted (a phone linked first) brings one needless line at their first call.

## Out of scope

- Telling whether a ChatGPT user already refreshed. The maintainer's step 4 (#923) on his own setup at the 0.30 App update tunes the wording in a follow-up.
- The terminal line after an App update (0.29) stays as is.
- The hosted demo: its data dir resets, so its users are always new.

## Compatibility

No shipped contract changes: tool text, plan file schema, `chat-apps.json`, `phone-links.json`, and `min_relay_version` stay. The control route is new; the App and the bridge ship in one image. Rolled back to an older App, nothing reads the two files and nothing shows. The hosted demo's bridge sends no hint.

## Cross-checks (2026-11-10)

- Astra (openai/gpt-6-astra, medium): agrees with the core (hash of tools and instructions, no version in descriptions, advisory through results and the page). Dissented on the first draft's "If this chat is ChatGPT" wording: a Claude call could use up the hint of a user on both apps. Fixed by the unconditional wording above. Asked for synchronized first-request handling and best-effort wording: done.
- Fable (claude-code/claude-fable-5-2, medium): agrees. Asked for the hint on invalid-argument results and skill texts, the both-apps case, and folders the phone link creates: all in. Kept: start-up fails loud when `tools-version.json` can't be read or written (the same folder holds every plan), and 30 days for the box (Fable would take 14).

## Tests

Go: the version is stable across calls and changes with a description or the instructions; the start-up cases of `tools-version.json`; a new user gets no hint; a user with an old record, or none, gets it exactly once, in an envelope result, an invalid-argument result, or a skill's text; the record is written after it; the control route. App: the box shows within 30 days with chat apps on, not before or after, not when the bridge can't say; the client refuses a malformed answer.
