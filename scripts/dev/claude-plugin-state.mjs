#!/usr/bin/env node

import { readFileSync, writeFileSync } from "node:fs";

const PLUGIN_ID = "ha-nova@ha-nova";

function fail(message) {
  console.error(message);
  process.exit(1);
}

function readJson(path) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (error) {
    fail(error instanceof Error ? error.message : String(error));
  }
}

function writeJson(path, value) {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
}

function findPluginEntry(raw) {
  if (!raw || typeof raw !== "object") {
    return null;
  }

  if (
    Object.prototype.hasOwnProperty.call(raw, PLUGIN_ID) &&
    raw[PLUGIN_ID] &&
    typeof raw[PLUGIN_ID] === "object" &&
    !Array.isArray(raw[PLUGIN_ID])
  ) {
    return { kind: "top-level-record", record: raw[PLUGIN_ID] };
  }

  const plugins = raw.plugins;
  if (plugins && typeof plugins === "object" && !Array.isArray(plugins)) {
    const record = plugins[PLUGIN_ID];
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
      if (
        entry &&
        typeof entry === "object" &&
        [entry.name, entry.id, entry.plugin].includes(PLUGIN_ID)
      ) {
        return { kind: "plugins-list-record", index, record: entry };
      }
    }
  }

  return null;
}

function inspectInstalledPlugin(path) {
  const raw = readJson(path);
  const entry = findPluginEntry(raw);
  const installPath = entry?.record && typeof entry.record.installPath === "string"
    ? entry.record.installPath
    : "";
  const version = entry?.record && typeof entry.record.version === "string"
    ? entry.record.version
    : "";

  console.log(`installed=${entry ? "1" : "0"}`);
  console.log(`install_path=${installPath}`);
  console.log(`version=${version}`);
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

  if (!entry.record) {
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

function* iterateMarketplaceEntries(value) {
  if (Array.isArray(value)) {
    yield* value;
    return;
  }

  if (!value || typeof value !== "object") {
    return;
  }

  if (Object.prototype.hasOwnProperty.call(value, "ha-nova")) {
    yield value["ha-nova"];
    return;
  }

  yield* Object.values(value);
}

function readKnownMarketplaceSource(path) {
  const raw = readJson(path);

  for (const entry of iterateMarketplaceEntries(raw)) {
    if (!entry || typeof entry !== "object") {
      continue;
    }
    const name = entry.name;
    if (name && name !== "ha-nova") {
      continue;
    }

    let source = entry.source;
    if (source && typeof source === "object") {
      source = source.url ?? source.path;
    }

    if (typeof source === "string" && source.trim()) {
      console.log(source.trim());
      return;
    }
  }
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
  default:
    fail("unknown command");
}
