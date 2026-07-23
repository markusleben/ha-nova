//go:build windows

package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/windows"
)

func acquireAutoRepairPlatformLock(configDir string) (func(), bool) {
	runtime.LockOSThread()
	fail := func() (func(), bool) {
		runtime.UnlockOSThread()
		return func() {}, false
	}
	canonical := strings.ToLower(filepath.Clean(configDir))
	digest := sha256.Sum256([]byte(canonical))
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`Global\HA_NOVA_CLIENT_MUTATION_%x`, digest))
	if err != nil {
		return fail()
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		return fail()
	}
	if handle == 0 {
		return fail()
	}
	result, waitErr := windows.WaitForSingleObject(handle, 0)
	if waitErr != nil || (result != windows.WAIT_OBJECT_0 && result != windows.WAIT_ABANDONED) {
		_ = windows.CloseHandle(handle)
		return fail()
	}
	return func() {
		_ = windows.ReleaseMutex(handle)
		_ = windows.CloseHandle(handle)
		runtime.UnlockOSThread()
	}, true
}
