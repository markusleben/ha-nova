//go:build windows

package main

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// platformRemoteResumeProcessElevated checks ONLY real token elevation.
// Unlike the native-prompt gate it tolerates session 0: Windows OpenSSH runs
// there by design, DPAPI works prompt-free in that session, and the remote
// resume forces every secret-store operation to ForbidUI anyway — the
// session-0 refusal exists to keep native PROMPTS off service sessions, a
// concern the resume mode has already eliminated.
func platformRemoteResumeProcessElevated() bool {
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(
		uint32(os.Getpid()),
		&sessionID,
	); err != nil {
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
