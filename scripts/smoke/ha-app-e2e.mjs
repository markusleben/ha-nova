#!/usr/bin/env node
import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const HELP = `Usage:
  node scripts/smoke/ha-app-e2e.mjs [--apply]

Environment:
  SUPERVISOR_TOKEN   Optional. Required for Supervisor API preflight/apply.
  SUPERVISOR_URL     Optional. Default: http://supervisor
  APP_SLUG           Optional. Default: ha_nova_relay
  RELAY_BASE_URL     Required. Base URL for nova relay (example: http://homeassistant.local:8791)
  RELAY_AUTH_TOKEN   Required. Relay auth bearer token.
  HA_LLAT            Required. LLAT to validate/apply in app options.
  WS_TYPE            Optional. Default: ping
  Also loaded (if present): .env.local, .env

Behavior:
  - If SUPERVISOR_TOKEN is set: reads/validates app options via Supervisor API.
  - With --apply: writes options + restarts app (requires SUPERVISOR_TOKEN).
  - Verifies /health and /ws against current runtime.
`;

const args = process.argv.slice(2);
const apply = args.includes("--apply");
loadLocalEnv();

if (args.includes("-h") || args.includes("--help")) {
  console.log(HELP);
  process.exit(0);
}

const supervisorToken = readOptional("SUPERVISOR_TOKEN");
const relayBaseUrl = stripTrailingSlash(readRequired("RELAY_BASE_URL"));
const relayAuthToken = readRequired("RELAY_AUTH_TOKEN");
const requestedLlat = readRequired("HA_LLAT");

const supervisorUrl = stripTrailingSlash(readOptional("SUPERVISOR_URL") ?? "http://supervisor");
const appSlug = readOptional("APP_SLUG") ?? "ha_nova_relay";
const wsType = readOptional("WS_TYPE") ?? "ping";

if (apply && !supervisorToken) {
  fail("--apply requires SUPERVISOR_TOKEN");
}

let runtimeHasLlat = null;
let supervisorPreflight = false;

if (supervisorToken) {
  const supervisorHeaders = {
    authorization: `Bearer ${supervisorToken}`,
    "content-type": "application/json"
  };
  supervisorPreflight = true;

  const appInfo = await requestJson(`${supervisorUrl}/addons/${appSlug}/info`, {
    method: "GET",
    headers: supervisorHeaders
  });

  const currentOptions = toObject(appInfo.data?.options);
  const nextOptions = {
    ...currentOptions,
    relay_auth_token: relayAuthToken,
    ha_llat: requestedLlat
  };

  await requestJson(`${supervisorUrl}/addons/${appSlug}/options/validate`, {
    method: "POST",
    headers: supervisorHeaders,
    body: JSON.stringify(nextOptions)
  });

  if (apply) {
    await requestJson(`${supervisorUrl}/addons/${appSlug}/options`, {
      method: "POST",
      headers: supervisorHeaders,
      body: JSON.stringify({ options: nextOptions })
    });

    await requestJson(`${supervisorUrl}/addons/${appSlug}/restart`, {
      method: "POST",
      headers: supervisorHeaders
    });
  }

  runtimeHasLlat = apply
    ? Boolean(readOptionalFromUnknown(nextOptions.ha_llat))
    : Boolean(readOptionalFromUnknown(currentOptions.ha_llat));
}

const health = await waitForHealth({
  baseUrl: relayBaseUrl,
  authToken: relayAuthToken,
  maxAttempts: apply ? 30 : 5,
  delayMs: 1000
});

const ws = await callWs({
  baseUrl: relayBaseUrl,
  authToken: relayAuthToken,
  wsType
});

assertWsExpectation({ ws, expectFullScope: runtimeHasLlat });

console.log(
  JSON.stringify(
    {
      ok: true,
      app_slug: appSlug,
      apply,
      supervisor_preflight: supervisorPreflight,
      runtime_has_llat: runtimeHasLlat,
      health_status: health.status,
      ws_status: ws.status
    },
    null,
    2
  )
);

async function waitForHealth(input) {
  const { baseUrl, authToken, maxAttempts, delayMs } = input;

  let lastError = null;
  for (let attempt = 1; attempt <= maxAttempts; attempt += 1) {
    try {
      const response = await fetch(`${baseUrl}/health`, {
        method: "GET",
        headers: {
          authorization: `Bearer ${authToken}`
        },
        signal: AbortSignal.timeout(8_000)
      });

      if (response.ok) {
        const body = await parseJson(response);
        return { status: response.status, body };
      }

      lastError = new Error(`health status ${response.status}`);
    } catch (error) {
      lastError = error;
    }

    if (attempt < maxAttempts) {
      await sleep(delayMs);
    }
  }

  const message = lastError instanceof Error ? lastError.message : "unknown health error";
  fail(`Health check failed after ${maxAttempts} attempts: ${message}`);
}

async function callWs(input) {
  const { baseUrl, authToken, wsType } = input;

  const response = await fetch(`${baseUrl}/ws`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${authToken}`,
      "content-type": "application/json"
    },
    body: JSON.stringify({ type: wsType }),
    signal: AbortSignal.timeout(8_000)
  });

  const body = await parseJson(response);
  return {
    status: response.status,
    body
  };
}

function assertWsExpectation(input) {
  const { ws, expectFullScope } = input;

  if (expectFullScope === false) {
    fail("Supervisor preflight reports missing LLAT in app options. Apply HA_LLAT and restart the app.");
  }

  if (ws.status !== 200) {
    fail(`Expected /ws status 200 with mandatory LLAT, got ${ws.status}: ${JSON.stringify(ws.body)}`);
  }
}

async function requestJson(url, init) {
  const response = await fetch(url, {
    ...init,
    signal: AbortSignal.timeout(12_000)
  });

  const body = await parseJson(response);
  if (!response.ok) {
    fail(`Supervisor call failed (${response.status}) ${url}: ${JSON.stringify(body)}`);
  }

  return toObject(body);
}

async function parseJson(response) {
  const text = await response.text();
  if (!text) {
    return null;
  }

  try {
    return JSON.parse(text);
  } catch {
    return { raw: text };
  }
}

function readRequired(name) {
  const value = readOptional(name);
  if (!value) {
    fail(`${name} is required`);
  }
  return value;
}

function readOptional(name) {
  const value = process.env[name]?.trim();
  if (!value) {
    return undefined;
  }
  return value;
}

function readOptionalFromUnknown(value) {
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }
  return trimmed;
}

function toObject(value) {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value;
  }
  return {};
}

function stripTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function fail(message) {
  console.error(`[ha-app-e2e] ${message}`);
  process.exit(1);
}

function loadLocalEnv() {
  const root = process.cwd();
  const files = [resolve(root, ".env.local"), resolve(root, ".env")];
  for (const file of files) {
    if (!existsSync(file)) {
      continue;
    }

    parseEnvFile(file);
  }
}

function parseEnvFile(path) {
  const content = readFileSync(path, "utf8");
  const allowed = new Set([
    "SUPERVISOR_TOKEN",
    "SUPERVISOR_URL",
    "APP_SLUG",
    "RELAY_BASE_URL",
    "RELAY_AUTH_TOKEN",
    "HA_LLAT",
    "WS_TYPE"
  ]);

  for (const line of content.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }

    const idx = line.indexOf("=");
    if (idx <= 0) {
      continue;
    }

    const key = line.slice(0, idx).trim();
    if (!allowed.has(key)) {
      continue;
    }

    const existing = process.env[key];
    if (existing && existing.trim().length > 0) {
      continue;
    }

    let value = line.slice(idx + 1).trim();
    if (
      (value.startsWith("\"") && value.endsWith("\""))
      || (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }

    process.env[key] = value;
  }
}
