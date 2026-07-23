package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReplacePathAtomicBuildFailureLeavesTargetUntouched(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	writeBundleTestFile(t, filepath.Join(target, "value.txt"), "old\n", 0o600)

	err := replacePathAtomic(target, func(stage string) error {
		writeBundleTestFile(t, filepath.Join(stage, "value.txt"), "partial\n", 0o600)
		return errors.New("build failed")
	})
	if err == nil {
		t.Fatal("expected build failure")
	}
	content, readErr := os.ReadFile(filepath.Join(target, "value.txt"))
	if readErr != nil || string(content) != "old\n" {
		t.Fatalf("target changed after failed build: content=%q err=%v", content, readErr)
	}
}
