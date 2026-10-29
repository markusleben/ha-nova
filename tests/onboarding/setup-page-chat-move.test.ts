// The setup page's offer to move chat apps after "Use another phone" (cli/setupweb/chat.js,
// setupPageChatMove): Move chat apps too or Keep, each phone's step, then how it ended.
import { describe, expect, it } from "vitest";

import { chatFixtures, page } from "./setup-page-harness.js";

const { url, link } = chatFixtures;
const from = "Markus’ iPhone";
const to = "Markus’ Pixel";
const step3 = (move: object, accepts = ["finish"]) => ({ screen: "control", accepts, control: { window: true, phone: "linked", phone_name: to, chat: { state: "on", url, claude_link: link, move } } });
const fill = (text: string, args: Record<string, string>) => text.replace(/\{([a-z_]+)\}/g, (_, name: string) => args[name] ?? `{${name}}`);

describe("setup page: moving chat apps to the new phone", () => {
  it("asks first, and only its buttons post the answer", async () => {
    const p = page(step3({ state: "offer", from, to }, ["finish", "chat_move"]));
    await p.settle();
    expect(p.find("chat-move")!.textContent).toContain(fill(p.catalog["setup.chat.move_offer"], { old: from, phone: to }));
    expect(p.catalog["setup.chat.move_offer"]).toContain("Each phone asks once more for chat apps.");
    p.find("b-chat-move-yes")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "chat_move", value: "yes" }]);
    const keep = page(step3({ state: "offer", from, to }, ["finish", "chat_move"]));
    await keep.settle();
    keep.find("b-chat-move-no")!.dispatch("click");
    await keep.settle();
    expect(keep.posts).toEqual([{ action: "chat_move", value: "no" }]);
  });

  it("says which phone to check, with its tap wording, while the move runs", async () => {
    const old = page(step3({ state: "run", from, to, step: "old", phone: from, os: "ios" }));
    await old.settle();
    expect(old.find("chat-move")!.textContent).toContain(fill(old.catalog["setup.page.control.phone_moving"], { old: from, phone: to }));
    expect(old.find("b-chat-move-yes")).toBeUndefined();
    const next = page(step3({ state: "run", from, to, step: "new", phone: to, os: "android" }));
    await next.settle();
    expect(next.find("chat-move")!.textContent).toContain(fill(next.catalog["setup.page.control.phone_waiting_android"], { phone: to }));
  });

  it("names how it ended, and the way to try again", async () => {
    for (const [state, key, role] of [["done", "setup.chat.move_done", "status"], ["kept", "setup.chat.move_kept", "status"], ["declined", "setup.chat.move_declined", "alert"],
      ["timeout", "setup.chat.move_timeout", "alert"], ["busy", "setup.chat.move_busy", "alert"], ["unknown", "setup.chat.move_unknown", "alert"], ["failed", "setup.chat.move_failed", "alert"]] as const) {
      const p = page(step3({ state, from, to, phone: from }));
      await p.settle();
      const line = p.find("chat-move")!;
      expect(line.textContent, state).toBe(fill(p.catalog[key], { old: from, phone: to, asks: from }));
      expect(line.attributes.role, state).toBe(role);
    }
  });
});
