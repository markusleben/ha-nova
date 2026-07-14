import { existsSync, mkdirSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { gzipSync } from "node:zlib";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  createBackupsHandler,
  MAX_SNAPSHOT_FILES
} from "../../nova/src/http/handlers/backups.js";
import { HttpError } from "../../nova/src/http/errors.js";

let root = "";
let clockMs = Date.UTC(2026, 6, 14, 12, 0, 0, 0);

async function call(body: unknown): Promise<unknown> {
  return await createBackupsHandler({ snapshotRoot: root, now: () => clockMs })({ body } as never);
}

async function expectHttpError(promise: Promise<unknown>, status: number, code: string): Promise<void> {
  await expect(promise).rejects.toSatisfy((error: unknown) => {
    if (!(error instanceof HttpError)) return false;
    expect(error.status).toBe(status);
    expect(error.code).toBe(code);
    return true;
  });
}

beforeEach(() => {
  root = mkdtempSync(join(tmpdir(), "nova-backups-root-"));
  clockMs = Date.UTC(2026, 6, 14, 12, 0, 0, 0);
});

afterEach(() => {
  rmSync(root, { recursive: true, force: true });
});

describe("backups handler", () => {
  it("saves and loads a snapshot roundtrip through gzip", async () => {
    const data = { alias: "Porch light", triggers: [{ platform: "sun", event: "sunset" }] };
    const saved = (await call({ action: "save", category: "automations", name: "porch", data })) as {
      file: string;
      bytes: number;
      created_at: string;
    };
    expect(saved.file).toBe("automations/porch-20260714T120000000Z.json.gz");
    expect(saved.created_at).toBe("2026-07-14T12:00:00.000Z");
    expect(saved.bytes).toBeGreaterThan(0);

    const loaded = (await call({ action: "load", file: saved.file })) as { data: unknown };
    expect(loaded.data).toEqual(data);
  });

  it("lists snapshots newest first, optionally by category", async () => {
    await call({ action: "save", category: "automations", name: "auto-one", data: 1 });
    clockMs += 1000;
    await call({ action: "save", category: "scenes", name: "movie", data: 2 });
    clockMs += 1000;
    await call({ action: "save", category: "automations", name: "auto-two", data: 3 });

    const all = (await call({ action: "list" })) as Array<{ file: string; category: string }>;
    expect(all.map((entry) => entry.category)).toEqual(["automations", "scenes", "automations"]);
    expect(all[0]?.file).toContain("auto-two");

    const scenes = (await call({ action: "list", category: "scenes" })) as Array<{ file: string }>;
    expect(scenes).toHaveLength(1);
    expect(scenes[0]?.file).toContain("movie");
  });

  it("returns an empty list when the store does not exist yet", async () => {
    rmSync(root, { recursive: true, force: true });
    expect(await call({ action: "list" })).toEqual([]);
  });

  it("deletes a snapshot and 404s on the second attempt", async () => {
    const saved = (await call({ action: "save", category: "scenes", name: "movie", data: {} })) as {
      file: string;
    };
    expect(await call({ action: "delete", file: saved.file })).toEqual({ deleted: true });
    await expectHttpError(call({ action: "delete", file: saved.file }), 404, "SNAPSHOT_NOT_FOUND");
    await expectHttpError(call({ action: "load", file: saved.file }), 404, "SNAPSHOT_NOT_FOUND");
  });

  it("rejects free-form file references before touching the filesystem", async () => {
    for (const file of [
      "../secrets.yaml",
      "automations/../../etc/passwd",
      "automations/porch.json.gz",
      "automations/porch-20260714T120000000Z.json",
      "AUTOMATIONS/porch-20260714T120000000Z.json.gz",
      "a/b/c-20260714T120000000Z.json.gz"
    ]) {
      await expectHttpError(call({ action: "load", file }), 400, "VALIDATION_ERROR");
    }
  });

  it("rejects invalid slugs, missing data, and unknown actions", async () => {
    await expectHttpError(call({ action: "save", category: "Bad Cat", name: "x", data: 1 }), 400, "VALIDATION_ERROR");
    await expectHttpError(call({ action: "save", category: "ok", name: "über", data: 1 }), 400, "VALIDATION_ERROR");
    await expectHttpError(call({ action: "save", category: "ok", name: "x" }), 400, "VALIDATION_ERROR");
    await expectHttpError(call({ action: "restore" }), 400, "VALIDATION_ERROR");
    await expectHttpError(call({ action: "prune", max_age_days: 10_000_000 }), 400, "VALIDATION_ERROR");
    await expectHttpError(call({ action: "prune", max_files: 100_000 }), 400, "VALIDATION_ERROR");
    await expectHttpError(call(null), 400, "VALIDATION_ERROR");
  });

  it("refuses a save when the millisecond stamp collides", async () => {
    await call({ action: "save", category: "scenes", name: "movie", data: 1 });
    await expectHttpError(
      call({ action: "save", category: "scenes", name: "movie", data: 2 }),
      409,
      "SNAPSHOT_EXISTS"
    );
  });

  it("fails loud with a prune hint when the file-count cap is reached", async () => {
    mkdirSync(join(root, "bulk"), { recursive: true });
    for (let i = 0; i < MAX_SNAPSHOT_FILES; i += 1) {
      writeFileSync(
        join(root, "bulk", `auto-item${i}-20260101T${String(i).padStart(9, "0")}Z.json.gz`),
        gzipSync("{}")
      );
    }
    await expectHttpError(
      call({ action: "save", category: "scenes", name: "movie", data: 1 }),
      400,
      "SNAPSHOT_STORE_FULL"
    );
  });

  it("prunes auto snapshots by age and count while named ones survive", async () => {
    // Named snapshot, 90 days old.
    clockMs = Date.UTC(2026, 3, 15);
    await call({ action: "save", category: "automations", name: "keeper", data: 1 });
    // Auto snapshot, 90 days old — prunable by age.
    clockMs += 1000;
    await call({ action: "save", category: "automations", name: "auto-old", data: 2 });
    // Fresh auto snapshots — 3 of them, count cap 2 removes the oldest.
    clockMs = Date.UTC(2026, 6, 14, 9, 0, 0, 0);
    await call({ action: "save", category: "automations", name: "auto-a", data: 3 });
    clockMs += 1000;
    await call({ action: "save", category: "automations", name: "auto-b", data: 4 });
    clockMs += 1000;
    await call({ action: "save", category: "automations", name: "auto-c", data: 5 });

    clockMs = Date.UTC(2026, 6, 14, 12, 0, 0, 0);
    const result = (await call({ action: "prune", max_age_days: 30, max_files: 2 })) as {
      deleted: string[];
    };
    expect(result.deleted).toHaveLength(2);
    expect(result.deleted.some((file) => file.includes("auto-old"))).toBe(true);
    expect(result.deleted.some((file) => file.includes("auto-a"))).toBe(true);

    const remaining = (await call({ action: "list" })) as Array<{ file: string }>;
    const names = remaining.map((entry) => entry.file);
    expect(names.some((file) => file.includes("keeper"))).toBe(true);
    expect(names.some((file) => file.includes("auto-b"))).toBe(true);
    expect(names.some((file) => file.includes("auto-c"))).toBe(true);
    expect(names).toHaveLength(3);
  });

  it("prunes everything prunable when keep_named is false", async () => {
    clockMs = Date.UTC(2026, 3, 15);
    await call({ action: "save", category: "automations", name: "keeper", data: 1 });
    clockMs = Date.UTC(2026, 6, 14, 12, 0, 0, 0);
    const result = (await call({ action: "prune", max_age_days: 30, max_files: 10, keep_named: false })) as {
      deleted: string[];
    };
    expect(result.deleted).toHaveLength(1);
    expect(existsSync(join(root, "automations"))).toBe(true);
    expect(await call({ action: "list" })).toEqual([]);
  });

  it("refuses to operate through a symlinked category directory", async () => {
    const outside = mkdtempSync(join(tmpdir(), "nova-backups-outside-"));
    try {
      symlinkSync(outside, join(root, "automations"));
      await expectHttpError(
        call({ action: "save", category: "automations", name: "escape", data: 1 }),
        400,
        "VALIDATION_ERROR"
      );
      writeFileSync(join(outside, "escape-20260714T120000000Z.json.gz"), gzipSync("{}"));
      await expectHttpError(
        call({ action: "load", file: "automations/escape-20260714T120000000Z.json.gz" }),
        400,
        "VALIDATION_ERROR"
      );
      await expectHttpError(
        call({ action: "delete", file: "automations/escape-20260714T120000000Z.json.gz" }),
        400,
        "VALIDATION_ERROR"
      );
      expect(existsSync(join(outside, "escape-20260714T120000000Z.json.gz"))).toBe(true);

      // Leaf symlink inside a REAL category dir must be refused too.
      mkdirSync(join(root, "scenes"), { recursive: true });
      writeFileSync(join(outside, "leaf.json.gz"), gzipSync("{}"));
      symlinkSync(join(outside, "leaf.json.gz"), join(root, "scenes", "leak-20260714T120000000Z.json.gz"));
      await expectHttpError(
        call({ action: "load", file: "scenes/leak-20260714T120000000Z.json.gz" }),
        400,
        "VALIDATION_ERROR"
      );
      await expectHttpError(
        call({ action: "delete", file: "scenes/leak-20260714T120000000Z.json.gz" }),
        400,
        "VALIDATION_ERROR"
      );
      expect(existsSync(join(outside, "leaf.json.gz"))).toBe(true);

      // ... and the leaf symlink must not surface in list results either.
      const listed = (await call({ action: "list", category: "scenes" })) as Array<{ file: string }>;
      expect(listed.some((entry) => entry.file.includes("leak"))).toBe(false);
    } finally {
      rmSync(outside, { recursive: true, force: true });
    }
  });

  it("never parses or mutates the stored payload", async () => {
    const opaque = { nested: { deeply: [1, "two", null, { ok: true }] } };
    const saved = (await call({ action: "save", category: "yaml", name: "opaque", data: opaque })) as {
      file: string;
    };
    const loaded = (await call({ action: "load", file: saved.file })) as { data: unknown };
    expect(loaded.data).toEqual(opaque);
  });
});
