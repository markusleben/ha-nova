//go:build darwin

package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestDarwinNativeSecretWorkerRejectsNonHardenedExecutable(t *testing.T) {
	if darwinProcessWorkerEligible(os.Getpid()) {
		t.Skip("test executable is hardened")
	}
	if platformNativeSecretWorkerParentVerified() {
		t.Fatal("non-hardened executable passed the worker boundary")
	}
	if platformCloudRemoteSecureStorageBoundaryAvailable() {
		t.Fatal("Cloud Remote storage boundary accepted non-hardened executable")
	}
}

func TestDarwinNativeSecretWorkerVerifiesSameExecutableParent(
	t *testing.T,
) {
	if !darwinProcessWorkerEligible(os.Getpid()) {
		t.Skip("test executable is not hardened for the native-secret worker")
	}
	if os.Getenv("HA_NOVA_PARENT_IDENTITY_HELPER") == "1" {
		if !platformNativeSecretWorkerParentVerified() {
			t.Fatal("same-executable parent was rejected")
		}
		return
	}
	command := exec.Command(
		os.Args[0],
		"-test.run=TestDarwinNativeSecretWorkerVerifiesSameExecutableParent",
	)
	command.Env = append(
		os.Environ(),
		"HA_NOVA_PARENT_IDENTITY_HELPER=1",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("same-executable child: %v\n%s", err, output)
	}
}
