//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func newSetupInputEchoGuard() (func(), error) {
	handle := windows.Handle(os.Stdin.Fd())

	var currentMode uint32
	if err := windows.GetConsoleMode(handle, &currentMode); err != nil {
		return func() {}, err
	}

	mutedMode := currentMode &^ windows.ENABLE_ECHO_INPUT
	if err := windows.SetConsoleMode(handle, mutedMode); err != nil {
		return func() {}, err
	}

	return func() {
		_ = windows.SetConsoleMode(handle, currentMode)
	}, nil
}
