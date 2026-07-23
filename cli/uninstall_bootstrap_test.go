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
	if err := removeManagedCacheArtifacts(paths, &uninstallReport{}); err != nil {
		t.Fatalf("remove cache artifacts: %v", err)
	}
	for _, artifact := range artifacts {
		if _, err := os.Stat(artifact); !os.IsNotExist(err) {
			t.Fatalf("artifact survived uninstall cleanup: %s (%v)", artifact, err)
		}
	}
}
