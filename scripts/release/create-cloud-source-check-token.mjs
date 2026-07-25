#!/usr/bin/env node

import { createSign } from "node:crypto";
import { appendFileSync } from "node:fs";

const appId = process.env.HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID ?? "";
const privateKey =
  process.env.HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY ?? "";
const repository = process.env.GITHUB_REPOSITORY ?? "";
const outputPath = process.env.GITHUB_OUTPUT ?? "";
const apiVersion = "2026-03-10";

function fail(message) {
  console.error(`[create-cloud-source-check-token] ERROR: ${message}`);
  process.exit(1);
}

function base64url(value) {
  return Buffer.from(value).toString("base64url");
}

async function github(endpoint, options = {}) {
  const { token, ...init } = options;
  const response = await fetch(`https://api.github.com/${endpoint}`, {
    ...init,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "User-Agent": "ha-nova-cloud-source-gate",
      "X-GitHub-Api-Version": apiVersion,
      ...(init.headers ?? {}),
    },
  });
  if (!response.ok) {
    fail(`GitHub API ${endpoint} returned HTTP ${response.status}`);
  }
  return response.json();
}

if (!/^[1-9]\d*$/.test(appId)) {
  fail("HA_NOVA_CLOUD_SOURCE_CHECK_APP_ID must be a positive integer");
}
if (!privateKey.includes("BEGIN") || !privateKey.includes("PRIVATE KEY")) {
  fail("HA_NOVA_CLOUD_SOURCE_CHECK_APP_PRIVATE_KEY is missing or invalid");
}
if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) {
  fail("GITHUB_REPOSITORY must identify one repository");
}
if (outputPath.length === 0) {
  fail("GITHUB_OUTPUT is required");
}

const now = Math.floor(Date.now() / 1000);
const encodedHeader = base64url(JSON.stringify({ alg: "RS256", typ: "JWT" }));
const encodedPayload = base64url(
  JSON.stringify({ iat: now - 60, exp: now + 540, iss: appId }),
);
const unsigned = `${encodedHeader}.${encodedPayload}`;
let signature;
try {
  const signer = createSign("RSA-SHA256");
  signer.update(unsigned);
  signer.end();
  signature = signer.sign(privateKey).toString("base64url");
} catch {
  fail("cannot sign the GitHub App JWT");
}
const jwt = `${unsigned}.${signature}`;
const installation = await github(`repos/${repository}/installation`, {
  token: jwt,
});
if (!Number.isSafeInteger(installation.id) || installation.id <= 0) {
  fail("GitHub App installation response is invalid");
}
const access = await github(
  `app/installations/${installation.id}/access_tokens`,
  {
    method: "POST",
    token: jwt,
    body: JSON.stringify({
      repositories: [repository.split("/")[1]],
      permissions: { administration: "read", checks: "write" },
    }),
    headers: { "Content-Type": "application/json" },
  },
);
if (
  typeof access.token !== "string" ||
  access.token.length < 20 ||
  access.permissions?.administration !== "read" ||
  access.permissions?.checks !== "write"
) {
  fail("GitHub App installation token response is invalid");
}
console.log(`::add-mask::${access.token}`);
appendFileSync(outputPath, `app-id=${appId}\n`, {
  encoding: "utf8",
  mode: 0o600,
});
appendFileSync(outputPath, `token=${access.token}\n`, {
  encoding: "utf8",
  mode: 0o600,
});
