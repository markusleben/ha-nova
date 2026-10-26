// The setup page says which Claude gets HA NOVA: Claude Code is the claude command and Claude
// Desktop's Code tab, not its chat tab; and Claude Desktop alone still needs Claude Code.
import { describe, expect, it } from "vitest";

import { page } from "./setup-page-harness.js";

const install = "curl -fsSL https://claude.ai/install.sh | bash";

describe("setup page: Claude Desktop without Claude Code", () => {
  it("says Claude Code is still needed, with its install line and Check again", async () => {
    const p = page({ screen: "noclient", desktop: true, accepts: ["recheck", "web"] });
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.noclient.title_desktop"]);
    expect(p.screen.textContent).toContain(p.catalog["setup.page.noclient.lead_desktop"]);
    expect(p.screen.textContent).toContain(p.catalog["setup.page.noclient.install_desktop"]);
    expect(p.find("claude-install")!.textContent).toBe(install);
    const copied: string[] = [];
    p.navigator.clipboard = { writeText: (text: string) => { copied.push(text); return Promise.resolve(); } };
    p.find("b-claude-install-copy")!.dispatch("click");
    await p.settle();
    expect(copied).toEqual([install]);
    expect(p.body.textContent).toContain(p.catalog["setup.page.noclient.copied"]);
    expect(p.find("l-desktop")).toBeUndefined(); // already here
    expect(p.find("l-code")!.attributes.href).toBe("https://claude.com/claude-code");
    p.find("b-recheck")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "recheck" }]);
  });

  it("keeps the plain no-app screen without Claude Desktop", async () => {
    const p = page({ screen: "noclient", accepts: ["recheck", "web"] });
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.noclient.title"]);
    expect(p.find("claude-install")).toBeUndefined();
    expect(p.find("l-desktop")).toBeTruthy();
    expect(p.find("l-code")!.textContent).toBe(p.catalog["setup.page.link.claude_code"]);
  });
});

describe("setup page: which Claude got HA NOVA", () => {
  const clientsRow = (clients: string[], state = "ok") => ({
    id: "clients", state,
    msg: { key: state === "ok" ? "setup.page.rerun.clients_ok" : "setup.page.rerun.clients_fail", args: { clients } },
  });
  const rerun = (row: object) => ({ screen: "rerun_fix", rows: [row], accepts: ["fix"] });

  it("names the Code tab and the chat tab's way in, under Set up in Claude Code", async () => {
    const p = page(rerun(clientsRow(["Claude Code", "Codex CLI"])));
    await p.settle();
    const where = p.find("claude-where")!;
    expect(where.textContent).toContain(p.catalog["setup.claude.where"]);
    for (const words of ["Code tab", "chat tab", "chat apps", "reachable from the internet"]) expect(where.textContent).toContain(words);
    const guide = p.find("l-chat-guide")!;
    expect([guide.attributes.href, guide.attributes.target]).toEqual(["https://github.com/markusleben/ha-nova/blob/main/docs/reference/chat-apps.md", "_blank"]);
  });

  it("says nothing about Claude where Claude Code isn't set up, or isn't ready", async () => {
    for (const row of [clientsRow(["Codex CLI"]), clientsRow(["Claude Code"], "fail")]) {
      const p = page(rerun(row));
      await p.settle();
      expect(p.find("claude-where")).toBeUndefined();
    }
  });

  it("says it on step 4, where a first run ends, only with Claude Code", async () => {
    for (const claude of [true, false]) {
      const p = page({ screen: "home", home: { claude }, accepts: ["done", "claude"] });
      await p.settle();
      if (claude) expect(p.find("claude-where")!.textContent).toContain(p.catalog["setup.claude.where"]);
      else expect(p.find("claude-where")).toBeUndefined();
    }
  });

  it("says it on the terminal path's end too", async () => {
    const p = page({ screen: "done", clients: ["Claude Code"], accepts: [] });
    await p.settle();
    expect(p.find("claude-where")!.textContent).toContain(p.catalog["setup.claude.where"]);
  });
});
