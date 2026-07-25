package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestSetupPairingRejectsCloudBeforeProbeBrowserOrCodePrompt(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudOnlyCommandConfig(cloudStateAuthorizing)
	cfg.RelayBaseURL = "http://relay:8791"
	previousProbe := probePairingV1ForSetup
	probePairingV1ForSetup = func(string) bool {
		t.Fatal("Cloud guard reached Relay pairing probe")
		return false
	}
	t.Cleanup(func() {
		probePairingV1ForSetup = previousProbe
	})
	var output strings.Builder

	_, err := runSetupPairingFlow(
		bufio.NewReader(strings.NewReader("")),
		&output,
		paths,
		&cfg,
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "Cloud access is configured") {
		t.Fatalf("Cloud-guarded setup error=%v output=%s", err, output.String())
	}
	for _, forbidden := range []string{
		"Open NOVA",
		"Press Enter",
		"Six-digit code",
	} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf(
				"Cloud-guarded setup rendered %q: %s",
				forbidden,
				output.String(),
			)
		}
	}
}
