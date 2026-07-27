package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerRemoveFinalAbsenceProofPreservesRecreatedPendingFile(
	t *testing.T,
) {
	previousHook := serverRemovalPhaseHook
	t.Cleanup(func() {
		serverRemovalPhaseHook = previousHook
	})
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		testProfileCredentialB,
	); err != nil {
		t.Fatal(err)
	}
	stubServerRevoke(t)
	stubServerCommandStdin(t, "cabin\n")
	replacement := validCredential(205)
	pendingPath, err := deviceSecretFilePath(
		deviceCredentialPendingServiceForProfile("cabin"),
	)
	if err != nil {
		t.Fatal(err)
	}
	serverRemovalPhaseHook = func(phase string) error {
		if phase != "credentials-purged" {
			return nil
		}
		if err := os.MkdirAll(
			filepath.Dir(pendingPath),
			0o700,
		); err != nil {
			return err
		}
		return os.WriteFile(
			pendingPath,
			[]byte(replacement),
			0o600,
		)
	}
	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 1 ||
		!strings.Contains(output, "reappeared after cleanup") {
		t.Fatalf("remove exit=%d output=%s", exit, output)
	}
	raw, err := os.ReadFile(pendingPath)
	if err != nil || string(raw) != replacement {
		t.Fatalf("pending replacement=%q err=%v", raw, err)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil || !doc.hasProfile("cabin") {
		t.Fatalf("profile preserved=%v err=%v", doc != nil, err)
	}
}

func TestFullPurgePersistsPendingOutcomeBeforeSlotDeletion(
	t *testing.T,
) {
	paths := checkpointCabinServerRemovalWithPending(t)
	stubServerRevoke(t)
	targets, err := collectProfilePurgeTargets(paths)
	if err != nil {
		t.Fatal(err)
	}
	previousHook := profilePurgePhaseHook
	profilePurgePhaseHook = func(profile string, phase string) error {
		if profile == "cabin" &&
			phase == "pending-slot-deleted" {
			return errors.New("simulated pending full-purge crash")
		}
		return nil
	}
	t.Cleanup(func() {
		profilePurgePhaseHook = previousHook
	})
	err = purgeAllDeviceCredentialsWithReport(
		targets,
		&uninstallReport{},
		nil,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"simulated pending full-purge crash",
		) {
		t.Fatalf("first purge error = %v", err)
	}
	profilePurgePhaseHook = func(string, string) error {
		return nil
	}
	targets, err = collectProfilePurgeTargets(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := purgeAllDeviceCredentialsWithReport(
		targets,
		&uninstallReport{},
		nil,
	); err != nil {
		t.Fatalf("resumed full purge: %v", err)
	}
}

func TestFullPurgeFinalAbsenceProofPreservesRecreatedCredential(
	t *testing.T,
) {
	paths := checkpointCabinServerRemoval(t)
	stubServerRevoke(t)
	targets, err := collectProfilePurgeTargets(paths)
	if err != nil {
		t.Fatal(err)
	}
	previousHook := profilePurgeFinalProofHook
	replacement := validCredential(207)
	profilePurgeFinalProofHook = func() error {
		return secretSet(
			deviceCredentialServiceForProfile("cabin"),
			replacement,
		)
	}
	t.Cleanup(func() {
		profilePurgeFinalProofHook = previousHook
	})
	err = purgeAllDeviceCredentialsWithReport(
		targets,
		&uninstallReport{},
		nil,
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			"reappeared after full cleanup",
		) {
		t.Fatalf("full purge error = %v", err)
	}
	got, readErr := secretGet(
		deviceCredentialServiceForProfile("cabin"),
	)
	if readErr != nil || got != replacement {
		t.Fatalf(
			"replacement got=%q err=%v",
			got,
			readErr,
		)
	}
}

func checkpointCabinServerRemovalWithPending(
	t *testing.T,
) runtimePaths {
	t.Helper()
	previousConfirmation :=
		readServerRemoveConfirmationForCommand
	previousHook := serverRemovalPhaseHook
	t.Cleanup(func() {
		readServerRemoveConfirmationForCommand =
			previousConfirmation
		serverRemovalPhaseHook = previousHook
	})
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	for service, credential := range map[string]string{
		deviceCredentialServiceForProfile(
			"cabin",
		): testProfileCredentialB,
		deviceCredentialPendingServiceForProfile(
			"cabin",
		): validCredential(206),
	} {
		if err := secretSet(service, credential); err != nil {
			t.Fatal(err)
		}
	}
	readServerRemoveConfirmationForCommand = func(
		string,
	) (string, error) {
		return "cabin", nil
	}
	serverRemovalPhaseHook = func(phase string) error {
		if phase == "checkpoint-persisted" {
			return errors.New("simulated checkpoint crash")
		}
		return nil
	}
	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 1 ||
		!strings.Contains(output, "simulated checkpoint crash") {
		t.Fatalf("checkpoint exit=%d output=%s", exit, output)
	}
	serverRemovalPhaseHook = func(string) error { return nil }
	return paths
}
