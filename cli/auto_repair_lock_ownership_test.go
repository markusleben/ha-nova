package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutoRepairLockLoserPreservesConfigDirCreatedByWinner(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "ha-nova")
	originalPlatformLock := acquireAutoRepairPlatformLockForMutation
	acquireAutoRepairPlatformLockForMutation = func(string) (func(), bool) {
		if err := os.Mkdir(configDir, 0o755); err != nil {
			t.Fatalf("simulate winning process creating config dir: %v", err)
		}
		return func() {}, false
	}
	t.Cleanup(func() {
		acquireAutoRepairPlatformLockForMutation = originalPlatformLock
	})

	if release, acquired := acquireAutoRepairLock(runtimePaths{ConfigDir: configDir}); acquired {
		release()
		t.Fatal("losing process unexpectedly acquired the platform lock")
	}
	if info, err := os.Stat(configDir); err != nil || !info.IsDir() {
		t.Fatalf("losing process removed the winner's config dir: info=%v err=%v", info, err)
	}
}
