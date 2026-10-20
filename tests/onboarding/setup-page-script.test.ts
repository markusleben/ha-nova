// The setup page's script (cli/setupweb/app.js, steps.js, rerun.js) against a small
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
  runInNewContext(read("rerun.js"), context);
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

describe("setup page: no AI app on this computer", () => {
  it("goes on with Claude or ChatGPT in the browser, and keeps the downloads", async () => {
    const p = page({ screen: "noclient", accepts: ["recheck", "web"] });
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.page.noclient.lead"]);
    expect(p.find("b-web")!.className).toContain("btn-primary");
    expect(p.find("b-recheck")!.className).toContain("btn-ghost");
    expect(p.find("l-desktop")!.className).toContain("btn-ghost");
    p.find("b-web")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "web" }]);
  });

  it("says under the browser button what it needs, with What's this? to the chat-apps guide", async () => {
    const guide = "https://github.com/markusleben/ha-nova/blob/main/docs/reference/chat-apps.md";
    const p = page({ screen: "noclient", accepts: ["recheck", "web"], links: { chat: guide } });
    await p.settle();
    const needs = p.find("web-needs")!;
    expect(needs.textContent).toContain("For this, Home Assistant must be reachable from the internet: Home Assistant Link or your own HTTPS address.");
    const help = p.find("l-chat-help")!;
    expect([help.textContent, help.attributes.href, help.attributes.target]).toEqual(["What's this?", guide, "_blank"]);
    const kids = p.screen.children;
    expect(kids.indexOf(needs)).toBe(kids.findIndex((n) => n.all().includes(p.find("b-web")!)) + 1); // right under the button
    const bare = page({ screen: "noclient", accepts: ["recheck", "web"] });
    await bare.settle();
    expect(bare.find("l-chat-help")).toBeUndefined(); // no link without the CLI's
  });

  it("says before pairing what chat apps miss, with check again or this computer instead", async () => {
    const guide = "https://github.com/markusleben/ha-nova/blob/main/docs/reference/chat-apps.md";
    for (const [key, fix] of [["setup.chat.need_address_web", "web"], ["setup.chat.problem_old", "web"], ["setup.chat.problem_no_apps", "web_never"]] as const) {
      const p = page({ screen: "connect", web: true, rows: [{ id: "signed_in", state: "ok" }, { id: "connect", state: "todo" }], fix,
        links: { chat: guide }, accepts: ["address", "terminal", "signin", "local"], problem: { key } });
      await p.settle();
      expect(p.screen.textContent).toContain(p.catalog[key]);
      expect(p.screen.textContent).not.toContain("open NOVA");
      if (fix === "web") {
        expect(p.find("b-signin")!.textContent).toBe("Set it up, then check again");
        expect(p.find("b-signin")!.className).toContain("btn-primary");
        expect(p.screen.textContent).toContain(p.catalog["setup.page.connect.web_signin"]);
        expect(p.find("b-local")!.className).toContain("btn-ghost");
      } else { // nothing to set up here: only this computer instead
        expect(p.find("b-signin")).toBeUndefined();
        expect(p.find("b-local")!.className).toContain("btn-primary");
      }
      expect(p.find("b-local")!.textContent).toBe("Use HA NOVA on this computer instead");
      expect(p.find("l-chat-help")!.attributes.href).toBe(guide);
      expect(p.find("b-terminal")).toBeUndefined();
      p.find("b-local")!.dispatch("click");
      await p.settle();
      expect(p.posts).toEqual([{ action: "local" }]);
    }
  });

  it("names the browser as the AI assistant on step 1, with nothing to set up here and no code", async () => {
    const p = page({ ...found, clients: undefined, web: true });
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.page.clients.web"]);
    expect(p.screen.textContent).not.toMatch(/get set up|Gets set up/);
    expect(p.find("b-terminal")).toBeUndefined(); // chat apps need the owner's sign-in here
  });

  it("shows chat apps first on step 3, with the question open; only its button turns them on", async () => {
    const p = page({ screen: "control", web: true, accepts: ["finish", "chat"], control: { window: true, phone: "none", chat: { state: "offer", ask: true } } });
    await p.settle();
    expect(p.screen.textContent).not.toContain(p.catalog["setup.page.control.window_key"]); // never asks for the browser
    const rows = p.screen.all().find((n) => n.className === "rows tall")!.children;
    expect(rows[0]!.textContent).toContain(p.catalog["setup.chat.key"]);
    expect(p.find("b-chat-on")).toBeTruthy();
    expect(p.find("b-finish")!.attributes.disabled).toBe("");
    expect(p.posts).toEqual([]);
    p.find("b-chat-cancel")!.dispatch("click");
    expect(p.find("b-chat-ask")).toBeTruthy();
    expect(p.find("b-finish")!.attributes.disabled).toBeUndefined();
  });
});

describe("setup page hand-over to the terminal", () => {
  it("says why the terminal waits", async () => {
    for (const [handover, key] of [["code", "lead_code"], ["keyring_locked", "lead_keyring_locked"], ["keyring_setup", "lead_keyring_setup"], [undefined, "lead"]]) {
      const p = page({ screen: "terminal", accepts: [], handover });
      await p.settle();
      expect(p.screen.textContent).toContain(p.catalog[`setup.page.terminal.${key}`]);
    }
    const p = page({ screen: "terminal", accepts: [], handover: "keyring_locked" });
    await p.settle();
    expect(p.screen.textContent).toContain("password store is locked");
    expect(p.screen.textContent).not.toContain("keyring");
  });
});

describe("setup page sign-in", () => {
  // Home Assistant sends the sign-in back to 127.0.0.1 on this computer, so
  // the page never offers its link for another device; the code is that way.
  const url = "http://10.0.2.2:8123/auth/authorize?state=x";
  const signingIn = { screen: "connect", signin_url: url, rows: [{ id: "signed_in", state: "run" }], accepts: [] };

  it("says who signs in, and puts the six-digit code in a text link that says it goes on in the terminal", async () => {
    for (const state of [found, { ...found, app: "missing", primary: "install" }]) {
      const p = page(state);
      await p.settle();
      expect(p.screen.textContent).toContain(p.catalog["setup.page.found.owner"]);
      expect(p.catalog["setup.page.found.owner"]).toContain("first account made when Home Assistant was set up");
      const code = p.find("b-terminal")!;
      expect(code.className).toBe("linkish");
      expect(code.textContent).toBe(p.catalog["setup.page.button.code"]);
      expect(code.textContent).toContain("(continues in the terminal)");
      code.dispatch("click");
      await p.settle();
      expect(p.posts).toEqual([{ action: "terminal" }]);
    }
  });

  it("tells someone who isn't the owner who to ask, and how the owner can help from another device", async () => {
    const notOwner = { screen: "connect", rows: [{ id: "signed_in", state: "ok" }], fix: "owner", links: {}, accepts: ["address", "terminal", "signin"],
      problem: { key: "setup.page.problem.not_owner", args: { name: "Anna" } } };
    const p = page(notOwner);
    await p.settle();
    expect(p.screen.textContent).toContain("Anna isn't the owner of this Home Assistant. Ask the person who set up Home Assistant to sign in here.");
    expect(p.find("b-signin")!.textContent).toBe(p.catalog["setup.page.button.owner"]);
    expect(p.screen.textContent).toContain(p.catalog["setup.page.found.owner_away"]);
    expect(p.find("b-terminal")!.className).toBe("linkish");
    const other = page({ ...notOwner, fix: "retry", problem: { key: "setup.page.problem.timeout" } });
    await other.settle();
    expect(other.find("b-terminal")!.className).toBe("linkish");
    expect(other.screen.textContent).not.toContain(other.catalog["setup.page.found.owner_away"]);
  });

  it("points to Stop and the code while it waits, with no copy of the sign-in link", async () => {
    const p = page(signingIn);
    await p.settle();
    const line = p.catalog["setup.page.signin.elsewhere"];
    expect(p.screen.textContent).toContain(line);
    expect(line).toContain(`“${p.catalog["setup.page.signin.stop"]}”, then use a six-digit code`);
    expect(p.screen.textContent).not.toMatch(/copy/i);
    expect(p.screen.textContent).not.toContain(url);
    expect(p.screen.all().filter((n) => Object.values(n.attributes).includes(url)).map((n) => n.id)).toEqual(["l-signin"]);
  });

  it("doesn't promise a code while the App installs", async () => {
    const p = page({ ...signingIn, rows: [{ id: "signed_in", state: "run" }, { id: "app_add", state: "todo" }] });
    await p.settle();
    expect(p.find("b-stop")).toBeTruthy();
    expect(p.screen.textContent).not.toContain(p.catalog["setup.page.signin.elsewhere"]);
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

describe("setup page step 3: buttons act now, switches wait for the button", () => {
  const phones = [{ id: "d1", name: "iPhone Ben", person: "Ben" }, { id: "d2", name: "Pixel Lisa", person: "Lisa" }];

  it("sends the phone's notification with a button, never a switch", async () => {
    const p = page({ screen: "control", accepts: ["finish", "phone"], control: { window: true, phone: "pick", phones: phones.slice(0, 1), rule: "offer", rule_count: 3 } });
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.page.control.lead"]);
    expect(p.catalog["setup.page.control.lead"]).not.toContain("never sees a code");
    const use = p.find("b-phone-0")!;
    expect(use.tag).toBe("button");
    expect(use.attributes.role).toBeUndefined();
    expect(use.textContent).toBe(p.catalog["setup.page.control.phone_use"]);
    expect(p.find("b-rule")!.attributes.role).toBe("switch");
    expect(p.screen.textContent).toContain("Added when you click Continue.");
    p.find("b-rule")!.dispatch("click");
    expect(p.screen.textContent).not.toContain("Added when you click Continue.");
    use.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "phone", value: "d1" }]);
  });

  it("moves to another phone from a linked one; the linked phone says OK first", async () => {
    const linked = { screen: "rerun", rows: [], accepts: ["finish", "phone"], control: { window: true, phone: "linked", phone_name: "iPhone Anna", phones } };
    const moving = { ...linked, accepts: [], control: { ...linked.control, phone: "moving", old_phone: "iPhone Anna", phone_name: "Pixel Lisa" } };
    const p = page(linked, moving);
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.page.rerun.lead"]);
    expect(p.catalog["setup.page.rerun.lead"]).toContain("Turn on what you need, then click Done.");
    p.find("b-phone-move")!.dispatch("click");
    expect(p.screen.textContent).toContain("“iPhone Anna” asks you first to OK the move.");
    p.find("b-phone-move-cancel")!.dispatch("click");
    expect(p.find("b-phone-1")).toBeUndefined();
    p.find("b-phone-move")!.dispatch("click");
    p.find("b-phone-1")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "phone", value: "d2" }]);
    expect(p.screen.textContent).toContain("Tap “Move” on iPhone Anna first. Then tap “Allow” on Pixel Lisa.");
    const refused = page({ ...linked, control: { ...linked.control, phone_problem: { key: "setup.page.control.move_refused", args: { phone: "iPhone Anna" } } } });
    await refused.settle();
    expect(refused.screen.textContent).toContain("Nothing changed. The move didn't go through, and the terminal says why. Lost iPhone Anna?");
  });

  it("shows a linked phone as On when there is no other phone", async () => {
    const p = page({ screen: "control", accepts: ["finish"], control: { window: true, phone: "linked", phone_name: "iPhone Anna" } });
    await p.settle();
    expect(p.find("b-phone-move")).toBeUndefined();
    expect(p.screen.textContent).toContain(p.catalog["setup.page.control.on"]);
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

  it("says what's next once on, with one primary button and no German menu names", async () => {
    const p = page(control({ state: "on", url, claude_link: link }));
    await p.settle();
    expect(p.screen.textContent).toContain(p.catalog["setup.chat.on_next"]);
    expect(p.find("l-chat-claude")!.className).toContain("btn-ghost");
    expect(p.find("b-chat-how-claude")).toBeTruthy();
    expect(p.find("b-chat-how-chatgpt")).toBeTruthy();
    expect(p.screen.textContent).not.toMatch(/German|ha-nova chat-apps/);
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
    expect(p.find("b-chat-ask")).toBeUndefined();
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
      expect(p.find("b-chat-ask")!.attributes.role).toBeUndefined(); // a button: it opens the question
      p.find("b-chat-ask")!.dispatch("click");
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
    control: { window: true, phone: "linked", phone_name: "iPhone", rule: "offer", rule_count: 2, link: "on", chat: { state: "page_off" } },
  };

  it("shows what is set up with the switches; Done adds the offered rules, as the terminal's default", async () => {
    const p = page(working);
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.rerun.title"]);
    expect(p.screen.textContent).toContain("homeassistant.local");
    expect(p.screen.textContent).toContain("Set up in Claude Code");
    expect(p.screen.textContent).toContain(p.catalog["setup.page.control.link_on"]);
    expect(p.find("b-chat-switch")!.attributes["aria-checked"]).toBe("false");
    expect(p.find("b-rule")!.attributes["aria-checked"]).toBe("true");
    expect(p.find("b-finish")!.textContent).toBe(p.catalog["setup.page.rerun.done"]);
    p.find("b-finish")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "finish", value: "rule" }]);
  });

  it("changes one thing in two clicks", async () => {
    const p = page(working);
    await p.settle();
    p.find("b-rule")!.dispatch("click");
    p.find("b-finish")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "finish", value: "" }]);
  });

  const url = "https://abc123.ui.nabu.casa/api/ha_nova/mcp";
  const claude_link = "https://claude.ai/customize/connectors?modal=add-custom-connector&connectorName=HA%20NOVA&connectorUrl=https%3A%2F%2Fabc123.ui.nabu.casa%2Fapi%2Fha_nova%2Fmcp";
  const later = "You sign in to Home Assistant when you click Done.";

  it("shows chat apps on as a switch with Add to Claude and how, and turns them off on Done", async () => {
    const p = page({ ...working, control: { ...working.control, chat: { state: "page_on", url, claude_link } } });
    await p.settle();
    expect(p.find("b-chat-switch")!.attributes["aria-checked"]).toBe("true");
    expect(p.find("l-chat-claude")!.attributes.href).toBe(claude_link);
    expect(p.find("b-chat-how-claude")).toBeTruthy();
    expect(p.screen.textContent).not.toContain("open NOVA in Home Assistant, then Chat apps");
    expect(p.screen.textContent).not.toContain(later);
    p.find("b-chat-switch")!.dispatch("click");
    expect(p.find("b-chat-switch")!.attributes["aria-checked"]).toBe("false");
    expect(p.find("l-chat-claude")).toBeUndefined();
    expect(p.screen.textContent).toContain(p.catalog["setup.chat.off_later"]);
    p.find("b-finish")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "finish", value: "rule,chat_off" }]);
  });

  it("turns chat apps on with Done's sign-in, then asks as step 3 does", async () => {
    const signin = { ...working, accepts: [], control: { ...working.control, chat: { state: "signin" } } };
    const asking = { ...working, accepts: ["finish", "chat"], control: { ...working.control, chat: { state: "offer", ask: true, phone: "iPhone" } } };
    const p = page(working, signin, signin, asking);
    await p.settle();
    p.find("b-chat-switch")!.dispatch("click");
    expect(p.find("b-chat-switch")!.attributes["aria-checked"]).toBe("true");
    expect(p.screen.textContent).toContain(p.catalog["setup.chat.on_later"]);
    p.find("b-finish")!.dispatch("click");
    await p.settle(3);
    expect(p.posts).toEqual([{ action: "finish", value: "rule,chat_on" }]);
    expect(p.screen.textContent).toContain(p.catalog["setup.chat.signin"]);
    await p.settle();
    expect(p.find("b-chat-on")).toBeTruthy(); // the consent, open
    expect(p.screen.textContent).toContain("iPhone");
    expect(p.find("b-finish")!.attributes.disabled).toBe(""); // Done waits for its answer, and says why
    expect(p.screen.textContent).toContain(p.catalog["setup.chat.answer_first"]);
  });

  it("shows a failed sign-in even when no poll saw it run (Codex review)", async () => {
    const failed = { ...working, control: { ...working.control, chat: { state: "page_off", attempt: 1, problem: { key: "setup.chat.problem_signin" } } } };
    const p = page(working, failed);
    await p.settle();
    p.find("b-chat-switch")!.dispatch("click");
    p.find("b-finish")!.dispatch("click");
    await p.settle();
    expect(p.find("b-chat-switch")!.attributes["aria-checked"]).toBe("false");
    expect(p.screen.textContent).toContain(p.catalog["setup.chat.problem_signin"]);
  });

  it("says a switch's sign-in waits for the button", async () => {
    const first = page({ screen: "control", accepts: ["finish"], control: { window: true, phone: "none", link: "offer" } });
    await first.settle();
    expect(first.find("b-finish")!.textContent).toBe("Continue");
    first.find("b-link")!.dispatch("click");
    expect(first.screen.textContent).toContain("You sign in to Home Assistant when you click Continue.");
    expect(first.find("b-finish")!.textContent).toBe(first.catalog["setup.page.control.finish_signin"]);
    const again = page({ ...working, control: { ...working.control, link: "offer" } });
    await again.settle();
    expect(again.screen.textContent).not.toContain(later);
    again.find("b-link")!.dispatch("click");
    expect(again.screen.textContent).toContain(later);
    expect(again.find("b-finish")!.textContent).toBe(again.catalog["setup.page.rerun.done_signin"]);
  });

  it("offers an AI assistant found since, with a button", async () => {
    const p = page({ ...working, new_clients: ["Codex CLI"], accepts: ["finish", "clients"] });
    await p.settle();
    expect(p.find("b-clients")!.textContent).toBe("Set up Codex CLI too");
    p.find("b-clients")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "clients" }]);
    const updating = page({ screen: "repair", accepts: [], rows: [{ id: "teach", state: "run", msg: { key: "setup.page.row.teach", args: { clients: ["Codex CLI"] } } }] });
    await updating.settle();
    expect(updating.screen.querySelector("h1")!.textContent).toBe(updating.catalog["setup.page.rerun.repair_title"]);
    expect(updating.screen.textContent).toContain("Teaching Codex CLI");
  });

  it("offers another Home Assistant server", async () => {
    const p = page(working);
    await p.settle();
    p.find("b-server")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "server" }]);
  });

  it("asks Fix it when something doesn't work, and Check again while the App doesn't answer", async () => {
    const broken = { screen: "rerun_fix", rows: [...rows.slice(0, 3), { ...rows[3], state: "fail", msg: { key: "setup.page.rerun.clients_fail", args: { clients: ["Claude Code"] } } }], accepts: ["fix"] };
    const p = page(broken);
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.rerun.title_fix"]);
    expect(p.find("b-finish")).toBeUndefined();
    p.find("b-fix")!.dispatch("click");
    await p.settle();
    expect(p.posts).toEqual([{ action: "fix" }]);

    const down = page({ ...broken, accepts: ["recheck", "fix"], problem: { key: "setup.page.rerun.problem_app" }, links: { app: "http://homeassistant.local:8123/_my_redirect/supervisor_addon?addon=x" } });
    await down.settle();
    expect(down.find("b-fix")!.className).toContain("btn-ghost"); // another address, or the sign-in that starts the App
    expect(down.find("b-recheck")!.className).toContain("btn-primary");
    expect(down.find("l-app")).toBeTruthy();
    expect(down.screen.textContent).toContain(down.catalog["setup.page.rerun.problem_app"]);
  });

  it("says Checking… after Check again until setup waits for a click again (A7)", async () => {
    const down = { screen: "rerun_fix", rows, accepts: ["recheck", "fix"], problem: { key: "setup.page.rerun.problem_app_only" } };
    const p = page(down, ...Array(8).fill({ ...down, accepts: [] }), down); // setup checks, then waits again
    await p.settle(1);
    expect(p.find("b-recheck")!.textContent).toBe(p.catalog["setup.page.button.recheck"]);
    p.find("b-recheck")!.dispatch("click");
    for (let round = 0; round < 3; round++) { // a poll begun before the click never re-enables it (Codex #793)
      await p.settle(1);
      expect(p.find("b-recheck")!.textContent).toBe(p.catalog["setup.page.button.checking"]);
      expect(p.find("b-recheck")!.attributes.disabled).toBe("");
    }
    await p.settle();
    expect(p.find("b-recheck")!.textContent).toBe(p.catalog["setup.page.button.recheck"]);
    expect(p.find("b-recheck")!.attributes.disabled).toBeUndefined();
    // Home Assistant answers: no "Is this computer at home?" (that's for Home Assistant down).
    expect(p.screen.textContent).toContain(p.catalog["setup.page.rerun.problem_app_only"]);
    expect(p.catalog["setup.page.rerun.problem_app_only"]).not.toContain("at home");
    expect(p.catalog["setup.page.rerun.problem_app"]).toContain("Is this computer at home?");
  });

  it("ends with what Done changed", async () => {
    const p = page({ screen: "saved", accepts: [], home: { notes: [{ key: "setup.page.home.note_rule", args: { count: 2 } }] } });
    await p.settle();
    expect(p.screen.querySelector("h1")!.textContent).toBe(p.catalog["setup.page.saved.title"]);
    expect(p.screen.textContent).toContain("Claude Code got its 2 rules");
  });

  it("styles only a row list's own rows, not a list inside a row", () => {
    // "Show me how" puts an <ol> inside a row; `.rows li` made each step a
    // three-column grid, one word per line.
    expect(read("style.css")).not.toMatch(/\.rows[\w.-]*\s+li\b/);
  });
});
