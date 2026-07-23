//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func acquireAutoRepairPlatformLock(configDir string) (func(), bool) {
	file, err := os.Open(filepath.Dir(filepath.Clean(configDir)))
	if err != nil {
		return func() {}, false
	}
	err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return func() {}, false
		}
		return func() {}, false
	}
	return func() {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
	}, true
}
