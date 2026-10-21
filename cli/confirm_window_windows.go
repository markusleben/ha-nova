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
// guards against slips, not a determined model. raiseBox brings it to the
// front; OK is off from the moment the box is set up (initBox) until it has
// shown for okDelay, so keys or a click meant for the terminal can't
// confirm.

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
	isWindow             = user32.NewProc("IsWindow")
	getForegroundWindow  = user32.NewProc("GetForegroundWindow")
	getWindowThreadPID   = user32.NewProc("GetWindowThreadProcessId")
	attachThreadInput    = user32.NewProc("AttachThreadInput")
	isHungAppWindow      = user32.NewProc("IsHungAppWindow")
	setForegroundWindow  = user32.NewProc("SetForegroundWindow")
	getParent            = user32.NewProc("GetParent")
	setWindowsHookEx     = user32.NewProc("SetWindowsHookExW")
	unhookWindowsHookEx  = user32.NewProc("UnhookWindowsHookEx")
	callNextHookEx       = user32.NewProc("CallNextHookEx")
	rtlMoveMemory        = kernel32.NewProc("RtlMoveMemory")
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
	// initBox (a WH_CALLWNDPROCRET hook on the box thread only) turns OK off
	// right after the box handled WM_INITDIALOG, before it shows: Cancel has
	// the focus and is the default, and Tab skips a disabled button. No OK
	// button at all: the box is closed (Cancel), never shown without the
	// guard.
	initBox = syscall.NewCallback(func(code, wParam, lParam uintptr) uintptr {
		if int32(code) >= 0 { // HC_ACTION
			var m struct { // CWPRETSTRUCT
				lResult, lParam, wParam uintptr
				message                 uint32
				hwnd                    uintptr
			}
			_, _, _ = rtlMoveMemory.Call(uintptr(unsafe.Pointer(&m)), lParam, unsafe.Sizeof(m))
			const wmInitDialog, wmClose, idOK = 0x110, 0x10, 1
			if m.message == wmInitDialog && isDialog(m.hwnd) {
				okButton, _, _ := getDlgItem.Call(m.hwnd, idOK)
				if okButton == 0 {
					_, _, _ = postMessage.Call(m.hwnd, wmClose, 0, 0)
				} else {
					_, _, _ = enableWindow.Call(okButton, 0)
					thread, _, _ := getCurrentThreadID.Call()
					foundBoxes.Store(thread, setUpBox{box: m.hwnd, ok: okButton})
				}
			}
		}
		r, _, _ := callNextHookEx.Call(0, code, wParam, lParam)
		return r
	})
	foundBoxes sync.Map // box thread id → setUpBox
)

// setUpBox: a box past WM_INITDIALOG with its OK button off.
type setUpBox struct{ box, ok uintptr }

// isDialog: a dialog window (class #32770), as a message box is.
func isDialog(hwnd uintptr) bool {
	var class [16]uint16
	n, _, _ := getClassName.Call(hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
	return n > 0 && syscall.UTF16ToString(class[:n]) == "#32770"
}

// raiseBox brings the box on thread to the front and on top once initBox
// saw it set up and it has shown itself (or a second later), flashes its
// taskbar button, and turns OK on okDelay later. Until then the keyboard the
// box gets can only cancel: a terminal habit like Tab then Enter lands on
// Cancel, and a click meant for the terminal can't hit OK either.
func raiseBox(thread uintptr, stop <-chan struct{}) {
	defer foundBoxes.Delete(thread)
	wait := func(d time.Duration) bool { // false: the box is gone
		select {
		case <-stop:
			return false
		case <-time.After(d):
			return true
		}
	}
	var b setUpBox
	for {
		if found, ok := foundBoxes.Load(thread); ok {
			b = found.(setUpBox)
			break
		}
		if !wait(10 * time.Millisecond) {
			return
		}
	}
	for i := 0; i < 20; i++ { // until it shows itself, at most a second
		if visible, _, _ := isWindowVisible.Call(b.box); visible != 0 {
			break
		}
		if !wait(50 * time.Millisecond) {
			return
		}
	}
	const swShowNoActivate = 4 // also restores a box started minimized or hidden
	_, _, _ = showWindow.Call(b.box, swShowNoActivate)
	bringToFront(b.box, thread)
	putOnTop(b.box)
	const flashAll, flashUntilFront = 0x3, 0xc // FLASHW_ALL | FLASHW_TIMERNOFG
	info := struct {
		size             uint32
		hwnd             uintptr
		flags, count, ms uint32
	}{hwnd: b.box, flags: flashAll | flashUntilFront}
	info.size = uint32(unsafe.Sizeof(info))
	_, _, _ = flashWindowEx.Call(uintptr(unsafe.Pointer(&info)))
	if !wait(okDelay) {
		return
	}
	if live, _, _ := isWindow.Call(b.box); live != 0 {
		if parent, _, _ := getParent.Call(b.ok); parent == b.box {
			_, _, _ = enableWindow.Call(b.ok, 1)
		}
	}
}

// bringToFront gives box the foreground. Windows refuses it (and with it
// "on top": MB_TOPMOST, SetWindowPos) to a process without the right to
// take the foreground, such as `ha-nova mcp` started by an AI app long ago:
// the box waited behind the terminal (0.27 upgrade lane D6; seen again on a
// Win11 VM, where this call fixed it).
// A thread whose input is attached to the foreground window's thread may
// set the foreground: attach this thread and the box's for just this call,
// then detach. A hung foreground window is left alone (attaching could hang
// this goroutine; nativeWindow never waits for it, only the box's own
// thread does).
func bringToFront(box, boxThread uintptr) {
	runtime.LockOSThread() // input attaches per OS thread
	defer runtime.UnlockOSThread()
	fg, _, _ := getForegroundWindow.Call()
	if fg == box || fg == 0 {
		return
	}
	if hung, _, _ := isHungAppWindow.Call(fg); hung != 0 {
		return
	}
	fgThread, _, _ := getWindowThreadPID.Call(fg, 0)
	me, _, _ := getCurrentThreadID.Call()
	for _, from := range []uintptr{me, boxThread} {
		if from != fgThread && fgThread != 0 {
			if attached, _, _ := attachThreadInput.Call(from, fgThread, 1); attached != 0 {
				defer attachThreadInput.Call(from, fgThread, 0)
			}
		}
	}
	_, _, _ = setForegroundWindow.Call(box)
}

// putOnTop moves box above every window that isn't on top itself, without
// activating it.
func putOnTop(box uintptr) {
	const hwndTopmost = ^uintptr(0) // HWND_TOPMOST (-1)
	const swpNoSize, swpNoMove, swpNoActivate, swpShowWindow = 0x1, 0x2, 0x10, 0x40
	_, _, _ = setWindowPos.Call(box, hwndTopmost, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoActivate|swpShowWindow)
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
		const whCallWndProcRet = 12
		r := uintptr(0) // no hook, no box: OK would have no guard (windowFailed)
		if hook, _, _ := setWindowsHookEx.Call(whCallWndProcRet, initBox, 0, id); hook != 0 {
			r, _, _ = messageBoxTimeout.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), windowsBoxStyle, 0, uintptr(max(wait.Milliseconds(), 1000)))
			_, _, _ = unhookWindowsHookEx.Call(hook)
		}
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
