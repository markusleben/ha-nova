// The setup page's script (cli/setupweb/app.js + steps.js) against a small
// fake DOM: what a click shows next. The Go tests cover the CLI side.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { runInNewContext } from "node:vm";
import { describe, expect, it } from "vitest";

const WEB = resolve(__dirname, "../../cli/setupweb");
const read = (name: string) => readFileSync(resolve(WEB, name), "utf8");

type Listener = (event: { preventDefault: () => void }) => void;

class FakeNode {
  children: FakeNode[] = [];
  attributes: Record<string, string> = {};
  listeners: Record<string, Listener[]> = {};
  className = "";
  value = "";
  open = false;
  dataset: Record<string, string> = {};
  classList = { toggle: () => undefined };
  constructor(public tag: string, public text = "") {}
  get id() { return this.attributes.id ?? ""; }
  get textContent(): string { return this.text + this.children.map((c) => c.textContent).join(""); }
  set textContent(value: string) { this.text = value; this.children = []; }
  setAttribute(name: string, value: string) { this.attributes[name] = String(value); }
  addEventListener(type: string, fn: Listener) { (this.listeners[type] ??= []).push(fn); }
  dispatch(type: string) { for (const fn of this.listeners[type] ?? []) fn({ preventDefault: () => undefined }); }
  append(...nodes: FakeNode[]) { this.children.push(...nodes); }
  replaceChildren(...nodes: FakeNode[]) { this.children = nodes; }
  remove() { /* toasts only */ }
  focus() { /* no focus tracking */ }
  all(): FakeNode[] { return this.children.flatMap((c) => [c, ...c.all()]); }
  querySelector(selector: string) { return this.all().find((n) => n.tag === selector) ?? null; }
  querySelectorAll() { return []; }
}

// A page whose /api/state answers before, then (once something was posted)
// each afterPost state in turn, the last one repeating; /api/action records
// each post.
function page(before: object, ...afterPost: object[]) {
  const nodes = { screen: new FakeNode("main"), dots: new FakeNode("div"), status: new FakeNode("div") };
  const posts: unknown[] = [];
  const timers: (() => void)[] = [];
  const json = (body: unknown) => Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) });
  const catalog = JSON.parse(read("strings.en.json"));
  const document = {
    documentElement: { lang: "en" },
    activeElement: null,
    body: new FakeNode("body"),
    title: "",
    getElementById: (id: string) =>
      (nodes as Record<string, FakeNode>)[id] ?? nodes.screen.all().find((n) => n.id === id) ?? null,
    createElement: (tag: string) => new FakeNode(tag),
    createElementNS: (_: string, tag: string) => new FakeNode(tag),
    createTextNode: (text: string) => new FakeNode("#text", text),
  };
  const fetch = (url: string, init?: { body?: string }) => {
    if (url.includes("strings")) return json(catalog);
    if (url === "/api/action") {
      posts.push(JSON.parse(init?.body ?? "{}"));
      return json({});
    }
    if (!posts.length || !afterPost.length) return json(before);
    return json(afterPost.length > 1 ? afterPost.shift() : afterPost[0]);
  };
  const window: Record<string, unknown> = {};
  const context = {
    window, document, fetch, Intl, JSON, Promise, Object, Array, Map, String, Boolean,
    navigator: { language: "en" },
    location: { hash: "", origin: "http://127.0.0.1:1", assign: () => undefined },
    sessionStorage: { getItem: () => null, setItem: () => undefined },
    setTimeout: (fn: () => void) => timers.push(fn),
    clearTimeout: () => undefined,
  };
  runInNewContext(read("steps.js"), context);
  runInNewContext(read("app.js"), context);
  const settle = async (rounds = 20) => {
    for (let i = 0; i < rounds; i++) {
      await new Promise((r) => setImmediate(r));
      const due = timers.splice(0);
      due.forEach((fn) => fn());
    }
  };
  const find = (id: string) => nodes.screen.all().find((n) => n.id === id);
  return { screen: nodes.screen, posts, settle, find, catalog };
}

const found = {
  screen: "found", ha: "10.0.2.2", clients: ["OpenCode"], app: "running", app_version: "0.10.0",
  primary: "signin", accepts: ["address", "terminal", "signin"],
};

describe("setup page step 1", () => {
  it("leaves the address field after Change → Connect (lane E1)", async () => {
    const checking = { ...found, accepts: [] }; // setup checks the address
    const p = page(found, ...Array(6).fill(checking), found);
    await p.settle();
    expect(p.find("b-signin")).toBeTruthy();
    p.find("b-change")!.dispatch("click");
    expect(p.find("ha-address")).toBeTruthy();
    expect(p.find("b-signin")).toBeUndefined();
    const input = p.find("ha-address")!;
    input.value = "http://10.0.2.2:8123";
    input.dispatch("input");
    p.screen.all().find((n) => n.tag === "form")!.dispatch("submit");
    await p.settle(2);
    expect(p.posts).toEqual([{ action: "address", value: "http://10.0.2.2:8123" }]);
    expect(p.find("ha-address")).toBeTruthy(); // still checking: no old address with a ✓
    await p.settle();
    expect(p.find("ha-address")).toBeUndefined();
    expect(p.find("b-signin")).toBeTruthy();
  });

  it("keeps the field for an address Home Assistant didn't answer at", async () => {
    const wrong = { ...found, ask_ha: true, problem: { key: "setup.page.problem.address", args: { address: "nope" } } };
    const p = page(found, wrong);
    await p.settle();
    p.find("b-change")!.dispatch("click");
    const input = p.find("ha-address")!;
    input.value = "nope";
    input.dispatch("input");
    p.screen.all().find((n) => n.tag === "form")!.dispatch("submit");
    await p.settle();
    expect(p.find("ha-address")).toBeTruthy();
  });

  it("says a typed address was reached, not found (lane E5)", async () => {
    const p = page({ ...found, typed: true });
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.found.title_typed"]);
    expect(p.screen.textContent).not.toContain(p.catalog["setup.page.found.lead"]);
  });

  it("asks for the address while its field is open (lane F6)", async () => {
    const p = page({ ...found, typed: true });
    await p.settle();
    p.find("b-change")!.dispatch("click");
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.found.title_ask"]);
    expect(p.screen.textContent).toContain(p.catalog["setup.page.found.lead_change"]);
  });
});

describe("setup page step 3", () => {
  it("keeps the phone row when the phones couldn't be read, with Check again (lane F4)", async () => {
    const p = page({ screen: "control", accepts: ["finish", "recheck"], control: { window: true, phone: "unread", rule: "offer", rule_count: 2 } });
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.page.control.phone_unread"]);
    p.find("b-phone-recheck")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "recheck", value: "" }]);
    expect(p.screen.textContent).toContain("Adds 2 rules to Claude Code");
  });
});

describe("setup page step 4", () => {
  it("words the undo preview as the way back (lane E6)", async () => {
    const home = {
      screen: "home", accepts: ["save", "undo", "done"],
      home: {
        stats: { rooms: 4, devices: 68 },
        insights: [{ kind: "no_room", count: 55, action: "rooms" }],
        previews: { rooms: { state: "preview", kind: "undo", plan_id: "p1", changes: [{ name: "Kitchen Window", from_area: "Küche" }] } },
      },
    };
    const p = page(home);
    await p.settle();
    expect(p.screen.textContent).toContain("Back to no room: Kitchen Window");
    expect(p.screen.textContent).toContain("55 devices aren't in a room");
    expect(p.screen.textContent).not.toContain("←");
  });

  it("says every device is in a room once the count reaches 0", async () => {
    const p = page({ screen: "home", accepts: ["done"], home: { stats: { rooms: 4 }, insights: [{ kind: "no_room", count: 0, action: "rooms" }] } });
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.page.home.no_room_none"]);
  });
});

describe("setup page: Add to Claude", () => {
  const url = "https://abc123.ui.nabu.casa/api/ha_nova/mcp";
  const link = "https://claude.ai/customize/connectors?modal=add-custom-connector&connectorName=HA%20NOVA&connectorUrl=https%3A%2F%2Fabc123.ui.nabu.casa%2Fapi%2Fha_nova%2Fmcp";
  const control = (chat: object) => ({ screen: "control", accepts: ["finish"], control: { window: true, phone: "none", chat } });

  it("opens Claude's prefilled dialog in a new tab once chat apps are on, next to the address to copy", async () => {
    const p = page(control({ state: "on", url, claude_link: link }));
    await p.settle();
    const button = p.find("l-chat-claude")!;
    expect(button.attributes).toMatchObject({ href: link, target: "_blank", rel: "noopener noreferrer" });
    expect(button.textContent).toBe(p.catalog["setup.chat.add_claude"]);
    expect(p.find("chat-url")!.textContent).toBe(url);
    expect(p.screen.textContent).toContain("Adds abc123.ui.nabu.casa");
    expect(p.find("b-chat-copy")).toBeTruthy();
  });

  it("never makes a button of a link that doesn't go to claude.ai", async () => {
    for (const claude_link of ["https://claude.ai.evil.com/x", "javascript:alert(1)", ""]) {
      const p = page(control({ state: "on", url, claude_link }));
      await p.settle();
      expect(p.find("l-chat-claude")).toBeUndefined();
      expect(p.find("b-chat-copy")).toBeTruthy();
    }
  });

  it("says in one line what's missing without Link or a public HTTPS address", async () => {
    const p = page(control({ state: "noaddress" }));
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.chat.need_address"]);
    expect(p.find("b-chat-switch")).toBeUndefined();
    expect(p.find("l-chat-claude")).toBeUndefined();
  });

  it("says chat apps are on when the address is missing now", async () => {
    const p = page(control({ state: "noaddress", problem: { key: "setup.chat.on_noaddress" } }));
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.chat.on_noaddress"]);
    expect(p.screen.textContent).not.toContain(p.catalog["setup.chat.need_address"]);
  });

  it("promises the restart only when one comes (lane S2)", async () => {
    for (const no_restart of [false, true]) {
      const p = page({ screen: "control", accepts: ["finish", "chat"], control: { window: true, phone: "none", chat: { state: "offer", no_restart } } });
      await p.settle();
      p.find("b-chat-switch")!.dispatch("click");
      await p.settle();
      expect(p.screen.textContent.includes(p.catalog["setup.chat.restart"]!)).toBe(!no_restart);
      expect(p.find("b-chat-on")!.textContent).toBe(p.catalog[no_restart ? "setup.chat.turn_on_now" : "setup.chat.turn_on"]);
      const running = page({ screen: "control", accepts: ["finish"], control: { window: true, phone: "none", chat: { state: "run", no_restart } } });
      await running.settle();
      expect(running.screen.textContent).toContain(running.catalog[no_restart ? "setup.chat.wait_now" : "setup.chat.wait"]);
    }
  });

  it("repeats it on step 4", async () => {
    const p = page({ screen: "home", accepts: ["done"], home: { notes: [{ key: "setup.page.home.note_chat", args: { url } }], chat_link: link } });
    await p.settle();
    expect(p.find("l-chat-claude-4")!.attributes.href).toBe(link);
  });
});

describe("setup page re-run (G1)", () => {
  const rows = [
    { id: "ha", state: "ok", msg: { key: "setup.page.rerun.ha", args: { host: "homeassistant.local" } } },
    { id: "app", state: "ok", msg: { key: "setup.page.app.running_version", args: { version: "0.11.0" } } },
    { id: "computer", state: "ok", msg: { key: "setup.page.rerun.computer_ok" } },
    { id: "clients", state: "ok", msg: { key: "setup.page.rerun.clients_ok", args: { clients: ["Claude Code"] } } },
  ];
  const working = {
    screen: "rerun", rows, accepts: ["finish", "server"],
    control: { window: true, phone: "linked", phone_name: "iPhone", rule: "offer", rule_count: 2, link: "on", chat: { state: "page_off", page: "http://homeassistant.local:8123/app/x" } },
  };

  it("shows what is set up with the switches, and Done changes nothing unless one is switched", async () => {
    const p = page(working);
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.rerun.title"]);
    expect(p.screen.textContent).toContain("homeassistant.local");
    expect(p.screen.textContent).toContain("Set up in Claude Code");
    expect(p.screen.textContent).toContain(p.catalog["setup.page.control.link_on"]);
    expect(p.find("l-chat-nova")!.attributes.href).toBe("http://homeassistant.local:8123/app/x");
    expect(p.find("b-rule")!.attributes["aria-checked"]).toBe("false");
    expect(p.find("b-finish")!.textContent).toBe(p.catalog["setup.page.rerun.done"]);
    p.find("b-finish")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "finish", value: "" }]);
  });

  it("changes one thing in two clicks", async () => {
    const p = page(working);
    await p.settle();
    p.find("b-rule")!.dispatch("click");
    p.find("b-finish")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "finish", value: "rule" }]);
  });

  it("offers another Home Assistant server", async () => {
    const p = page(working);
    await p.settle();
    p.find("b-server")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "server" }]);
  });

  it("asks Fix it when something doesn't work, and Check again while the App doesn't answer", async () => {
    const broken = { screen: "rerun", rows: [...rows.slice(0, 3), { ...rows[3], state: "fail", msg: { key: "setup.page.rerun.clients_fail", args: { clients: ["Claude Code"] } } }], accepts: ["fix"] };
    const p = page(broken);
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.rerun.title_fix"]);
    expect(p.find("b-finish")).toBeUndefined();
    p.find("b-fix")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "fix" }]);

    const down = page({ ...broken, accepts: ["recheck"], problem: { key: "setup.page.rerun.problem_app" }, links: { app: "http://homeassistant.local:8123/_my_redirect/supervisor_addon?addon=x" } });
    await down.settle();
    expect(down.find("b-fix")).toBeUndefined();
    expect(down.find("b-recheck")).toBeTruthy();
    expect(down.find("l-app")).toBeTruthy();
    expect(down.screen.textContent).toContain(down.catalog["setup.page.rerun.problem_app"]);
  });

  it("ends with what Done changed", async () => {
    const p = page({ screen: "saved", accepts: [], home: { notes: [{ key: "setup.page.home.note_rule", args: { count: 2 } }] } });
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.saved.title"]);
    expect(p.screen.textContent).toContain("Claude Code got its 2 rules");
  });
});
