import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import { join } from "node:path";
import { pathToFileURL } from "node:url";

import { describe, expect, it } from "vitest";

import { createApprovePageHandler, type ApprovePageDeps } from "../../nova/src/http/handlers/nova-approve.js";
import { createNovaPageHandler, type NovaPageDeps } from "../../nova/src/http/handlers/nova-page.js";
import { PAGE_KEYS, pageTexts } from "../../nova/src/http/page-texts.js";
import { createCsrfStore } from "../../nova/src/security/csrf.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";

// The vm lane's 0.31 rows (scripts/e2e/vm-lane/app-asks.mjs, guided-update.mjs) judged on
// recorded shapes, no VM: each passes on the fixed output and fails on the defect it guards.
type Judged = { ok: boolean; detail: string; ref?: string };
type Run = { code: number; out: unknown };
const load = async (f: string) => import(pathToFileURL(join(process.cwd(), "scripts/e2e/vm-lane", f)).href);
const asks = (await load("app-asks.mjs")) as {
  judgeMarketplace: (plans: Record<string, Run>, why: Record<string, string>) => Judged;
  judgeCheckup: (r: { code: number; out: string }) => Judged;
  judgeWindowPage: (i: Record<string, unknown>) => Judged;
  approvalAsks: (html: string) => { ref: string; text: string }[];
  envelope: (out: string) => Record<string, unknown>;
};
const guided = (await load("guided-update.mjs")) as { judgeGuided: (i: Record<string, unknown>) => Judged };

const catalog = (lang: string) => JSON.parse(readFileSync(join(process.cwd(), "cli/confirm_strings." + lang + ".json"), "utf8"))["raw.why.marketplace_warning"] as string;
const WHY = { en: catalog("en"), de: catalog("de") };

describe("vm-lane row: Marketplace warning through the App (#1132)", () => {
  const typed = (lang: "en" | "de", say = WHY[lang] + ", so I need you to confirm it.") => ({ code: 3, out: { tier: "typed", plan: { id: "p_0123456789", channel: "relay" }, result: { items: [{ change: "accept" }] }, reasons: [{ code: "RAW_TYPED", detail: WHY.en }], say } });
  const bundled = { code: 4, out: { error: { code: "PLAN_INVALID", message: "the Marketplace warning is accepted in a plan of its own; nothing was planned or sent" } } };

  it("passes on a typed plan of its own with the full reason in both languages", () => {
    const j = asks.judgeMarketplace({ en: typed("en"), de: typed("de"), bundled }, WHY);
    expect(j).toMatchObject({ ok: true });
    expect([...WHY.en].length).toBeLessThan(300);
    expect([...WHY.de].length).toBeLessThan(300);
  });

  it("fails on a refusal (0.30, or a person's account), a cut reason, or a bundled plan", () => {
    const refused = { code: 4, out: { error: { code: "REFUSED", message: "item 1: accepting the Marketplace warning for HA NOVA's account, which is a person's account" } } };
    for (const plans of [
      { en: refused, de: refused, bundled },
      { en: typed("en", WHY.en.slice(0, 275) + " … more on the computer"), de: typed("de"), bundled },
      { en: typed("en"), de: typed("de", WHY.en), bundled },
      { en: typed("en"), de: typed("de"), bundled: typed("en") },
    ]) expect(asks.judgeMarketplace(plans, WHY).ok).toBe(false);
  });
});

describe("vm-lane row: check-up stats through the App (#1148)", () => {
  const out = (result: unknown) => JSON.stringify({ verb: "map", result }) + "\n[ha-nova] note {not json}\n";
  it("passes when every stats read answered with CPU and memory", () => {
    const resources = { memory_mb: 1210.4, cpu_percent: 3.1, memory_percent: 31.2, top_memory: [{ name: "Home Assistant", kind: "core", cpu_percent: 2.2, memory_mb: 840 }, { name: "Supervisor", kind: "supervisor", cpu_percent: 0.4, memory_mb: 260 }, { name: "NOVA Relay", kind: "app", cpu_percent: 0.5, memory_mb: 110.4 }] };
    expect(asks.judgeCheckup({ code: 0, out: out({ status: "ok", findings: [], not_checked: [], facts: { resources } }) })).toMatchObject({ ok: true });
  });
  it("fails on an App that refuses the stats paths, or a CLI without the section", () => {
    expect(asks.judgeCheckup({ code: 0, out: out({ status: "limited", findings: [], not_checked: [{ check: "resources", reason: "SUPERVISOR_REQUEST_REFUSED" }], facts: {} }) }).ok).toBe(false);
    expect(asks.judgeCheckup({ code: 2, out: "[ha-nova] ERROR: unknown section checkup" }).ok).toBe(false);
  });
});

describe("vm-lane row: the window's button opens its ask (#1161)", () => {
  const OWNER: HaAuthUser = { id: "owner-1", name: "Owner", is_owner: true, is_active: true, system_generated: false, group_ids: ["system-admin"] };
  const RELAY: HaAuthUser = { id: "relay-1", name: "Supervisor", is_owner: false, is_active: true, system_generated: true, group_ids: ["system-admin"] };
  const TEXTS = Object.fromEntries(PAGE_KEYS.map((key) => [key, "‹" + key + "›"]));
  const ref = (id: string) => createHash("sha256").update(id).digest("hex");
  const ask = (id: string, text: string, ids: string[], expiresAt: number) => ({ ref: ref(id), body: "Requested by vm-lane PC.", from: "Requested by vm-lane PC.", items: [{ verdict: "typed" as const, text, entity_ids: ids }], expiresAt });
  const decoy = ask("decoy", "Open “Garage”", ["cover.garage_door"], 5_000), mine = ask("mine", "Unlock “Front Door”", ["lock.front_door"], 9_000);
  const page = { waiting: () => [decoy, mine], pausedDevices: () => [], approve: () => false, cancel: () => false, resume: () => false, texts: pageTexts(async () => ({ lang: "en", texts: TEXTS }), () => 0) };
  const get = async (handler: ReturnType<typeof createApprovePageHandler>, url: string) => {
    const headers: Record<string, string> = { "x-ingress-path": "/api/hassio_ingress/tok", "x-remote-user-id": OWNER.id };
    const request = { url, socket: { remoteAddress: "172.30.32.2" }, headers, rawHeaders: Object.entries(headers).flat() } as unknown as IncomingMessage;
    const r = { statusCode: 0, body: "", setHeader: () => undefined, end(b?: string) { if (b) this.body = b; } };
    await handler({ request, response: r as unknown as ServerResponse, path: url.split("?")[0]!, body: null });
    return r.body;
  };
  const approval = (q: string) => get(createApprovePageHandler({ fetchAuthUsers: async () => [OWNER, RELAY], relayUserId: async () => RELAY.id, csrf: createCsrfStore(), page, panel: async () => "/app/2368fcfa_ha_nova_relay", now: () => 1_000 } as ApprovePageDeps), "/approve" + q);
  const novaPage = () => get(createNovaPageHandler({
    fetchAuthUsers: async () => [OWNER], gatePage: page, csrf: createCsrfStore(), now: () => 1000,
    registry: { list: () => [] }, pairing: { getStatus: () => ({ phase: "idle" }) }, connection: () => ({ haConnected: true }),
    update: async () => ({ version: "0.13.28", versionLatest: "0.13.28", updateAvailable: false, error: false }), relayVersion: "0.13.28",
  } as unknown as NovaPageDeps) as ReturnType<typeof createApprovePageHandler>, "/");
  const base = { guestHa: "http://10.0.2.2:8123", app: "2368fcfa_ha_nova_relay", ingress: "/api/hassio_ingress/tok//home", panelStatus: 200 }; // as Supervisor's ingress_url, with the App's ingress_entry
  const opened = "http://10.0.2.2:8123/app/2368fcfa_ha_nova_relay?nova_ask=" + mine.ref;

  it("reads the approval view's asks in order, with what each says", async () => {
    expect(asks.approvalAsks(await approval("")).map((a) => a.ref)).toEqual([decoy.ref, mine.ref]);
    const named = asks.approvalAsks(await approval("?nova_ask=" + mine.ref));
    expect(named.map((a) => a.ref)).toEqual([mine.ref, decoy.ref]);
    expect(named[0]!.text).toContain("Front Door");
  });

  it("passes when the window opens the panel with its ask's ref and the view shows that ask first", async () => {
    const j = asks.judgeWindowPage({ ...base, opened, page: await novaPage(), approval: await approval("?nova_ask=" + mine.ref) });
    expect(j).toMatchObject({ ok: true, ref: mine.ref });
  });

  it("fails on a window that opens only the panel, a page without the hand-off, or a view that ignores the ref", async () => {
    const [page0, view] = [await novaPage(), await approval("?nova_ask=" + mine.ref)];
    for (const input of [
      { opened: "http://10.0.2.2:8123/app/2368fcfa_ha_nova_relay", page: page0, approval: view },
      { opened: "", page: page0, approval: view },
      { opened, page: page0.replace(/ data-ask="[^"]*"/, ""), approval: view },
      { opened, page: page0, approval: await approval("") },
      { opened, page: page0, approval: view, panelStatus: 404 },
    ]) expect(asks.judgeWindowPage({ ...base, ...input }).ok).toBe(false);
  });
});

describe("vm-lane row: the guided App update asks typed (#1162)", () => {
  const head = "Checking the NOVA App in Home Assistant...\r\nHA NOVA installs the newest NOVA App (now v0.13.28), with a backup of the App first. The App restarts while it installs.\r\n";
  const base = { cancelled: "", skipped: false, versionBefore: "0.13.27", versionAfter: "0.13.27", preview: "0.13.28" };
  it("passes on the typed ask, cancelled on the page or skipped at its code, nothing installed", () => {
    const typed = head + "This update needs your confirmation first, the same way an unlock does:\r\n...\r\nNothing was installed (the change was cancelled on the phone, in Home Assistant, or in the window; nothing was sent).\r\n";
    expect(guided.judgeGuided({ ...base, out: typed, cancelled: "Install NOVA Relay update" })).toMatchObject({ ok: true });
    expect(guided.judgeGuided({ ...base, out: typed.replace("...", "Code: 4821\r\nCode (Enter skips): ").replace(/Nothing was installed \(.*\)\./, "Nothing was installed."), skipped: true })).toMatchObject({ ok: true });
  });
  it("fails on the yes question and the refusal (0.30), or an App that changed", () => {
    const old = head + "Update the NOVA App now?\r\n> Yes, update now\r\n  Not now\r\n^C";
    expect(guided.judgeGuided({ ...base, out: old }).ok).toBe(false);
    expect(guided.judgeGuided({ ...base, out: head + "HA NOVA can't start the NOVA App update from here. Nothing was installed.\r\n" }).ok).toBe(false);
    expect(guided.judgeGuided({ ...base, out: head + "This update needs your confirmation first\r\nNothing was installed.\r\n", cancelled: "x", versionAfter: "0.13.28" }).ok).toBe(false);
    expect(guided.judgeGuided({ ...base, out: head + "This update needs your confirmation first\r\nNothing was installed.\r\n" }).ok).toBe(false);
  });
});
