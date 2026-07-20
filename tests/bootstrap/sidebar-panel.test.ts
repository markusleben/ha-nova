import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { existsSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { ensureSidebarPanel } from "../../nova/src/runtime/sidebar-panel.js";

const MARKER = "sidebar_default_applied";

function fakeSupervisor(overrides: { ingressPanel?: boolean; infoError?: Error; setError?: Error } = {}) {
  const getSelfInfo = vi.fn(async () => {
    if (overrides.infoError) throw overrides.infoError;
    return {
      version: "0.7.0",
      versionLatest: null,
      updateAvailable: false,
      ingressPanel: overrides.ingressPanel ?? false,
      network: {},
    };
  });
  const setIngressPanel = vi.fn(async () => {
    if (overrides.setError) throw overrides.setError;
  });
  return {
    getSelfInfo,
    getMappedHostPort: vi.fn(),
    setOptions: vi.fn(),
    setIngressPanel,
  } as unknown as Parameters<typeof ensureSidebarPanel>[0]["supervisor"] & {
    getSelfInfo: ReturnType<typeof vi.fn>;
    setIngressPanel: ReturnType<typeof vi.fn>;
  };
}

function fakeLogger() {
  return { info: vi.fn(), warn: vi.fn(), error: vi.fn() };
}

describe("ensureSidebarPanel", () => {
  let dataDir: string;

  beforeEach(() => {
    dataDir = mkdtempSync(join(tmpdir(), "nova-sidebar-"));
  });

  afterEach(() => {
    rmSync(dataDir, { recursive: true, force: true });
    vi.restoreAllMocks();
  });

  it("enables the panel once and stamps the marker when it is off", async () => {
    const supervisor = fakeSupervisor({ ingressPanel: false });
    const logger = fakeLogger();
    await ensureSidebarPanel({ supervisor, dataDir, logger });
    expect(supervisor.setIngressPanel).toHaveBeenCalledWith(true);
    expect(existsSync(join(dataDir, MARKER))).toBe(true);
    expect(logger.warn).not.toHaveBeenCalled();
  });

  it("stamps the marker without a write when the panel is already on", async () => {
    const supervisor = fakeSupervisor({ ingressPanel: true });
    await ensureSidebarPanel({ supervisor, dataDir, logger: fakeLogger() });
    expect(supervisor.setIngressPanel).not.toHaveBeenCalled();
    expect(existsSync(join(dataDir, MARKER))).toBe(true);
  });

  it("does nothing when the marker exists — an owner who hides the panel is never overridden", async () => {
    writeFileSync(join(dataDir, MARKER), "");
    const supervisor = fakeSupervisor({ ingressPanel: false });
    await ensureSidebarPanel({ supervisor, dataDir, logger: fakeLogger() });
    expect(supervisor.getSelfInfo).not.toHaveBeenCalled();
    expect(supervisor.setIngressPanel).not.toHaveBeenCalled();
  });

  it("warns without a marker on Supervisor failure, so the default retries next boot", async () => {
    const supervisor = fakeSupervisor({ setError: new Error("supervisor down") });
    const logger = fakeLogger();
    await ensureSidebarPanel({ supervisor, dataDir, logger });
    expect(logger.warn).toHaveBeenCalled();
    expect(existsSync(join(dataDir, MARKER))).toBe(false);
  });
});
