//go:build darwin

package main

import (
	"context"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestDarwinKeychainStoresTheDeviceKeyWithItsPin(t *testing.T) {
	withSealedDevicePins(t)
	keyring.MockInit()
	key := generateTestDeviceCredential(t)
	rememberSealedDevicePin(key, testSealedPinA)
	if err := darwinDeviceSecretSet(context.Background(), deviceCredentialService, secretUser(), key, SecretStoreForbidUI); err != nil {
		t.Fatal(err)
	}
	if raw, _ := keyring.Get(deviceCredentialService, secretUser()); raw != key+"\n"+testSealedPinA {
		t.Fatalf("Keychain value %q, want the key with its pin", raw)
	}
	sealedDevicePins.byKey = map[string]string{}
	got, err := darwinDeviceSecretGet(context.Background(), deviceCredentialService, secretUser(), SecretStoreForbidUI)
	if err != nil || got != key {
		t.Fatalf("read %q err=%v, want the key alone", got, err)
	}
	if err := requireSealedDevicePin(key, testSealedPinB, "https://relay:8792"); err == nil {
		t.Fatal("a pin other than the sealed one must be refused after a fresh read")
	}
}
