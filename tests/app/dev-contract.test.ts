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

  it("keeps the shipped relay dependency tree aligned with root development", () => {
    const rootPkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      dependencies?: Record<string, string>;
    };
    const relayPkg = JSON.parse(readFileSync("nova/package.json", "utf8")) as {
      dependencies?: Record<string, string>;
    };
    const rootLock = JSON.parse(readFileSync("package-lock.json", "utf8")) as {
      lockfileVersion?: number;
      packages?: Record<
        string,
        { dependencies?: Record<string, string>; dev?: boolean; integrity?: string; version?: string }
      >;
    };
    const relayLock = JSON.parse(readFileSync("nova/package-lock.json", "utf8")) as {
      lockfileVersion?: number;
      packages?: Record<
        string,
        { dependencies?: Record<string, string>; dev?: boolean; integrity?: string; version?: string }
      >;
    };

    const relayDependencies = relayPkg.dependencies ?? {};
    expect(Object.keys(relayDependencies).length).toBeGreaterThan(0);
    expect(rootLock.lockfileVersion).toBe(3);
    expect(relayLock.lockfileVersion).toBe(3);

    for (const [name, range] of Object.entries(relayDependencies)) {
      const rootRange = rootPkg.dependencies?.[name];
      expect(rootRange, `${name} declared range`).toBe(range);
      expect(relayLock.packages?.[""]?.dependencies?.[name], `${name} relay lock range`).toBe(range);
      expect(rootLock.packages?.[""]?.dependencies?.[name], `${name} root lock range`).toBe(rootRange);

      const lockKey = `node_modules/${name}`;
      const relayVersion = relayLock.packages?.[lockKey]?.version;
      const rootVersion = rootLock.packages?.[lockKey]?.version;
      expect(relayVersion, `${name} relay lock resolution`).toBeTypeOf("string");
      expect(rootVersion, `${name} root lock resolution`).toBeTypeOf("string");
      expect(rootVersion, `${name} resolved version parity`).toBe(relayVersion);
    }

    for (const [lockPath, relayPackage] of Object.entries(relayLock.packages ?? {})) {
      if (lockPath === "" || relayPackage.dev === true) {
        continue;
      }
      const rootPackage = rootLock.packages?.[lockPath];
      expect(rootPackage, `${lockPath} exists in root lock`).toBeDefined();
      expect(relayPackage.version, `${lockPath} relay version`).toBeTypeOf("string");
      expect(relayPackage.integrity, `${lockPath} relay integrity`).toBeTypeOf("string");
      expect(rootPackage?.version, `${lockPath} production version parity`).toBe(relayPackage.version);
      expect(rootPackage?.integrity, `${lockPath} production integrity parity`).toBe(relayPackage.integrity);
    }
  });
});
