import { linkSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createPhoneLinkStore } from "../../nova/src/runtime/gate-phone-store.js";

let dir: string;
beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "hn-link-"));
});
afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

const pixel = { service: "mobile_app_pixel", device_id: "dev1", user_id: "admin1" };
const iphone = { service: "mobile_app_iphone", device_id: "dev2", user_id: "admin1", platform: "ios" };

describe("the standalone phone link store", () => {
  it("sets a link only while the file still holds the one the ask started from", () => {
    const store = createPhoneLinkStore(join(dir, "gate-phone.json"));
    expect(store.link()).toBeUndefined();
    expect(store.setLink(pixel, undefined)).toBe(true);
    expect(store.link()).toEqual(pixel);
    expect(store.setLink(iphone, undefined)).toBe(false); // changed meanwhile
    expect(store.setLink(iphone, pixel)).toBe(true);
    expect(JSON.parse(readFileSync(join(dir, "gate-phone.json"), "utf8"))).toEqual(iphone);
  });

  it("never reads a link through a symlink or a second name", () => {
    const file = join(dir, "gate-phone.json");
    const store = createPhoneLinkStore(file);
    writeFileSync(join(dir, "elsewhere.json"), JSON.stringify(pixel));
    symlinkSync(join(dir, "elsewhere.json"), file);
    expect(store.link()).toBeUndefined();
    rmSync(file);
    linkSync(join(dir, "elsewhere.json"), file);
    expect(store.link()).toBeUndefined();
  });

  it("reads a broken or foreign file as no link", () => {
    const file = join(dir, "gate-phone.json");
    const store = createPhoneLinkStore(file);
    for (const content of ["{", "[]", JSON.stringify({ ...pixel, service: "notify.all" }), JSON.stringify({ ...pixel, platform: "web" }), JSON.stringify({ ...pixel, user_id: "" })]) {
      writeFileSync(file, content);
      expect([content, store.link()]).toEqual([content, undefined]);
    }
  });
});
