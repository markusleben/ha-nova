//go:build darwin || linux

package main

import "os"

func platformNativePromptProcessElevated() bool {
	return os.Geteuid() == 0
}
