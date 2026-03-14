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
jq --arg v "$NEW_VERSION" '.plugins[0].version = $v' "$REPO_ROOT/.claude-plugin/marketplace.json" > "$tmp" && mv "$tmp" "$REPO_ROOT/.claude-plugin/marketplace.json"

# 5. Migrate Claude Code plugin cache (if installed)
plugins_json="${HOME}/.claude/plugins/installed_plugins.json"
if [[ -f "$plugins_json" ]]; then
  old_path=$(
    sed -n '/"ha-nova@ha-nova"/,/installPath/s/.*"installPath"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
      "$plugins_json" | head -1
  )
  old_path="${old_path/#\~/$HOME}"

  if [[ -n "$old_path" ]]; then
    cache_parent="$(dirname "$old_path")"  # e.g. ~/.claude/plugins/cache/ha-nova/ha-nova
    new_path="${cache_parent}/${NEW_VERSION}"

    if [[ -d "$old_path" && "$old_path" != "$new_path" ]]; then
      mv "$old_path" "$new_path" || { echo "  Warning: could not rename plugin cache dir"; }
      echo "  Plugin cache: renamed $(basename "$old_path") → ${NEW_VERSION}"
    elif [[ ! -d "$old_path" && -d "$cache_parent" ]]; then
      # Old dir already gone (Claude Code cleaned up) — find latest and rename
      actual=$(ls -1d "${cache_parent}"/[0-9]* 2>/dev/null | sort -V | tail -1 || true)
      if [[ -n "$actual" && "$actual" != "$new_path" ]]; then
        mv "$actual" "$new_path" || { echo "  Warning: could not rename plugin cache dir"; }
        echo "  Plugin cache: renamed $(basename "$actual") → ${NEW_VERSION}"
      fi
    fi

    # Update installed_plugins.json — keep absolute paths (Claude Code native format)
    if [[ -d "$new_path" ]]; then
      old_stored=$(
        sed -n '/"ha-nova@ha-nova"/,/installPath/s/.*"installPath"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
          "$plugins_json" | head -1
      )
      if [[ -n "$old_stored" && "$old_stored" != "$new_path" ]]; then
        # Scope replacement to ha-nova block only
        sed -i '' "/"ha-nova@ha-nova"/,/installPath/{s|\"installPath\": \"${old_stored}\"|\"installPath\": \"${new_path}\"|;}" "$plugins_json"
      fi
      old_ver=$(sed -n '/"ha-nova@ha-nova"/,/\"version\"/s/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$plugins_json" | head -1)
      if [[ -n "$old_ver" && "$old_ver" != "$NEW_VERSION" ]]; then
        sed -i '' "/"ha-nova@ha-nova"/,/\"version\"/{s/\"version\": \"${old_ver}\"/\"version\": \"${NEW_VERSION}\"/;}" "$plugins_json"
      fi
      echo "  installed_plugins.json: updated to v${NEW_VERSION}"
    fi
  fi
fi

echo ""
echo "Bumped skill version to $NEW_VERSION in:"
echo "  version.json"
echo "  package.json"
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
