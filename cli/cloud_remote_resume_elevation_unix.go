//go:build darwin || linux

package main

import "os"

// platformRemoteResumeProcessElevated matches the native-prompt elevation
// check on Unix: a remote resume never tolerates running as root.
func platformRemoteResumeProcessElevated() bool {
	return os.Geteuid() == 0
}
