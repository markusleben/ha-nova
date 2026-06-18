package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathHasTransientBackupResidueCopyClient(t *testing.T) {
	root := t.TempDir()
	// A copy client whose synced markdown bakes a transient-backup path is residue.
	writeBundleTestFile(t, filepath.Join(root, "ha-nova", "SKILL.md"),
		"name: ha-nova\nSee `"+filepath.Join("/home/u", installBackupPrefixOld+"123", "docs", "reference", "x.md")+"`\n", 0o644)
	if !pathHasTransientBackupResidue(root) {
		t.Fatal("expected residue for a copy tree referencing a transient backup")
	}

	clean := t.TempDir()
	writeBundleTestFile(t, filepath.Join(clean, "ha-nova", "SKILL.md"), "name: ha-nova\nSee `docs/reference/x.md`\n", 0o644)
	if pathHasTransientBackupResidue(clean) {
		t.Fatal("did not expect residue for a clean copy tree")
	}
}

func TestPathHasTransientBackupResidueSymlinkClient(t *testing.T) {
	if isWindowsRuntime() {
		t.Skip("symlink residue is a non-Windows skill-tree concern")
	}
	parent := t.TempDir()

	// Symlink whose target path points into a transient backup → residue.
	intoBackup := filepath.Join(parent, "into-backup")
	if err := os.Symlink(filepath.Join(parent, installBackupPrefixOld+"9", "skills"), intoBackup); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if !pathHasTransientBackupResidue(intoBackup) {
		t.Fatal("expected residue for a symlink targeting a transient backup")
	}

	// Dangling symlink (target missing, not a backup name) → residue.
	dangling := filepath.Join(parent, "dangling")
	if err := os.Symlink(filepath.Join(parent, "gone", "skills"), dangling); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if !pathHasTransientBackupResidue(dangling) {
		t.Fatal("expected residue for a dangling symlink")
	}

	// Valid symlink to a real, non-backup tree → clean.
	realDir := filepath.Join(parent, "ha-nova", "skills")
	writeBundleTestFile(t, filepath.Join(realDir, "SKILL.md"), "name: ha-nova\n", 0o644)
	valid := filepath.Join(parent, "valid")
	if err := os.Symlink(realDir, valid); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if pathHasTransientBackupResidue(valid) {
		t.Fatal("did not expect residue for a valid symlink to a clean tree")
	}
}

func TestTransientBackupResidueAggregatesDirtyClients(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	// Hermes (copy) clean.
	writeBundleTestFile(t, filepath.Join(home, ".hermes", "skills", "ha-nova", "ha-nova", "SKILL.md"), "name: ha-nova\n", 0o644)
	// Gemini (copy) dirty: a baked transient-backup path.
	writeBundleTestFile(t, filepath.Join(home, ".gemini", "skills", "ha-nova", "SKILL.md"),
		"name: ha-nova\n`"+filepath.Join(home, installBackupPrefixOld+"7", "docs")+"`\n", 0o644)

	dirty := transientBackupResidue(paths, []string{"hermes", "gemini", "claude"})
	if len(dirty) != 1 || dirty[0] != "gemini" {
		t.Fatalf("expected only gemini flagged as residue, got %v", dirty)
	}
}

func isWindowsRuntime() bool {
	return os.PathSeparator == '\\'
}
