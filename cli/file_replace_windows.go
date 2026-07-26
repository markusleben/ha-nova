//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	moveFileReplaceExisting = 0x00000001
	moveFileWriteThrough    = 0x00000008
)

var (
	kernel32ReplaceFile = windows.NewLazySystemDLL(
		"kernel32.dll",
	).NewProc("ReplaceFileW")
)

func replaceFileKeepingPrior(
	target string,
	replacement string,
	prior string,
) error {
	targetUTF16, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	replacementUTF16, err := windows.UTF16PtrFromString(replacement)
	if err != nil {
		return err
	}
	priorUTF16, err := windows.UTF16PtrFromString(prior)
	if err != nil {
		return err
	}
	result, _, callErr := kernel32ReplaceFile.Call(
		uintptr(unsafe.Pointer(targetUTF16)),
		uintptr(unsafe.Pointer(replacementUTF16)),
		uintptr(unsafe.Pointer(priorUTF16)),
		0,
		0,
		0,
	)
	if result == 0 {
		return callErr
	}
	if err := flushWindowsFile(target); err != nil {
		return err
	}
	return flushWindowsFile(prior)
}

func replaceFileDurably(source string, target string) error {
	sourceUTF16, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	targetUTF16, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourceUTF16,
		targetUTF16,
		moveFileReplaceExisting|moveFileWriteThrough,
	)
}

func flushWindowsFile(path string) error {
	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		pathUTF16,
		windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|
			windows.FILE_SHARE_WRITE|
			windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	return windows.FlushFileBuffers(handle)
}

func removeTransactionMarkerDurably(path string) error {
	sourceUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	tombstoneUTF16, err := windows.UTF16PtrFromString(
		path + ".committed",
	)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(
		sourceUTF16,
		tombstoneUTF16,
		moveFileReplaceExisting|moveFileWriteThrough,
	); err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND {
			return nil
		}
		return err
	}
	_ = windows.DeleteFile(tombstoneUTF16)
	return nil
}

// MoveFileExW supplies the documented write-through metadata commit on
// Windows. ReplaceFileW is followed by FlushFileBuffers on both generations.
func syncParentDirectory(string) error {
	return nil
}
