// A re-run that needs a fix can move to another Home Assistant: "Use another
// Home Assistant" opens step 1's address field (cli/setup_page_rerun.go).
import { describe, expect, it } from "vitest";

import { page } from "./setup-page-harness.js";

describe("setup page re-run: another Home Assistant", () => {
  const rows = [
    { id: "ha", state: "ok", msg: { key: "setup.page.rerun.ha", args: { host: "10.85.1.2" } } },
    { id: "app", state: "todo", msg: { key: "setup.page.app.old" } },
    { id: "computer", state: "ok", msg: { key: "setup.page.rerun.computer_ok" } },
    { id: "clients", state: "ok", msg: { key: "setup.page.rerun.clients_ok", args: { clients: ["Claude Code"] } } },
  ];
  const old = { screen: "rerun_fix", rows, fix: "app_update", accepts: ["recheck", "fix", "change"], problem: { key: "setup.page.rerun.problem_app_old" } };

  it("offers it under the App update, and the click asks for the address", async () => {
    const p = page(old);
    await p.settle();
    expect(p.find("b-fix")!.textContent).toBe(p.catalog["setup.page.rerun.update_app"]);
    expect(p.find("b-change-ha")!.textContent).toBe("Use another Home Assistant");
    p.find("b-change-ha")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "change" }]);
  });

  it("shows it only while the engine takes it", async () => {
    const p = page({ ...old, accepts: ["recheck", "fix"] });
    await p.settle();
    expect(p.find("b-change-ha")).toBeUndefined();
  });
});
