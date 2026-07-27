//go:build darwin

package main

import "testing"

func TestDarwinPromptContextRequiresConsoleUserSession(t *testing.T) {
	original := darwinConsoleUserSessionAvailable
	t.Cleanup(func() {
		darwinConsoleUserSessionAvailable = original
	})

	darwinConsoleUserSessionAvailable = func() bool { return false }
	if platformNativeSecretPromptContextAvailable() {
		t.Fatal("headless macOS context unexpectedly accepted")
	}
}
