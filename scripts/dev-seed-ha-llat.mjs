#!/usr/bin/env node

const HELP = `Usage:
  node scripts/dev-seed-ha-llat.mjs <LLAT> [--dry-run]

Environment:
  SUPERVISOR_TOKEN  Required. Token used for Supervisor API calls.
  SUPERVISOR_URL    Optional. Default: http://supervisor
  ADDON_SLUG        Optional. Default: self

Behavior:
  - Reads current addon options.
  - Merges and validates ha_llat.
  - Writes options only when value changed.
`;

const args = process.argv.slice(2);
const dryRun = args.includes("--dry-run");
const positional = args.filter((value) => !value.startsWith("--"));

if (args.includes("-h") || args.includes("--help")) {
  console.log(HELP);
  process.exit(0);
}

const seedToken = normalize(positional[0] ?? process.env.HA_LLAT);
if (!seedToken) {
  fail("Missing LLAT. Pass it as first argument or set HA_LLAT.");
}

const supervisorToken = normalize(process.env.SUPERVISOR_TOKEN);
if (!supervisorToken) {
  fail("SUPERVISOR_TOKEN is required.");
}

const supervisorUrl = normalize(process.env.SUPERVISOR_URL) ?? "http://supervisor";
const addonSlug = normalize(process.env.ADDON_SLUG) ?? "self";

const headers = {
  authorization: `Bearer ${supervisorToken}`,
  "content-type": "application/json"
};

const info = await requestJson(`/addons/${addonSlug}/info`, { method: "GET", headers });
const currentOptions = toOptionsObject(info.data?.options);

if (currentOptions.ha_llat === seedToken) {
  console.log(`No changes needed. ha_llat already set for '${addonSlug}'.`);
  process.exit(0);
}

const nextOptions = {
  ...currentOptions,
  ha_llat: seedToken
};

const validation = await requestJson(`/addons/${addonSlug}/options/validate`, {
  method: "POST",
  headers,
  body: JSON.stringify(nextOptions)
});

const valid = validation.data?.valid;
if (valid !== true) {
  const message = typeof validation.data?.message === "string"
    ? validation.data.message
    : "Options validation failed";
  fail(message);
}

if (dryRun) {
  console.log(`Dry run only. Would update ha_llat for '${addonSlug}'.`);
  process.exit(0);
}

await requestJson(`/addons/${addonSlug}/options`, {
  method: "POST",
  headers,
  body: JSON.stringify({ options: nextOptions })
});

console.log(`Updated ha_llat in options for '${addonSlug}'.`);

function normalize(value) {
  const trimmed = value?.trim();
  if (!trimmed) {
    return undefined;
  }
  return trimmed;
}

function toOptionsObject(value) {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value;
  }
  return {};
}

async function requestJson(path, init) {
  const response = await fetch(`${supervisorUrl}${path}`, init);
  const text = await response.text();
  const payload = text.length > 0 ? safeJsonParse(text) : null;

  if (!response.ok) {
    const details = payload && typeof payload === "object" ? JSON.stringify(payload) : text;
    fail(`Supervisor API call failed (${response.status}) on ${path}: ${details}`);
  }

  if (payload && typeof payload === "object") {
    return payload;
  }

  return { result: "ok", data: null };
}

function safeJsonParse(value) {
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}

function fail(message) {
  console.error(`[dev-seed-ha-llat] ${message}`);
  process.exit(1);
}
