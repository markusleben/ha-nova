package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// CLI-05: only absence proves an install root removed; a root whose removal
// can't be checked fails the uninstall instead of passing it.
func TestDiscardInstallRootSucceedsOnlyWhenTheRootIsGone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows answers a path under a file as missing")
	}
	saved := installRootRemoveRetry
	t.Cleanup(func() { installRootRemoveRetry = saved })
	installRootRemoveRetry = 0
	file := filepath.Join(t.TempDir(), "not-a-folder")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := discardInstallRoot(filepath.Join(file, "ha-nova")); err == nil {
		t.Fatal("an install root that can't be checked passed as removed")
	}
	if err := discardInstallRoot(filepath.Join(t.TempDir(), "never-there")); err != nil {
		t.Fatalf("an absent install root: %v", err)
	}
}
