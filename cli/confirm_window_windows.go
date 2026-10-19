package main

import (
	"context"
	"os"
	"runtime"
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
	enumThreadWindows    = user32.NewProc("EnumThreadWindows")
	getClassName         = user32.NewProc("GetClassNameW")
	postMessage          = user32.NewProc("PostMessageW")
	// closeBox posts WM_CLOSE (Cancel's effect on an OK/Cancel box) to each
	// dialog window (class #32770) of the thread it is enumerated for: only
	// our message box lives there. One callback for the process (Go keeps
	// a fixed number).
	closeBox = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var class [16]uint16
		if n, _, _ := getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class))); n > 0 && syscall.UTF16ToString(class[:n]) == "#32770" {
			const wmClose = 0x10
			_, _, _ = postMessage.Call(hwnd, wmClose, 0, 0)
		}
		return 1 // go on
	})
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
	done, thread, release := make(chan uintptr, 1), make(chan uintptr, 1), make(chan struct{})
	defer close(release)
	go func() {
		// One OS thread for the box, alive until nativeWindow returns, so the
		// withdrawal below never enumerates a reused thread id.
		runtime.LockOSThread()
		id, _, _ := getCurrentThreadID.Call()
		thread <- id
		r, _, _ := messageBoxTimeout.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), style, 0, uintptr(max(wait.Milliseconds(), 1000)))
		done <- r
		<-release
	}()
	select {
	case <-ctx.Done():
		// Withdraw it by its window handle, retried while the box may not
		// exist yet; its answer is never read. A box that won't close within
		// two seconds stays until its own timeout, and its text says a later
		// answer changes nothing.
		id := <-thread
		for give := time.Now().Add(2 * time.Second); time.Now().Before(give); {
			_, _, _ = enumThreadWindows.Call(id, closeBox, 0)
			select {
			case <-done:
				return windowNoAnswer
			case <-time.After(50 * time.Millisecond):
			}
		}
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
