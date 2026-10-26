// The setup page's advice under Add to Claude: what to set so Claude stops asking before each step.
import { describe, expect, it } from "vitest";

import { chatFixtures, page } from "./setup-page-harness.js";

describe("setup page: Add to Claude advice", () => {
  const { url, link, control } = chatFixtures;

  // Decision 2 of the chat-parity spec: with a phone that confirms, every tool on “Always allow”;
  // without one, all but “Run a confirmed change”, so Claude's question still guards a code in the chat.
  it("says under Add to Claude what to set so Claude stops asking, by whether a phone confirms", async () => {
    const next = (p: ReturnType<typeof page>, key: string) => `${p.catalog["setup.chat.next_title"]} ${p.catalog[key]}`;
    for (const [phone, key, other] of [["Pixel", "setup.chat.next_phone", "setup.chat.next_code"], ["", "setup.chat.next_code", "setup.chat.next_phone"]] as const) {
      const p = page(control({ state: "on", url, claude_link: link, phone: "Pixel", ...(phone ? { phone_confirmed: true } : {}) })); // a name alone confirms nothing
      await p.settle();
      expect(p.find("chat-next")!.textContent).toBe(next(p, key));
      expect(p.screen.textContent).not.toContain(p.catalog[other]);
      const home = page({ screen: "home", accepts: ["done"], home: { chat_link: link, ...(phone ? { chat_phone: true } : {}) } });
      await home.settle();
      expect(home.find("chat-next-4")!.textContent).toBe(next(home, key));
    }
    const rerun = page(control({ state: "page_on", url, claude_link: link })); // a re-run reads no phone: the safe advice, and where to look
    await rerun.settle();
    expect(rerun.find("chat-next")!.textContent).toBe(`${next(rerun, "setup.chat.next_code")} ${rerun.catalog["setup.chat.next_check"]}`);
    const rerunPhone = page(control({ state: "page_on", url, claude_link: link, phone_confirmed: true }));
    await rerunPhone.settle();
    expect(rerunPhone.find("chat-next")!.textContent).toBe(next(rerunPhone, "setup.chat.next_phone"));
    const plain = page({ screen: "home", accepts: ["done"], home: {} });
    await plain.settle();
    expect(plain.find("chat-next-4")).toBeUndefined(); // chat apps off: nothing to set
    for (const key of ["setup.chat.next_phone", "setup.chat.next_code"]) {
      expect(rerun.catalog[key]).toContain("That's Claude, not HA NOVA.");
      expect(rerun.catalog[key]).toContain("Customize → Connectors → HA NOVA");
    }
    expect(rerun.catalog["setup.chat.next_code"]).toContain("except “Run a confirmed change”: leave it on “Needs approval”. Then Claude still asks before each bigger change. The AI can see a chat code, so that question is your own check.");
  });
});
