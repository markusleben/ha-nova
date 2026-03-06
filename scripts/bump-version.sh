#!/usr/bin/env bash
# Bump skill_version across all version-bearing files.
# Usage: bash scripts/bump-version.sh 0.2.0
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NEW_VERSION="${1:-}"

if [[ -z "$NEW_VERSION" ]]; then
  echo "Usage: $0 <new-version>"
  echo "Example: $0 0.2.0"
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "Error: jq is required but not installed. Install with: brew install jq"
  exit 1
fi

if ! [[ "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Error: version must be semver (MAJOR.MINOR.PATCH), got: $NEW_VERSION"
  exit 1
fi

# 1. version.json (source of truth)
# Update skill_version only; min_relay_version stays as-is
tmp=$(mktemp)
jq --arg v "$NEW_VERSION" '.skill_version = $v' "$REPO_ROOT/version.json" > "$tmp" && mv "$tmp" "$REPO_ROOT/version.json"

# 2. package.json
tmp=$(mktemp)
jq --arg v "$NEW_VERSION" '.version = $v' "$REPO_ROOT/package.json" > "$tmp" && mv "$tmp" "$REPO_ROOT/package.json"

# 3. plugin.json
tmp=$(mktemp)
jq --arg v "$NEW_VERSION" '.version = $v' "$REPO_ROOT/.claude-plugin/plugin.json" > "$tmp" && mv "$tmp" "$REPO_ROOT/.claude-plugin/plugin.json"

# 4. marketplace.json
tmp=$(mktemp)
jq --arg v "$NEW_VERSION" '.plugins[0].version = $v' "$REPO_ROOT/.claude-plugin/marketplace.json" > "$tmp" && mv "$tmp" "$REPO_ROOT/.claude-plugin/marketplace.json"

# 5. config.yaml (HA App version — Supervisor uses this for update detection)
# Use temp file instead of sed -i (BSD vs GNU portability)
tmp=$(mktemp)
sed "s/^version: \".*\"/version: \"$NEW_VERSION\"/" "$REPO_ROOT/config.yaml" > "$tmp" && mv "$tmp" "$REPO_ROOT/config.yaml"

echo "Bumped to $NEW_VERSION in:"
echo "  version.json"
echo "  package.json"
echo "  .claude-plugin/plugin.json"
echo "  .claude-plugin/marketplace.json"
echo "  config.yaml"
echo ""
echo "Next: npm test && git add version.json package.json .claude-plugin/plugin.json .claude-plugin/marketplace.json config.yaml && git commit -m 'chore: bump version to $NEW_VERSION'"
