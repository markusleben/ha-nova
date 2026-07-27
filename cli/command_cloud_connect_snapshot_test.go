package main

import (
	"context"
	"testing"
)

func TestCloudConnectDoesNotUseURLAfterConfigChangesDuringPrompt(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)
	installCloudCommandPromptSession(t, true)
	installCloudCommandURLPrompt(t, func(
		context.Context,
	) (CloudOrigin, error) {
		top := readTestConfigTopLevel(t, paths)
		top["concurrent_edit"] = []byte(`true`)
		if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
			t.Fatal(err)
		}
		return cloudOriginFromCanonical(productionCloudTestOrigin)
	})

	exit, _ := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(paths, nil, false)
	})
	if exit != 1 {
		t.Fatalf("config-drift connect exit=%d", exit)
	}
	if coordinator.preflightCalls != 0 || coordinator.remoteCalls != 0 {
		t.Fatalf(
			"config drift reached Cloud: preflight=%d remote=%d",
			coordinator.preflightCalls,
			coordinator.remoteCalls,
		)
	}
	top := readTestConfigTopLevel(t, paths)
	if string(top["concurrent_edit"]) != "true" {
		t.Fatalf("concurrent config edit was overwritten: %v", top)
	}
}
