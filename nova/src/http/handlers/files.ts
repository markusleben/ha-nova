import type { FileAccessConfig } from "../../config/file-access.js";
import { HttpError } from "../errors.js";
import type { RouteContext, RouteHandler } from "../router.js";
import { deleteFile, listDir, MAX_WRITE_BYTES, readTextFile, writeTextFile } from "./files-ops.js";
import { resolveTargetPath, type FileAction } from "./files-paths.js";

export interface FilesHandlerOptions {
  fileAccess: FileAccessConfig;
}

const WRITE_ACTIONS = new Set<FileAction>(["write_file", "delete_file"]);

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

    const target = await resolveTargetPath(configRoot, request.path, request.action);

    switch (request.action) {
      case "list_dir":
        return await listDir(target, request.path);
      case "read_file":
        return await readTextFile(target, request.path);
      case "write_file":
        return await writeTextFile(target, request.path, request.content, request.backup, configRoot);
      case "delete_file":
        return await deleteFile(target, request.path);
    }
  };
}

interface FilesRequest {
  action: FileAction;
  path: string;
  content: string;
  backup: boolean;
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
    action !== "delete_file"
  ) {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "action must be one of: list_dir, read_file, write_file, delete_file"
    );
  }

  const path = raw.path;
  if (typeof path !== "string" || path.trim().length === 0 || path.length > 2048) {
    throw new HttpError(400, "VALIDATION_ERROR", "path must be a non-empty string");
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

  return { action, path, content, backup };
}
