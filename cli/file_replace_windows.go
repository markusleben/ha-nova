//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	replaceFileWriteThrough = 0x00000001
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
		uintptr(replaceFileWriteThrough),
		0,
		0,
	)
	if result == 0 {
		return callErr
	}
	return nil
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

// ReplaceFileW and MoveFileExW use write-through metadata commits on Windows.
func syncParentDirectory(string) error {
	return nil
}
