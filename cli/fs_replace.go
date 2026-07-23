package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// replacePathAtomic builds a complete sibling first, then swaps it into place.
// A failed build leaves the target untouched; a failed swap restores the old
// target. Crash residue is a sibling, never a partially written target tree.
func replacePathAtomic(target string, build func(stage string) error) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	placeholder, err := os.CreateTemp(parent, "."+filepath.Base(target)+".stage-*")
	if err != nil {
		return err
	}
	stage := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		_ = os.Remove(stage)
		return err
	}
	if err := os.Remove(stage); err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := build(stage); err != nil {
		return err
	}

	backup := stage + ".previous"
	hadTarget := false
	if _, err := os.Lstat(target); err == nil {
		hadTarget = true
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		if hadTarget {
			if restoreErr := os.Rename(backup, target); restoreErr != nil {
				return fmt.Errorf("replace %s failed: %w; restore also failed: %v", target, err, restoreErr)
			}
		}
		return err
	}
	if hadTarget {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func removePathAtomic(target string) error {
	parent := filepath.Dir(target)
	placeholder, err := os.CreateTemp(parent, "."+filepath.Base(target)+".remove-*")
	if err != nil {
		return err
	}
	trashPath := placeholder.Name()
	if err := placeholder.Close(); err != nil {
		_ = os.Remove(trashPath)
		return err
	}
	if err := os.Remove(trashPath); err != nil {
		return err
	}
	if err := os.Rename(target, trashPath); err != nil {
		return err
	}
	return os.RemoveAll(trashPath)
}
