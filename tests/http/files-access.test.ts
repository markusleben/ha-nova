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
    "/config/ssl/fullchain.pem",
    // Home Assistant EXECUTES what lives in these: allowing writes here would
    // turn "edit my YAML" into arbitrary code execution on the home server.
    "/config/custom_components/evil/__init__.py",
    "/config/python_scripts/evil.py",
    "/config/www/evil.js"
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

  it("refuses to write executable file types even outside the denied directories", async () => {
    for (const path of ["/config/evil.py", "/config/evil.sh", "/config/ha_nova/hook.js"]) {
      await expectHttpError(
        call("readwrite", { action: "write_file", path, content: "print('pwned')\n" }),
        403,
        "FILE_TYPE_DENIED"
      );
    }
  });

  // A caller that may not CREATE a .py has no business DELETING one: the
  // file-type boundary covers every mutation, not just writes.
  it("refuses to delete file types it would never write", async () => {
    writeFileSync(join(root, "keep.py"), "print('important')\n");

    await expectHttpError(
      call("readwrite", { action: "delete_file", path: "/config/keep.py" }),
      403,
      "FILE_TYPE_DENIED"
    );
    expect(readFileSync(join(root, "keep.py"), "utf8")).toContain("important");
  });

  it("still writes ordinary configuration formats", async () => {
    const result = (await call("readwrite", {
      action: "write_file",
      path: "/config/ha_nova/templates.yaml",
      content: "- sensor: []\n"
    })) as { written: boolean };
    expect(result.written).toBe(true);
  });

  it("never writes to a denied path either", async () => {
    await expectHttpError(
      call("readwrite", { action: "write_file", path: "/config/secrets.yaml", content: "x" }),
      403,
      "FILE_PATH_DENIED"
    );
    expect(readFileSync(join(root, "secrets.yaml"), "utf8")).toContain("super-secret");
  });
});
