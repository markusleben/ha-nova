package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveLegacyWindowsPackageResidueRemovesOldPackageArtifacts(t *testing.T) {
	home := t.TempDir()
	localAppData := filepath.Join(home, "AppData", "Local")
	t.Setenv("HOME", home)
	t.Setenv("LOCALAPPDATA", localAppData)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	linkPath := legacyWindowsPackageLinkPath(paths)
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		t.Fatalf("mkdir link dir: %v", err)
	}
	if err := os.WriteFile(linkPath, []byte("legacy-link"), 0o644); err != nil {
		t.Fatalf("write legacy link: %v", err)
	}

	packagesRoot := legacyWindowsPackagesRoot(paths)
	matchingPackage := filepath.Join(packagesRoot, "markusleben.ha-nova_0.3.1_x64__test")
	lookalikePackage := filepath.Join(packagesRoot, "markusleben.ha-nova-tools_1.0.0_x64__test")
	unrelatedPackage := filepath.Join(packagesRoot, "other.vendor.app_1.0.0_x64__test")
	for _, dir := range []string{matchingPackage, lookalikePackage, unrelatedPackage} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	report := &uninstallReport{}
	if err := removeLegacyWindowsPackageResidue(paths, report); err != nil {
		t.Fatalf("removeLegacyWindowsPackageResidue() error: %v", err)
	}

	for _, removed := range []string{linkPath, matchingPackage} {
		if _, err := os.Stat(removed); !isNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", removed, err)
		}
	}
	for _, kept := range []string{lookalikePackage, unrelatedPackage} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("expected %s to remain, stat err=%v", kept, err)
		}
	}
	if len(report.removed) != 2 {
		t.Fatalf("expected two removed artifacts, got %#v", report.removed)
	}
}
