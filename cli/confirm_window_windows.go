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
// guards against slips, not a determined model. raiseBox puts it on top
// without asking for the keyboard, and keeps OK off at first, so keys or a
// click meant for the terminal can't confirm.

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
	setWindowPos         = user32.NewProc("SetWindowPos")
	flashWindowEx        = user32.NewProc("FlashWindowEx")
	getDlgItem           = user32.NewProc("GetDlgItem")
	enableWindow         = user32.NewProc("EnableWindow")
	isWindowVisible      = user32.NewProc("IsWindowVisible")
	showWindow           = user32.NewProc("ShowWindow")
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
	// findBox records the dialog of the thread it is enumerated for (raiseBox).
	findBox = syscall.NewCallback(func(hwnd, thread uintptr) uintptr {
		if isDialog(hwnd) {
			foundBoxes.Store(thread, hwnd)
			return 0 // done
		}
		return 1
	})
	foundBoxes sync.Map // box thread id → its dialog window
)

// isDialog: a dialog window (class #32770), as a message box is.
func isDialog(hwnd uintptr) bool {
	var class [16]uint16
	n, _, _ := getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	return n > 0 && syscall.UTF16ToString(class[:n]) == "#32770"
}

// raiseBox puts the box on thread above every other window once it exists
// (looked for during at most two seconds, or until stop) and flashes its
// taskbar button until it is clicked: MB_SETFOREGROUND is refused to
// `ha-nova mcp`, which an AI app started long ago (0.27 upgrade lane D6:
// the box waited behind the terminal). A window may always move itself on
// top; that needs no foreground right, and it asks for no keyboard focus.
// Windows may still give the box the keyboard as it opens, so OK stays off
// for its first okDelay: a terminal habit like Tab then Enter lands on
// Cancel, and a click meant for the terminal can't hit OK either.
func raiseBox(thread uintptr, stop <-chan struct{}) {
	defer foundBoxes.Delete(thread)
	wait := func() bool { // false: stop
		select {
		case <-stop:
			return false
		case <-time.After(50 * time.Millisecond):
			return true
		}
	}
	var box uintptr
	for give := time.Now().Add(2 * time.Second); box == 0; {
		_, _, _ = enumThreadWindows.Call(thread, findBox, thread)
		if found, ok := foundBoxes.Load(thread); ok {
			box = found.(uintptr)
		} else if !time.Now().Before(give) || !wait() {
			return
		}
	}
	// Until it has shown itself (set up, buttons in place), at most a second.
	for i := 0; i < 20; i++ {
		if visible, _, _ := isWindowVisible.Call(box); visible != 0 || !wait() {
			break
		}
	}
	const idOK = 1
	okButton, _, _ := getDlgItem.Call(box, idOK)
	if okButton != 0 {
		_, _, _ = enableWindow.Call(okButton, 0)
	}
	const swShowNoActivate = 4 // also restores a box started minimized or hidden
	_, _, _ = showWindow.Call(box, swShowNoActivate)
	const hwndTopmost = ^uintptr(0) // HWND_TOPMOST (-1)
	const swpNoSize, swpNoMove, swpNoActivate, swpShowWindow = 0x1, 0x2, 0x10, 0x40
	_, _, _ = setWindowPos.Call(box, hwndTopmost, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoActivate|swpShowWindow)
	const flashAll, flashUntilFront = 0x3, 0xc // FLASHW_ALL | FLASHW_TIMERNOFG
	info := struct {
		size             uint32
		hwnd             uintptr
		flags, count, ms uint32
	}{hwnd: box, flags: flashAll | flashUntilFront}
	info.size = uint32(unsafe.Sizeof(info))
	_, _, _ = flashWindowEx.Call(uintptr(unsafe.Pointer(&info)))
	if okButton != 0 {
		select {
		case <-stop: // the box is gone
		case <-time.After(okDelay):
			_, _, _ = enableWindow.Call(okButton, 1)
		}
	}
}

// okDelay: how long OK stays off after the box opens (raiseBox).
const okDelay = 1500 * time.Millisecond

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
	var raised sync.WaitGroup
	raised.Add(1)
	go func() {
		// One OS thread for the box, alive until nativeWindow returns and
		// raiseBox is done, so neither the withdrawal below nor raiseBox ever
		// enumerates a reused thread id. nativeWindow itself never waits.
		runtime.LockOSThread()
		id, _, _ := getCurrentThreadID.Call()
		thread <- id
		r, _, _ := messageBoxTimeout.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), windowsBoxStyle, 0, uintptr(max(wait.Milliseconds(), 1000)))
		done <- r
		<-release
		raised.Wait()
	}()
	id := <-thread
	stop := make(chan struct{})
	go func() { defer raised.Done(); raiseBox(id, stop) }()
	defer close(stop) // before release: deferred calls run last-in first-out
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
