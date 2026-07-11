export type FileAccessMode = "off" | "read" | "readwrite";

export interface FileAccessConfig {
  mode: FileAccessMode;
  /** Absolute path the container sees for Home Assistant's config directory. */
  configRoot: string;
  /** Why the effective mode is lower than the one that was asked for. */
  warnings: string[];
}

export interface RootProbe {
  isDirectory: boolean;
  readable: boolean;
  writable: boolean;
}

const ALLOWED_MODES = new Set<FileAccessMode>(["off", "read", "readwrite"]);

// The App pins its mount at /config (nova/config.yaml), but the relay also runs
// as a standalone container where the user chooses the mount, and Supervisor
// defaults have moved over time. So the relay probes the known roots rather
// than assuming one — and CONFIG_ROOT overrides all of it.
const CANDIDATE_ROOTS = ["/config", "/homeassistant", "/homeassistant_config"];

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
  probeRoot: (path: string) => RootProbe
): FileAccessConfig {
  // The two values are passed explicitly rather than handing this function the
  // whole environment: a spread of process.env makes every variable a taint
  // source, and CodeQL is right that config values must not flow into logs.
  const requested = parseFileAccessMode(input.mode);
  const warnings: string[] = [];

  const explicitRoot = input.configRootOverride?.trim();
  const candidates = explicitRoot ? [explicitRoot] : CANDIDATE_ROOTS;

  for (const candidate of candidates) {
    const probe = probeRoot(candidate);
    if (!probe.isDirectory) {
      continue;
    }
    return {
      mode: effectiveMode(requested, probe, candidate, warnings),
      configRoot: candidate,
      warnings
    };
  }

  // No mount: the endpoint stays disabled regardless of the requested mode —
  // reporting "readwrite" while nothing is mounted would be a lie.
  if (requested !== "off") {
    warnings.push(
      "file_access is set, but no Home Assistant configuration directory is mounted — file access stays off."
    );
  }
  return { mode: "off", configRoot: "", warnings };
}

/**
 * Degrades the requested mode to what the process can ACTUALLY do. A bind mount
 * can easily be read-only or owned by another user; reporting "readwrite" and
 * then failing on the first write with EACCES would be a lie that surfaces at
 * the worst possible moment.
 */
function effectiveMode(
  requested: FileAccessMode,
  probe: RootProbe,
  root: string,
  warnings: string[]
): FileAccessMode {
  if (requested === "off") {
    return "off";
  }
  if (!probe.readable) {
    warnings.push(`file_access is set, but ${root} is not readable by the relay — file access stays off.`);
    return "off";
  }
  if (requested === "readwrite" && !probe.writable) {
    warnings.push(`file_access is 'readwrite', but ${root} is not writable by the relay — falling back to read-only.`);
    return "read";
  }
  return requested;
}
