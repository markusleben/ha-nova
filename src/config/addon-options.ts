import { readFileSync } from "node:fs";

export interface AddonOptions {
  ha_llat?: string;
  [key: string]: unknown;
}

export function readAddonOptions(path: string): AddonOptions {
  try {
    const raw = readFileSync(path, "utf8");
    const parsed = JSON.parse(raw) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }

    return parsed as AddonOptions;
  } catch (error) {
    if (isMissingFile(error)) {
      return {};
    }

    const message = error instanceof Error ? error.message : "Unknown error";
    throw new Error(`Failed to read addon options from '${path}': ${message}`);
  }
}

function isMissingFile(error: unknown): boolean {
  if (!error || typeof error !== "object") {
    return false;
  }

  const code = (error as NodeJS.ErrnoException).code;
  return code === "ENOENT";
}
