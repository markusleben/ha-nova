package main

import (
	"errors"
	"strings"
	"testing"
)

func TestCloudAddExplicitServerSeedsMissingProfileAndPreservesSibling(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	original := completedLocalCloudTestConfig()
	original.ProfileID = "profile-default-existing"
	original.ClientInstallID = "inst-existing"
	if err := saveConfig(paths, original); err != nil {
		t.Fatal(err)
	}
	setServerSelectionOverride("cabin")
	setActiveServerProfile("cabin")
	options := cloudCommandFlags{
		server: "cabin",
		url:    productionCloudTestOrigin,
	}

	seeded, err := loadCloudConnectConfig(paths, options, false)
	if err != nil {
		t.Fatalf("seed explicit Cloud profile: %v", err)
	}
	if seeded.Cloud != nil || seeded.RoutePolicy != routePolicyLocal {
		t.Fatalf("seeded Cloud profile = %+v", seeded)
	}
	if seeded.ClientInstallID != original.ClientInstallID {
		t.Fatalf(
			"seeded client_install_id=%q want %q",
			seeded.ClientInstallID,
			original.ClientInstallID,
		)
	}
	if seeded.ProfileID == "" {
		t.Fatal("seeded Cloud profile is missing its stable profile_id")
	}
	seededProfileID := seeded.ProfileID
	if err := saveConfig(paths, seeded); err != nil {
		t.Fatal(err)
	}

	setServerSelectionOverride(defaultServerProfileName)
	setActiveServerProfile(defaultServerProfileName)
	savedDefault, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if savedDefault.ProfileID != original.ProfileID ||
		savedDefault.RelaySecureBaseURL != original.RelaySecureBaseURL ||
		savedDefault.ClientInstallID != original.ClientInstallID {
		t.Fatalf("explicit Cloud profile changed default sibling: %+v", savedDefault)
	}
	setServerSelectionOverride("cabin")
	setActiveServerProfile("cabin")
	savedCabin, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if savedCabin.ProfileID != seededProfileID {
		t.Fatalf("new Cloud profile = %+v", savedCabin)
	}
}

func TestCloudReconnectAndImplicitSelectionNeverCreateMissingProfile(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		options   cloudCommandFlags
		reconnect bool
	}{
		{
			name: "reconnect",
			options: cloudCommandFlags{
				server: "cabin",
				url:    productionCloudTestOrigin,
			},
			reconnect: true,
		},
		{
			name: "implicit selection",
			options: cloudCommandFlags{
				url: productionCloudTestOrigin,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			if err := saveConfig(
				paths,
				completedLocalCloudTestConfig(),
			); err != nil {
				t.Fatal(err)
			}
			setServerSelectionOverride("cabin")
			setActiveServerProfile("cabin")

			_, err := loadCloudConnectConfig(
				paths,
				testCase.options,
				testCase.reconnect,
			)
			if !errors.Is(err, errUnknownServerProfile) {
				t.Fatalf("missing profile error = %v", err)
			}
		})
	}
}

func TestNamedCloudPreflightFailurePersistsScopedIdentityWithoutRotatingInstall(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(
		t,
		failingRemoteCloudCommandCoordinator{
			err: errDesktopKeyringLocked,
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(
			paths,
			[]string{
				"--server", "cabin-new",
				"--url", productionCloudTestOrigin,
			},
			false,
		)
	})
	if exit != 1 ||
		!strings.Contains(
			output,
			"ha-nova cloud unlock --server cabin-new",
		) {
		t.Fatalf("preflight failure exit=%d output=%s", exit, output)
	}

	setServerSelectionOverride("cabin-new")
	partial, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if partial.ProfileID == "" ||
		partial.ClientInstallID != "inst-abc" ||
		partial.Cloud != nil {
		t.Fatalf("partial named profile=%+v", partial)
	}
	setServerSelectionOverride(defaultServerProfileName)
	original, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if original.ClientInstallID != "inst-abc" {
		t.Fatalf("default client_install_id=%q", original.ClientInstallID)
	}
}
