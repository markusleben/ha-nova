//go:build !darwin && !linux && !windows

package main

func platformNativePromptProcessElevated() bool {
	// Cloud setup is unavailable on these platforms. Fail closed if this
	// context helper is ever reached independently.
	return true
}

func platformNativePromptRunsUnderWSL() bool {
	return false
}
