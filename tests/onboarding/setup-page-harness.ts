// The setup page's script (cli/setupweb/app.js, steps.js, chat.js, rerun.js) in a small fake
// DOM, shared by the setup-page tests: page() loads the scripts against canned /api/state answers.
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { runInNewContext } from "node:vm";

const WEB = resolve(__dirname, "../../cli/setupweb");
export const read = (name: string) => readFileSync(resolve(WEB, name), "utf8");

type Listener = (event: { preventDefault: () => void }) => void;

export class FakeNode {
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
// each post. ctl.down: /api/state no longer answers (setup closed the page).
export function page(before: object, ...afterPost: object[]) {
  const ctl = { down: false };
  const nodes = { screen: new FakeNode("main"), dots: new FakeNode("div"), status: new FakeNode("div") };
  const posts: unknown[] = [];
  const timers: (() => void)[] = [];
  const held: (() => void)[] = []; // 30 s and longer: run only when a test says so
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
    if (ctl.down) return Promise.reject(new Error("closed"));
    if (!posts.length || !afterPost.length) return json(before);
    return json(afterPost.length > 1 ? afterPost.shift() : afterPost[0]);
  };
  const window: Record<string, unknown> = {};
  const context = {
    window, document, fetch, Intl, JSON, Promise, Object, Array, Map, String, Boolean,
    navigator: { language: "en" },
    location: { hash: "", origin: "http://127.0.0.1:1", assign: () => undefined },
    sessionStorage: { getItem: () => null, setItem: () => undefined },
    setTimeout: (fn: () => void, ms = 0) => (ms >= 30_000 ? held : timers).push(fn),
    clearTimeout: () => undefined,
  };
  runInNewContext(read("chat.js"), context);
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
  return { screen: nodes.screen, posts, settle, find, catalog, timers, held, ctl };
}

// Chat apps on: this home's connector address, its Add to Claude links, and step 3 with that row.
export const chatFixtures = {
  url: "https://abc123.ui.nabu.casa/api/ha_nova/mcp",
  link: "https://claude.ai/customize/connectors?modal=add-custom-connector&connectorName=HA%20NOVA&connectorUrl=https%3A%2F%2Fabc123.ui.nabu.casa%2Fapi%2Fha_nova%2Fmcp",
  org_link: "https://claude.ai/admin-settings/connectors?modal=add-custom-connector&connectorName=HA%20NOVA&connectorUrl=https%3A%2F%2Fabc123.ui.nabu.casa%2Fapi%2Fha_nova%2Fmcp",
  control: (chat: object) => ({ screen: "control", accepts: ["finish"], control: { window: true, phone: "none", chat } }),
};
