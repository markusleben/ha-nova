package main

import (
	"errors"
	"strings"
	"testing"
)

// Regression: device-slot reads/writes must run the keyring preflight (Linux
// wires it to the Secret Service check) before touching go-keyring, so a locked
// backend fails fast with a classified error instead of hanging in an unlock
// prompt.
func TestSecretOpsHonorDeviceCredentialPreflight(t *testing.T) {
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "") // force the real backend path, not the file escape hatch

	orig := deviceCredentialPreflight
	deviceCredentialPreflight = func() error { return errors.New("backend locked") }
	t.Cleanup(func() { deviceCredentialPreflight = orig })

	if _, err := secretGet("ha-nova.device-credential"); err == nil || !strings.Contains(err.Error(), "backend locked") {
		t.Fatalf("secretGet bypassed the preflight: %v", err)
	}
	if err := secretSet("ha-nova.device-credential", "v"); err == nil || !strings.Contains(err.Error(), "backend locked") {
		t.Fatalf("secretSet bypassed the preflight: %v", err)
	}
	if err := secretDelete("ha-nova.device-credential"); err == nil || !strings.Contains(err.Error(), "backend locked") {
		t.Fatalf("secretDelete bypassed the preflight: %v", err)
	}
}
