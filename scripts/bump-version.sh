#!/usr/bin/env bash
# Bump skill_version across all skill version-bearing files.
# Does NOT touch config.yaml (Relay version) — managed independently.
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
jq --arg v "$NEW_VERSION" '.metadata.version = $v | .plugins[0].version = $v | .plugins[0].source = "./"' "$REPO_ROOT/.claude-plugin/marketplace.json" > "$tmp" && mv "$tmp" "$REPO_ROOT/.claude-plugin/marketplace.json"

# 5. package-lock.json root metadata
if [[ -f "$REPO_ROOT/package-lock.json" ]]; then
  tmp=$(mktemp)
  jq --arg v "$NEW_VERSION" '.version = $v | .packages[""].version = $v' "$REPO_ROOT/package-lock.json" > "$tmp" && mv "$tmp" "$REPO_ROOT/package-lock.json"
fi

echo ""
echo "Bumped skill version to $NEW_VERSION in:"
echo "  version.json"
echo "  package.json"
echo "  package-lock.json"
echo "  .claude-plugin/plugin.json"
echo "  .claude-plugin/marketplace.json"
echo ""
echo "Note: config.yaml (Relay version) is managed independently."
echo ""
echo "Next steps:"
echo "  1. npm install && npm test"
echo "  2. git add version.json package.json package-lock.json .claude-plugin/plugin.json .claude-plugin/marketplace.json"
echo "  3. git commit -m 'chore: bump skill version to $NEW_VERSION'"
echo "  4. git tag v$NEW_VERSION"
echo "  5. git push && git push origin v$NEW_VERSION"
echo ""
echo "Pushing the tag triggers the release workflow which builds"
echo "and publishes relay binaries to GitHub Releases."
echo ""
echo "Optional local Claude dev sync:"
echo "  bash scripts/dev-sync.sh"
