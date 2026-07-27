package main

import (
	"errors"
	"strings"
	"testing"
)

func TestExistingReservedProfileCannotBecomeActive(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "pending",
		"client_install_id": "install-1",
		"servers": {
			"pending": {
				"profile_id": "profile-pending",
				"route_policy": "local"
			}
		}
	}`)

	_, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved selected profile error = %v", err)
	}
	if activeServerProfile() != defaultServerProfileName {
		t.Fatalf("active profile changed to reserved name %q", activeServerProfile())
	}
}

func TestSetupRejectsExistingReservedDefaultBeforeCloudResume(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "pending",
		"client_install_id": "install-1",
		"servers": {
			"pending": {
				"profile_id": "profile-pending",
				"route_policy": "cloud",
				"cloud": {
					"state": "authorizing"
				}
			}
		}
	}`)

	exit, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{"--non-interactive"})
	})
	if exit != 1 || !strings.Contains(output, "reserved") {
		t.Fatalf("reserved setup exit=%d output=%s", exit, output)
	}
	if activeServerProfile() != defaultServerProfileName {
		t.Fatalf("setup selected reserved profile %q", activeServerProfile())
	}
}

func TestExistingInvalidEnvironmentProfileCannotBecomeActive(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "default",
		"client_install_id": "install-1",
		"servers": {
			"default": {
				"profile_id": "profile-default",
				"route_policy": "local"
			},
			"BAD PROFILE": {
				"profile_id": "profile-bad",
				"route_policy": "local"
			}
		}
	}`)
	t.Setenv(serverSelectionEnvVar, "BAD PROFILE")

	_, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err == nil || !strings.Contains(err.Error(), "invalid server profile") {
		t.Fatalf("invalid selected profile error = %v", err)
	}
	if activeServerProfile() != defaultServerProfileName {
		t.Fatalf("active profile changed to invalid name %q", activeServerProfile())
	}
}

func TestEnvironmentOnlyUnknownProfileNeverGetsCreationGuidance(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV1FlatConfig)
	t.Setenv(serverSelectionEnvVar, "typo")

	var output strings.Builder
	renderDurableCloudRecoveryGuidance(
		&output,
		paths,
		&cloudProblem{Remediation: cloudRemediationRetry},
	)
	if !strings.Contains(output.String(), "does not exist") ||
		!strings.Contains(output.String(), "Repair default_server") ||
		strings.Contains(output.String(), "cloud add --server typo") {
		t.Fatalf("environment typo guidance = %s", output.String())
	}
}

func TestExplicitUnknownProfileKeepsGuidedCreationPath(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV1FlatConfig)
	setServerSelectionOverride("cabin")

	var output strings.Builder
	renderDurableCloudRecoveryGuidance(
		&output,
		paths,
		&cloudProblem{Remediation: cloudRemediationRetry},
	)
	if !strings.Contains(output.String(), "cloud add --server cabin") {
		t.Fatalf("explicit creation guidance = %s", output.String())
	}
}

func TestSaveRejectsExistingReservedTargetProfile(t *testing.T) {
	resetServerProfileSelection(t)
	doc, err := loadConfigDocument(
		writeTestConfigFile(t, `{
			"schema_version": 3,
			"default_server": "pending",
			"servers": {"pending": {"route_policy": "local"}}
		}`).ConfigFile,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveTargetProfileName(doc); err == nil ||
		!strings.Contains(err.Error(), "reserved") {
		t.Fatalf("save target error = %v", err)
	}
}

func TestUnknownProfileErrorStillClassifiesForExplicitCreation(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV1FlatConfig)
	setServerSelectionOverride("cabin")
	_, err := loadSelectedRuntimeConfigUnchecked(paths)
	if !errors.Is(err, errUnknownServerProfile) {
		t.Fatalf("unknown explicit profile error = %v", err)
	}
}
