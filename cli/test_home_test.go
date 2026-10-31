package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireTestHome fails the test unless home is a temp dir. Every fake that
// writes Claude Code or HA NOVA state checks its home with it first: a test
// fake once overwrote a developer's real Claude Code plugin registry.
func requireTestHome(t *testing.T, home string) {
	t.Helper()
	if err := testHomeError(home); err != nil {
		t.Fatal(err)
	}
}

func testHomeError(home string) error {
	tmp, tmpErr := filepath.EvalSymlinks(os.TempDir())
	resolved, err := filepath.EvalSymlinks(home)
	if tmpErr != nil || err != nil || !strings.HasPrefix(resolved, tmp+string(filepath.Separator)) {
		return fmt.Errorf("test home %q is not a temp dir: test fakes never write outside one", home)
	}
	return nil
}
