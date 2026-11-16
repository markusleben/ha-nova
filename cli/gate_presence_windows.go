package main

import (
	"time"
	"unsafe"
)

var (
	getLastInputInfo = user32.NewProc("GetLastInputInfo")
	getTickCount     = kernel32.NewProc("GetTickCount")
)

func nativeIdleTime() (time.Duration, bool) {
	info := struct{ size, time uint32 }{size: 8} // LASTINPUTINFO
	if ok, _, _ := getLastInputInfo.Call(uintptr(unsafe.Pointer(&info))); ok == 0 {
		return 0, false
	}
	now, _, _ := getTickCount.Call()
	return time.Duration(uint32(now)-info.time) * time.Millisecond, true
}
