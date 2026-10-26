// The setup page tells a family member how HA NOVA works with one owner: the
// owner's sign-in connects this computer once, for everyone who uses it.
import { describe, expect, it } from "vitest";

import { page } from "./setup-page-harness.js";

const found = {
  screen: "found", ha: "10.0.2.2", clients: ["OpenCode"], app: "running", app_version: "0.10.0",
  primary: "signin", accepts: ["address", "terminal", "signin"],
};
const notOwner = { screen: "connect", rows: [{ id: "signed_in", state: "ok" }], fix: "owner", links: {}, accepts: ["address", "terminal", "signin"],
  problem: { key: "setup.page.problem.not_owner", args: { name: "Anna" } } };
const ONCE = "The owner's sign-in connects this computer once. After that, anyone who uses this computer can use HA NOVA.";

describe("setup page: the owner connects this computer once", () => {
  it("says who signs in and that it connects this computer for everyone who uses it", async () => {
    for (const primary of ["signin", "install", "update"]) {
      const p = page({ ...found, primary });
      await p.settle();
      expect(p.catalog["setup.page.found.owner_once"]).toBe(ONCE);
      expect(p.screen.all().find((n) => n.className === "owner")!.textContent)
        .toBe(`Sign in as the owner: the first account made when Home Assistant was set up. ${ONCE}`);
    }
    const web = page({ ...found, clients: undefined, web: true }); // Claude or ChatGPT: no computer gets connected
    await web.settle();
    expect(web.screen.textContent).toContain(web.catalog["setup.page.found.owner"]);
    expect(web.screen.textContent).not.toContain(ONCE);
  });

  it("asks a non-owner for the owner, and offers the code only when NOVA already runs", async () => {
    const p = page({ ...notOwner, primary: "signin" });
    await p.settle();
    expect(p.find("b-signin")!.textContent).toBe("Sign in as the owner");
    expect(p.screen.textContent).toContain(ONCE);
    expect(p.screen.textContent).toContain(p.catalog["setup.page.found.owner_away"]);
    for (const primary of ["install", "update"]) { // no NOVA App to make a code with yet
      const q = page({ ...notOwner, primary, problem: { key: "setup.page.problem.not_owner_unnamed" } });
      await q.settle();
      expect(q.screen.textContent).toContain("That account isn't the owner of this Home Assistant. Only the owner can set up HA NOVA. Ask the owner to sign in here.");
      expect(q.screen.textContent).toContain(ONCE);
      expect(q.screen.textContent).not.toContain(q.catalog["setup.page.found.owner_away"]);
      expect(q.screen.textContent).not.toContain("six-digit code in Home Assistant");
    }
  });
});
