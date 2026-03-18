package main

import "testing"

func withClientRuntimeAvailability(t *testing.T, available map[string]bool) {
	t.Helper()

	original := clientRuntimeDetectedForStatus
	clientRuntimeDetectedForStatus = func(client string) bool {
		if value, ok := available[client]; ok {
			return value
		}
		return false
	}
	t.Cleanup(func() {
		clientRuntimeDetectedForStatus = original
	})
}

func withAllClientRuntimesAvailable(t *testing.T) {
	t.Helper()
	withClientRuntimeAvailability(t, map[string]bool{
		"claude":   true,
		"codex":    true,
		"opencode": true,
		"gemini":   true,
	})
}
