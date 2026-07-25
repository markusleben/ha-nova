package main

import (
	"strings"
	"testing"
)

func TestServerRemoveBlocksPendingDeviceRetirement(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, exists := doc.flatProfile("cabin")
	if !exists {
		t.Fatal("cabin profile missing")
	}
	resetServerProfileSelection(t)
	setActiveServerProfile("cabin")
	if err := writeDeviceCredentialRetirementCheckpoint(
		paths,
		cfg,
	); err != nil {
		t.Fatal(err)
	}
	setActiveServerProfile(defaultServerProfileName)

	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 1 ||
		!strings.Contains(output, "pending device retirement") ||
		!strings.Contains(
			output,
			"ha-nova setup --server cabin",
		) ||
		!strings.Contains(output, "Nothing was removed") {
		t.Fatalf("server remove exit=%d output=%q", exit, output)
	}
}
