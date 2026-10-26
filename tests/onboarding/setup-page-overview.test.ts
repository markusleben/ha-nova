import { describe, expect, it } from "vitest";

import { chatFixtures, page } from "./setup-page-harness.js";

const sections: Record<string, string> = { link: "home", window: "confirm", phone: "confirm", chat: "chat", chat_phone: "chat" };
const item = (id: string, state: string, label: string, key: string, action = "", value = "") =>
  ({ id, state, label, msg: { key: `setup.overview.${key}` }, action, value, section: sections[id] || "apps" });
const state = {
  screen: "rerun", rows: [], accepts: ["clients", "window", "phone", "finish"],
  links: { nova: "http://homeassistant.local:8123/app" },
  control: { window: false, phone: "pick", phones: [{ id: "p1", name: "My phone", person: "Me" }], link: "offer", chat: { state: "page_off" } },
  overview: [
    item("claude", "ok", "Claude Code", "client_on"),
    item("codex", "todo", "Codex", "client_off", "clients", "codex"),
    item("window", "todo", "Confirm on this computer", "window_off", "window"),
    item("phone", "todo", "Phone for this computer", "phone_off", "phone"),
    item("chat", "todo", "Chat apps", "chat_off", "chat"),
    item("chat_phone", "unknown", "Phone for chat apps", "chat_phone_unread", "nova"),
    item("link", "todo", "Home Assistant Link", "link_off", "link"),
  ],
};

describe("setup re-run overview", () => {
  it("lists each app and optional step without claiming unknown phones are off", async () => {
    const p = page(state);
    await p.settle();
    expect(p.screen.textContent).toContain("Your setup");
    for (const row of state.overview) expect(p.screen.textContent).toContain(row.label);
    expect(p.screen.textContent).toContain(p.catalog["setup.overview.chat_phone_unread"]);
    expect(p.find("l-overview-nova")!.attributes.href).toBe(state.links.nova);
    expect(p.posts).toEqual([]);
    p.find("b-overview-codex")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "clients", value: "codex" }]);
  });

  it("reuses the window action and phone linking controls", async () => {
    const p = page(state);
    await p.settle();
    p.find("b-overview-window")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "window", value: "" }]);
    const phone = page(state);
    await phone.settle();
    expect(phone.screen.all().some((node) => node.tag === "ul" && node.className === "rows" && node.all().includes(phone.find("b-phone-0")!))).toBe(true);
    phone.find("b-phone-0")!.dispatch("click");
    await phone.settle();
    expect(phone.posts).toEqual([{ action: "phone", value: "p1" }]);
  });

  it("keeps chat and Link switches pending until Done", async () => {
    const p = page(state);
    await p.settle();
    p.find("b-chat-switch")!.dispatch("click");
    p.find("b-link")!.dispatch("click");
    expect(p.posts).toEqual([]);
    p.find("b-finish")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "finish", value: "link,chat_on" }]);
  });

  it("shows Add to Claude and the ChatGPT address without claiming either was added", async () => {
    const p = page({ ...state, control: { ...state.control, chat: { state: "page_on", url: chatFixtures.url, claude_link: chatFixtures.link } },
      overview: [item("chat", "ok", "Chat apps", "chat_on", "chat"), item("chat_phone", "unknown", "Phone for chat apps", "chat_phone_unread", "nova")] });
    await p.settle();
    expect(p.screen.textContent).toContain("Add to Claude if you haven't yet");
    expect(p.find("l-chat-claude")!.attributes.href).toBe(chatFixtures.link);
    expect(p.find("chat-url")!.textContent).toBe(chatFixtures.url);
    expect(p.find("chat-next")!.textContent).toContain(p.catalog["setup.chat.next_check"]);
  });

  it("does not invent unavailable setup actions or a Link row", async () => {
    const p = page({ ...state, overview: [item("window", "unknown", "Confirm on this computer", "window_unavailable"),
      item("phone", "unknown", "Phone for this computer", "phone_unread", "phone")], control: { window: false, phone: "unread" }, accepts: ["finish", "recheck"] });
    await p.settle();
    expect(p.find("b-overview-window")).toBeUndefined();
    expect(p.find("b-link")).toBeUndefined();
    expect(p.find("b-phone-recheck")).toBeDefined();
  });

  it("shows the manual MCP snippet and copies it without changing other apps", async () => {
    const snippet = JSON.stringify({ mcpServers: { "ha-nova": { command: "C:\\Program Files\\HA NOVA\\ha-nova.exe", args: ["mcp", "--server", "home"] } } }, null, 2);
    const p = page({ ...state, overview: [{ ...item("other", "unknown", "Other AI apps", "other", "mcp"), snippet }] });
    const copied: string[] = [];
    p.navigator.clipboard = { writeText: async (text: string) => { copied.push(text); } };
    await p.settle();
    expect(p.screen.textContent).toContain("Another AI app can use HA NOVA too");
    expect(p.find("other-mcp")!.textContent).toBe(snippet);
    p.find("b-other-mcp-copy")!.dispatch("click");
    await p.settle();
    expect(copied).toEqual([snippet]);
    expect(p.posts).toEqual([]);
  });

  it("groups rows under headings and folds apps not found here", async () => {
    const p = page({ ...state, rows: [{ id: "ha", state: "ok", msg: { key: "setup.page.rerun.ha", args: { host: "homeassistant.local" } } }],
      overview: [...state.overview, { ...item("hermes", "unknown", "Hermes Agent", "client_missing"), more: true },
        { ...item("other", "unknown", "Other AI apps", "other", "mcp"), snippet: "{}" }] });
    await p.settle();
    const headings = p.screen.all().filter((node) => node.tag === "h2").map((node) => node.textContent);
    expect(headings).toEqual(["setup.overview.section_home", "setup.overview.section_apps", "setup.overview.section_confirm", "setup.overview.section_chat"].map((key) => p.catalog[key]));
    const text = p.screen.textContent;
    expect(text.indexOf("homeassistant.local")).toBeLessThan(text.indexOf("Home Assistant Link"));
    expect(text.indexOf("Codex")).toBeLessThan(text.indexOf(p.catalog["setup.overview.more_label"]));
    expect(text.indexOf(p.catalog["setup.overview.more_label"])).toBeLessThan(text.indexOf("Other AI apps"));
    expect(text.indexOf("Other AI apps")).toBeLessThan(text.indexOf("Confirm on this computer"));
    const fold = p.screen.all().find((node) => node.tag === "details" && node.all().includes(p.find("overview-more")!))!;
    expect(fold.open).toBeFalsy();
    expect(fold.textContent).toContain("Hermes Agent");
    expect(p.find("b-overview-hermes")).toBeUndefined();
  });

  it("opens pending consent and progress after a page refresh", async () => {
    for (const chat of [{ state: "offer", ask: true }, { state: "signin" }, { state: "run" }]) {
      const p = page({ ...state, control: { ...state.control, chat }, overview: [item("chat", "unknown", "Chat apps", "chat_wait", "chat")] });
      await p.settle();
      expect(p.screen.all().find((node) => node.tag === "details")!.open).toBe(true);
    }
  });
});
