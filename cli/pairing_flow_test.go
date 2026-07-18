package main

import "testing"

// Regression: an IPv6 relay host must stay bracketed when building the secure
// endpoint URL, or activation and later functional calls get an invalid host.
func TestSecureBaseFromBootstrapBracketsIPv6(t *testing.T) {
	got, err := secureBaseFromBootstrap("http://[2001:db8::1]:8791", 8792)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://[2001:db8::1]:8792" {
		t.Fatalf("IPv6 host not bracketed: got %q", got)
	}
}

func TestSecureBaseFromBootstrapIPv4(t *testing.T) {
	got, err := secureBaseFromBootstrap("http://192.168.1.5:8791", 8792)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "https://192.168.1.5:8792" {
		t.Fatalf("got %q, want https://192.168.1.5:8792", got)
	}
}

func TestSecureBaseFromBootstrapRejectsBadPort(t *testing.T) {
	if _, err := secureBaseFromBootstrap("http://192.168.1.5:8791", 0); err == nil {
		t.Fatal("expected an error for an invalid secure port")
	}
}
