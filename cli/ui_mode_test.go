package main

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestResolveStatusUISessionHonorsPlainEnv(t *testing.T) {
	originalEnv := uiEnvLookup
	originalTTY := writerSupportsTTYForSetup
	originalANSI := uiOutputSupportsANSI
	defer func() {
		uiEnvLookup = originalEnv
		writerSupportsTTYForSetup = originalTTY
		uiOutputSupportsANSI = originalANSI
	}()

	uiEnvLookup = func(key string) string {
		if key == "HA_NOVA_PLAIN_UI" {
			return "1"
		}
		return ""
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	session := resolveSetupUISession(&bytes.Buffer{})
	if !session.plain() {
		t.Fatalf("expected plain session, got %s", session.mode)
	}
}

func TestResolveSetupUISessionUsesEnhancedWhenTTYInputAndOutputExist(t *testing.T) {
	originalEnv := uiEnvLookup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	defer func() {
		uiEnvLookup = originalEnv
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
	}()

	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	session := resolveSetupUISession(os.Stdout)
	if !session.enhanced() {
		t.Fatalf("expected enhanced session, got %s", session.mode)
	}
}

func TestResolveSetupUISessionFallsBackToPlainWhenANSIUnavailable(t *testing.T) {
	originalEnv := uiEnvLookup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	defer func() {
		uiEnvLookup = originalEnv
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
	}()

	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return false }

	session := resolveSetupUISession(os.Stdout)
	if !session.plain() {
		t.Fatalf("expected plain session when ANSI is unavailable, got %s", session.mode)
	}
}

func TestResolveSetupUISessionFallsBackToPlainWhenInputIsNotInteractive(t *testing.T) {
	originalEnv := uiEnvLookup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	defer func() {
		uiEnvLookup = originalEnv
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
	}()

	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return false }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	session := resolveSetupUISession(os.Stdout)
	if !session.plain() {
		t.Fatalf("expected plain session when setup input is not interactive, got %s", session.mode)
	}
}
