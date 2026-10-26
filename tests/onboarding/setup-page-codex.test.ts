// The setup page says where "Codex" works: the codex command and Codex in the ChatGPT
// desktop app (a new chat there loads HA NOVA), wherever it names Codex as set up or found.
import { describe, expect, it } from "vitest";

import { page } from "./setup-page-harness.js";

const found = {
  screen: "found", ha: "10.0.2.2", app: "running", app_version: "0.10.0",
  primary: "signin", accepts: ["address", "terminal", "signin"],
};
const clientsRow = (clients: string[], state = "ok") => ({
  id: "clients", state,
  msg: { key: state === "ok" ? "setup.page.rerun.clients_ok" : "setup.page.rerun.clients_fail", args: { clients } },
});

describe("setup page: where Codex works", () => {
  it("says it under the AI apps found on step 1, only with Codex", async () => {
    for (const [clients, says] of [[["Claude Code", "Codex"], true], [["OpenCode"], false]] as const) {
      const p = page({ ...found, clients });
      await p.settle();
      if (!says) {
        expect(p.find("codex-where")).toBeUndefined();
        continue;
      }
      const where = p.find("codex-where")!.textContent;
      expect(where).toBe(p.catalog["setup.codex.where"]);
      for (const words of ["codex command", "ChatGPT desktop app", "new chat"]) expect(where).toContain(words);
    }
  });

  it("says it on a re-run when Codex is set up, not when it failed or isn't there", async () => {
    for (const [row, says] of [[clientsRow(["Claude Code", "Codex"]), true], [clientsRow(["Codex"], "fail"), false], [clientsRow(["OpenCode"]), false]] as const) {
      const p = page({ screen: "rerun_fix", rows: [row], accepts: ["fix"] });
      await p.settle();
      expect(Boolean(p.find("codex-where"))).toBe(says);
    }
  });

  it("keeps a re-run's add button for apps found since", async () => {
    const p = page({ screen: "rerun", rows: [clientsRow(["Codex"])], new_clients: ["OpenCode"], accepts: ["clients"] });
    await p.settle();
    expect(p.find("codex-where")).toBeTruthy();
    p.find("b-clients")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "clients" }]);
  });

  it("says it on the terminal path's end too", async () => {
    const p = page({ screen: "done", clients: ["Codex"], accepts: [] });
    await p.settle();
    expect(p.find("codex-where")!.textContent).toBe(p.catalog["setup.codex.where"]);
  });
});
