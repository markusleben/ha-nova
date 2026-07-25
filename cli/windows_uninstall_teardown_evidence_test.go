package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsUninstallTeardownEvidenceFailsClosed(t *testing.T) {
	enableWindowsUninstallStatusChecks(t)

	for _, testCase := range []struct {
		name string
		raw  string
	}{
		{
			name: "evidence without completed teardown",
			raw: `{
				"status": "failed",
				"mode": "purge",
				"guided_teardown_relay_removal": {
					"profile_name": "default",
					"relay_instance_id": "relay-guided"
				}
			}`,
		},
		{
			name: "unsupported profile",
			raw: `{
				"status": "failed",
				"mode": "purge",
				"guided_teardown_completed": true,
				"guided_teardown_relay_removal": {
					"profile_name": "cabin",
					"relay_instance_id": "relay-guided"
				}
			}`,
		},
		{
			name: "invalid Relay identity",
			raw: `{
				"status": "failed",
				"mode": "purge",
				"guided_teardown_completed": true,
				"guided_teardown_relay_removal": {
					"profile_name": "default",
					"relay_instance_id": " relay-guided "
				}
			}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			paths := runtimePaths{
				UninstallStatusFile: filepath.Join(
					t.TempDir(),
					"uninstall-status.json",
				),
			}
			if err := os.WriteFile(
				paths.UninstallStatusFile,
				[]byte(testCase.raw),
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			inspection := inspectWindowsUninstallStatus(paths)
			if inspection.Kind != windowsUninstallStatusKindCorrupt {
				t.Fatalf(
					"malformed evidence kind = %q, want corrupt",
					inspection.Kind,
				)
			}
			if _, err := loadWindowsUninstallStatus(paths); err == nil {
				t.Fatal("malformed evidence loaded successfully")
			}
		})
	}
}

func TestWindowsUninstallTeardownEvidenceMatchesOnlyExactRelay(
	t *testing.T,
) {
	status := windowsUninstallStatus{
		GuidedTeardownCompleted: true,
		GuidedTeardownRelayRemoval: &windowsUninstallRelayRemovalRef{
			ProfileName:     defaultServerProfileName,
			RelayInstanceID: "relay-guided",
		},
	}
	teardownDone, removedRelays, err :=
		windowsUninstallTeardownEvidence(status)
	if err != nil {
		t.Fatal(err)
	}
	if !teardownDone ||
		!removedRelays.matches(
			defaultServerProfileName,
			"relay-guided",
		) {
		t.Fatalf(
			"exact evidence did not rehydrate: done=%t evidence=%#v",
			teardownDone,
			removedRelays,
		)
	}
	if removedRelays.matches(
		defaultServerProfileName,
		"relay-current",
	) {
		t.Fatal("stale evidence matched a different Relay identity")
	}
	if removedRelays.matches("cabin", "relay-guided") {
		t.Fatal("default evidence matched a sibling profile")
	}
}

func TestLaunchWindowsUninstallPersistsEvidenceBeforeHelperStarts(
	t *testing.T,
) {
	root := t.TempDir()
	paths := runtimePaths{
		InstallRoot: filepath.Join(root, "missing-install-root"),
		UninstallStatusFile: filepath.Join(
			root,
			"state",
			"uninstall-status.json",
		),
	}
	err := launchWindowsUninstall(
		paths,
		uninstallModePurge,
		true,
		uninstallRelayRemovalEvidence{
			defaultServerProfileName: "relay-guided",
		},
	)
	if err == nil {
		t.Fatal("launch unexpectedly succeeded without a helper binary")
	}

	status, loadErr := loadWindowsUninstallStatus(paths)
	if loadErr != nil {
		t.Fatalf("load persisted recovery status: %v", loadErr)
	}
	if status.Status != windowsUninstallStatusFailed {
		t.Fatalf("launch failure status = %q, want failed", status.Status)
	}
	teardownDone, removedRelays, evidenceErr :=
		windowsUninstallTeardownEvidence(status)
	if evidenceErr != nil {
		t.Fatalf("rehydrate persisted evidence: %v", evidenceErr)
	}
	if !teardownDone ||
		!removedRelays.matches(
			defaultServerProfileName,
			"relay-guided",
		) {
		t.Fatalf(
			"launch failure lost evidence: done=%t evidence=%#v",
			teardownDone,
			removedRelays,
		)
	}
}

func TestWindowsUninstallHelperRejectsUnboundRelayEvidence(t *testing.T) {
	if _, err := windowsUninstallHelperTeardownEvidence(
		false,
		"relay-guided",
	); err == nil {
		t.Fatal("helper accepted Relay evidence without completed teardown")
	}
	if _, err := windowsUninstallHelperTeardownEvidence(
		true,
		" relay-guided ",
	); err == nil {
		t.Fatal("helper accepted malformed Relay evidence")
	}
}
