package main

import (
	"os"
	"strings"
	"testing"
)

func TestServerRenameRejectsCloudProfileBeforeCredentialMutation(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	setServerSelectionOverride("cabin")
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-cabin"
	cfg.RelayInstanceID = "relay-cabin"
	current := cloudMetadataForTest(strings.Repeat("c", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runServerRename(paths, []string{"cabin", "renamed"})
	})
	if exit != 1 ||
		!strings.Contains(output, "cloud remove --server cabin") {
		t.Fatalf("rename exit=%d output=%s", exit, output)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("rejected Cloud profile rename changed config")
	}
}
