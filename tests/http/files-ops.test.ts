import { execFileSync } from "node:child_process";

import { chmodSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from "node:fs";
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

  // A config file kept at 0600 because it holds tokens must not become
  // world-readable just because HA NOVA edited it — and neither must its backup.
  it("preserves restrictive file permissions on the file and its backup", async () => {
    const secretish = join(root, "tokens.yaml");
    writeFileSync(secretish, "api_key: old\n", { mode: 0o600 });
    chmodSync(secretish, 0o600);

    await call("readwrite", {
      action: "write_file",
      path: "/config/tokens.yaml",
      content: "api_key: new\n"
    });

    expect(statSync(secretish).mode & 0o777).toBe(0o600);
    expect(statSync(join(root, "tokens.yaml.bak")).mode & 0o777).toBe(0o600);
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

  // Deleting a link is fine — but the DIRECTORIES on the way to it are followed
  // by unlink, so a symlinked directory must not become a delete-anywhere
  // primitive. This is the hole the "let deletes remove any link" fix opened,
  // and the reason the parent is resolved while the leaf is not.
  it("refuses to delete through a symlinked directory that escapes the root", async () => {
    symlinkSync(outside, join(root, "escape-dir"));

    await expectHttpError(
      call("readwrite", { action: "delete_file", path: "/config/escape-dir/loot.txt" }),
      400,
      "FILE_PATH_INVALID"
    );
    expect(readFileSync(join(outside, "loot.txt"), "utf8")).toBe("secrets outside the root");
  });

  it("refuses to delete through a symlinked directory that points at a denied path", async () => {
    symlinkSync(join(root, ".storage"), join(root, "store-link"));

    await expectHttpError(
      call("readwrite", { action: "delete_file", path: "/config/store-link/auth" }),
      403,
      "FILE_PATH_DENIED"
    );
    expect(readFileSync(join(root, ".storage", "auth"), "utf8")).toContain("very secret");
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

  // A FIFO reports size 0 and then blocks readFile forever, holding a libuv
  // worker hostage. Only regular files may be read or written.
  it.skipIf(process.platform === "win32")(
    "refuses to read or write a FIFO instead of hanging on it",
    async () => {
      const fifo = join(root, "pipe.yaml");
      execFileSync("mkfifo", [fifo]);

      await expectHttpError(
        call("read", { action: "read_file", path: "/config/pipe.yaml" }),
        400,
        "FILE_NOT_REGULAR"
      );
      await expectHttpError(
        call("readwrite", { action: "write_file", path: "/config/pipe.yaml", content: "x: 1\n" }),
        400,
        "FILE_NOT_REGULAR"
      );
    }
  );

  // The server rejects the whole JSON body above 1 MiB. A content limit of
  // exactly 1 MiB would therefore surface as an opaque 413 instead of a
  // FILE_TOO_LARGE that names the problem — the write ceiling leaves room for
  // the envelope and the escaping.
  it("refuses oversized content with a clear error, not an opaque body rejection", async () => {
    await expectHttpError(
      call("readwrite", {
        action: "write_file",
        path: "/config/big.yaml",
        content: "x".repeat(768 * 1024 + 1)
      }),
      400,
      "FILE_TOO_LARGE"
    );
    expect(existsSync(join(root, "big.yaml"))).toBe(false);
  });

  it("reports a missing file honestly", async () => {
    await expectHttpError(
      call("read", { action: "read_file", path: "/config/nope.yaml" }),
      404,
      "FILE_NOT_FOUND"
    );
  });
});
