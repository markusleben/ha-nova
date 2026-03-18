//go:build windows

package main

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

const enableVirtualTerminalProcessing = 0x0004

func writerSupportsANSI(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	if !writerSupportsTTY(file) {
		return false
	}

	handle := windows.Handle(file.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true
	}
	if err := windows.SetConsoleMode(handle, mode|enableVirtualTerminalProcessing); err != nil {
		return false
	}
	return true
}
