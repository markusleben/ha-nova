package main

import "testing"

// Regression: pointing setup at a different relay must clear the pinned secure
// endpoint (tied to the OLD relay's TLS identity), or functional calls keep
// hitting the stale pinned host until the next re-pair.
func TestSetRelayBaseURLClearsStaleSecureEndpointOnChange(t *testing.T) {
	paired := runtimeConfig{
		RelayBaseURL:         "http://old-host:8791",
		RelaySecureBaseURL:   "https://old-host:8792",
		RelaySpkiPin:         "old-pin",
		PendingSecureBaseURL: "https://old-host:8792",
		PendingSpkiPin:       "old-pin",
	}

	got := setRelayBaseURL(paired, "http://new-host:8791")
	if got.RelayBaseURL != "http://new-host:8791" {
		t.Fatalf("relay URL not updated: %q", got.RelayBaseURL)
	}
	if got.RelaySecureBaseURL != "" || got.RelaySpkiPin != "" || got.PendingSecureBaseURL != "" || got.PendingSpkiPin != "" {
		t.Fatalf("stale secure endpoint not cleared: %+v", got)
	}

	same := setRelayBaseURL(paired, "http://old-host:8791")
	if same.RelaySecureBaseURL != "https://old-host:8792" || same.RelaySpkiPin != "old-pin" {
		t.Fatalf("unchanged URL must preserve the secure endpoint: %+v", same)
	}
}

func TestApplySetupFlagOverridesRelayURLClearsSecureEndpoint(t *testing.T) {
	paired := runtimeConfig{
		RelayBaseURL:       "http://old:8791",
		RelaySecureBaseURL: "https://old:8792",
		RelaySpkiPin:       "pin",
	}
	got, err := applySetupFlagOverrides(paired, "", "", "http://new:8791")
	if err != nil {
		t.Fatalf("applySetupFlagOverrides: %v", err)
	}
	if got.RelaySecureBaseURL != "" || got.RelaySpkiPin != "" {
		t.Fatalf("relay-url override did not clear the stale secure endpoint: %+v", got)
	}
}
