// The setup page tells a family member how HA NOVA works with one owner: the
// owner's sign-in connects this computer once, for anyone using this login on it.
import { describe, expect, it } from "vitest";

import { page } from "./setup-page-harness.js";

const found = {
  screen: "found", ha: "10.0.2.2", clients: ["OpenCode"], app: "running", app_version: "0.10.0",
  primary: "signin", accepts: ["address", "terminal", "signin"],
};
const notOwner = { screen: "connect", rows: [{ id: "signed_in", state: "ok" }], fix: "owner", links: {}, accepts: ["address", "terminal", "signin"],
  problem: { key: "setup.page.problem.not_owner", args: { name: "Anna" } } };
const ONCE = "The owner's sign-in connects this computer once. After that, anyone who uses this login on this computer can use HA NOVA.";

describe("setup page: the owner connects this computer once", () => {
  it("says who signs in and that it connects this computer for anyone using this login on it", async () => {
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

describe("setup page: the six-digit code while the App still needs installing or updating", () => {
  it("asks for the owner to install or update the App first, on the sign-in screen and after a failed sign-in", async () => {
    for (const [primary, label] of [
      ["signin", "Use a six-digit code instead (continues in the terminal)"],
      ["install", "Ask the owner to install the NOVA App, then use a six-digit code (continues in the terminal)"],
      ["update", "Ask the owner to update the NOVA App, then use a six-digit code (continues in the terminal)"],
    ]) {
      for (const state of [{ ...found, primary }, { ...notOwner, primary }, { ...notOwner, primary, fix: "retry", problem: { key: "setup.page.problem.timeout" } }]) {
        const p = page(state);
        await p.settle();
        const code = p.find("b-terminal")!;
        expect(code.textContent).toBe(label);
        expect(p.screen.textContent).toContain(`Can't sign in on this computer? ${label}`);
        code.dispatch("click");
        await p.settle();
        expect(p.posts).toEqual([{ action: "terminal" }]);
      }
    }
  });
});

describe("setup page: hand-over for the code while the App still needs installing or updating", () => {
  it("says the terminal first shows how the owner installs or updates the App, then asks for the code", async () => {
    for (const [handover, verb] of [["code_install", "installs"], ["code_update", "updates"]]) {
      const p = page({ screen: "terminal", accepts: [], handover });
      await p.settle();
      expect(p.screen.textContent).toContain(`It first shows how the owner ${verb} the NOVA App in Home Assistant, then asks for a six-digit code.`);
      expect(p.screen.textContent).not.toContain(p.catalog["setup.page.terminal.lead_code"]);
    }
  });
});
