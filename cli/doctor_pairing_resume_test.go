package main

import (
	"strings"
	"testing"
)

func TestDoctorPendingResumeBlocksEveryDeviceRetirementPhase(t *testing.T) {
	for _, phase := range []string{
		deviceCredentialRetirementPrepared,
		deviceCredentialRetirementRevoked,
	} {
		t.Run(phase, func(t *testing.T) {
			paths, cfg, lifecycle, snapshot, hadSnapshot :=
				setupDoctorPendingResumeTest(t)
			if err := writeDeviceCredentialRetirementCheckpoint(
				paths,
				cfg,
			); err != nil {
				t.Fatal(err)
			}
			if phase == deviceCredentialRetirementRevoked {
				checkpoint, exists, err :=
					readDeviceCredentialRetirementCheckpoint(paths)
				if err != nil || !exists {
					t.Fatalf(
						"read checkpoint: exists=%v err=%v",
						exists,
						err,
					)
				}
				if _, err := markDeviceCredentialRetirementRevoked(
					paths,
					checkpoint,
				); err != nil {
					t.Fatal(err)
				}
			}

			calls := stubDoctorPendingActivation(t, true)
			resumed, err := resumeInterruptedPairingForDoctor(
				paths,
				&cfg,
				lifecycle,
				snapshot,
				hadSnapshot,
			)
			if err == nil ||
				!strings.Contains(err.Error(), "pending device retirement") {
				t.Fatalf("resume error = %v", err)
			}
			if resumed || *calls != 0 {
				t.Fatalf(
					"retirement phase %q activated pending state: resumed=%v calls=%d",
					phase,
					resumed,
					*calls,
				)
			}
			checkpoint, exists, readErr :=
				readDeviceCredentialRetirementCheckpoint(paths)
			if readErr != nil || !exists || checkpoint.Phase != phase {
				t.Fatalf(
					"checkpoint changed: phase=%q exists=%v err=%v",
					checkpoint.Phase,
					exists,
					readErr,
				)
			}
		})
	}
}

func TestDoctorPendingResumeWithoutRetirementStillRuns(t *testing.T) {
	paths, cfg, lifecycle, snapshot, hadSnapshot :=
		setupDoctorPendingResumeTest(t)
	calls := stubDoctorPendingActivation(t, true)

	resumed, err := resumeInterruptedPairingForDoctor(
		paths,
		&cfg,
		lifecycle,
		snapshot,
		hadSnapshot,
	)
	if err != nil || !resumed || *calls != 1 {
		t.Fatalf(
			"resume = (%v, %v), calls=%d; want (true, nil), 1",
			resumed,
			err,
			*calls,
		)
	}
}

func TestRunDoctorUsesRetirementGuardBeforePendingResume(t *testing.T) {
	paths, cfg, _, _, _ := setupDoctorPendingResumeTest(t)
	if err := writeDeviceCredentialRetirementCheckpoint(paths, cfg); err != nil {
		t.Fatal(err)
	}
	calls := stubDoctorPendingActivation(t, true)

	exit, _ := captureCommandOutput(t, func() int {
		return runDoctor(paths, []string{"--quiet"})
	})
	if exit == 0 {
		t.Fatal("doctor unexpectedly passed with incomplete pending pairing")
	}
	if *calls != 0 {
		t.Fatalf(
			"runDoctor bypassed retirement before pending activation: calls=%d",
			*calls,
		)
	}
}

func setupDoctorPendingResumeTest(
	t *testing.T,
) (runtimePaths, runtimeConfig, []byte, []byte, bool) {
	t.Helper()
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := runtimeConfig{
		ProfileID:            "profile-doctor-retirement",
		RelayBaseURL:         "http://relay:8791",
		PendingSecureBaseURL: "https://relay:8792",
		PendingSpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := readInstallLifecycleGeneration(paths)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, hadSnapshot, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	return paths, cfg, lifecycle, snapshot, hadSnapshot
}

func stubDoctorPendingActivation(
	t *testing.T,
	result bool,
) *int {
	t.Helper()
	calls := 0
	original := resumePendingActivationAfterRetirementCheck
	resumePendingActivationAfterRetirementCheck = func(
		*runtimeConfig,
		func(*runtimeConfig) error,
	) (bool, error) {
		calls++
		return result, nil
	}
	t.Cleanup(func() {
		resumePendingActivationAfterRetirementCheck = original
	})
	return &calls
}
