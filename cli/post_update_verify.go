package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// transientBackupResidue returns the configured file/skill-tree clients whose
// just-synced artifacts still reference a transient update backup
// (.ha-nova-old-/next-/failed-). A clean sync leaves none: the adapters
// RemoveAll+rebuild from the resolved source root, so any residue means the sync
// resolved its source from a stale, about-to-be-deleted backup (the pre-0.6.1
// update bug) and the install is not actually up to date. It is the in-tool
// version of `grep -R .ha-nova-old ~/.hermes/skills/ha-nova`.
//
// Claude is intentionally not scanned here (its marketplace registration is
// re-derived from the source root and repaired by the session-start hook); this
// targets the file/symlink skill trees where stale paths are baked into content.
func transientBackupResidue(paths runtimePaths, clients []string) []string {
	dirty := []string{}
	for _, client := range clients {
		for _, root := range clientSkillTreeRoots(paths, client) {
			if pathHasTransientBackupResidue(root) {
				dirty = append(dirty, client)
				break
			}
		}
	}
	return dirty
}

// clientSkillTreeRoots maps a client id to the on-disk root(s) where ITS synced
// HA NOVA skills live. Returns nil for clients without a scannable skill tree.
// Antigravity uses a flat layout shared with the user's other skills, so only
// the exact HA NOVA-managed skill names are ours — never scan unrelated
// Antigravity skills just because they share a prefix.
func clientSkillTreeRoots(paths runtimePaths, client string) []string {
	switch client {
	case "hermes":
		return []string{filepath.Join(paths.Home, ".hermes", "skills", "ha-nova")}
	case "codex":
		return []string{filepath.Join(paths.Home, ".agents", "skills", "ha-nova")}
	case "opencode":
		return []string{filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova")}
	case "antigravity":
		subSkills, _ := sourceSubSkills(resolveSourceRoot(paths))
		roots := []string{}
		for skill := range managedAntigravitySkillNames(subSkills) {
			roots = append(roots,
				filepath.Join(antigravitySkillsRoot(paths.Home), skill),
				filepath.Join(legacyGeminiSkillsRoot(paths.Home), skill),
			)
		}
		sort.Strings(roots)
		return roots
	default:
		return nil
	}
}

// pathHasTransientBackupResidue reports whether the install at root still points
// at, or was copied from, a transient update backup. Symlink installs
// (codex/opencode) are residue when the link targets a backup or dangles; copy
// installs (hermes/antigravity) are residue when any synced markdown bakes a
// backup path.
func pathHasTransientBackupResidue(root string) bool {
	info, err := os.Lstat(root)
	if err != nil {
		return false // not installed here; nothing to verify
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(root)
		if err != nil {
			return true
		}
		if pathMentionsTransientBackup(target) {
			return true
		}
		if _, err := os.Stat(root); err != nil {
			return true // dangling symlink (its backup target was deleted)
		}
		return false
	}
	found := false
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		if pathMentionsTransientBackup(string(data)) {
			found = true
		}
		return nil
	})
	return found
}

func pathMentionsTransientBackup(s string) bool {
	return strings.Contains(s, installBackupPrefixOld) ||
		strings.Contains(s, installBackupPrefixNext) ||
		strings.Contains(s, installBackupPrefixFailed)
}
