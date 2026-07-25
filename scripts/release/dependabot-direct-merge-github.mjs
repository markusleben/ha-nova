import { createHash } from "node:crypto";

const repository = process.env.GITHUB_REPOSITORY ?? "";
const token = process.env.GH_TOKEN ?? "";
export const apiVersion = "2026-03-10";

export function fail(message) {
  throw new Error(message);
}

export function requireSHA(value, label) {
  if (!/^[0-9a-f]{40}$/.test(value ?? "")) {
    fail(`${label} must be a full lowercase SHA-1`);
  }
  return value;
}

export async function github(endpoint, init = {}) {
  const response = await fetch(`https://api.github.com/${endpoint}`, {
    ...init,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
      "User-Agent": "ha-nova-dependabot-direct-merge",
      "X-GitHub-Api-Version": apiVersion,
      ...(init.headers ?? {}),
    },
  });
  if (!response.ok) {
    fail(`GitHub API ${endpoint} returned HTTP ${response.status}`);
  }
  if (response.status === 204) {
    return undefined;
  }
  return response.json();
}

export async function githubPages(endpoint) {
  const results = [];
  for (let page = 1; page <= 10; page += 1) {
    const separator = endpoint.includes("?") ? "&" : "?";
    const batch = await github(
      `${endpoint}${separator}per_page=100&page=${page}`,
    );
    if (!Array.isArray(batch)) {
      fail(`GitHub API ${endpoint} did not return a list`);
    }
    results.push(...batch);
    if (batch.length < 100) {
      return results;
    }
  }
  fail(`GitHub API ${endpoint} exceeds the supported pagination limit`);
}

export async function policyAt(ref) {
  const response = await github(
    `repos/${repository}/contents/.github/policy/repo-policy.json?ref=${ref}`,
  );
  if (response?.encoding !== "base64" || typeof response.content !== "string") {
    fail("repository policy response is invalid");
  }
  const bytes = Buffer.from(response.content.replaceAll("\n", ""), "base64");
  let policy;
  try {
    policy = JSON.parse(bytes.toString("utf8"));
  } catch {
    fail("repository policy must contain valid JSON");
  }
  return {
    policy,
    sha256: createHash("sha256").update(bytes).digest("hex"),
  };
}
