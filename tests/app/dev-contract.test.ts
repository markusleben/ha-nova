import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("app dev contract", () => {
  it("runs the relay entrypoint through tsx watch instead of raw node on source ts files", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.dev).toContain("tsx watch");
    expect(pkg.scripts?.dev).toContain("nova/src/runtime/main.ts");
    expect(pkg.scripts?.dev).not.toContain("node --enable-source-maps --watch nova/src/runtime/main.ts");
  });

  it("keeps test-only skill contracts out of the relay build tsconfig", () => {
    const tsconfig = JSON.parse(readFileSync("nova/tsconfig.json", "utf8")) as {
      exclude?: string[];
    };

    expect(tsconfig.exclude).toContain("src/skills/**/*.ts");
  });

  it("cleans stale relay dist output before rebuilding", () => {
    const rootPkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };
    const relayPkg = JSON.parse(readFileSync("nova/package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(rootPkg.scripts?.build).toContain("rmSync('nova/dist'");
    expect(relayPkg.scripts?.build).toContain("rmSync('dist'");
  });
});
