package main

import (
	"context"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// Windows (untested on a real desktop): MessageBoxTimeoutW in process, so
// no program path or environment is involved. Any same-user process can
// click it (UI Automation), so it guards against slips, not a determined model.

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	messageBoxTimeout    = user32.NewProc("MessageBoxTimeoutW")
	getThreadDesktop     = user32.NewProc("GetThreadDesktop")
	getUserObjectInfo    = user32.NewProc("GetUserObjectInformationW")
	processIDToSessionID = kernel32.NewProc("ProcessIdToSessionId")
	getCurrentThreadID   = kernel32.NewProc("GetCurrentThreadId")
)

// nativeWindowAvailable: an interactive session (not session 0, where SSH
// and services run) on the visible desktop "Default" (Codex's sandbox uses
// a private desktop no one sees).
func nativeWindowAvailable() bool {
	var session uint32
	if ok, _, _ := processIDToSessionID.Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&session))); ok == 0 || session == 0 {
		return false
	}
	thread, _, _ := getCurrentThreadID.Call()
	desktop, _, _ := getThreadDesktop.Call(thread)
	var name [64]uint16
	var needed uint32
	const uoiName = 2
	if ok, _, _ := getUserObjectInfo.Call(desktop, uoiName, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2), uintptr(unsafe.Pointer(&needed))); ok == 0 {
		return false
	}
	return syscall.UTF16ToString(name[:]) == "Default"
}

func nativeWindow(ctx context.Context, message string, wait time.Duration) windowAnswer {
	text, err1 := syscall.UTF16PtrFromString(message)
	title, err2 := syscall.UTF16PtrFromString("HA NOVA")
	if err1 != nil || err2 != nil {
		return windowFailed
	}
	// OK/Cancel, warning icon, Cancel the default, in front of other windows.
	const style = 0x1 | 0x30 | 0x100 | 0x10000 | 0x40000
	done := make(chan uintptr, 1)
	go func() {
		r, _, _ := messageBoxTimeout.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), style, 0, uintptr(wait.Milliseconds()))
		done <- r
	}()
	select {
	case <-ctx.Done():
		return windowNoAnswer
	case r := <-done:
		switch r {
		case 0: // the box could not be created
			return windowFailed
		case 1: // IDOK
			return windowConfirmed
		case 2: // IDCANCEL (also Esc and the close box)
			return windowCancelled
		}
		return windowNoAnswer // 32000: MB_TIMEDOUT
	}
}
