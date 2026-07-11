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

describe("files handler — access gate", () => {
  it("refuses everything when file access is off", async () => {
    await expectHttpError(
      call("off", { action: "read_file", path: "/config/configuration.yaml" }),
      403,
      "FILE_ACCESS_DISABLED"
    );
  });

  it("allows reads but refuses writes in read mode", async () => {
    const read = (await call("read", {
      action: "read_file",
      path: "/config/configuration.yaml"
    })) as { content: string };
    expect(read.content).toContain("homeassistant:");

    await expectHttpError(
      call("read", { action: "write_file", path: "/config/x.yaml", content: "a: 1\n" }),
      403,
      "FILE_ACCESS_READONLY"
    );
    await expectHttpError(
      call("read", { action: "delete_file", path: "/config/configuration.yaml" }),
      403,
      "FILE_ACCESS_READONLY"
    );
  });
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

describe("files handler — always-denied paths", () => {
  const denied = [
    "/config/.storage/auth",
    "/config/secrets.yaml",
    // Backup/editor variants hold the SAME credentials — an exact-name deny
    // would have served every one of these.
    "/config/secrets.yaml.bak",
    "/config/secrets.yaml~",
    "/config/secrets.yaml.old",
    "/config/secrets.yml",
    "/config/home-assistant_v2.db",
    "/config/home-assistant_v2.db-wal",
    "/config/home-assistant.log",
    // Home Assistant itself writes .log.fault; rotation and backups add more.
    "/config/home-assistant.log.fault",
    "/config/home-assistant.log.1",
    "/config/home-assistant.log.bak",
    "/config/.env",
    "/config/.env.bak",
    "/config/.ssh/id_rsa",
    "/config/ssl/fullchain.pem"
  ];

  for (const path of denied) {
    it(`never serves ${path}`, async () => {
      await expectHttpError(
        call("readwrite", { action: "read_file", path }),
        403,
        "FILE_PATH_DENIED"
      );
    });
  }

  it("never writes to a denied path either", async () => {
    await expectHttpError(
      call("readwrite", { action: "write_file", path: "/config/secrets.yaml", content: "x" }),
      403,
      "FILE_PATH_DENIED"
    );
    expect(readFileSync(join(root, "secrets.yaml"), "utf8")).toContain("super-secret");
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

describe("files handler — operations", () => {
  it("lists a directory", async () => {
    const result = (await call("read", { action: "list_dir", path: "/config" })) as {
      entries: Array<{ name: string; type: string }>;
    };
    const names = result.entries.map((e) => e.name);
    expect(names).toContain("configuration.yaml");
    expect(result.entries.find((e) => e.name === ".storage")?.type).toBe("dir");
  });

  // stat() follows symlinks; lstat() does not. Listing a directory that holds a
  // link to a denied or outside file must not leak the target's metadata — a
  // path this endpoint refuses to serve directly.
  it("lists symlinks as links and leaks no metadata about their targets", async () => {
    symlinkSync(join(root, "secrets.yaml"), join(root, "to-secrets.yaml"));
    symlinkSync(join(outside, "loot.txt"), join(root, "to-outside.txt"));

    const result = (await call("read", { action: "list_dir", path: "/config" })) as {
      entries: Array<{ name: string; type: string; size: number | null }>;
    };

    for (const name of ["to-secrets.yaml", "to-outside.txt"]) {
      const entry = result.entries.find((e) => e.name === name);
      expect(entry?.type, name).toBe("symlink");
      expect(entry?.size, name).toBeNull();
    }
  });

  it("writes a new file and reports that it was created", async () => {
    const result = (await call("readwrite", {
      action: "write_file",
      path: "/config/ha_nova/templates.yaml",
      content: "- sensor:\n    - name: Test\n"
    })) as { written: boolean; created: boolean; backup: string | null };

    expect(result.written).toBe(true);
    expect(result.created).toBe(true);
    expect(result.backup).toBeNull();
    expect(readFileSync(join(root, "ha_nova", "templates.yaml"), "utf8")).toContain("name: Test");
  });

  it("backs up an existing file before overwriting it", async () => {
    const result = (await call("readwrite", {
      action: "write_file",
      path: "/config/configuration.yaml",
      content: "homeassistant:\n  name: Changed\n"
    })) as { created: boolean; backup: string | null };

    expect(result.created).toBe(false);
    expect(result.backup).toBe("/config/configuration.yaml.bak");
    expect(readFileSync(join(root, "configuration.yaml"), "utf8")).toContain("Changed");
    // The .bak must hold the ORIGINAL — that is the whole point of the rollback path.
    expect(readFileSync(join(root, "configuration.yaml.bak"), "utf8")).toContain("name: Home");
  });

  // A symlink to another ALLOWED file inside the root is a legitimate structure
  // (packages, themes). The read side follows it, so the write must too —
  // otherwise the rename replaces the link with a regular file and the real
  // target silently keeps the old content.
  it("writes through an in-root symlink instead of replacing it", async () => {
    mkdirSync(join(root, "ha_nova"));
    writeFileSync(join(root, "ha_nova", "pkg.yaml"), "old: true\n");
    symlinkSync(join(root, "ha_nova", "pkg.yaml"), join(root, "packages.yaml"));

    const result = (await call("readwrite", {
      action: "write_file",
      path: "/config/packages.yaml",
      content: "new: true\n"
    })) as { backup: string | null };

    // The link survives, and the real file carries the new content.
    expect(lstatSync(join(root, "packages.yaml")).isSymbolicLink()).toBe(true);
    expect(readFileSync(join(root, "ha_nova", "pkg.yaml"), "utf8")).toContain("new: true");

    // The reported backup path must be the one that EXISTS — beside the real
    // target, not beside the link — or the rollback would read a missing file.
    expect(result.backup).toBe("/config/ha_nova/pkg.yaml.bak");
    expect(readFileSync(join(root, "ha_nova", "pkg.yaml.bak"), "utf8")).toContain("old: true");
  });

  // ...but deleting a link must remove the LINK, never its target.
  it("deletes the symlink itself, not the file it points at", async () => {
    mkdirSync(join(root, "ha_nova2"));
    writeFileSync(join(root, "ha_nova2", "keep.yaml"), "keep: true\n");
    symlinkSync(join(root, "ha_nova2", "keep.yaml"), join(root, "link.yaml"));

    await call("readwrite", { action: "delete_file", path: "/config/link.yaml" });

    expect(existsSync(join(root, "link.yaml"))).toBe(false);
    expect(readFileSync(join(root, "ha_nova2", "keep.yaml"), "utf8")).toContain("keep: true");
  });

  it("leaves no temp file behind after a write", async () => {
    await call("readwrite", {
      action: "write_file",
      path: "/config/configuration.yaml",
      content: "homeassistant:\n  name: Atomic\n"
    });
    expect(existsSync(join(root, "configuration.yaml.nova-tmp"))).toBe(false);
  });

  it("deletes a file but never a directory", async () => {
    await call("readwrite", { action: "write_file", path: "/config/tmp.yaml", content: "a: 1\n" });
    const deleted = (await call("readwrite", { action: "delete_file", path: "/config/tmp.yaml" })) as {
      deleted: boolean;
    };
    expect(deleted.deleted).toBe(true);
    expect(existsSync(join(root, "tmp.yaml"))).toBe(false);

    mkdirSync(join(root, "adir"));
    await expectHttpError(
      call("readwrite", { action: "delete_file", path: "/config/adir" }),
      400,
      "FILE_IS_DIRECTORY"
    );
    expect(existsSync(join(root, "adir"))).toBe(true);
  });

  // Deleting a link removes the LINK. A link that happens to point at a
  // directory must therefore still be deletable — the directory guard applies
  // to real directories, not to links.
  it("deletes a symlink that points at a directory, leaving the directory intact", async () => {
    mkdirSync(join(root, "themes"));
    writeFileSync(join(root, "themes", "dark.yaml"), "dark: {}\n");
    symlinkSync(join(root, "themes"), join(root, "themes-link"));

    await call("readwrite", { action: "delete_file", path: "/config/themes-link" });

    expect(existsSync(join(root, "themes-link"))).toBe(false);
    expect(readFileSync(join(root, "themes", "dark.yaml"), "utf8")).toContain("dark:");
  });

  it("refuses to serve a binary file as text", async () => {
    writeFileSync(join(root, "blob.bin"), Buffer.from([0x00, 0x01, 0x02]));
    await expectHttpError(
      call("read", { action: "read_file", path: "/config/blob.bin" }),
      400,
      "FILE_NOT_TEXT"
    );
  });

  // Invalid UTF-8 with no NUL bytes would pass a naive check and then be
  // silently repaired into U+FFFD — and the read -> diff -> write flow would
  // write that corruption back into the user's config. Refuse instead.
  it("refuses a file with invalid UTF-8 instead of silently mangling it", async () => {
    // 0x80 is a lone continuation byte: valid in latin-1, invalid in UTF-8.
    writeFileSync(join(root, "latin1.yaml"), Buffer.from([0x6e, 0x61, 0x6d, 0x65, 0x3a, 0x20, 0x80, 0x0a]));
    await expectHttpError(
      call("read", { action: "read_file", path: "/config/latin1.yaml" }),
      400,
      "FILE_NOT_TEXT"
    );
  });

  it("refuses to WRITE content with NUL bytes, matching the read contract", async () => {
    await expectHttpError(
      call("readwrite", {
        action: "write_file",
        path: "/config/binary.yaml",
        content: "name: \u0000evil\n"
      }),
      400,
      "VALIDATION_ERROR"
    );
    expect(existsSync(join(root, "binary.yaml"))).toBe(false);
  });

  it("reports a missing file honestly", async () => {
    await expectHttpError(
      call("read", { action: "read_file", path: "/config/nope.yaml" }),
      404,
      "FILE_NOT_FOUND"
    );
  });
});
