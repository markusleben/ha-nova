package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCloudStatusReportsInstallRecoveryInspectionFailure(
	t *testing.T,
) {
	paths, _, _, _ := cloudRemoveCommandFixture(t)
	corruptClientInstallID(t, paths)
	top := readTestConfigTopLevel(t, paths)
	var servers map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	delete(servers[defaultServerProfileName], "cloud")
	serversRaw, err := json.Marshal(servers)
	if err != nil {
		t.Fatal(err)
	}
	top["servers"] = serversRaw
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
	previousLoad := loadConfigDocumentForInvalidInstallRecovery
	loadConfigDocumentForInvalidInstallRecovery = func(
		string,
	) (*configDocument, error) {
		return nil, errors.New("simulated recovery read failure")
	}
	t.Cleanup(func() {
		loadConfigDocumentForInvalidInstallRecovery = previousLoad
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatal(err)
	}
	if exit != 1 ||
		summary.NextCommand != "" ||
		summary.VerificationError == nil ||
		!strings.Contains(
			summary.VerificationError.Detail,
			"simulated recovery read failure",
		) {
		t.Fatalf("status exit=%d summary=%+v", exit, summary)
	}
}

func TestCloudRemovePausesWhenInstallRecoveryCannotBeInspected(
	t *testing.T,
) {
	paths, store, backend, _ := cloudRemoveCommandFixture(t)
	corruptClientInstallID(t, paths)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			return nil
		},
	)
	resetProductionCloudPolicies(backend)
	previousLoad := loadConfigDocumentForInvalidInstallRecovery
	loadConfigDocumentForInvalidInstallRecovery = func(
		string,
	) (*configDocument, error) {
		return nil, errors.New("simulated post-remove read failure")
	}
	t.Cleanup(func() {
		loadConfigDocumentForInvalidInstallRecovery = previousLoad
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 ||
		!strings.Contains(output, "recovery is paused") ||
		!strings.Contains(output, "simulated post-remove read failure") ||
		strings.Contains(output, "Local access remains ready") ||
		strings.Contains(output, "now needs a local pairing") {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
}
