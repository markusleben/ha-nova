//go:build linux

package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func replaceFileKeepingPrior(
	target string,
	replacement string,
	prior string,
) error {
	if err := unix.Renameat2(
		unix.AT_FDCWD,
		target,
		unix.AT_FDCWD,
		replacement,
		unix.RENAME_EXCHANGE,
	); err != nil {
		return err
	}
	if err := os.Rename(replacement, prior); err != nil {
		return err
	}
	return syncParentDirectory(target)
}

func replaceFileDurably(source string, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	return syncParentDirectory(target)
}

func syncParentDirectory(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func removeTransactionMarkerDurably(path string) error {
	if err := os.Rename(
		path,
		path+".committed",
	); err != nil &&
		!os.IsNotExist(err) {
		return err
	}
	return syncParentDirectory(path)
}

func removeCommittedTransactionMarkerDurably(path string) error {
	if err := os.Remove(path); err != nil &&
		!os.IsNotExist(err) {
		return err
	}
	return syncParentDirectory(path)
}
