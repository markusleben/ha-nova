import { join } from "node:path";
import { pathToFileURL } from "node:url";

import { describe, expect, it } from "vitest";

// The VM lane's App preview (scripts/e2e/vm-lane/app-preview.mjs) against a fake Supervisor and
// host shell: no VM. The store switch, the in-place update, and the digest pin are judged by rows.
type Row = { step: string; name: string; ok: boolean };
type Preview = { install: (branch: string, registry: string) => Promise<boolean>; restoreStore: () => Promise<boolean> };
const { appPreview, APP_BRANCH_RE } = (await import(pathToFileURL(join(process.cwd(), "scripts/e2e/vm-lane/app-preview.mjs")).href)) as {
  appPreview: (ctx: Record<string, unknown>) => Preview;
  APP_BRANCH_RE: RegExp;
};

const D = { aarch64: "sha256:" + "a".repeat(64), amd64: "sha256:" + "b".repeat(64) };
const IMAGE = "ghcr.io/markusleben/aarch64-ha-nova-relay";
// Real verify-app-image.sh output shape.
const verified = (v: string) => [
  "[verify-app-image] OK: ghcr.io/markusleben/aarch64-ha-nova-relay:" + v + " (linux/arm64) aarch64=" + D.aarch64,
  "[verify-app-image] OK: ghcr.io/markusleben/amd64-ha-nova-relay:" + v + " (linux/amd64) amd64=" + D.amd64,
].join("\n") + "\n";

function vm({ preview = "0.14.0", digests = [IMAGE + "@" + D.aarch64] as readonly string[], running = "sha256:img", registry = "r1", extra = {} as Record<string, unknown> } = {}) {
  const rows: Row[] = [], shell: string[] = [];
  let store = "main", installed = "0.13.23", offered = "0.13.24";
  const configs: Record<string, string> = { main: "0.13.24", "chore/app-0.14.0": preview };
  const sup = async (endpoint: string, method = "get") => {
    if (endpoint === "/info") return { arch: "aarch64" };
    if (endpoint === "/supervisor/restart") { offered = configs[store] ?? "?"; return {}; }
    if (endpoint.endsWith("/update") && method === "post") { installed = offered; return {}; }
    if (endpoint.startsWith("/store/addons/")) return { version_latest: offered };
    if (endpoint.startsWith("/addons/")) return { state: "started", version: installed };
    throw new Error("unexpected " + endpoint);
  };
  const host = (cmd: string) => {
    shell.push(cmd);
    const checkout = cmd.match(/git checkout -q -B (\S+) /)?.[1];
    if (checkout) store = checkout;
    if (cmd.startsWith("ha store repair")) store = "main";
    if (cmd.includes("rev-parse --abbrev-ref HEAD")) return store + "\n";
    if (cmd.startsWith("docker image inspect")) return "sha256:img " + JSON.stringify(digests) + "\n" + running + "\n";
    return "";
  };
  const fetch = async (url: string) => {
    const ref = url.match(/ha-nova\/(.+)\/nova\/config\.yaml$/)?.[1] ?? "";
    return { ok: ref in configs, status: ref in configs ? 200 : 404, text: async () => 'name: NOVA Relay\nversion: "' + configs[ref] + '"\n' };
  };
  const ctx = {
    repo: "markusleben/ha-nova", app: "2368fcfa_ha_nova_relay", fetch, sup, host, imageCheck: verified,
    page: async () => "<h2>Home Assistant</h2><p>Connected.</p><footer>Signed in as owner · Relay " + installed + "</footer>",
    registryHash: () => registry,
    check: (step: string, name: string, ok: boolean) => { rows.push({ step, name, ok }); return ok; },
    sleep: async () => {},
  };
  return { preview: appPreview({ ...ctx, ...extra }), rows, shell, state: () => ({ store, installed, offered }) };
}

describe("vm-lane App preview (promote branch on the throwaway HAOS VM)", () => {
  it("only takes promote branches", async () => {
    expect(APP_BRANCH_RE.test("chore/app-0.14.0")).toBe(true);
    for (const b of ["main", "chore/app-0.14", "chore/app-0.14.0;id", "release/0.30"]) expect(APP_BRANCH_RE.test(b)).toBe(false);
    await expect(vm().preview.install("main", "r1")).rejects.toThrow("chore/app-X.Y.Z");
  });

  it("switches the store, updates in place, and checks pull, digest, pairing, and page", async () => {
    const lane = vm();
    expect(await lane.preview.install("chore/app-0.14.0", "r1")).toBe(true);
    expect(lane.rows.every((r) => r.ok)).toBe(true);
    expect(lane.state()).toEqual({ store: "chore/app-0.14.0", installed: "0.14.0", offered: "0.14.0" });
    expect(lane.shell[0]).toBe("docker exec hassio_supervisor sh -c 'cd /data/apps/git/2368fcfa && git fetch -q --depth 1 origin +refs/heads/chore/app-0.14.0:refs/remotes/origin/chore/app-0.14.0 && git checkout -q -B chore/app-0.14.0 origin/chore/app-0.14.0'");
    expect(lane.rows.find((r) => r.name.startsWith("App preview images"))).toBeTruthy();
  });

  it("refuses a preview that is not newer than main's App, before any switch", async () => {
    const lane = vm({ preview: "0.13.24" });
    expect(await lane.preview.install("chore/app-0.14.0", "r1")).toBe(false);
    expect(lane.shell).toEqual([]);
    expect(await lane.preview.restoreStore()).toBe(true);
    expect(lane.shell).toEqual([]);
  });

  it("fails on a re-pushed image, a local build, or a changed registry", async () => {
    for (const [opts, row] of [
      [{ digests: [IMAGE + "@sha256:" + "c".repeat(64)] }, "App preview runs the image pulled at the recorded digest"],
      [{ digests: [] }, "App preview runs the image pulled at the recorded digest"],
      [{ running: "sha256:old" }, "App preview runs the image pulled at the recorded digest"],
      [{ registry: "r2" }, "pairing kept (device registry and TLS key unchanged)"],
    ] as const) {
      const lane = vm(opts);
      expect(await lane.preview.install("chore/app-0.14.0", "r1")).toBe(false);
      expect(lane.rows.filter((r) => !r.ok).map((r) => r.name)).toEqual([row]);
    }
  });

  it("runs beforeUpdate while the store offers the preview and the stable App runs, then judges pairing from there", async () => {
    let seen = {}, registry = "r1";
    const lane = vm({ extra: { registryHash: () => registry, beforeUpdate: async (v: string) => { seen = { v, ...lane.state() }; registry = "r2"; } } });
    expect(await lane.preview.install("chore/app-0.14.0", "r1")).toBe(true);
    expect(seen).toEqual({ v: "0.14.0", store: "chore/app-0.14.0", installed: "0.13.23", offered: "0.14.0" });
    expect(lane.rows.every((r) => r.ok)).toBe(true);
  });

  it("returns the store to main and checks the offered version", async () => {
    const lane = vm();
    await lane.preview.install("chore/app-0.14.0", "r1");
    expect(await lane.preview.restoreStore()).toBe(true);
    expect(lane.shell).toContain("ha store repair 2368fcfa --no-progress");
    expect(lane.state()).toMatchObject({ store: "main", offered: "0.13.24" });
  });
});
