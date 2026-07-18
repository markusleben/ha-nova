import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { clearLegacyOptions, importLegacyToken } from "../../nova/src/runtime/legacy-migration.js";
import type { DeviceRegistry } from "../../nova/src/security/device-registry.js";

function fakeSupervisor(setOptions: (options: Record<string, unknown>) => Promise<void>) {
  return {
    getSelfInfo: vi.fn(),
    getMappedHostPort: vi.fn(),
    setOptions: vi.fn(setOptions),
  } as unknown as Parameters<typeof clearLegacyOptions>[0]["supervisor"];
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

describe("clearLegacyOptions", () => {
  let dir: string;
  let optionsPath: string;

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "ha-nova-opts-"));
    optionsPath = join(dir, "options.json");
  });

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true });
  });

  it("clears relay_auth_token and ha_llat in a SINGLE write when clearRelayToken is true", async () => {
    // Two separate writes would each spread the stale options.json and the second
    // would resurrect the first's cleared value — so both clear in one write.
    writeFileSync(
      optionsPath,
      JSON.stringify({ ha_llat: "secret-llat", relay_auth_token: "shared", file_access: "off" }),
    );
    const writes: Array<Record<string, unknown>> = [];
    const supervisor = fakeSupervisor(async (options) => {
      writes.push(options);
    });

    await clearLegacyOptions({ supervisor, appOptionsPath: optionsPath, logger: fakeLogger() }, true);

    expect(writes).toHaveLength(1);
    expect(writes[0]).toEqual({ ha_llat: "", relay_auth_token: "", file_access: "off" });
  });

  it("clears only ha_llat when clearRelayToken is false (corrupt registry keeps it recoverable)", async () => {
    writeFileSync(
      optionsPath,
      JSON.stringify({ ha_llat: "secret-llat", relay_auth_token: "shared", file_access: "off" }),
    );
    const writes: Array<Record<string, unknown>> = [];
    const supervisor = fakeSupervisor(async (options) => {
      writes.push(options);
    });

    await clearLegacyOptions({ supervisor, appOptionsPath: optionsPath, logger: fakeLogger() }, false);

    expect(writes).toHaveLength(1);
    expect(writes[0]).toEqual({ ha_llat: "", relay_auth_token: "shared", file_access: "off" });
  });

  it("is a no-op when nothing needs clearing", async () => {
    writeFileSync(optionsPath, JSON.stringify({ ha_llat: "", relay_auth_token: "", file_access: "off" }));
    const supervisor = fakeSupervisor(async () => {});

    await clearLegacyOptions({ supervisor, appOptionsPath: optionsPath, logger: fakeLogger() }, true);

    expect(supervisor.setOptions).not.toHaveBeenCalled();
  });

  it("warns without throwing when the option write fails", async () => {
    writeFileSync(optionsPath, JSON.stringify({ ha_llat: "secret-llat" }));
    const supervisor = fakeSupervisor(async () => {
      throw new Error("supervisor unavailable");
    });
    const logger = fakeLogger();

    await expect(
      clearLegacyOptions({ supervisor, appOptionsPath: optionsPath, logger }, true),
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

  it("imports the digest once and removes the plaintext file, without writing options", async () => {
    writeFileSync(optionsPath, JSON.stringify({ relay_auth_token: "shared-secret", file_access: "off" }));
    writeFileSync(tokenFilePath, "shared-secret");
    const { registry, importLegacy } = fakeRegistry({ imported: false });
    const supervisor = fakeSupervisor(async () => {});

    await importLegacyToken({ registry, supervisor, dataDir: dir, appOptionsPath: optionsPath, now: () => 1, logger: fakeLogger() });

    expect(importLegacy).toHaveBeenCalledOnce();
    expect(existsSync(tokenFilePath)).toBe(false);
    expect(supervisor.setOptions).not.toHaveBeenCalled(); // options are cleared elsewhere, in one write
  });

  it("retries file removal on a later boot without re-importing", async () => {
    // Prior boot stamped the tombstone (imported) but left the plaintext file.
    writeFileSync(optionsPath, JSON.stringify({ relay_auth_token: "residual", file_access: "off" }));
    writeFileSync(tokenFilePath, "residual");
    const { registry, importLegacy } = fakeRegistry({ imported: true });
    const supervisor = fakeSupervisor(async () => {});

    await importLegacyToken({ registry, supervisor, dataDir: dir, appOptionsPath: optionsPath, now: () => 1, logger: fakeLogger() });

    expect(importLegacy).not.toHaveBeenCalled(); // never re-import
    expect(existsSync(tokenFilePath)).toBe(false); // residual file removed
  });

  it("is a no-op once imported and the file is already gone", async () => {
    writeFileSync(optionsPath, JSON.stringify({ file_access: "off" }));
    const { registry, importLegacy } = fakeRegistry({ imported: true });
    const supervisor = fakeSupervisor(async () => {});

    await importLegacyToken({ registry, supervisor, dataDir: dir, appOptionsPath: optionsPath, now: () => 1, logger: fakeLogger() });

    expect(importLegacy).not.toHaveBeenCalled();
    expect(supervisor.setOptions).not.toHaveBeenCalled();
  });
});
