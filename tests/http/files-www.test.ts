import { chmodSync, existsSync, linkSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, symlinkSync, truncateSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { FileAccessConfig } from "../../nova/src/config/file-access.js";
import { HttpError } from "../../nova/src/http/errors.js";
import { createFilesHandler } from "../../nova/src/http/handlers/files.js";

// Images and videos in /config/www (docs/work/2026-11-10-www-media-delete-spec.md).

let root = "";
let data = "";
const DAY = 24 * 60 * 60 * 1000;
const T0 = Date.parse("2026-11-10T12:00:00Z");

async function call(body: unknown, options: { mode?: FileAccessConfig["mode"]; now?: number; dataDir?: string | null } = {}): Promise<unknown> {
  const dataDir = options.dataDir === null ? undefined : (options.dataDir ?? data);
  const handler = createFilesHandler({
    fileAccess: { mode: options.mode ?? "readwrite", configRoot: root, warnings: [], ...(dataDir === undefined ? {} : { dataDir }) },
    now: () => options.now ?? T0
  });
  return await handler({ body } as never);
}

async function refused(promise: Promise<unknown>, status: number, code: string): Promise<void> {
  await expect(promise).rejects.toSatisfy((error: unknown) => {
    expect(error).toBeInstanceOf(HttpError);
    expect([(error as HttpError).status, (error as HttpError).code]).toEqual([status, code]);
    return true;
  });
}

const www = (...parts: string[]): string => join(root, "www", ...parts);
const store = (): string[] => (existsSync(join(data, "www-undo")) ? readdirSync(join(data, "www-undo")).sort() : []);

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "nova-www-root-"));
  data = mkdtempSync(join(tmpdir(), "nova-www-data-"));
  mkdirSync(www("snapshots"), { recursive: true });
  mkdirSync(www("community", "card"), { recursive: true });
  writeFileSync(www("snapshots", "door.jpg"), Buffer.from([0xff, 0xd8, 0x00, 0x01, 0x02]));
  writeFileSync(www("community", "card", "card.js"), "evil()");
  writeFileSync(www("community", "card", "logo.png"), "png");
  writeFileSync(www("card.js"), "run()");
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
  rmSync(data, { recursive: true, force: true });
});

describe("www: list, never read or write", () => {
  it("lists www with size and time", async () => {
    const result = (await call({ action: "list_dir", path: "/config/www/snapshots" }, { mode: "read" })) as { entries: Array<Record<string, unknown>> };
    expect(result.entries).toEqual([{ name: "door.jpg", type: "file", size: 5, modified: statSync(www("snapshots", "door.jpg")).mtime.toISOString() }]);
  });

  it("never reads or writes in www", async () => {
    await refused(call({ action: "read_file", path: "/config/www/snapshots/door.jpg" }), 403, "FILE_PATH_DENIED");
    await refused(call({ action: "write_file", path: "/config/www/notes.txt", content: "x" }), 403, "FILE_PATH_DENIED");
    expect(existsSync(www("notes.txt"))).toBe(false);
  });

  it("refuses community by every name and link", async () => {
    symlinkSync(www("community"), www("cards"));
    symlinkSync(www("community"), join(root, "cards"));
    for (const path of ["/config/www/community", "/config/www/community/card/logo.png", "/config/www/Community/card/logo.png",
      "/config/www/./community/card/logo.png", "/config/www%2Fcommunity/card/logo.png", "/config/www/cards/card", "/config/www/cards/card/logo.png",
      "/config/cards/card", "/config/cards/card/logo.png"]) {
      const action = path.endsWith(".png") ? "delete_file" : "list_dir";
      await refused(call({ action, path }), 403, "FILE_PATH_DENIED");
    }
    expect(existsSync(www("community", "card", "logo.png"))).toBe(true);
    expect(store()).toEqual([]);
  });
});

describe("www: delete with a kept copy, restore", () => {
  it("deletes each media type only after its copy is kept, and puts it back", async () => {
    for (const name of ["a.jpg", "b.JPEG", "c.png", "d.gif", "e.webp", "f.mp4"]) {
      writeFileSync(www(name), `bytes of ${name}`);
      chmodSync(www(name), 0o640);
      const deleted = (await call({ action: "delete_file", path: `/config/www/${name}` })) as { deleted: boolean; copy: string; kept_until: string };
      expect(deleted).toEqual({ deleted: true, copy: expect.stringMatching(/^[0-9a-f]{32}$/), kept_until: new Date(T0 + 30 * DAY).toISOString() });
      expect(existsSync(www(name))).toBe(false);
      expect(readFileSync(join(data, "www-undo", `${deleted.copy}.bin`), "utf8")).toBe(`bytes of ${name}`);
      const listed = (await call({ action: "list_deleted" }, { mode: "read" })) as { copies: Array<{ copy: string; path: string }>; used_bytes: number };
      expect(listed.copies[0]).toMatchObject({ copy: deleted.copy, path: `/config/www/${name}`, bytes: name.length + 9 });
      expect(await call({ action: "restore_file", path: `/config/www/${name}`, copy: deleted.copy })).toEqual({ restored: true });
      expect(readFileSync(www(name), "utf8")).toBe(`bytes of ${name}`);
      expect(statSync(www(name)).mode & 0o777).toBe(0o640);
    }
    expect(store()).toEqual([]);
  });

  it("refuses what isn't a single media file, and changes nothing", async () => {
    linkSync(www("snapshots", "door.jpg"), www("twin.jpg"));
    symlinkSync(www("snapshots", "door.jpg"), www("link.jpg"));
    mkdirSync(www("folder.jpg"));
    writeFileSync(www("big.mp4"), "");
    truncateSync(www("big.mp4"), 25 * 1024 * 1024 + 1);
    await refused(call({ action: "delete_file", path: "/config/www/card.js" }), 403, "FILE_TYPE_DENIED");
    await refused(call({ action: "delete_file", path: "/config/www/twin.jpg" }), 400, "FILE_NOT_REGULAR");
    await refused(call({ action: "delete_file", path: "/config/www/link.jpg" }), 400, "FILE_NOT_REGULAR");
    await refused(call({ action: "delete_file", path: "/config/www/folder.jpg" }), 400, "FILE_IS_DIRECTORY");
    await refused(call({ action: "delete_file", path: "/config/www/big.mp4" }), 400, "FILE_TOO_LARGE");
    await refused(call({ action: "delete_file", path: "/config/www/snapshots/door.jpg" }, { mode: "read" }), 403, "FILE_ACCESS_READONLY");
    await refused(call({ action: "delete_file", path: "/config/www/snapshots/door.jpg" }, { dataDir: null }), 500, "UNDO_STORE_UNAVAILABLE");
    for (const name of ["card.js", "twin.jpg", "link.jpg", "folder.jpg", "big.mp4", "snapshots/door.jpg"]) {
      expect(existsSync(www(name)), name).toBe(true);
    }
    expect(store()).toEqual([]);
  });

  it("applies the www rules to a link into www from outside", async () => {
    symlinkSync(www("snapshots"), join(root, "pics"));
    writeFileSync(www("snapshots", "notes.yaml"), "a: 1\n");
    await refused(call({ action: "delete_file", path: "/config/pics/notes.yaml" }), 403, "FILE_TYPE_DENIED");
    await refused(call({ action: "write_file", path: "/config/pics/notes.yaml", content: "b: 2\n" }), 403, "FILE_PATH_DENIED");
    expect(readFileSync(www("snapshots", "notes.yaml"), "utf8")).toBe("a: 1\n");
    const deleted = (await call({ action: "delete_file", path: "/config/pics/door.jpg" })) as { copy: string };
    expect(existsSync(www("snapshots", "door.jpg"))).toBe(false);
    expect(store()).toEqual([`${deleted.copy}.bin`, `${deleted.copy}.json`]);
  });

  it("refuses a delete when the copies are full, and keeps the file", async () => {
    const big = "f".repeat(32);
    mkdirSync(join(data, "www-undo"));
    writeFileSync(join(data, "www-undo", `${big}.bin`), "");
    truncateSync(join(data, "www-undo", `${big}.bin`), 100 * 1024 * 1024 - 8192);
    writeFileSync(join(data, "www-undo", `${big}.json`), JSON.stringify({ path: "/config/www/old.mp4", bytes: 100 * 1024 * 1024 - 8192, mode: 0o644, sha256: "0".repeat(64), deleted_at: new Date(T0 - DAY).toISOString() }));
    await refused(call({ action: "delete_file", path: "/config/www/snapshots/door.jpg" }), 409, "UNDO_STORE_FULL");
    expect(existsSync(www("snapshots", "door.jpg"))).toBe(true);
    expect(((await call({ action: "list_deleted" })) as { copies: unknown[] }).copies).toHaveLength(1);
    // Thirty days after its delete the old copy expires and makes room.
    await call({ action: "delete_file", path: "/config/www/snapshots/door.jpg" }, { now: T0 + 29 * DAY + 1 });
    expect(store()).not.toContain(`${big}.bin`);
  });

  it("restores nothing expired, foreign, corrupt, or over an existing entry", async () => {
    const { copy } = (await call({ action: "delete_file", path: "/config/www/snapshots/door.jpg" })) as { copy: string };
    await refused(call({ action: "restore_file", path: "/config/www/snapshots/other.jpg", copy }), 400, "COPY_INVALID");
    await refused(call({ action: "restore_file", path: "/config/www/snapshots/door.jpg", copy: "0".repeat(32) }), 404, "COPY_NOT_FOUND");
    await refused(call({ action: "restore_file", path: "/config/www/snapshots/door.jpg", copy }, { mode: "read" }), 403, "FILE_ACCESS_READONLY");
    await refused(call({ action: "restore_file", path: "/config/door.jpg", copy }), 403, "FILE_PATH_DENIED");
    writeFileSync(www("snapshots", "door.jpg"), "new picture");
    await refused(call({ action: "restore_file", path: "/config/www/snapshots/door.jpg", copy }), 409, "FILE_EXISTS");
    expect(readFileSync(www("snapshots", "door.jpg"), "utf8")).toBe("new picture");
    rmSync(www("snapshots", "door.jpg"));
    symlinkSync(join(root, "elsewhere.jpg"), www("snapshots", "door.jpg"));
    await refused(call({ action: "restore_file", path: "/config/www/snapshots/door.jpg", copy }), 409, "FILE_EXISTS");
    expect(existsSync(join(root, "elsewhere.jpg"))).toBe(false);
    rmSync(www("snapshots", "door.jpg"));
    writeFileSync(join(data, "www-undo", `${copy}.bin`), Buffer.from([0xff, 0xd8, 0x00, 0x01, 0x03]));
    await refused(call({ action: "restore_file", path: "/config/www/snapshots/door.jpg", copy }), 400, "COPY_INVALID");
    expect(existsSync(www("snapshots", "door.jpg"))).toBe(false);
    await refused(call({ action: "restore_file", path: "/config/www/snapshots/door.jpg", copy }, { now: T0 + 30 * DAY }), 404, "COPY_NOT_FOUND");
    expect(store()).toEqual([]);
  });

  it("never prunes a copy a running delete is still writing", async () => {
    const [deleted] = (await Promise.all([
      call({ action: "delete_file", path: "/config/www/snapshots/door.jpg" }),
      ...Array.from({ length: 5 }, () => call({ action: "list_deleted" }))
    ])) as [{ copy: string }];
    expect(store()).toEqual([`${deleted.copy}.bin`, `${deleted.copy}.json`]);
    expect(existsSync(www("snapshots", "door.jpg"))).toBe(false);
  });
});
