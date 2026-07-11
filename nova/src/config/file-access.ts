export type FileAccessMode = "off" | "read" | "readwrite";

export interface FileAccessConfig {
  mode: FileAccessMode;
  /** Absolute path the container sees for Home Assistant's config directory. */
  configRoot: string;
}

const ALLOWED_MODES = new Set<FileAccessMode>(["off", "read", "readwrite"]);

// Home Assistant OS mounts the config directory at /homeassistant for newer
// add-ons and at /config for older ones. The relay resolves whichever exists so
// skills can always speak the logical "/config/..." prefix.
const CANDIDATE_ROOTS = ["/homeassistant", "/config"];

export function parseFileAccessMode(raw: unknown): FileAccessMode {
  if (raw === undefined || raw === null || raw === "") {
    return "off";
  }
  const value = String(raw).trim().toLowerCase();
  if (!ALLOWED_MODES.has(value as FileAccessMode)) {
    // The rejected value is deliberately NOT echoed: this message travels into
    // the startup log, and a config value must never be reflected there.
    throw new Error("file_access must be one of: off, read, readwrite");
  }
  return value as FileAccessMode;
}

/**
 * Resolves the file-access configuration. Default is "off": the endpoint exists
 * but refuses everything until the user opts in through the App option (or
 * FILE_ACCESS for the standalone container). File access is the single largest
 * capability the relay can hand an agent, so it is never on by accident.
 */
export function resolveFileAccess(
  input: { mode?: unknown; configRootOverride?: string | undefined },
  directoryExists: (path: string) => boolean
): FileAccessConfig {
  // The two values are passed explicitly rather than handing this function the
  // whole environment: a spread of process.env makes every variable a taint
  // source, and CodeQL is right that config values must not flow into logs.
  const mode = parseFileAccessMode(input.mode);

  const explicitRoot = input.configRootOverride?.trim();
  if (explicitRoot) {
    return { mode, configRoot: explicitRoot };
  }

  for (const candidate of CANDIDATE_ROOTS) {
    if (directoryExists(candidate)) {
      return { mode, configRoot: candidate };
    }
  }

  // No mount: the endpoint stays disabled regardless of the requested mode —
  // reporting "readwrite" while nothing is mounted would be a lie.
  return { mode: "off", configRoot: "" };
}
