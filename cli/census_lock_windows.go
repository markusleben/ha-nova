//go:build windows

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func acquireCensusPlatformLock(configDir string, timeout, retry time.Duration) (func(), bool) {
	// Windows mutex ownership belongs to an OS thread, not a Go goroutine.
	// Keep this goroutine pinned from WaitForSingleObject through ReleaseMutex.
	runtime.LockOSThread()
	fail := func() (func(), bool) {
		runtime.UnlockOSThread()
		return func() {}, false
	}
	canonical := strings.ToLower(filepath.Clean(configDir))
	digest := sha256.Sum256([]byte(canonical))
	// Global namespace is required: Console, RDP, and service sessions must all
	// serialize the same config path. Microsoft limits SeCreateGlobalPrivilege
	// to global file mappings/symbolic links; named mutex creation is allowed.
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`Global\HA_NOVA_CENSUS_%x`, digest))
	if err != nil {
		return fail()
	}
	handle, err := windows.CreateMutex(nil, false, name)
	// CreateMutex returns a usable handle plus ERROR_ALREADY_EXISTS when the
	// named mutex already exists. That is the normal contention path, not a
	// creation failure; wait on that handle and always close it below.
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return fail()
	}
	if handle == 0 {
		return fail()
	}
	deadline := time.Now().Add(timeout)
	for {
		result, waitErr := windows.WaitForSingleObject(handle, 0)
		if waitErr != nil {
			_ = windows.CloseHandle(handle)
			return fail()
		}
		switch result {
		case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
			return func() {
				_ = windows.ReleaseMutex(handle)
				_ = windows.CloseHandle(handle)
				runtime.UnlockOSThread()
			}, true
		case uint32(windows.WAIT_TIMEOUT):
			if time.Now().After(deadline) {
				_ = windows.CloseHandle(handle)
				return fail()
			}
			time.Sleep(retry)
		default:
			_ = windows.CloseHandle(handle)
			return fail()
		}
	}
}
