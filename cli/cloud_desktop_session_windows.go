//go:build windows

package main

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func platformNativePromptProcessElevated() bool {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(
		uint32(os.Getpid()),
		&sessionID,
	); err != nil || sessionID == 0 {
		// Windows OpenSSH and services run outside an interactive console/RDP
		// session. Fail closed before Credential Manager or browser access.
		return true
	}
	var elevated uint32
	var resultSize uint32
	err := windows.GetTokenInformation(
		windows.GetCurrentProcessToken(),
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevated)),
		uint32(unsafe.Sizeof(elevated)),
		&resultSize,
	)
	return err != nil ||
		resultSize != uint32(unsafe.Sizeof(elevated)) ||
		elevated != 0
}

func platformNativePromptRunsUnderWSL() bool {
	return strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" ||
		strings.TrimSpace(os.Getenv("WSL_INTEROP")) != ""
}
