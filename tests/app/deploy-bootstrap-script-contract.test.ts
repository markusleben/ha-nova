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
    expect(content).toContain("ha apps start");
    expect(content).toContain("/options/validate");
    expect(content).toContain("/options");
    expect(content).toContain("SUPERVISOR_TOKEN");
    expect(content).toContain('"ha_llat": os.environ.get("HA_LLAT", "")');
    expect(content).toContain('if section != "options":');
    expect(content).toContain('line.startswith("  relay_auth_token:")');
    expect(content).toContain("curl -fsS");
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
