//go:build !darwin && !linux && !windows

package main

func platformRemoteResumeProcessElevated() bool {
	// Cloud setup is unavailable on these platforms; fail closed.
	return true
}
