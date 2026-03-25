package main

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestRunSetupSpinnerWithResultRestoresInputEchoGuardOnSuccess(t *testing.T) {
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	originalEnv := uiEnvLookup
	originalGuard := acquireSetupInputEchoGuard
	defer func() {
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
		uiEnvLookup = originalEnv
		acquireSetupInputEchoGuard = originalGuard
	}()

	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	uiEnvLookup = func(string) string { return "" }

	guardCalls := 0
	restoreCalls := 0
	acquireSetupInputEchoGuard = func() (func(), error) {
		guardCalls++
		return func() {
			restoreCalls++
		}, nil
	}

	output := &bytes.Buffer{}
	value, err := runSetupSpinnerWithResult(output, "Discovering...", 0, func() (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("runSetupSpinnerWithResult() error: %v", err)
	}
	if value != "ok" {
		t.Fatalf("value = %q, want %q", value, "ok")
	}
	if guardCalls != 1 || restoreCalls != 1 {
		t.Fatalf("guardCalls=%d restoreCalls=%d, want 1/1", guardCalls, restoreCalls)
	}
}

func TestRunSetupSpinnerWithResultRestoresInputEchoGuardOnError(t *testing.T) {
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	originalEnv := uiEnvLookup
	originalGuard := acquireSetupInputEchoGuard
	defer func() {
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
		uiEnvLookup = originalEnv
		acquireSetupInputEchoGuard = originalGuard
	}()

	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	uiEnvLookup = func(string) string { return "" }

	restoreCalls := 0
	acquireSetupInputEchoGuard = func() (func(), error) {
		return func() {
			restoreCalls++
		}, nil
	}

	output := &bytes.Buffer{}
	_, err := runSetupSpinnerWithResult(output, "Checking...", 0, func() (string, error) {
		return "", errors.New("boom")
	})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
	if restoreCalls != 1 {
		t.Fatalf("restoreCalls=%d, want 1", restoreCalls)
	}
}
