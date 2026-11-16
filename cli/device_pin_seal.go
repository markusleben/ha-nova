package main

import (
	"errors"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// On macOS a local device key is stored in the Keychain together with the SPKI
// pin of the Relay it paired with ("<key>\n<pin>"), and it is only ever sent
// over a connection pinned to that pin (0.32 spec §5.5). Editing config.json
// can then redirect nothing: a different address can't pass the sealed pin,
// and a different pin is refused before anything is sent. A Link key has no
// pin, so it never travels over a pinned connection; a file key is readable
// by any same-user program anyway and is not sealed.
var devicePinSealed = runtime.GOOS == "darwin"

var sealedPinPattern = regexp.MustCompile(`^[A-Za-z0-9+/_=-]{20,100}$`)

// sealedDevicePins remembers, for this process, the pin each Keychain key was
// read or provisioned with; "" marks a Keychain key that carries none.
var sealedDevicePins = struct {
	sync.Mutex
	byKey map[string]string
}{byKey: map[string]string{}}

func rememberSealedDevicePin(credential, pin string) {
	sealedDevicePins.Lock()
	defer sealedDevicePins.Unlock()
	sealedDevicePins.byKey[credential] = pin
}

// sealDeviceSecret is the Keychain value for a device key: the key and its
// pin when this process knows one, else the value unchanged (a Link key or a
// pending Link envelope).
func sealDeviceSecret(value string) string {
	sealedDevicePins.Lock()
	defer sealedDevicePins.Unlock()
	if pin := sealedDevicePins.byKey[value]; pin != "" {
		return value + "\n" + pin
	}
	return value
}

// unsealDeviceSecret returns the key a Keychain value holds and remembers its
// pin, or "" when it carries none.
func unsealDeviceSecret(value string) string {
	key, pin, sealed := strings.Cut(value, "\n")
	if !sealed || parseDeviceCredential(key) == nil || !sealedPinPattern.MatchString(pin) {
		if parseDeviceCredential(value) != nil {
			rememberSealedDevicePin(value, "")
		}
		return value
	}
	rememberSealedDevicePin(key, pin)
	return key
}

var errDevicePinNotSealed = errors.New("this computer's key is only sent to the Relay it was paired with, and this connection isn't that Relay; run: ha-nova setup")

// requireSealedDevicePin refuses a pinned send of a Keychain key whose sealed
// pin differs from the connection's. Keys this process never read from the
// Keychain (file keys, the App's own) are not sealed.
func requireSealedDevicePin(credential, pin string) error {
	if !devicePinSealed {
		return nil
	}
	sealedDevicePins.Lock()
	defer sealedDevicePins.Unlock()
	if sealed, keychain := sealedDevicePins.byKey[credential]; keychain && (sealed == "" || sealed != pin) {
		return errDevicePinNotSealed
	}
	return nil
}
