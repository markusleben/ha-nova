import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("app bootstrap dev script contract", () => {
  it("provides executable bootstrap helper script", () => {
    const file = "scripts/dev/ha-app-bootstrap.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  });

  it("supports first install, source sync, and option provisioning", () => {
    const content = readFileSync("scripts/dev/ha-app-bootstrap.sh", "utf8");

    expect(content).toContain("rsync -az");
    expect(content).toContain("/addons/local/${APP_SLUG}");
    expect(content).toContain("ha store reload");
    expect(content).toContain("ha apps install");
    expect(content).toContain("ha apps rebuild");
    expect(content).toContain("ha apps start");
    expect(content).toContain("/options/validate");
    expect(content).toContain("/options");
    expect(content).toContain("SUPERVISOR_TOKEN");
    expect(content).toContain('"file_access": current_options.get("file_access", "off")');
    expect(content).toContain('if section != "options":');
    expect(content).toContain('line.startswith("  relay_auth_token:")');
    expect(content).toContain("curl -fsS");
  });

  it("does not require an LLAT but preserves a stored legacy option", () => {
    const content = readFileSync("scripts/dev/ha-app-bootstrap.sh", "utf8");

    // No HA_LLAT requirement and no env-var plumbing for it: the App gets its
    // upstream access from Supervisor.
    expect(content).not.toContain("HA_LLAT is required");
    expect(content).not.toContain("HA_LLAT|");
    expect(content).not.toContain('os.environ.get("HA_LLAT"');
    // But a stored legacy option survives the options rewrite (read-only
    // passthrough) so an existing install keeps its one-time migration source.
    expect(content).toContain('current_options.get("ha_llat", "")');
    expect(content).toContain("/options");
  });

  it("exposes npm shortcut in dev namespace", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.["dev:app:bootstrap"]).toBe(
      "bash scripts/dev/ha-app-bootstrap.sh"
    );
  });
});
