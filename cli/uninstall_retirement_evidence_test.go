package main

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRetirementRevokesBeforeTemporaryPostRemovalOutage(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths, previous, _ := pendingRetirementUninstallFixture(t)
	revokeCalls := 0
	oldRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		revokeCalls++
		return nil
	}
	t.Cleanup(func() {
		revokeSelfDeviceV1ForRetire = oldRevoke
	})
	if err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	); err != nil {
		t.Fatal(err)
	}
	if revokeCalls != 1 {
		t.Fatalf("retirement revokes before walkthrough = %d", revokeCalls)
	}
	if _, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths); err != nil ||
		exists {
		t.Fatalf("checkpoint remains: exists=%v err=%v", exists, err)
	}

	preflight := collectUninstallPreflight(paths)
	preflight.haURL = "http://ha.local:8123"
	preflight.relayToken = "legacy-token"
	recorder := &teardownRecorder{relayGone: true}
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
	if outcome != teardownCompleted {
		t.Fatalf("teardown outcome=%v output=%s", outcome, output.String())
	}
	if recorder.healthProbes != 1 ||
		!strings.Contains(output.String(), "Relay no longer answers") {
		t.Fatalf(
			"temporary post-removal outage was not exercised: probes=%d output=%s",
			recorder.healthProbes,
			output.String(),
		)
	}
	evidence := uninstallRelayRemovalEvidenceFromPreflight(
		preflight,
		outcome == teardownCompleted,
	)
	if evidence.matches(
		defaultServerProfileName,
		previous.RelayInstanceID,
	) {
		t.Fatalf("post-removal outage recreated retirement evidence = %v", evidence)
	}
	if err := settleDeviceCredentialRetirementsForPurge(
		paths,
		&uninstallReport{},
	); err != nil {
		t.Fatal(err)
	}
	if revokeCalls != 1 {
		t.Fatalf("retirement was retried after walkthrough: %d", revokeCalls)
	}
}

func TestGuidedPreflightRejectsInvalidRetirementBeforeTeardown(t *testing.T) {
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
			err := prepareUninstallBeforeGuidedTeardown(
				paths,
				uninstallModePurge,
			)
			if err == nil ||
				!strings.Contains(err.Error(), "before Home Assistant removal") {
				t.Fatalf("invalid checkpoint preflight error = %v", err)
			}
			if uninstallRelayRemovalEvidenceFromPreflight(
				collectUninstallPreflight(paths),
				true,
			).matches(defaultServerProfileName, previous.RelayInstanceID) {
				t.Fatal("invalid retirement yielded Relay removal evidence")
			}
		})
	}
}
