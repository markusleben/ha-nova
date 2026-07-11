import { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { createFilesHandler } from "../../nova/src/http/handlers/files.js";
import { HttpError } from "../../nova/src/http/errors.js";
import type { FileAccessConfig } from "../../nova/src/config/file-access.js";

let root = "";
let outside = "";

function handler(mode: FileAccessConfig["mode"]) {
  return createFilesHandler({ fileAccess: { mode, configRoot: root, warnings: [] } });
}

async function call(mode: FileAccessConfig["mode"], body: unknown): Promise<unknown> {
  return await handler(mode)({ body } as never);
}

async function expectHttpError(
  promise: Promise<unknown>,
  status: number,
  code: string
): Promise<void> {
  await expect(promise).rejects.toSatisfy((error: unknown) => {
    if (!(error instanceof HttpError)) return false;
    expect(error.status).toBe(status);
    expect(error.code).toBe(code);
    return true;
  });
}

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "nova-files-root-"));
  outside = mkdtempSync(join(tmpdir(), "nova-files-outside-"));
  writeFileSync(join(root, "configuration.yaml"), "homeassistant:\n  name: Home\n");
  writeFileSync(join(outside, "loot.txt"), "secrets outside the root");
  mkdirSync(join(root, ".storage"));
  writeFileSync(join(root, ".storage", "auth"), '{"tokens":"very secret"}');
  writeFileSync(join(root, "secrets.yaml"), "llat: super-secret\n");
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
  rmSync(outside, { recursive: true, force: true });
});

describe("files handler — containment", () => {
  // Each of these would hand an agent (or a prompt injection) files outside the
  // Home Assistant config directory. They are the whole reason this endpoint is
  // opt-in.
  const escapes = [
    "/config/../../etc/passwd",
    "/config/../.ssh/id_rsa",
    "/config/subdir/../../outside.txt",
    "/config/%2e%2e/%2e%2e/etc/passwd",
    "/config/%252e%252e/etc/passwd",
    "/etc/passwd",
    "config/relative.yaml",
    "/config/back\\slash.yaml"
  ];

  for (const path of escapes) {
    it(`rejects the escape attempt ${JSON.stringify(path)}`, async () => {
      await expect(call("readwrite", { action: "read_file", path })).rejects.toBeInstanceOf(HttpError);
    });
  }

  it("rejects control characters in the path", async () => {
    await expectHttpError(
      call("readwrite", { action: "read_file", path: "/config/evil\u0000.yaml" }),
      400,
      "FILE_PATH_INVALID"
    );
  });

  it("refuses to follow a symlink that points outside the config root", async () => {
    symlinkSync(join(outside, "loot.txt"), join(root, "escape.txt"));
    await expectHttpError(
      call("readwrite", { action: "read_file", path: "/config/escape.txt" }),
      400,
      "FILE_PATH_INVALID"
    );
  });

  it("refuses to write through a symlinked directory that escapes the root", async () => {
    symlinkSync(outside, join(root, "escape-dir"));
    await expectHttpError(
      call("readwrite", {
        action: "write_file",
        path: "/config/escape-dir/pwned.yaml",
        content: "x: 1\n"
      }),
      400,
      "FILE_PATH_INVALID"
    );
    expect(existsSync(join(outside, "pwned.yaml"))).toBe(false);
  });
});

describe("files handler — derived paths cannot be hijacked", () => {
  // .bak and .nova-tmp are DERIVED from the requested path, so the containment
  // check never sees them. A symlink planted at either name would otherwise be
  // followed straight out of the config directory.
  it("does not follow a symlinked .nova-tmp out of the config root", async () => {
    symlinkSync(join(outside, "loot.txt"), join(root, "configuration.yaml.nova-tmp"));

    await call("readwrite", {
      action: "write_file",
      path: "/config/configuration.yaml",
      content: "homeassistant:\n  name: Safe\n"
    });

    // The write landed on the real file, and the symlink target was untouched.
    expect(readFileSync(join(root, "configuration.yaml"), "utf8")).toContain("Safe");
    expect(readFileSync(join(outside, "loot.txt"), "utf8")).toBe("secrets outside the root");
  });

  it("does not follow a symlinked .bak out of the config root", async () => {
    symlinkSync(join(outside, "loot.txt"), join(root, "configuration.yaml.bak"));

    await call("readwrite", {
      action: "write_file",
      path: "/config/configuration.yaml",
      content: "homeassistant:\n  name: Safe\n"
    });

    // The backup was written inside the root; the outside file is intact.
    expect(readFileSync(join(root, "configuration.yaml.bak"), "utf8")).toContain("name: Home");
    expect(readFileSync(join(outside, "loot.txt"), "utf8")).toBe("secrets outside the root");
  });

  it("rejects a non-boolean backup instead of silently skipping the rollback copy", async () => {
    await expectHttpError(
      call("readwrite", {
        action: "write_file",
        path: "/config/configuration.yaml",
        content: "x: 1\n",
        backup: "true"
      }),
      400,
      "VALIDATION_ERROR"
    );
    // The original file is untouched: a malformed request must not overwrite.
    expect(readFileSync(join(root, "configuration.yaml"), "utf8")).toContain("name: Home");
  });
});

describe("files handler — symlinks cannot launder a denied path", () => {
  // The writable-extension guard runs on the requested name. A link with a
  // .yaml name pointing at a shell script would otherwise let the write land on
  // the executable file the guard exists to protect.
  it("refuses to write through a .yaml symlink that points at a shell script", async () => {
    mkdirSync(join(root, "scripts"));
    writeFileSync(join(root, "scripts", "publish.sh"), "#!/bin/sh\necho original\n");
    symlinkSync(join(root, "scripts", "publish.sh"), join(root, "safe.yaml"));

    await expectHttpError(
      call("readwrite", { action: "write_file", path: "/config/safe.yaml", content: "rm -rf /\n" }),
      403,
      "FILE_TYPE_DENIED"
    );
    expect(readFileSync(join(root, "scripts", "publish.sh"), "utf8")).toContain("echo original");
  });

  // The deny list is applied to the requested NAME. A symlink with an innocent
  // name that points at a denied path INSIDE the root passes containment (the
  // target really is in the root) — so the deny rules have to be applied again
  // after resolution, or the symlink is a bypass straight to secrets.
  it("refuses a symlink that points at secrets.yaml", async () => {
    symlinkSync(join(root, "secrets.yaml"), join(root, "innocent.yaml"));
    await expectHttpError(
      call("read", { action: "read_file", path: "/config/innocent.yaml" }),
      403,
      "FILE_PATH_DENIED"
    );
  });

  it("refuses a symlink that points into .storage", async () => {
    symlinkSync(join(root, ".storage", "auth"), join(root, "harmless.json"));
    await expectHttpError(
      call("read", { action: "read_file", path: "/config/harmless.json" }),
      403,
      "FILE_PATH_DENIED"
    );
  });

  it("refuses to write through a symlink that points at a denied path", async () => {
    symlinkSync(join(root, "secrets.yaml"), join(root, "sneaky.yaml"));
    await expectHttpError(
      call("readwrite", { action: "write_file", path: "/config/sneaky.yaml", content: "pwned: true\n" }),
      403,
      "FILE_PATH_DENIED"
    );
    expect(readFileSync(join(root, "secrets.yaml"), "utf8")).toContain("super-secret");
  });

  it("refuses to back up a file that is too large to copy", async () => {
    const big = join(root, "huge.yaml");
    writeFileSync(big, "x".repeat(1024 * 1024 + 10));
    await expectHttpError(
      call("readwrite", { action: "write_file", path: "/config/huge.yaml", content: "small: true\n" }),
      400,
      "FILE_TOO_LARGE"
    );
    // The original is untouched: a refused backup must not have overwritten it.
    expect(readFileSync(big, "utf8").length).toBeGreaterThan(1024 * 1024);
  });
});
