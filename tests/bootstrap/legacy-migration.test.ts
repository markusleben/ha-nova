import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { clearLegacyUpstreamOption, importLegacyToken } from "../../nova/src/runtime/legacy-migration.js";
import type { DeviceRegistry } from "../../nova/src/security/device-registry.js";

function fakeSupervisor(setOptions: (options: Record<string, unknown>) => Promise<void>) {
  return {
    getSelfInfo: vi.fn(),
    getMappedHostPort: vi.fn(),
    setOptions: vi.fn(setOptions),
  } as unknown as Parameters<typeof clearLegacyUpstreamOption>[0]["supervisor"];
}

function fakeLogger() {
  return { info: vi.fn(), warn: vi.fn(), error: vi.fn() };
}

function fakeRegistry(state: { imported?: boolean; hasLegacy?: boolean }) {
  const importLegacy = vi.fn();
  const registry: DeviceRegistry = {
    list: () => [],
    hasLegacy: () => state.hasLegacy ?? false,
    legacyImportCompleted: () => state.imported ?? false,
    resolveDeviceSecret: () => null,
    resolveLegacySecret: () => null,
    createPending: () => {},
    activate: () => {
      throw new Error("not used");
    },
    activatePending: () => null,
    revoke: () => false,
    importLegacy,
    revokeLegacy: () => {},
  };
  return { registry, importLegacy };
}

describe("clearLegacyUpstreamOption", () => {
  let dir: string;
  let optionsPath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "ha-nova-llat-"));
    optionsPath = join(dir, "options.json");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("clears a stored ha_llat while preserving every other option", async () => {
    writeFileSync(
      optionsPath,
      JSON.stringify({ ha_llat: "secret-llat", relay_auth_token: "shared", file_access: "off" }),
    );
    const writes: Array<Record<string, unknown>> = [];
    const supervisor = fakeSupervisor(async (options) => {
      writes.push(options);
    });

    await clearLegacyUpstreamOption({ supervisor, appOptionsPath: optionsPath, logger: fakeLogger() });

    expect(writes).toHaveLength(1);
    expect(writes[0]).toEqual({ ha_llat: "", relay_auth_token: "shared", file_access: "off" });
  });

  it("is a no-op when ha_llat is already empty", async () => {
    writeFileSync(optionsPath, JSON.stringify({ ha_llat: "", file_access: "off" }));
    const supervisor = fakeSupervisor(async () => {});

    await clearLegacyUpstreamOption({ supervisor, appOptionsPath: optionsPath, logger: fakeLogger() });

    expect(supervisor.setOptions).not.toHaveBeenCalled();
  });

  it("is a no-op when ha_llat is absent", async () => {
    writeFileSync(optionsPath, JSON.stringify({ file_access: "off" }));
    const supervisor = fakeSupervisor(async () => {});

    await clearLegacyUpstreamOption({ supervisor, appOptionsPath: optionsPath, logger: fakeLogger() });

    expect(supervisor.setOptions).not.toHaveBeenCalled();
  });

  it("warns without throwing when the option write fails", async () => {
    writeFileSync(optionsPath, JSON.stringify({ ha_llat: "secret-llat" }));
    const supervisor = fakeSupervisor(async () => {
      throw new Error("supervisor unavailable");
    });
    const logger = fakeLogger();

    await expect(
      clearLegacyUpstreamOption({ supervisor, appOptionsPath: optionsPath, logger }),
    ).resolves.toBeUndefined();
    expect(logger.warn).toHaveBeenCalledOnce();
  });
});

describe("importLegacyToken", () => {
  let dir: string;
  let optionsPath: string;
  let tokenFilePath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "ha-nova-import-"));
    optionsPath = join(dir, "options.json");
    tokenFilePath = join(dir, "relay_auth_token");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("imports the digest once and removes both the file and the option", async () => {
    writeFileSync(optionsPath, JSON.stringify({ relay_auth_token: "shared-secret", file_access: "off" }));
    writeFileSync(tokenFilePath, "shared-secret");
    const { registry, importLegacy } = fakeRegistry({ imported: false });
    const writes: Array<Record<string, unknown>> = [];
    const supervisor = fakeSupervisor(async (options) => {
      writes.push(options);
    });

    await importLegacyToken({ registry, supervisor, dataDir: dir, appOptionsPath: optionsPath, now: () => 1, logger: fakeLogger() });

    expect(importLegacy).toHaveBeenCalledOnce();
    expect(existsSync(tokenFilePath)).toBe(false);
    expect(writes.at(-1)).toEqual({ relay_auth_token: "", file_access: "off" });
  });

  it("retries residual cleanup on a later boot without re-importing", async () => {
    // Prior boot stamped the tombstone (imported) but left the plaintext behind.
    writeFileSync(optionsPath, JSON.stringify({ relay_auth_token: "residual", file_access: "off" }));
    writeFileSync(tokenFilePath, "residual");
    const { registry, importLegacy } = fakeRegistry({ imported: true });
    const writes: Array<Record<string, unknown>> = [];
    const supervisor = fakeSupervisor(async (options) => {
      writes.push(options);
    });

    await importLegacyToken({ registry, supervisor, dataDir: dir, appOptionsPath: optionsPath, now: () => 1, logger: fakeLogger() });

    expect(importLegacy).not.toHaveBeenCalled(); // never re-import
    expect(existsSync(tokenFilePath)).toBe(false); // residual file removed
    expect(writes.at(-1)).toEqual({ relay_auth_token: "", file_access: "off" }); // option cleared
  });

  it("is a no-op once imported and already clean", async () => {
    writeFileSync(optionsPath, JSON.stringify({ file_access: "off" }));
    const { registry, importLegacy } = fakeRegistry({ imported: true });
    const supervisor = fakeSupervisor(async () => {});

    await importLegacyToken({ registry, supervisor, dataDir: dir, appOptionsPath: optionsPath, now: () => 1, logger: fakeLogger() });

    expect(importLegacy).not.toHaveBeenCalled();
    expect(supervisor.setOptions).not.toHaveBeenCalled();
  });
});
