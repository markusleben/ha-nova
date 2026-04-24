//go:build !linux

package main

func classifyAmbiguousDesktopKeyringSetupError(err error) error {
	_ = err
	return nil
}
