package main

import (
	"errors"
	"strings"
	"testing"
)

const (
	testSealedPinA = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	testSealedPinB = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB="
)

func distinctTestDeviceKey(t *testing.T, c string) string {
	t.Helper()
	key := "hanova-dev-v1." + strings.Repeat(c, 22) + "." + strings.Repeat("B", 43)
	if parseDeviceCredential(key) == nil {
		t.Fatalf("test key malformed: %q", key)
	}
	return key
}

func withSealedDevicePins(t *testing.T) {
	t.Helper()
	devicePinSealed = true
	sealedDevicePins.Lock()
	saved := sealedDevicePins.byKey
	sealedDevicePins.byKey = map[string]string{}
	sealedDevicePins.Unlock()
	t.Cleanup(func() {
		devicePinSealed = false
		sealedDevicePins.Lock()
		sealedDevicePins.byKey = saved
		sealedDevicePins.Unlock()
	})
}

func TestSealedDeviceSecretCarriesItsPinThroughTheKeychain(t *testing.T) {
	withSealedDevicePins(t)
	key := generateTestDeviceCredential(t)
	rememberSealedDevicePin(key, testSealedPinA)
	stored := sealDeviceSecret(key)
	if stored != key+"\n"+testSealedPinA {
		t.Fatalf("stored %q, want the key and its pin", stored)
	}
	sealedDevicePins.byKey = map[string]string{} // a later process reads it
	if got := unsealDeviceSecret(stored); got != key {
		t.Fatalf("read %q, want the key alone", got)
	}
	if err := requireSealedDevicePin(key, testSealedPinA, "https://relay:8792"); err != nil {
		t.Fatalf("the sealed pin was refused: %v", err)
	}
	if envelope := `{"version":1,"source":"cloud"}`; sealDeviceSecret(envelope) != envelope || unsealDeviceSecret(envelope) != envelope {
		t.Fatal("a Link pending envelope must pass through unchanged")
	}
}

func TestKeychainKeyIsOnlySentOverItsSealedPin(t *testing.T) {
	withSealedDevicePins(t)
	sealed, unsealed, file := distinctTestDeviceKey(t, "C"), distinctTestDeviceKey(t, "D"), distinctTestDeviceKey(t, "E")
	unsealDeviceSecret(sealed + "\n" + testSealedPinA)
	unsealDeviceSecret(unsealed) // a Keychain key without a pin (a Link key)
	for name, tc := range map[string]struct {
		key, pin string
		refused  bool
	}{
		"its own pin":            {sealed, testSealedPinA, false},
		"a pin from config.json": {sealed, testSealedPinB, true},
		"a key with no pin":      {unsealed, testSealedPinA, true},
		"a file key":             {file, testSealedPinB, false},
	} {
		if err := requireSealedDevicePin(tc.key, tc.pin, "https://relay:8792"); (err != nil) != tc.refused {
			t.Errorf("%s: err=%v, refused want %v", name, err, tc.refused)
		}
	}
	for _, address := range []string{"http://attacker:8792", "attacker:8792", "https://", ""} {
		if err := requireSealedDevicePin(sealed, testSealedPinA, address); !errors.Is(err, errPinnedSendNeedsHTTPS) {
			t.Errorf("address %q: err=%v, want an https refusal even with the sealed pin", address, err)
		}
	}
	devicePinSealed = false
	if err := requireSealedDevicePin(file, testSealedPinB, "http://attacker:8792"); !errors.Is(err, errPinnedSendNeedsHTTPS) {
		t.Errorf("an http address must be refused on every OS: %v", err)
	}
	devicePinSealed = true
	for _, send := range []func(string, string, string) error{activateDeviceV1, revokeSelfDeviceV1} {
		if err := send("https://127.0.0.1:1", testSealedPinB, sealed); !errors.Is(err, errDevicePinNotSealed) {
			t.Errorf("a pairing call with another pin must stop before the network: %v", err)
		}
		if err := send("http://127.0.0.1:1", testSealedPinA, sealed); !errors.Is(err, errPinnedSendNeedsHTTPS) {
			t.Errorf("a pairing call over http must stop before the network: %v", err)
		}
	}
}

func TestFunctionalTransportRefusesAConfigPinTheKeyWasNotSealedWith(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	withSealedDevicePins(t)
	key := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(key); err != nil {
		t.Fatal(err)
	}
	rememberSealedDevicePin(key, testSealedPinA)
	cfg := runtimeConfig{RelayBaseURL: "http://ha:8791", RelaySecureBaseURL: "https://attacker:8792", RelaySpkiPin: testSealedPinB}
	if _, _, _, _, err := relayFunctionalTransport(cfg); !errors.Is(err, errDevicePinNotSealed) || !strings.Contains(err.Error(), "ha-nova setup") {
		t.Fatalf("err=%v, want the edited pin refused with the next step", err)
	}
	cfg.RelaySecureBaseURL = "http://attacker:8792"
	cfg.RelaySpkiPin = testSealedPinA
	if _, _, _, _, err := relayFunctionalTransport(cfg); !errors.Is(err, errPinnedSendNeedsHTTPS) {
		t.Fatalf("err=%v, want an http address refused", err)
	}
	cfg.RelaySecureBaseURL = "https://relay:8792"
	cfg.RelaySpkiPin = testSealedPinA
	if _, _, got, device, err := relayFunctionalTransport(cfg); err != nil || !device || got != key {
		t.Fatalf("the sealed pin must still work: device=%v err=%v", device, err)
	}
}

func TestFunctionalTransportNamesTheNamedProfilesPairCommand(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	withSealedDevicePins(t)
	setActiveServerProfile("markus-home")
	key := generateTestDeviceCredential(t)
	if err := writeDeviceCredential(key); err != nil {
		t.Fatal(err)
	}
	rememberSealedDevicePin(key, testSealedPinA)
	cfg := runtimeConfig{RelayBaseURL: "http://ha:8791", RelaySecureBaseURL: "https://attacker:8792", RelaySpkiPin: testSealedPinB}
	_, _, _, _, err := relayFunctionalTransport(cfg)
	if !errors.Is(err, errDevicePinNotSealed) || !strings.Contains(err.Error(), "ha-nova pair --server markus-home") {
		t.Fatalf("err=%v, want the named profile's pair command", err)
	}
}

func TestUnsealedKeychainPendingIsReplacedByTheNextPairing(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	withSealedDevicePins(t)
	pending := distinctTestDeviceKey(t, "F")
	if err := writePendingDeviceCredential(pending); err != nil {
		t.Fatal(err)
	}
	unsealDeviceSecret(pending) // an earlier test build kept it without a pin
	saved := activateDeviceV1ForPairing
	t.Cleanup(func() { activateDeviceV1ForPairing = saved })
	activateDeviceV1ForPairing = func(string, string, string) error {
		t.Error("a key without its pin must never be activated")
		return nil
	}
	cfg := runtimeConfig{RelayBaseURL: "http://ha:8791", PendingSecureBaseURL: "https://relay:8792", PendingSpkiPin: testSealedPinA}
	resumed, err := resumePendingActivationWithPolicy(&cfg, func(*runtimeConfig) error { return nil }, SecretStoreForbidUI)
	if err != nil || resumed {
		t.Fatalf("resumed=%v err=%v, want nothing resumed so a fresh pairing runs", resumed, err)
	}
	if err := validateLocalDeviceReplacementAllowedWithPolicy(cfg, SecretStoreForbidUI); err != nil {
		t.Fatalf("a fresh pairing must be allowed to replace it: %v", err)
	}
	rememberSealedDevicePin(pending, testSealedPinA)
	if err := validateLocalDeviceReplacementAllowedWithPolicy(cfg, SecretStoreForbidUI); err == nil {
		t.Fatal("a sealed pending key must be resumed, not replaced")
	}
}
