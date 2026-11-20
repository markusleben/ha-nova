package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// CLI-05: only a final look that finds nothing proves an install root
// removed. A dangling link is removed, not taken for absence, and a root
// whose removal can't be checked fails the uninstall instead of passing it.
func TestDiscardInstallRootSucceedsOnlyWhenTheRootIsGone(t *testing.T) {
	saved, lstat := installRootRemoveRetry, installRootLstat
	t.Cleanup(func() { installRootRemoveRetry, installRootLstat = saved, lstat })
	installRootRemoveRetry = 0
	dir := t.TempDir()

	link := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := discardInstallRoot(link); err != nil {
		t.Fatalf("a dangling link: %v", err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the dangling link stayed: %v", err)
	}

	root := filepath.Join(dir, "ha-nova")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	installRootLstat = func(string) (os.FileInfo, error) { return nil, os.ErrPermission }
	if err := discardInstallRoot(root); err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("a removal that can't be checked: %v", err)
	}
	installRootLstat = os.Lstat
	if err := discardInstallRoot(filepath.Join(dir, "never-there")); err != nil {
		t.Fatalf("an absent install root: %v", err)
	}
}
