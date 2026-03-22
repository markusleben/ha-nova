//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

const setupInputEchoReadRequest = unix.TCGETS
const setupInputEchoWriteRequest = unix.TCSETS

func newSetupInputEchoGuard() (func(), error) {
	fd := int(os.Stdin.Fd())

	current, err := unix.IoctlGetTermios(fd, setupInputEchoReadRequest)
	if err != nil {
		return func() {}, err
	}

	muted := *current
	muted.Lflag &^= unix.ECHO
	muted.Lflag &^= unix.ECHONL
	if err := unix.IoctlSetTermios(fd, setupInputEchoWriteRequest, &muted); err != nil {
		return func() {}, err
	}

	return func() {
		_ = unix.IoctlSetTermios(fd, setupInputEchoWriteRequest, current)
	}, nil
}
