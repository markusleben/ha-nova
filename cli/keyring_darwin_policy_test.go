//go:build darwin

package main

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestDecodeDarwinGoKeyringValue(t *testing.T) {
	const secret = "device-secret\nwith-unicode-ß"
	for name, encoded := range map[string]string{
		"base64": "go-keyring-base64:" +
			base64.StdEncoding.EncodeToString([]byte(secret)),
		"hex": "go-keyring-encoded:" +
			hex.EncodeToString([]byte(secret)),
		"legacy raw": secret,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := decodeDarwinGoKeyringValue(encoded)
			if err != nil {
				t.Fatal(err)
			}
			if got != secret {
				t.Fatalf("decoded value = %q", got)
			}
		})
	}
}
