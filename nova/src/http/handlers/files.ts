import { join } from "node:path";

import type { FileAccessConfig } from "../../config/file-access.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import { deleteFile, listDir, MAX_WRITE_BYTES, readTextFile, writeTextFile } from "./files-ops.js";
import { assertOutsideData, assertWritableExtension, resolveTargetPath, type FileAction } from "./files-paths.js";
import { COPY_ID, deleteMedia, listDeleted, restoreMedia } from "./files-www.js";

export interface FilesHandlerOptions {
  fileAccess: FileAccessConfig;
  now?: () => number;
}

const WRITE_ACTIONS = new Set<FileAction>(["write_file", "delete_file", "restore_file"]);

/**
 * Generic file transport for Home Assistant's configuration directory — the one
 * capability the relay does NOT ship on by default. Path containment and the
 * deny rules live in files-paths.ts; the filesystem work lives in files-ops.ts.
 * This module only gates access, validates the request, and shapes the response.
 */
export function createFilesHandler(options: FilesHandlerOptions): RouteHandler {
  return async ({ body }: RouteContext) => {
    const { mode, configRoot } = options.fileAccess;
    if (mode === "off") {
      throw new HttpError(
        403,
        "FILE_ACCESS_DISABLED",
        "File access is disabled. Set the 'file_access' option (read or readwrite) on the NOVA Relay App and restart it."
      );
    }

    const request = parseFilesRequest(body);
    if (WRITE_ACTIONS.has(request.action) && mode !== "readwrite") {
      throw new HttpError(
        403,
        "FILE_ACCESS_READONLY",
        `File access is set to 'read'. Set 'file_access' to 'readwrite' to allow ${request.action}.`
      );
    }

    if (request.action === "write_file") {
      assertWritableExtension(request.path);
    }

    const now = options.now ?? Date.now;
    // The copies of deleted www media live in the Relay's own data folder;
    // without one nothing in www is deleted or put back.
    const store = (): string => {
      const { dataDir } = options.fileAccess;
      if (dataDir === undefined) {
        throw new HttpError(500, "UNDO_STORE_UNAVAILABLE", "This Relay has no data folder to keep copies in, so it deletes nothing in www; nothing was changed");
      }
      return join(dataDir, "www-undo");
    };
    if (request.action === "list_deleted") {
      return await listDeleted(store(), now);
    }

    const target = await resolveTargetPath(configRoot, request.path, request.action);
    await assertOutsideData(target.absolute, options.fileAccess.dataDir);

    switch (request.action) {
      case "list_dir":
        return await listDir(target.absolute, request.path);
      case "read_file":
        return await readTextFile(target.absolute, request.path);
      case "write_file":
        return await writeTextFile(target.absolute, request.path, request.content, request.backup, configRoot);
      case "delete_file":
        return target.www ? await deleteMedia(target, store(), now) : await deleteFile(target.absolute, request.path);
      case "restore_file":
        if (!target.www) {
          throw new HttpError(403, "FILE_PATH_DENIED", "the relay puts back only images and videos it deleted from www");
        }
        return await restoreMedia(target, request.copy, store(), now);
    }
  };
}

interface FilesRequest {
  action: FileAction;
  path: string;
  content: string;
  backup: boolean;
  copy: string;
}

function parseFilesRequest(body: unknown): FilesRequest {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw new HttpError(400, "VALIDATION_ERROR", "Request body must be an object");
  }
  const raw = body as Record<string, unknown>;

  const action = raw.action;
  if (
    action !== "list_dir" &&
    action !== "read_file" &&
    action !== "write_file" &&
    action !== "delete_file" &&
    action !== "restore_file" &&
    action !== "list_deleted"
  ) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "action must be one of: list_dir, read_file, write_file, delete_file, restore_file, list_deleted"
    );
  }
  if (action === "list_deleted") {
    return { action, path: "", content: "", backup: true, copy: "" };
  }

  const path = raw.path;
  if (typeof path !== "string" || path.trim().length === 0 || path.length > 2048) {
    throw new HttpError(400, "VALIDATION_ERROR", "path must be a non-empty string");
  }

  let copy = "";
  if (action === "restore_file") {
    if (typeof raw.copy !== "string" || !COPY_ID.test(raw.copy)) {
      throw new HttpError(400, "VALIDATION_ERROR", "restore_file requires 'copy': the id delete_file or list_deleted answered");
    }
    copy = raw.copy;
  }

  let content = "";
  if (action === "write_file") {
    if (typeof raw.content !== "string") {
      throw new HttpError(400, "VALIDATION_ERROR", "write_file requires a string 'content'");
    }
    content = raw.content;
    // read_file refuses NUL bytes as non-text; write_file must not be able to
    // create a file that this same endpoint would then refuse to read (and that
    // Home Assistant would choke on). The text-only contract runs both ways.
    if (content.includes("\u0000")) {
      throw new HttpError(
        400,
        "VALIDATION_ERROR",
        "content contains NUL bytes — /files is a text-only endpoint"
      );
    }
    if (Buffer.byteLength(content, "utf8") > MAX_WRITE_BYTES) {
      throw new HttpError(
        400,
        "FILE_TOO_LARGE",
        `content exceeds the ${MAX_WRITE_BYTES}-byte write limit`
      );
    }
  }

  // A non-boolean 'backup' (e.g. the string "true", or 1) must NOT silently
  // disable the rollback copy — backups are the safety net for every overwrite.
  if (raw.backup !== undefined && typeof raw.backup !== "boolean") {
    throw new HttpError(400, "VALIDATION_ERROR", "backup must be a boolean");
  }
  const backup = raw.backup ?? true;

  return { action, path, content, backup, copy };
}
