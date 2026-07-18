import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { clearLegacyUpstreamOption } from "../../nova/src/runtime/legacy-migration.js";

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
