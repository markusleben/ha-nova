// The setup page names a named server profile in its header (MAR-241), so a
// person with two Home Assistants sees which one this setup changes.
import { describe, expect, it } from "vitest";

import { page } from "./setup-page-harness.js";

const found = {
  screen: "found", ha: "ha.local", clients: ["OpenCode"], app: "running", app_version: "0.10.0",
  primary: "signin", accepts: ["address", "terminal", "signin"],
};

describe("setup page: the server it sets up", () => {
  it("names a named profile and its address, and nothing for the default one", async () => {
    const named = page({ ...found, server: "markus-home" });
    await named.settle();
    expect((named.server as unknown as { hidden: boolean }).hidden).toBe(false);
    expect(named.server.textContent).toContain("markus-home");
    expect(named.server.textContent).toContain("ha.local");
    const plain = page(found);
    await plain.settle();
    expect((plain.server as unknown as { hidden: boolean }).hidden).toBe(true);
    expect(plain.server.textContent).toBe("");
  });
});
