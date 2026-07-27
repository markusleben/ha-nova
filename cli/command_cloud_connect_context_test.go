package main

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"testing"
)

func installCloudCommandURLPrompt(
	t *testing.T,
	prompt func(context.Context) (CloudOrigin, error),
) {
	t.Helper()
	old := promptCloudRemoteOriginForCommand
	promptCloudRemoteOriginForCommand = prompt
	t.Cleanup(func() {
		promptCloudRemoteOriginForCommand = old
	})
}

func TestCloudRemoteURLPromptRejectsBlankAndNonHTTPSInput(t *testing.T) {
	reader := bufio.NewReader(
		strings.NewReader(
			"\nhttp://unsafe.example\nhttps://remote.example\n",
		),
	)
	var output strings.Builder
	origin, err := promptCloudRemoteOriginFromReader(
		context.Background(),
		reader,
		&output,
		&fakeCloudResolver{canonical: "unit.ui.nabu.casa."},
	)
	if err != nil {
		t.Fatal(err)
	}
	if origin.InputOrigin != "https://remote.example" ||
		origin.CanonicalOrigin != "https://unit.ui.nabu.casa" {
		t.Fatalf("prompted origin=%+v", origin)
	}
	if strings.Count(
		output.String(),
		"Enter the complete HTTPS remote URL shown by Home Assistant.",
	) != 2 {
		t.Fatalf("prompt output=%s", output.String())
	}
}

func TestCloudConnectUnsafeDesktopContextNeverTouchesStorage(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		reconnect bool
	}{
		{name: "add"},
		{name: "reconnect", reconnect: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			cfg := completedLocalCloudTestConfig()
			cfg.ProfileID = "profile-unsafe-desktop"
			cfg.RelayInstanceID = "relay-unsafe-desktop"
			if testCase.reconnect {
				current := cloudMetadataForTest(strings.Repeat("d", 32))
				cfg.Cloud = &cloudLifecycleMetadata{
					State:   cloudStateReady,
					Current: &current,
				}
			}
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}
			coordinator := successfulCloudCoordinatorForTest()
			installCloudCommandCoordinator(t, coordinator)
			installCloudCommandPromptSession(t, false)

			exit, output := captureCommandOutput(t, func() int {
				return runCloudConnectCommand(
					paths,
					nil,
					testCase.reconnect,
				)
			})
			if exit != 1 ||
				!strings.Contains(output, "requires an interactive desktop session") ||
				!strings.Contains(output, "not SSH, sudo/root, or WSL") {
				t.Fatalf("unsafe connect exit=%d output=%s", exit, output)
			}
			if coordinator.preflightCalls != 0 || coordinator.addCalls != 0 {
				t.Fatalf(
					"unsafe connect reached secure storage: calls=%d/%d",
					coordinator.preflightCalls,
					coordinator.addCalls,
				)
			}
		})
	}
}

func TestCloudOnlyConnectFailureDoesNotClaimLocalConnectionExists(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(
		paths,
		pendingCloudOnlyCommandConfig(cloudStateAuthorizing),
	); err != nil {
		t.Fatal(err)
	}
	installCloudCommandCoordinator(
		t,
		failingRemoteCloudCommandCoordinator{
			err: newCloudError(
				CloudErrSecretStoreLocked,
				"prepare Cloud authorization",
				nil,
			),
		},
	)
	installCloudCommandPromptSession(t, true)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(paths, nil, false)
	})
	if exit != 1 ||
		!strings.Contains(output, `Cloud setup checkpoint saved at "authorizing"`) ||
		!strings.Contains(output, "ha-nova cloud unlock --server default") ||
		!strings.Contains(output, "ha-nova cloud add --server default") ||
		strings.Contains(output, "working local connection") ||
		strings.Contains(output, "The local connection remains available.") {
		t.Fatalf("Cloud-only failure exit=%d output=%s", exit, output)
	}
}

func TestCloudOnlyConnectSuccessDoesNotClaimLocalPreference(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)
	installCloudCommandPromptSession(t, true)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(
			paths,
			[]string{"--url", productionCloudTestOrigin},
			false,
		)
	})
	if exit != 0 ||
		!strings.Contains(
			output,
			"Home Assistant Cloud access is ready. This profile uses Cloud routing.",
		) ||
		strings.Contains(output, "Local access stays preferred.") {
		t.Fatalf("Cloud-only success exit=%d output=%s", exit, output)
	}
	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RoutePolicy != routePolicyCloud {
		t.Fatalf("Cloud-only route policy = %q", saved.RoutePolicy)
	}
}

func TestCloudConnectMissingNamedProfileStartsGuidedURLRecovery(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(paths, completedLocalCloudTestConfig()); err != nil {
		t.Fatal(err)
	}
	installCloudCommandPromptSession(t, true)
	prompted := false
	installCloudCommandURLPrompt(t, func(
		context.Context,
	) (CloudOrigin, error) {
		prompted = true
		return CloudOrigin{}, errSetupExit
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(
			paths,
			[]string{"--server", "cabin"},
			false,
		)
	})
	if exit != 0 || !prompted ||
		!strings.Contains(output, "cancelled before a checkpoint was saved") ||
		strings.Contains(output, "repair config.json") {
		t.Fatalf(
			"guided missing-profile connect exit=%d prompted=%v output=%s",
			exit,
			prompted,
			output,
		)
	}
	setServerSelectionOverride("cabin")
	if _, err := loadSelectedRuntimeConfigUnchecked(paths); !errors.Is(
		err,
		errUnknownServerProfile,
	) {
		t.Fatalf("cancelled guided setup created cabin: %v", err)
	}
}

func TestCloudURLCancelReportsExistingCheckpoint(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := runtimeConfig{
		ClientInstallID: "inst-existing",
		ProfileID:       "profile-existing",
		RoutePolicy:     routePolicyLocal,
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandPromptSession(t, true)
	installCloudCommandURLPrompt(t, func(
		context.Context,
	) (CloudOrigin, error) {
		return CloudOrigin{}, errSetupExit
	})
	exit, output := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(paths, nil, false)
	})
	if exit != 0 ||
		!strings.Contains(output, `saved checkpoint "authorizing"`) ||
		!strings.Contains(output, "Resume with: ha-nova cloud add --server default") ||
		strings.Contains(output, "before a checkpoint was saved") {
		t.Fatalf("checkpoint cancellation exit=%d output=%s", exit, output)
	}
}

func TestCloudURLCancelReportsReconnectAndHeldRecoveryActions(t *testing.T) {
	for name, testCase := range map[string]struct {
		hold        *cloudRecoveryHold
		want        []string
		notExpected []string
	}{
		"reconnect": {
			want: []string{
				"Resume with: ha-nova cloud reconnect --server default",
			},
		},
		"held": {
			hold: &cloudRecoveryHold{
				Code:        cloudProblemSecureStorage,
				Remediation: cloudRemediationVerifyState,
			},
			want: []string{
				"ha-nova cloud unlock --server default",
				"ha-nova cloud remove --server default",
			},
			notExpected: []string{"cloud add", "cloud reconnect"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			cfg := completedLocalCloudTestConfig()
			cfg.RelayInstanceID = "relay-instance-1"
			cfg.Cloud = &cloudLifecycleMetadata{
				State: cloudStateAuthorizing,
				Current: &cloudConnectionMetadata{
					Origin:               "https://example.ui.nabu.casa",
					CanonicalOrigin:      "https://example.ui.nabu.casa",
					OAuthClientID:        "http://127.0.0.1:49152/ha-nova",
					CredentialGeneration: strings.Repeat("a", 32),
					HAUserID:             "user-1",
				},
				RecoveryHold: testCase.hold,
			}
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}

			exit, output := captureCommandOutput(t, func() int {
				printCloudURLPromptCancellation(paths)
				return 0
			})
			if exit != 0 {
				t.Fatalf("cancellation exit = %d", exit)
			}
			for _, expected := range testCase.want {
				if !strings.Contains(output, expected) {
					t.Fatalf("cancellation output missing %q: %s", expected, output)
				}
			}
			for _, unexpected := range testCase.notExpected {
				if strings.Contains(output, unexpected) {
					t.Fatalf("cancellation output contains %q: %s", unexpected, output)
				}
			}
		})
	}
}

func TestCloudURLCancelWithInvalidSelectionPrintsNoMutationCommand(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "pending",
		"servers": {
			"pending": {
				"profile_id": "profile-pending",
				"route_policy": "cloud",
				"cloud": {"state": "authorizing"}
			}
		}
	}`)

	_, output := captureCommandOutput(t, func() int {
		printCloudURLPromptCancellation(paths)
		return 0
	})
	for _, mutation := range []string{
		"cloud add",
		"cloud reconnect",
		"cloud unlock",
		"cloud remove",
	} {
		if strings.Contains(output, mutation) {
			t.Fatalf("invalid selection exposed %q: %s", mutation, output)
		}
	}
}

func TestCloudCommandUsesPromptResolvedOriginWithoutResolvingAgain(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandURLPrompt(t, func(
		context.Context,
	) (CloudOrigin, error) {
		return CloudOrigin{
			InputOrigin:     "https://custom.example.com",
			InputHost:       "custom.example.com",
			CanonicalOrigin: productionCloudTestOrigin,
			CanonicalHost:   "unit.ui.nabu.casa",
			CustomDomain:    true,
		}, nil
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(paths, nil, false)
	})
	if exit != 0 || coordinator.remoteCalls != 1 {
		t.Fatalf(
			"resolved-origin command exit=%d calls=%d output=%s",
			exit,
			coordinator.remoteCalls,
			output,
		)
	}
}

func TestCloudUnlockHeadlessNeverTouchesSecureStorage(t *testing.T) {
	coordinator := successfulCloudCoordinatorForTest()
	installCloudCommandCoordinator(t, coordinator)
	oldInputTTY := uiInputSupportsTTY
	uiInputSupportsTTY = func() bool { return true }
	t.Cleanup(func() { uiInputSupportsTTY = oldInputTTY })

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(runtimePaths{}, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, "use a local, non-elevated graphical desktop terminal") {
		t.Fatalf("headless unlock exit=%d output=%s", exit, output)
	}
	if coordinator.preflightCalls != 0 || coordinator.addCalls != 0 {
		t.Fatalf(
			"headless unlock reached secure storage: calls=%d/%d",
			coordinator.preflightCalls,
			coordinator.addCalls,
		)
	}
}
