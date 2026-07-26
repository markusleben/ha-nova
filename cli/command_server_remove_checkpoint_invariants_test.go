package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestServerRemoveRejectsIncompleteCurrentEndpointBeforeCheckpoint(
	t *testing.T,
) {
	tests := []struct {
		name   string
		remove string
	}{
		{
			name:   "missing pin",
			remove: `, "relay_spki_pin": "PINB"`,
		},
		{
			name:   "missing URL",
			remove: `, "relay_secure_base_url": "https://cabin:18792"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := strings.Replace(
				testV2TwoProfileConfig,
				test.remove,
				"",
				1,
			)
			paths := setupServerCommandTest(t, config)
			if err := secretSet(
				deviceCredentialServiceForProfile("cabin"),
				testProfileCredentialB,
			); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			stubServerCommandStdin(t, "cabin\n")
			exit, output := captureCommandOutput(t, func() int {
				return runServerRemove(paths, []string{"cabin"})
			})
			if exit != 1 ||
				!strings.Contains(output, "endpoint") ||
				!strings.Contains(output, "incomplete") {
				t.Fatalf("remove exit=%d output=%s", exit, output)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("incomplete endpoint created a checkpoint")
			}
		})
	}
}

func TestServerRemoveRejectsCredentialAppearingAfterAbsentCheckpoint(
	t *testing.T,
) {
	tests := []struct {
		name    string
		service string
		value   func(t *testing.T) string
	}{
		{
			name:    "current",
			service: deviceCredentialServiceForProfile("cabin"),
			value: func(*testing.T) string {
				return validCredential(201)
			},
		},
		{
			name: "raw pending",
			service: deviceCredentialPendingServiceForProfile(
				"cabin",
			),
			value: func(*testing.T) string {
				return validCredential(202)
			},
		},
		{
			name: "Cloud pending envelope",
			service: deviceCredentialPendingServiceForProfile(
				"cabin",
			),
			value: func(t *testing.T) string {
				encoded, err := json.Marshal(
					pendingDeviceCredentialEnvelope{
						Version: pendingDeviceCredentialEnvelopeVersion,
						Source:  pendingDeviceCredentialSourceCloud,
						Credential: validCredential(
							203,
						),
						RelayInstanceID: "relay-cabin",
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				return string(encoded)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := checkpointCabinRemovalWithoutSlots(t)
			replacement := test.value(t)
			if err := secretSet(
				test.service,
				replacement,
			); err != nil {
				t.Fatal(err)
			}
			exit, output := captureCommandOutput(t, func() int {
				return runServerRemove(paths, []string{"cabin"})
			})
			if exit != 1 ||
				!strings.Contains(output, "appeared after") {
				t.Fatalf("resume exit=%d output=%s", exit, output)
			}
			got, err := secretGet(test.service)
			if err != nil || got != replacement {
				t.Fatalf("replacement got=%q err=%v", got, err)
			}
			doc, err := loadConfigDocument(paths.ConfigFile)
			if err != nil || !doc.hasProfile("cabin") {
				t.Fatalf("profile exists=%v err=%v", doc != nil, err)
			}
		})
	}
}

func TestServerRemoveFinalAbsenceProofPreservesRecreatedCredential(
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
	replacement := validCredential(204)
	serverRemovalPhaseHook = func(phase string) error {
		if phase == "credentials-purged" {
			return secretSet(
				deviceCredentialServiceForProfile("cabin"),
				replacement,
			)
		}
		return nil
	}
	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 1 ||
		!strings.Contains(output, "reappeared after cleanup") {
		t.Fatalf("remove exit=%d output=%s", exit, output)
	}
	got, err := secretGet(
		deviceCredentialServiceForProfile("cabin"),
	)
	if err != nil || got != replacement {
		t.Fatalf("replacement got=%q err=%v", got, err)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil || !doc.hasProfile("cabin") {
		t.Fatalf("profile preserved=%v err=%v", doc != nil, err)
	}
}

func TestFullPurgePersistsServerRemovalOutcomeBeforeSlotDeletion(
	t *testing.T,
) {
	paths := checkpointCabinServerRemoval(t)
	stubServerRevoke(t)
	targets, err := collectProfilePurgeTargets(paths)
	if err != nil {
		t.Fatal(err)
	}
	previousHook := profilePurgePhaseHook
	profilePurgePhaseHook = func(profile string, phase string) error {
		if profile == "cabin" && phase == "current-slot-deleted" {
			return errors.New("simulated full-purge crash")
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
		!strings.Contains(err.Error(), "simulated full-purge crash") {
		t.Fatalf("first purge error = %v", err)
	}
	profilePurgePhaseHook = func(string, string) error { return nil }
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

func checkpointCabinRemovalWithoutSlots(
	t *testing.T,
) runtimePaths {
	t.Helper()
	config := strings.Replace(
		testV2TwoProfileConfig,
		`, "relay_secure_base_url": "https://cabin:18792", "relay_spki_pin": "PINB"`,
		"",
		1,
	)
	paths := setupServerCommandTest(t, config)
	previousConfirmation := readServerRemoveConfirmationForCommand
	previousHook := serverRemovalPhaseHook
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
	t.Cleanup(func() {
		readServerRemoveConfirmationForCommand =
			previousConfirmation
		serverRemovalPhaseHook = previousHook
	})
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
