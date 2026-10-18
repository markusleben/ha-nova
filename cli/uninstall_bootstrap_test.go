package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedCacheCleanupRemovesSessionBootstrapRecoveryArtifacts(t *testing.T) {
	paths := runtimePaths{CacheDir: t.TempDir()}
	artifacts := []string{
		filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker),
		filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile),
	}
	for _, artifact := range artifacts {
		if err := os.WriteFile(artifact, []byte("state\n"), 0o600); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
	}
	// Regression (rc2 lane A, D9): pending plans survived uninstall and purge.
	plan := filepath.Join(planDir(paths), "p_70a375d11c0a.json")
	if err := os.MkdirAll(filepath.Dir(plan), 0o700); err != nil {
		t.Fatalf("mkdir plans: %v", err)
	}
	if err := os.WriteFile(plan, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	artifacts = append(artifacts, plan, planDir(paths))
	if err := removeManagedCacheArtifacts(paths, &uninstallReport{}); err != nil {
		t.Fatalf("remove cache artifacts: %v", err)
	}
	for _, artifact := range artifacts {
		if _, err := os.Stat(artifact); !os.IsNotExist(err) {
			t.Fatalf("artifact survived uninstall cleanup: %s (%v)", artifact, err)
		}
	}
}
