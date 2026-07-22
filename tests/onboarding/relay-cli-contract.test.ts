import { existsSync, readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

import { createMockHome, REPO_ROOT } from "./_helpers.js";

describe("relay cli contract", () => {
  it("Go relay binary source exists", () => {
    expect(existsSync("cli/main.go")).toBe(true);
    expect(existsSync("cli/go.mod")).toBe(true);
    expect(existsSync("cli/relay.go")).toBe(true);
    expect(existsSync("cli/config.go")).toBe(true);
    expect(existsSync("cli/jq.go")).toBe(true);
    expect(existsSync("cli/version.go")).toBe(true);
  });

  it("documents strict UTF-8 input and its Relay error code", () => {
    const contract = readFileSync("skills/ha-nova/relay-api.md", "utf8");
    expect(contract).toContain("One leading UTF-8 BOM is accepted");
    expect(contract).toContain("400 / INVALID_UTF8");
    expect(contract).toContain("do not retry automatically");
  });

  it("guides the user to setup when JSON config is missing", () => {
    const cliBinary = "/tmp/ha-nova";
    const build = spawnSync("go", ["build", "-o", cliBinary, "."], {
      cwd: `${REPO_ROOT}/cli`,
      encoding: "utf8",
      timeout: 120000,
    });
    if (build.status !== 0) {
      throw new Error(`Go build failed: ${build.stderr}`);
    }

    const home = createMockHome();
    const result = spawnSync(
      cliBinary,
      ["relay", "health"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 15000,
        env: { ...process.env, HOME: home },
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(result.status).not.toBe(0);
    expect(output).toContain("HA NOVA is not set up yet");
    expect(output).toContain("ha-nova setup");
  }, 120000);
});
