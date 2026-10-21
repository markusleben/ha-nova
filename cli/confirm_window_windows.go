package main

import (
	"context"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Windows: MessageBoxTimeoutW in process, so no program path or environment
// is involved. Any same-user process can click it (UI Automation), so it
// guards against slips, not a determined model. Bringing it to the front
// (raiseBox) moves focus only: Cancel stays the default button, so a key
// meant for the terminal cancels, never confirms.

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
	getForegroundWindow  = user32.NewProc("GetForegroundWindow")
	getWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	attachThreadInput    = user32.NewProc("AttachThreadInput")
	isHungAppWindow      = user32.NewProc("IsHungAppWindow")
	setWindowPos         = user32.NewProc("SetWindowPos")
	setForegroundWindow  = user32.NewProc("SetForegroundWindow")
	// closeBox posts WM_CLOSE (Cancel's effect on an OK/Cancel box) to each
	// dialog window of the thread it is enumerated for: only our message box
	// lives there. One callback each for the process (Go keeps a fixed number).
	closeBox = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if isDialog(hwnd) {
			const wmClose = 0x10
			_, _, _ = postMessage.Call(hwnd, wmClose, 0, 0)
		}
		return 1 // go on
	})
	// frontBox brings that dialog to the front (raiseBox).
	frontBox = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if isDialog(hwnd) {
			bringToFront(hwnd)
			return 0 // done
		}
		return 1
	})
)

// isDialog: a dialog window (class #32770), as a message box is.
func isDialog(hwnd uintptr) bool {
	var class [16]uint16
	n, _, _ := getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	return n > 0 && syscall.UTF16ToString(class[:n]) == "#32770"
}

// windowThread: the thread that owns a window (0 for none).
func windowThread(hwnd uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	id, _, _ := getWindowThreadPID.Call(hwnd, 0)
	return id
}

// bringToFront makes box the foreground window. MB_SETFOREGROUND alone is
// refused to a process that has had no input for a while, such as
// `ha-nova mcp` started by an AI app at session start (0.27 upgrade lane
// D6): the box then waits behind the terminal. Windows lets a thread whose
// input is attached to the foreground window's thread set the foreground,
// so attach for just this call (calling thread and box thread to the
// foreground thread), then detach. A hung foreground window is left alone:
// attaching to it could hang this thread too.
func bringToFront(box uintptr) {
	fg, _, _ := getForegroundWindow.Call()
	me, _, _ := getCurrentThreadID.Call()
	boxThread, fgThread := windowThread(box), windowThread(fg)
	if fgThread != 0 && fgThread != boxThread {
		if hung, _, _ := isHungAppWindow.Call(fg); hung == 0 {
			for _, from := range []uintptr{me, boxThread} {
				if from != fgThread {
					if ok, _, _ := attachThreadInput.Call(from, fgThread, 1); ok != 0 {
						defer attachThreadInput.Call(from, fgThread, 0)
					}
				}
			}
		}
	}
	const hwndTopmost = ^uintptr(0) // HWND_TOPMOST (-1)
	const swpNoSize, swpNoMove, swpShowWindow = 0x1, 0x2, 0x40
	_, _, _ = setWindowPos.Call(box, hwndTopmost, 0, 0, 0, 0, swpNoSize|swpNoMove|swpShowWindow)
	_, _, _ = setForegroundWindow.Call(box)
}

// raiseBox brings the box on thread to the front once it exists: until it
// is the foreground window, for at most two seconds, or until stop. Once in
// front it is left alone, so a user who clicks away keeps that choice.
func raiseBox(thread uintptr, stop <-chan struct{}) {
	runtime.LockOSThread() // input attaches per OS thread; Go ends it on return
	for give := time.Now().Add(2 * time.Second); time.Now().Before(give); {
		if fg, _, _ := getForegroundWindow.Call(); windowThread(fg) == thread {
			return
		}
		_, _, _ = enumThreadWindows.Call(thread, frontBox, 0)
		select {
		case <-stop:
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

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
	done, thread, release := make(chan uintptr, 1), make(chan uintptr, 1), make(chan struct{})
	defer close(release)
	go func() {
		// One OS thread for the box, alive until nativeWindow returns, so the
		// withdrawal below never enumerates a reused thread id.
		runtime.LockOSThread()
		id, _, _ := getCurrentThreadID.Call()
		thread <- id
		r, _, _ := messageBoxTimeout.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), windowsBoxStyle, 0, uintptr(max(wait.Milliseconds(), 1000)))
		done <- r
		<-release
	}()
	id := <-thread
	// Raised while the box thread is alive (release closes after this
	// defer), so no reused thread id is ever enumerated.
	stop, raised := make(chan struct{}), sync.WaitGroup{}
	raised.Add(1)
	go func() { defer raised.Done(); raiseBox(id, stop) }()
	defer func() { close(stop); raised.Wait() }()
	select {
	case <-ctx.Done():
		// Withdraw it by its window handle, retried while the box may not
		// exist yet; its answer is never read. A box that won't close within
		// two seconds stays until its own timeout, and its text says a later
		// answer changes nothing.
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
