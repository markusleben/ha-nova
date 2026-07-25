package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestGuidedTeardownDerivesRetirementEvidenceFromExactCheckpoint(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths, previous, _ := pendingRetirementUninstallFixture(t)
	var probed runtimeConfig
	deviceAnswers := true
	oldVerify := verifyDeviceHealth
	verifyDeviceHealth = func(cfg runtimeConfig) bool {
		probed = cfg
		return deviceAnswers
	}
	t.Cleanup(func() {
		verifyDeviceHealth = oldVerify
	})

	preflight := collectUninstallPreflight(paths)
	if !preflight.relayStillRunning {
		t.Fatal("checkpoint endpoint was not used for pre-removal health")
	}
	if probed.RelayInstanceID != previous.RelayInstanceID ||
		probed.RelaySecureBaseURL != previous.RelaySecureBaseURL ||
		probed.RelaySpkiPin != previous.RelaySpkiPin {
		t.Fatalf("checkpoint probe config = %+v", probed)
	}
	preflight.haURL = "http://ha.local:8123"
	deviceAnswers = false
	recorder := &teardownRecorder{}
	var output bytes.Buffer
	outcome, err := maybeOfferGuidedTeardown(
		bufio.NewReader(strings.NewReader("y\n\n\n\n\n\n\n")),
		&output,
		preflight,
		recorder.deps(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != teardownCompleted ||
		!strings.Contains(output.String(), "Relay no longer answers") {
		t.Fatalf("teardown outcome=%v output=%s", outcome, output.String())
	}
	evidence := uninstallRelayRemovalEvidenceFromPreflight(
		preflight,
		outcome == teardownCompleted,
	)
	if !evidence.matches(
		defaultServerProfileName,
		previous.RelayInstanceID,
	) {
		t.Fatalf("retirement evidence = %v", evidence)
	}

	revokeCalls := 0
	oldRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return errors.New("deleted Relay must not be contacted")
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForRetire = oldRevoke
	})
	if err := settleDeviceCredentialRetirementsForPurge(
		paths,
		&uninstallReport{},
		evidence,
	); err != nil {
		t.Fatal(err)
	}
	if revokeCalls != 0 {
		t.Fatalf("deleted Relay received %d revoke attempt(s)", revokeCalls)
	}
}

func TestGuidedTeardownRejectsInvalidRetirementEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		corrupt func(t *testing.T, paths runtimePaths)
	}{
		{
			name: "corrupt checkpoint",
			corrupt: func(t *testing.T, paths runtimePaths) {
				t.Helper()
				path, err :=
					deviceCredentialRetirementCheckpointPathForProfile(
						paths,
						defaultServerProfileName,
					)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mismatched profile identity",
			corrupt: func(t *testing.T, paths runtimePaths) {
				t.Helper()
				checkpoint, exists, err :=
					readDeviceCredentialRetirementCheckpointForProfile(
						paths,
						defaultServerProfileName,
					)
				if err != nil || !exists {
					t.Fatalf(
						"checkpoint: exists=%v err=%v",
						exists,
						err,
					)
				}
				checkpoint.ProfileID = "profile-changed"
				path, err :=
					deviceCredentialRetirementCheckpointPathForProfile(
						paths,
						defaultServerProfileName,
					)
				if err != nil {
					t.Fatal(err)
				}
				if err := writeJSONFile(path, checkpoint, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths, previous, _ := pendingRetirementUninstallFixture(t)
			testCase.corrupt(t, paths)
			oldVerify := verifyDeviceHealth
			verifyDeviceHealth = func(runtimeConfig) bool {
				return false
			}
			t.Cleanup(func() {
				verifyDeviceHealth = oldVerify
			})

			preflight := collectUninstallPreflight(paths)
			if preflight.teardownVerificationProblem == "" {
				t.Fatal("invalid checkpoint did not block verification")
			}
			if uninstallRelayRemovalEvidenceFromPreflight(
				preflight,
				true,
			).matches(defaultServerProfileName, previous.RelayInstanceID) {
				t.Fatal("invalid checkpoint yielded Relay removal evidence")
			}
			preflight.haURL = "http://ha.local:8123"
			recorder := &teardownRecorder{relayGone: true}
			var output bytes.Buffer
			outcome, err := maybeOfferGuidedTeardown(
				bufio.NewReader(strings.NewReader("")),
				&output,
				preflight,
				recorder.deps(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if outcome != teardownNotOffered ||
				len(recorder.opened) != 0 ||
				!strings.Contains(
					output.String(),
					"Guided server-side removal is paused",
				) {
				t.Fatalf(
					"invalid checkpoint outcome=%v opened=%v output=%s",
					outcome,
					recorder.opened,
					output.String(),
				)
			}
		})
	}
}
