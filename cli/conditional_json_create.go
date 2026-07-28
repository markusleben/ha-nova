package main

import (
	"errors"
	"io"
	"os"
)

func createFileIfAbsentDurably(
	path string,
	data []byte,
	mode os.FileMode,
) error {
	file, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		mode,
	)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errConditionalJSONConflictRestored
		}
		return err
	}
	closed := false
	removeIncomplete := true
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if removeIncomplete {
			_ = os.Remove(path)
		}
	}()

	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	removeIncomplete = false
	return syncParentDirectory(path)
}
