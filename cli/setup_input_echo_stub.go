//go:build !darwin && !linux && !windows

package main

func newSetupInputEchoGuard() (func(), error) {
	return func() {}, nil
}
