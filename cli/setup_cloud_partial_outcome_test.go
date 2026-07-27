package main

import (
	"strings"
	"testing"
)

func TestSelectedHybridCloudFailureIsNotReportedAsComplete(t *testing.T) {
	var output strings.Builder
	exit := renderSetupCompletionOutcome(
		&output,
		[]string{"codex"},
		true,
	)
	if exit == 0 {
		t.Fatal("selected hybrid Cloud failure returned success")
	}
	if strings.Contains(output.String(), "Setup complete!") {
		t.Fatalf("partial hybrid setup claimed full success: %s", output.String())
	}
	for _, required := range []string{
		"Setup incomplete",
		"Local access and skills are ready",
		"Cloud connection is not",
		"Re-run setup",
	} {
		if !strings.Contains(output.String(), required) {
			t.Fatalf("partial hybrid setup missing %q: %s", required, output.String())
		}
	}
}
