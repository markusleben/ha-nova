# Release Checklist

## Version Bump

```bash
npm run bump -- 0.2.0
```

This updates all 4 version files atomically:
- `version.json` (source of truth)
- `package.json`
- `.claude-plugin/plugin.json`
- `.claude-plugin/marketplace.json`

To also bump `min_relay_version` (when skills require relay endpoints not present in older relay versions), edit `version.json` manually after the bump.

## Verify

```bash
npm test
```

The contract test asserts all 4 files are in sync.

## Commit & Tag

```bash
git add version.json package.json .claude-plugin/plugin.json .claude-plugin/marketplace.json
git commit -m "chore: bump version to X.Y.Z"
git tag -a vX.Y.Z -m "vX.Y.Z"
git push && git push --tags
```

## Relay Version Bump (independent from skill version)

Relay version lives in `nova/config.yaml` (`version:` field). Update manually:
```bash
# Edit nova/config.yaml version field
git add nova/config.yaml
git commit -m "chore: bump relay version to X.Y.Z"
```

Relay is rebuilt via Docker on the HA host — no npm publish. Users update by pulling the new image or rebuilding the app.

## Post-Release

- `git tag -l 'v*'` — verify tag exists
- Claude Code marketplace picks up new version automatically via git
- Codex/OpenCode users: `git pull` in their clone
- Gemini users: `git pull && npm run install:gemini-skill`
