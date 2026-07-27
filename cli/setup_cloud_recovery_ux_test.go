package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

func TestCompletedSetupExplainsLockedLocalCredentialStore(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	coordinator := successfulCloudCoordinatorForTest()
	installCloudSetupTestSeams(t, coordinator, true, true)
	reusableLocalDeviceForCloudSetup = func(runtimeConfig) (bool, error) {
		return false, errDesktopKeyringLocked
	}

	attempted := false
	code := -1
	_, output := captureCommandOutput(t, func() int {
		_, attempted, code = maybeOfferCloudForCompletedSetup(
			bufio.NewReader(strings.NewReader("y\n")),
			os.Stdout,
			paths,
			cfg,
			false,
		)
		return code
	})
	if attempted || code != 0 ||
		coordinator.preflightCalls != 0 || coordinator.addCalls != 0 {
		t.Fatalf(
			"attempted=%v code=%d calls=%d/%d",
			attempted,
			code,
			coordinator.preflightCalls,
			coordinator.addCalls,
		)
	}
	if !strings.Contains(output, string(cloudRemediationUnlockStorage)) ||
		!strings.Contains(output, "ha-nova cloud unlock --server default") ||
		!strings.Contains(output, "local connection was not changed") {
		t.Fatalf("missing keyring recovery guidance: %s", output)
	}
}

func TestCompletedSetupCommittedResumeClassifiesNativeKeyringFailure(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-resume-locked"
	cfg.RelayInstanceID = "relay-instance-1"
	metadata := cloudMetadataForTest(strings.Repeat("6", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateCommitted,
		Current: &metadata,
		Pending: &metadata,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	coordinator := successfulCloudCoordinatorForTest()
	coordinator.preflightErr = errDesktopKeyringLocked
	installCloudSetupTestSeams(t, coordinator, false, true)

	attempted := false
	code := -1
	_, output := captureCommandOutput(t, func() int {
		_, attempted, code = maybeOfferCloudForCompletedSetup(
			bufio.NewReader(strings.NewReader("")),
			os.Stdout,
			paths,
			cfg,
			false,
		)
		return code
	})
	if !attempted || code != 1 ||
		!strings.Contains(output, string(cloudProblemSecureStorage)) ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) ||
		!strings.Contains(output, "ha-nova cloud unlock --server default") ||
		!strings.Contains(output, `checkpoint saved at "committed"`) {
		t.Fatalf(
			"committed recovery attempted=%v code=%d output=%s",
			attempted,
			code,
			output,
		)
	}
}
