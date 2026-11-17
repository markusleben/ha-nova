package main

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	getLastInputInfo = user32.NewProc("GetLastInputInfo")
	getTickCount     = kernel32.NewProc("GetTickCount")
	openInputDesktop = user32.NewProc("OpenInputDesktop")
	closeDesktop     = user32.NewProc("CloseDesktop")
)

func nativeIdleTime() (time.Duration, bool) {
	if inputDesktopLocked() {
		return gateLocked, true
	}
	info := struct{ size, time uint32 }{size: 8} // LASTINPUTINFO
	if ok, _, _ := getLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		return 0, false
	}
	now, _, _ := getTickCount.Call()
	return time.Duration(uint32(now)-info.time) * time.Millisecond, true
}

// inputDesktopLocked: the desktop taking input isn't the user's own
// ("Default"): the lock screen (Winlogon) or a secure prompt. One this
// process can't open reads as locked.
func inputDesktopLocked() bool {
	const desktopReadObjects = 0x0001
	desktop, _, _ := openInputDesktop.Call(0, 0, desktopReadObjects)
	if desktop == 0 {
		return true
	}
	defer closeDesktop.Call(desktop)
	var name [64]uint16
	var needed uint32
	const uoiName = 2
	if ok, _, _ := getUserObjectInfo.Call(desktop, uoiName, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2), uintptr(unsafe.Pointer(&needed))); ok == 0 {
		return true
	}
	return syscall.UTF16ToString(name[:]) != "Default"
}
