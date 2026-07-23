//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func acquireCensusPlatformLock(_ string, timeout, retry time.Duration) (func(), bool) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return func() {}, false
	}
	directory, err := os.Open(home)
	if err != nil {
		return func() {}, false
	}
	deadline := time.Now().Add(timeout)
	for {
		err = unix.Flock(int(directory.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(directory.Fd()), unix.LOCK_UN)
				_ = directory.Close()
			}, true
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			_ = directory.Close()
			return func() {}, false
		}
		if time.Now().After(deadline) {
			_ = directory.Close()
			return func() {}, false
		}
		time.Sleep(retry)
	}
}
