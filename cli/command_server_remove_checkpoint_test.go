package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestServerRemoveResumesFromEveryDurableBoundary(
	t *testing.T,
) {
	for _, phase := range []string{
		"checkpoint-persisted",
		"pending-revoke-attempted",
		"pending-slot-deleted",
		"current-revoke-attempted",
		"current-slot-deleted",
		"credentials-purged",
	} {
		t.Run(phase, func(t *testing.T) {
			previousConfirmation :=
				readServerRemoveConfirmationForCommand
			t.Cleanup(func() {
				readServerRemoveConfirmationForCommand =
					previousConfirmation
			})
			paths := setupServerCommandTest(
				t,
				testV2TwoProfileConfig,
			)
			if err := secretSet(
				deviceCredentialServiceForProfile("cabin"),
				testProfileCredentialB,
			); err != nil {
				t.Fatal(err)
			}
			pendingCredential := testProfileCredentialB
			if phase == "pending-slot-deleted" {
				pendingCredential = "malformed-pending"
			}
			if err := secretSet(
				deviceCredentialPendingServiceForProfile(
					"cabin",
				),
				pendingCredential,
			); err != nil {
				t.Fatal(err)
			}
			stubServerRevoke(t)
			stubServerCommandStdin(t, "cabin\n")

			previousRemovalHook := serverRemovalPhaseHook
			previousPurgeHook := profilePurgePhaseHook
			serverRemovalPhaseHook = func(got string) error {
				if got == phase {
					return errors.New("simulated crash")
				}
				return nil
			}
			profilePurgePhaseHook = func(
				profile string,
				got string,
			) error {
				if profile == "cabin" && got == phase {
					return errors.New("simulated crash")
				}
				return nil
			}
			t.Cleanup(func() {
				serverRemovalPhaseHook = previousRemovalHook
				profilePurgePhaseHook = previousPurgeHook
			})

			exit, output := captureCommandOutput(
				t,
				func() int {
					return runServerRemove(
						paths,
						[]string{"cabin"},
					)
				},
			)
			if exit != 1 ||
				!strings.Contains(output, "simulated crash") {
				t.Fatalf(
					"phase %s exit=%d output=%s",
					phase,
					exit,
					output,
				)
			}
			doc, err := loadConfigDocument(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			cfg, exists := doc.flatProfile("cabin")
			if !exists ||
				cfg.ServerRemoval == nil ||
				validateServerRemovalCheckpoint(
					"cabin",
					cfg,
				) != nil {
				t.Fatalf(
					"phase %s lost durable inventory: %+v",
					phase,
					cfg,
				)
			}
			setServerSelectionOverride("cabin")
			_, runtimeErr := loadRuntimeConfig(
				paths,
			)
			setServerSelectionOverride("")
			if runtimeErr == nil ||
				!strings.Contains(
					runtimeErr.Error(),
					"ha-nova server remove cabin",
				) {
				t.Fatalf(
					"phase %s runtime gate error=%v",
					phase,
					runtimeErr,
				)
			}
			serverRemovalPhaseHook = func(string) error {
				return nil
			}
			profilePurgePhaseHook = func(
				string,
				string,
			) error {
				return nil
			}
			readServerRemoveConfirmationForCommand = func(
				string,
			) (string, error) {
				t.Fatal(
					"checkpoint resume asked for confirmation again",
				)
				return "", nil
			}
			exit, output = captureCommandOutput(
				t,
				func() int {
					return runServerRemove(
						paths,
						[]string{"cabin"},
					)
				},
			)
			if exit != 0 {
				t.Fatalf(
					"phase %s resume exit=%d output=%s",
					phase,
					exit,
					output,
				)
			}
			if phase == "pending-slot-deleted" &&
				!strings.Contains(output, "stale device entry") {
				t.Fatalf(
					"phase %s resume lost failed-revocation guidance: %s",
					phase,
					output,
				)
			}
			doc, err = loadConfigDocument(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if doc.hasProfile("cabin") {
				t.Fatalf(
					"phase %s resume kept profile",
					phase,
				)
			}
			for _, service := range []string{
				deviceCredentialServiceForProfile("cabin"),
				deviceCredentialPendingServiceForProfile(
					"cabin",
				),
			} {
				if _, exists, err := readCredentialSlot(
					service,
				); err != nil || exists {
					t.Fatalf(
						"phase %s slot %s exists=%v err=%v",
						phase,
						service,
						exists,
						err,
					)
				}
			}
		})
	}
}

func TestServerRemoveCheckpointRejectsReplacementCredential(
	t *testing.T,
) {
	previousConfirmation := readServerRemoveConfirmationForCommand
	t.Cleanup(func() {
		readServerRemoveConfirmationForCommand = previousConfirmation
	})
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		testProfileCredentialB,
	); err != nil {
		t.Fatal(err)
	}
	stubServerCommandStdin(t, "cabin\n")
	previousHook := serverRemovalPhaseHook
	serverRemovalPhaseHook = func(phase string) error {
		if phase == "checkpoint-persisted" {
			return errors.New("simulated crash")
		}
		return nil
	}
	t.Cleanup(func() {
		serverRemovalPhaseHook = previousHook
	})
	if exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	}); exit != 1 ||
		!strings.Contains(output, "simulated crash") {
		t.Fatalf("checkpoint exit=%d output=%s", exit, output)
	}

	// Keep the same device ID and change only the bearer secret. A checkpoint
	// must bind the complete credential, not merely the remote device identity.
	replacement := strings.Replace(
		testProfileCredentialB,
		strings.Repeat("D", 43),
		strings.Repeat("E", 43),
		1,
	)
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		replacement,
	); err != nil {
		t.Fatal(err)
	}
	serverRemovalPhaseHook = func(string) error { return nil }
	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 1 ||
		!strings.Contains(
			output,
			string(CloudErrIdentityMismatch),
		) {
		t.Fatalf("replacement exit=%d output=%s", exit, output)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.hasProfile("cabin") {
		t.Fatal("replacement failure removed profile inventory")
	}
	got, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile("cabin"),
	)
	if err != nil || !exists || got != replacement {
		t.Fatalf(
			"replacement changed: got=%q exists=%v err=%v",
			got,
			exists,
			err,
		)
	}
}

func TestServerRemoveCheckpointRejectsProfileGenerationChange(
	t *testing.T,
) {
	paths := checkpointCabinServerRemoval(t)
	top := readTestConfigTopLevel(t, paths)
	var servers map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	servers["cabin"]["ha_host"] = json.RawMessage(`"other-ha"`)
	serversRaw, err := json.Marshal(servers)
	if err != nil {
		t.Fatal(err)
	}
	top["servers"] = serversRaw
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 1 ||
		!strings.Contains(output, "profile generation mismatch") {
		t.Fatalf("resume exit=%d output=%s", exit, output)
	}
	if _, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile("cabin"),
	); err != nil || !exists {
		t.Fatalf(
			"generation mismatch touched credential: exists=%v err=%v",
			exists,
			err,
		)
	}
}

// A lost key (keyring reset): the command says so before the typed name,
// then removes the profile without a revoke and names the NOVA page step.
func TestServerRemoveLostKeyFinishesAfterTypedName(
	t *testing.T,
) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	revokedAt := stubServerRevoke(t)
	previousConfirmation :=
		readServerRemoveConfirmationForCommand
	readServerRemoveConfirmationForCommand = func(
		string,
	) (string, error) {
		return "cabin", nil
	}
	t.Cleanup(func() {
		readServerRemoveConfirmationForCommand =
			previousConfirmation
	})
	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 0 || len(*revokedAt) != 0 {
		t.Fatalf("remove exit=%d revokes=%v output=%s", exit, *revokedAt, output)
	}
	for _, want := range []string{
		`This computer's HA NOVA key for "cabin" is gone`,
		"click Revoke next to it",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "refusing cleanup") {
		t.Fatalf("technical refusal shown:\n%s", output)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if doc.hasProfile("cabin") || !doc.hasProfile(defaultServerProfileName) {
		t.Fatalf("profiles after removal: %v", doc.profileNames())
	}
}

// The checkpoint records the key as already gone, so a later run (no yes in
// memory) still finishes the removal.
func TestServerRemoveLostKeyResumesFromCheckpoint(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	previousConfirmation, previousHook :=
		readServerRemoveConfirmationForCommand, serverRemovalPhaseHook
	t.Cleanup(func() {
		readServerRemoveConfirmationForCommand, serverRemovalPhaseHook =
			previousConfirmation, previousHook
	})
	readServerRemoveConfirmationForCommand = func(string) (string, error) { return "cabin", nil }
	serverRemovalPhaseHook = func(phase string) error {
		if phase == "checkpoint-persisted" {
			return errors.New("simulated crash")
		}
		return nil
	}
	if exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	}); exit != 1 {
		t.Fatalf("crash run exit=%d output=%s", exit, output)
	}
	serverRemovalPhaseHook = previousHook
	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	doc, err := loadConfigDocument(paths.ConfigFile)
	if exit != 0 || err != nil || doc.hasProfile("cabin") || !strings.Contains(output, "click Revoke next to it") {
		t.Fatalf("resume exit=%d err=%v output=%s", exit, err, output)
	}
}

func TestServerRemoveObservedOnlyCheckpointRejectsLostSlot(
	t *testing.T,
) {
	paths := checkpointCabinServerRemoval(t)
	if err := secretDelete(
		deviceCredentialServiceForProfile("cabin"),
	); err != nil {
		t.Fatal(err)
	}
	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 1 ||
		!strings.Contains(
			output,
			"missing before its cleanup outcome was saved",
		) {
		t.Fatalf("resume exit=%d output=%s", exit, output)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.hasProfile("cabin") {
		t.Fatal("observed-only checkpoint removed the profile")
	}
}
