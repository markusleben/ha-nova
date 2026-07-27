package main

import (
	"crypto/sha256"
	"fmt"
)

func deviceCredentialRetirementFingerprint(credential string) string {
	sum := sha256.Sum256([]byte(credential))
	return fmt.Sprintf("%x", sum[:])
}
