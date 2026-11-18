// The owner's chat-code choice in the registry and where it shows (MAR-244):
// exclusive with the window, ended by a linked phone, bound to the owner,
// any key store, and a revision that rises with every mode or link write.
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createGateMeHandler } from "../../nova/src/http/handlers/gate-me.js";
import { codeRow, recentWindowSection, type WindowAction } from "../../nova/src/http/handlers/nova-page-window.js";
import { PAGE_KEYS, type PageTexts } from "../../nova/src/http/page-texts.js";
import { generateCredential } from "../../nova/src/security/device-credential.js";
import { openDeviceRegistry, RegistryCorruptError } from "../../nova/src/security/device-registry.js";
import { DEVICE_REGISTRY_FILE } from "../../nova/src/security/device-registry-schema.js";
import type { DeviceRecord } from "../../nova/src/security/device-registry-types.js";
import type { HaAuthUser } from "../../nova/src/security/owner-check.js";
import { GATE_INSTALL_ID } from "../../nova/src/security/pairing-metadata.js";
import { codeState } from "../../nova/src/security/window-confirm.js";

let dir: string;
beforeEach(() => { dir = mkdtempSync(join(tmpdir(), "ha-nova-code-")); });
afterEach(() => { rmSync(dir, { recursive: true, force: true }); });

const OWNER: HaAuthUser = { id: "owner", name: "Markus", is_owner: true, is_active: true, system_generated: false, group_ids: ["system-admin"] };
const link = { service: "mobile_app_phone", deviceId: "phone", userId: "owner" };

function pair(over: Partial<DeviceRecord> = {}, install = "install") {
  const registry = openDeviceRegistry(dir);
  const { deviceId, secretDigest } = generateCredential();
  registry.createPending({ deviceId, secretDigest, clientInstallId: install, name: "Headless", platform: "linux", client: "cli", createdAtMs: 1, credentialStore: "file", ...over }, 1);
  if (over.cloudUserId === undefined) registry.activate(deviceId, 2);
  else registry.activatePendingForCloud(deviceId, secretDigest, over.cloudUserId, over.cloudRelayInstanceId!, 2);
  const read = () => openDeviceRegistry(dir).list().find((d) => d.deviceId === deviceId)!; // from disk
  const state = () => codeState({ kind: "device", deviceId }, read());
  return { registry, deviceId, read, state };
}

describe("the owner's chat-code choice", () => {
  it("turns on for a file key, persists, and only the owner's record counts", () => {
    const { registry, deviceId, read, state } = pair();
    expect(state()).toEqual({ mode: "off", rev: 0, userId: "" });
    expect(registry.setCodeConfirm(deviceId, "owner", 5)).toBeNull();
    expect(read().codeConfirm).toEqual({ userId: "owner", atMs: 5 });
    expect(state()).toEqual({ mode: "on", rev: 1, userId: "owner" });
    expect(codeState({ kind: "device", deviceId: "other" }, read()).mode).toBe("off");
    expect(codeState({ kind: "shared" }, read()).mode).toBe("off");
    expect(codeState({ kind: "device", deviceId, internal: "gate" }, read()).mode).toBe("off");
    expect(registry.setCodeConfirm(deviceId, null, 6)).toBeNull();
    expect(read().codeConfirm).toBeUndefined();
    expect(state().mode).toBe("off");
  });

  it("is exclusive with the window in one write, both ways", () => {
    const { registry, deviceId, read } = pair({ credentialStore: "secret_service" });
    expect(registry.setOwnComputer(deviceId, "owner", 3)).toBeNull();
    expect(read().windowConfirm).toBe(true);
    expect(registry.setCodeConfirm(deviceId, "owner", 4)).toBeNull();
    expect(read()).toMatchObject({ windowConfirm: false, codeConfirm: { userId: "owner" } });
    expect(registry.setWindowConfirm(deviceId, true, "owner")).toBeNull();
    expect(read().windowConfirm).toBe(true);
    expect(read().codeConfirm).toBeUndefined();
    registry.setCodeConfirm(deviceId, "owner", 5);
    registry.setOwnComputer(deviceId, "owner", 6); // declaring it the owner's turns the window on
    expect(read().codeConfirm).toBeUndefined();
    expect(codeState({ kind: "device", deviceId }, { ...read(), codeConfirm: { userId: "owner", atMs: 1 } }).mode).toBe("off"); // never both
  });

  it("a linked phone ends it and raises its revision; with one linked it can't turn on", () => {
    const { registry, deviceId, state } = pair();
    registry.setCodeConfirm(deviceId, "owner", 4);
    const before = state().rev;
    expect(registry.setConfirmPhone(deviceId, link, undefined)).toBe(true);
    expect(state()).toEqual({ mode: "phone", rev: before + 1, userId: "owner" });
    const other = pair();
    other.registry.setConfirmPhone(other.deviceId, link, undefined);
    expect(other.registry.setCodeConfirm(other.deviceId, "owner", 4)).toBe("phone");
  });

  it.each([
    ["a Mac whose old sign-in isn't cleaned up", { credentialStore: "keychain", legacyCleanup: "pending" }, "owner", "legacy_cleanup_pending"],
    ["paired before 0.32", { credentialStore: undefined }, "owner", "pair_again"],
    ["someone else's Link", { cloudUserId: "anna", cloudRelayInstanceId: "hanova-relay-v1.AAAAAAAAAAAAAAAAAAAAAA" }, "owner", "owner"],
  ] as const)("refuses %s", (_, over, owner, why) => {
    const { registry, deviceId, state } = pair(over as Partial<DeviceRecord>);
    expect(registry.setCodeConfirm(deviceId, owner, 4)).toBe(why);
    expect(state().mode).toBe("off");
  });

  it("refuses the App's own connections", () => {
    const { registry, deviceId } = pair({}, GATE_INSTALL_ID);
    expect(registry.setCodeConfirm(deviceId, "owner", 4)).toBe("device");
  });

  it("refuses a corrupt record on disk", () => {
    const { registry, deviceId } = pair();
    registry.setCodeConfirm(deviceId, "owner", 4);
    const file = join(dir, DEVICE_REGISTRY_FILE);
    for (const [field, bad] of [["codeConfirm", { userId: "", atMs: 1 }], ["codeConfirm", "on"], ["confirmRev", -1], ["confirmRev", 1.5]] as const) {
      const data = JSON.parse(readFileSync(file, "utf8")) as { devices: Record<string, unknown>[] };
      const saved = data.devices[0]![field];
      data.devices[0]![field] = bad;
      writeFileSync(file, JSON.stringify(data));
      expect(() => openDeviceRegistry(dir)).toThrow(RegistryCorruptError);
      data.devices[0]![field] = saved;
      writeFileSync(file, JSON.stringify(data));
    }
  });
});

describe("/gate/v1/me's phone and code", () => {
  const context = (deviceId: string) => ({ request: {} as IncomingMessage, response: { setHeader: vi.fn() } as unknown as ServerResponse, path: "/gate/v1/me", body: undefined, principal: { kind: "device" as const, deviceId } });

  it("says on, owner, off, and phone", async () => {
    const { registry, deviceId } = pair();
    registry.setCodeConfirm(deviceId, "owner", 4);
    const me = (users: () => Promise<unknown>) => createGateMeHandler(registry, users)(context(deviceId)) as Promise<{ phone: boolean; code: string }>;
    expect(await me(async () => [OWNER])).toMatchObject({ phone: false, code: "on" });
    expect(await me(async () => { throw new Error("read failed"); })).toMatchObject({ code: "owner" });
    expect(await me(async () => [{ ...OWNER, is_owner: false }])).toMatchObject({ code: "off" });
    registry.setConfirmPhone(deviceId, link, undefined);
    expect(await me(async () => [OWNER])).toMatchObject({ phone: true, code: "phone" });
  });
});

describe("the NOVA page's chat-code line", () => {
  const TEXTS = Object.fromEntries(PAGE_KEYS.map((key) => [key, `‹${key}›`]));
  const words: PageTexts = { lang: "en", say: (key) => TEXTS[key]! };
  const button = (action: WindowAction, id: string, label: string) => `[${action}:${id}:${label}]`;
  const base: DeviceRecord = { deviceId: "pi", secretDigest: "x", state: "active", clientInstallId: "install", name: "Pi", platform: "linux", client: "cli", createdAtMs: 0, credentialStore: "file" };
  const owner = { isOwner: true, userId: OWNER.id };

  it("gives only the owner the choice, with the warning, and shows when it's on", () => {
    expect(codeRow(base, [OWNER], owner, words, button)).toBe("<span class=\"actions\">[code_on:pi:‹page.code_turn_on›]</span><span class=\"muted meta\">‹page.code_warning›</span>");
    expect(codeRow(base, [OWNER], { isOwner: false, userId: "anna" }, words, button)).toBe("");
    const on = { ...base, codeConfirm: { userId: OWNER.id, atMs: 1 } };
    expect(codeRow(on, [OWNER], owner, words, button)).toBe("<span class=\"muted meta\">‹page.code_on›</span><span class=\"actions\">[code_off:pi:‹page.code_turn_off›]</span>");
    expect(codeRow(on, [OWNER], { isOwner: false, userId: "anna" }, words, button)).toBe("<span class=\"muted meta\">‹page.code_on›</span>");
    expect(codeRow({ ...base, confirmPhone: link }, [OWNER], owner, words, button)).toBe("");
    expect(codeRow({ ...base, credentialStore: "keychain" }, [OWNER], owner, words, button)).toBe("");
  });

  it("marks a chat-code approval in the recent list", () => {
    const html = recentWindowSection([{ atMs: 1, computer: { id: "pi", name: "Pi" }, title: "Unlock Front?", code: true }], words);
    expect(html).toContain("Unlock Front? · ‹page.code_recent›");
  });
});
