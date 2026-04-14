#!/usr/bin/env node

import { appendFileSync, existsSync, mkdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { dirname, resolve } from "node:path";

const PLUGIN_ID = "ha-nova@ha-nova";
const MARKETPLACE_ID = "ha-nova";

function fail(message) {
  console.error(message);
  process.exit(1);
}

function parseJsonText(text) {
  const normalized = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text;
  return JSON.parse(normalized);
}

function readJson(path) {
  try {
    return parseJsonText(readFileSync(path, "utf8"));
  } catch (error) {
    fail(error instanceof Error ? error.message : String(error));
  }
}

function writeJson(path, value) {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
}

function fileSummary(path) {
  const resolved = resolve(path);
  if (!existsSync(resolved)) {
    return {
      path: resolved,
      exists: false,
      parseable: false,
      size: 0,
      mtimeMs: 0,
      sha256: "",
      error: "missing",
    };
  }

  const raw = readFileSync(resolved);
  const stat = statSync(resolved);
  const summary = {
    path: resolved,
    exists: true,
    parseable: false,
    size: stat.size,
    mtimeMs: stat.mtimeMs,
    sha256: createHash("sha256").update(raw).digest("hex"),
    error: "",
  };

  try {
    parseJsonText(raw.toString("utf8"));
    summary.parseable = true;
    return summary;
  } catch (error) {
    summary.error = error instanceof Error ? error.message : String(error);
    return summary;
  }
}

function hasPluginIdentity(value) {
  return [value.name, value.id, value.plugin].some((entry) => entry === PLUGIN_ID);
}

function normalizePluginEntries(value, options = {}) {
  const { keyedPluginRecord = false } = options;
  if (Array.isArray(value)) {
    return value.flatMap((entry) => normalizePluginEntries(entry, options));
  }
  if (value === PLUGIN_ID) {
    return [{ kind: "string", raw: value }];
  }
  if (!value || typeof value !== "object") {
    return [];
  }
  if (hasPluginIdentity(value) || (keyedPluginRecord && (typeof value.installPath === "string" || typeof value.version === "string"))) {
    return [{ kind: "record", raw: value }];
  }
  return [];
}

function findPluginEntries(raw) {
  if (!raw || typeof raw !== "object") {
    return [];
  }

  const directEntries = Object.prototype.hasOwnProperty.call(raw, PLUGIN_ID)
    ? normalizePluginEntries(raw[PLUGIN_ID], { keyedPluginRecord: true })
    : [];
  if (directEntries.length > 0) {
    return directEntries;
  }

  const plugins = raw.plugins;
  if (plugins && typeof plugins === "object" && !Array.isArray(plugins)) {
    if (Object.prototype.hasOwnProperty.call(plugins, PLUGIN_ID)) {
      return normalizePluginEntries(plugins[PLUGIN_ID], { keyedPluginRecord: true });
    }
  }

  if (Array.isArray(plugins)) {
    return plugins.flatMap((entry) => normalizePluginEntries(entry));
  }

  return [];
}

function findPluginEntry(raw) {
  if (!raw || typeof raw !== "object") {
    return null;
  }

  if (Object.prototype.hasOwnProperty.call(raw, PLUGIN_ID)) {
    const direct = raw[PLUGIN_ID];
    if (Array.isArray(direct)) {
      const index = direct.findIndex((entry) => normalizePluginEntries(entry, { keyedPluginRecord: true }).length > 0);
      if (index >= 0) {
        return { kind: "top-level-record-array", index, parent: direct, record: direct[index] };
      }
    }
    if (direct && typeof direct === "object" && !Array.isArray(direct)) {
      return { kind: "top-level-record", record: direct };
    }
  }

  const plugins = raw.plugins;
  if (plugins && typeof plugins === "object" && !Array.isArray(plugins)) {
    const record = plugins[PLUGIN_ID];
    if (Array.isArray(record)) {
      const index = record.findIndex((entry) => normalizePluginEntries(entry, { keyedPluginRecord: true }).length > 0);
      if (index >= 0) {
        return { kind: "plugins-map-record-array", index, parent: record, record: record[index] };
      }
    }
    if (record && typeof record === "object" && !Array.isArray(record)) {
      return { kind: "plugins-map-record", record };
    }
  }

  if (Array.isArray(plugins)) {
    for (let index = 0; index < plugins.length; index += 1) {
      const entry = plugins[index];
      if (entry === PLUGIN_ID) {
        return { kind: "plugins-list-string", index, record: null };
      }
      if (entry && typeof entry === "object" && hasPluginIdentity(entry)) {
        return { kind: "plugins-list-record", index, record: entry };
      }
    }
  }

  return null;
}

function readPluginSnapshot(path) {
  const summary = fileSummary(path);
  if (!summary.exists || !summary.parseable) {
    return {
      recordPresent: false,
      usableInstallPath: false,
      installPath: "",
      version: "",
      entries: 0,
    };
  }

  const raw = readJson(path);
  const entries = findPluginEntries(raw);
  const installPath = entries
    .map((entry) => entry.raw?.installPath)
    .find((value) => typeof value === "string" && value.trim()) ?? "";
  const version = entries
    .map((entry) => entry.raw?.version)
    .find((value) => typeof value === "string" && value.trim()) ?? "";

  return {
    recordPresent: entries.length > 0,
    usableInstallPath: installPath !== "",
    installPath,
    version,
    entries: entries.length,
  };
}

function inspectInstalledPlugin(path) {
  const snapshot = readPluginSnapshot(path);

  console.log(`installed=${snapshot.recordPresent ? "1" : "0"}`);
  console.log(`install_path=${snapshot.installPath}`);
  console.log(`version=${snapshot.version}`);
}

function removePluginRecord(path) {
  const raw = readJson(path);
  let changed = false;

  if (Object.prototype.hasOwnProperty.call(raw, PLUGIN_ID)) {
    delete raw[PLUGIN_ID];
    changed = true;
  }

  const plugins = raw.plugins;
  if (plugins && typeof plugins === "object" && !Array.isArray(plugins)) {
    if (Object.prototype.hasOwnProperty.call(plugins, PLUGIN_ID)) {
      delete plugins[PLUGIN_ID];
      changed = true;
    }
  } else if (Array.isArray(plugins)) {
    const filtered = plugins.filter((entry) => {
      if (entry === PLUGIN_ID) {
        changed = true;
        return false;
      }
      if (
        entry &&
        typeof entry === "object" &&
        [entry.name, entry.id, entry.plugin].includes(PLUGIN_ID)
      ) {
        changed = true;
        return false;
      }
      return true;
    });
    if (changed) {
      raw.plugins = filtered;
    }
  }

  if (changed) {
    writeJson(path, raw);
  }
}

function repairPluginRecord(path, installPath, version) {
  const raw = readJson(path);
  const entry = findPluginEntry(raw);
  if (!entry) {
    return;
  }

  if (entry.kind === "plugins-list-string") {
    if (!Array.isArray(raw.plugins)) {
      return;
    }

    raw.plugins[entry.index] = {
      name: PLUGIN_ID,
      installPath,
      ...(version ? { version } : {}),
    };
    writeJson(path, raw);
    return;
  }

  if (!entry.record || typeof entry.record !== "object" || Array.isArray(entry.record)) {
    return;
  }

  let changed = false;
  if (entry.record.installPath !== installPath) {
    entry.record.installPath = installPath;
    changed = true;
  }
  if (version && entry.record.version !== version) {
    entry.record.version = version;
    changed = true;
  }

  if (changed) {
    writeJson(path, raw);
  }
}

function findMarketplaceEntry(raw) {
  if (!raw || typeof raw !== "object") {
    return null;
  }

  if (Object.prototype.hasOwnProperty.call(raw, MARKETPLACE_ID)) {
    const entry = raw[MARKETPLACE_ID];
    return entry && typeof entry === "object" ? entry : null;
  }

  const marketplaces = raw.marketplaces;
  if (marketplaces && typeof marketplaces === "object" && !Array.isArray(marketplaces)) {
    if (Object.prototype.hasOwnProperty.call(marketplaces, MARKETPLACE_ID)) {
      const entry = marketplaces[MARKETPLACE_ID];
      return entry && typeof entry === "object" ? entry : null;
    }
  }

  const values = Array.isArray(raw)
    ? raw
    : Array.isArray(marketplaces)
      ? marketplaces
      : Object.values(raw);

  for (const entry of values) {
    if (entry && typeof entry === "object" && entry.name === MARKETPLACE_ID) {
      return entry;
    }
  }
  return null;
}

function readMarketplaceSource(path) {
  const raw = readJson(path);
  const entry = findMarketplaceEntry(raw);
  if (!entry) {
    return "";
  }
  let source = entry.source;
  if (source && typeof source === "object") {
    source = source.url ?? source.path ?? source.repo ?? "";
  }
  return typeof source === "string" ? source.trim() : "";
}

function readMarketplaceSnapshot(path) {
  const summary = fileSummary(path);
  if (!summary.exists || !summary.parseable) {
    return {
      present: false,
      source: "",
    };
  }

  const source = readMarketplaceSource(path);
  return {
    present: source !== "",
    source,
  };
}

function readKnownMarketplaceSource(path) {
  const source = readMarketplaceSource(path);
  if (source) {
    console.log(source);
  }
}

function readSettingsAttachment(path) {
  const summary = fileSummary(path);
  if (!summary.exists || !summary.parseable) {
    return {
      pluginEnabled: false,
      marketplacePresent: false,
      marketplacePath: "",
    };
  }

  const raw = readJson(path);
  const enabledPlugins = raw.enabledPlugins;
  const extraKnownMarketplaces = raw.extraKnownMarketplaces;
  const pluginEnabled = Boolean(
    enabledPlugins &&
      typeof enabledPlugins === "object" &&
      !Array.isArray(enabledPlugins) &&
      enabledPlugins[PLUGIN_ID] === true,
  );
  const marketplaceEntry =
    extraKnownMarketplaces &&
    typeof extraKnownMarketplaces === "object" &&
    !Array.isArray(extraKnownMarketplaces)
      ? extraKnownMarketplaces[MARKETPLACE_ID]
      : undefined;

  let marketplacePath = "";
  if (typeof marketplaceEntry === "string") {
    marketplacePath = marketplaceEntry.trim();
  } else if (marketplaceEntry && typeof marketplaceEntry === "object") {
    marketplacePath = String(
      marketplaceEntry.path ??
        marketplaceEntry.url ??
        marketplaceEntry.repo ??
        "",
    ).trim();
  }

  return {
    pluginEnabled,
    marketplacePresent: marketplacePath !== "",
    marketplacePath,
  };
}

function buildSnapshot(home) {
  const resolvedHome = resolve(home);
  const installedPath = `${resolvedHome}/.claude/plugins/installed_plugins.json`;
  const knownPath = `${resolvedHome}/.claude/plugins/known_marketplaces.json`;
  const settingsPath = `${resolvedHome}/.claude/settings.json`;
  const settingsLocalPath = `${resolvedHome}/.claude/settings.local.json`;

  const plugin = readPluginSnapshot(installedPath);
  const marketplace = readMarketplaceSnapshot(knownPath);
  const settings = readSettingsAttachment(settingsPath);
  const settingsLocal = readSettingsAttachment(settingsLocalPath);

  return {
    capturedAt: new Date().toISOString(),
    home: resolvedHome,
    attached: plugin.usableInstallPath && marketplace.source !== "",
    plugin: {
      recordPresent: plugin.recordPresent,
      usableInstallPath: plugin.usableInstallPath,
      installPath: plugin.installPath,
      version: plugin.version,
      entries: plugin.entries,
    },
    marketplace,
    settings: {
      pluginEnabled: settings.pluginEnabled,
      marketplacePresent: settings.marketplacePresent,
      marketplacePath: settings.marketplacePath,
    },
    settingsLocal: {
      pluginEnabled: settingsLocal.pluginEnabled,
      marketplacePresent: settingsLocal.marketplacePresent,
      marketplacePath: settingsLocal.marketplacePath,
    },
    files: {
      installedPlugins: fileSummary(installedPath),
      knownMarketplaces: fileSummary(knownPath),
      settings: fileSummary(settingsPath),
      settingsLocal: fileSummary(settingsLocalPath),
    },
  };
}

function snapshotHome(home) {
  console.log(JSON.stringify(buildSnapshot(home), null, 2));
}

function readSnapshotFile(path) {
  const resolved = resolve(path);
  if (!existsSync(resolved)) {
    return null;
  }

  try {
    return parseJsonText(readFileSync(resolved, "utf8"));
  } catch {
    return null;
  }
}

function buildChangeSet(previous, next) {
  if (!previous) {
    return {
      initialized: {
        from: null,
        to: true,
      },
    };
  }

  const fields = [
    "attached",
    "plugin.recordPresent",
    "plugin.usableInstallPath",
    "plugin.installPath",
    "plugin.version",
    "marketplace.present",
    "marketplace.source",
    "settings.pluginEnabled",
    "settings.marketplacePresent",
    "settings.marketplacePath",
    "settingsLocal.pluginEnabled",
    "settingsLocal.marketplacePresent",
    "settingsLocal.marketplacePath",
    "files.installedPlugins.parseable",
    "files.knownMarketplaces.parseable",
    "files.settings.parseable",
    "files.settingsLocal.parseable",
  ];
  const changes = {};

  for (const field of fields) {
    const previousValue = field.split(".").reduce((value, key) => value?.[key], previous);
    const nextValue = field.split(".").reduce((value, key) => value?.[key], next);
    if (JSON.stringify(previousValue) === JSON.stringify(nextValue)) {
      continue;
    }
    changes[field] = {
      from: previousValue ?? null,
      to: nextValue ?? null,
    };
  }

  return changes;
}

function writeWatchEvent(home, changedPath, eventLogPath, latestPath) {
  const previous = readSnapshotFile(latestPath);
  const snapshot = buildSnapshot(home);
  const event = {
    capturedAt: snapshot.capturedAt,
    triggerPath: changedPath === "__startup__" ? "__startup__" : resolve(changedPath),
    context: process.env.HA_NOVA_CLAUDE_AUDIT_CONTEXT?.trim() || "",
    changes: buildChangeSet(previous, snapshot),
    snapshot,
  };
  mkdirSync(dirname(resolve(eventLogPath)), { recursive: true });
  mkdirSync(dirname(resolve(latestPath)), { recursive: true });
  appendFileSync(resolve(eventLogPath), `${JSON.stringify(event)}\n`);
  writeFileSync(resolve(latestPath), `${JSON.stringify(snapshot, null, 2)}\n`);
}

const [command, ...args] = process.argv.slice(2);

switch (command) {
  case "inspect-installed-plugin":
    if (args.length !== 1) {
      fail("usage: inspect-installed-plugin <installed_plugins.json>");
    }
    inspectInstalledPlugin(args[0]);
    break;
  case "remove-plugin-record":
    if (args.length !== 1) {
      fail("usage: remove-plugin-record <installed_plugins.json>");
    }
    removePluginRecord(args[0]);
    break;
  case "repair-plugin-record":
    if (args.length !== 3) {
      fail("usage: repair-plugin-record <installed_plugins.json> <install_path> <version>");
    }
    repairPluginRecord(args[0], args[1], args[2]);
    break;
  case "read-known-marketplace-source":
    if (args.length !== 1) {
      fail("usage: read-known-marketplace-source <known_marketplaces.json>");
    }
    readKnownMarketplaceSource(args[0]);
    break;
  case "snapshot-home":
    if (args.length !== 1) {
      fail("usage: snapshot-home <home>");
    }
    snapshotHome(args[0]);
    break;
  case "write-watch-event":
    if (args.length !== 4) {
      fail("usage: write-watch-event <home> <changed_path> <event_log_path> <latest_path>");
    }
    writeWatchEvent(args[0], args[1], args[2], args[3]);
    break;
  default:
    fail("unknown command");
}
