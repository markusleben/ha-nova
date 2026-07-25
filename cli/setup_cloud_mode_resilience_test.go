package main

import (
	"bufio"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type selectingCloudCoordinator struct {
	result         cloudSetupResult
	preflightCalls int
	localCalls     int
	remoteCalls    int
}

func (c *selectingCloudCoordinator) Available() bool {
	return true
}

func (c *selectingCloudCoordinator) Preflight(
	context.Context,
	string,
) error {
	c.preflightCalls++
	return nil
}

func (c *selectingCloudCoordinator) AddAwayWithExistingDevice(
	_ context.Context,
	request cloudSetupRequest,
) (cloudSetupResult, error) {
	c.localCalls++
	if err := finishSelectingCloudRequest(request, c.result.Current); err != nil {
		return cloudSetupResult{}, err
	}
	return c.result, nil
}

func (c *selectingCloudCoordinator) AddRemoteWithPairing(
	_ context.Context,
	request cloudRemoteSetupRequest,
) (cloudSetupResult, error) {
	c.remoteCalls++
	if request.Origin.CanonicalOrigin == "" || request.PairingCode == nil {
		return cloudSetupResult{}, errors.New("remote setup request is incomplete")
	}
	if err := finishSelectingCloudRequest(
		request.cloudSetupRequest,
		c.result.Current,
	); err != nil {
		return cloudSetupResult{}, err
	}
	return c.result, nil
}

func finishSelectingCloudRequest(
	request cloudSetupRequest,
	metadata cloudConnectionMetadata,
) error {
	if err := request.PersistPendingMetadata(metadata); err != nil {
		return err
	}
	for _, state := range []cloudLifecycleState{
		cloudStateTokenStored,
		cloudStateCloudVerified,
		cloudStateDeviceBoundOrPaired,
	} {
		if err := request.AdvancePendingLifecycle(state); err != nil {
			return err
		}
	}
	return nil
}

func newSelectingCloudCoordinator() *selectingCloudCoordinator {
	return &selectingCloudCoordinator{
		result: cloudSetupResult{
			Current:         cloudMetadataForTest(strings.Repeat("8", 32)),
			RelayInstanceID: "relay-selected",
		},
	}
}

func TestCloudOnlyWizardInvalidOriginRepromptsWithoutCheckpoint(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	paths.ConfigFile += ".missing"
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)

	exit, output := captureCommandOutput(t, func() int {
		return runInteractiveCloudOnlySetup(
			bufio.NewReader(strings.NewReader(
				"http://unsafe.example\nexit\n",
			)),
			os.Stdout,
			paths,
			runtimeConfig{},
			&installState{},
			"",
			nil,
			nil,
		)
	})
	if exit != 0 ||
		!strings.Contains(
			output,
			"Enter the complete HTTPS remote URL shown by Home Assistant.",
		) ||
		strings.Contains(output, "No Cloud checkpoint was saved") ||
		strings.Contains(output, "ha-nova cloud add") ||
		coordinator.remoteCalls != 0 {
		t.Fatalf(
			"origin reprompt exit=%d calls=%d output=%s",
			exit,
			coordinator.remoteCalls,
			output,
		)
	}
}

func TestCloudWizardURLPromptContinuesFromInvalidToValid(t *testing.T) {
	var output strings.Builder
	got, err := promptValidatedCloudRemoteOrigin(
		context.Background(),
		bufio.NewReader(strings.NewReader(
			"http://unsafe.example\n"+productionCloudTestOrigin+"\n",
		)),
		&output,
		"",
		nil,
	)
	if err != nil || got.CanonicalOrigin != productionCloudTestOrigin {
		t.Fatalf("origin=%+v err=%v output=%s", got, err, output.String())
	}
	if strings.Count(
		output.String(),
		"Enter the complete HTTPS remote URL shown by Home Assistant.",
	) != 1 {
		t.Fatalf("URL prompt output=%s", output.String())
	}
}

func TestSetupConnectionModeDefaultsAndRecoversFromInvalidInput(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input string
		want  setupConnectionMode
	}{
		{name: "recommended default", input: "\n", want: setupConnectionLocal},
		{name: "recommended number", input: "1\n", want: setupConnectionLocal},
		{name: "hybrid number", input: "2\n", want: setupConnectionHybrid},
		{name: "local words", input: "local only\n", want: setupConnectionLocal},
		{name: "cloud number", input: "3\n", want: setupConnectionCloud},
		{name: "cloud words", input: "cloud only\n", want: setupConnectionCloud},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var output strings.Builder
			got, err := promptSetupConnectionMode(
				bufio.NewReader(strings.NewReader(testCase.input)),
				&output,
			)
			if err != nil || got != testCase.want {
				t.Fatalf("mode=%q err=%v output=%s", got, err, output.String())
			}
		})
	}

	var output strings.Builder
	got, err := promptSetupConnectionMode(
		bufio.NewReader(strings.NewReader("remote\n9\n1\n")),
		&output,
	)
	if err != nil || got != setupConnectionLocal {
		t.Fatalf("recovered mode=%q err=%v output=%s", got, err, output.String())
	}
	if count := strings.Count(output.String(), "Invalid choice"); count != 2 {
		t.Fatalf("invalid-choice count=%d output=%s", count, output.String())
	}
}

func TestSetupConnectionModeHonorsBackAndExit(t *testing.T) {
	for input, want := range map[string]error{
		"back\n": errSetupBack,
		"exit\n": errSetupExit,
	} {
		var output strings.Builder
		if _, err := promptSetupConnectionMode(
			bufio.NewReader(strings.NewReader(input)),
			&output,
		); !errors.Is(err, want) {
			t.Fatalf("input=%q error=%v, want %v", input, err, want)
		}
	}
}

func TestSetupConnectionModeAppearsOnlyForUnconstrainedFreshSetup(t *testing.T) {
	coordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, coordinator)
	if !shouldOfferSetupConnectionMode(
		runtimeConfig{},
		"",
		"",
		"",
		"",
		false,
	) {
		t.Fatal("fresh interactive setup did not offer a connection mode")
	}
	for name, testCase := range map[string]struct {
		cfg                               runtimeConfig
		host, haURL, relayURL, relayToken string
		service                           bool
	}{
		"service":     {service: true},
		"host flag":   {host: "ha.local"},
		"HA URL flag": {haURL: "http://ha.local:8123"},
		"relay flag":  {relayURL: "http://ha.local:8791"},
		"token flag":  {relayToken: "token"},
		"local state": {cfg: runtimeConfig{HAHost: "ha.local"}},
		"Cloud state": {
			cfg: runtimeConfig{
				Cloud: &cloudLifecycleMetadata{State: cloudStateAuthorizing},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if shouldOfferSetupConnectionMode(
				testCase.cfg,
				testCase.host,
				testCase.haURL,
				testCase.relayURL,
				testCase.relayToken,
				testCase.service,
			) {
				t.Fatal("constrained or resumed setup offered a fresh connection mode")
			}
		})
	}
}

func TestCloudConnectionFlowsSelectLocalOrRemoteCoordinatorMethod(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	localCoordinator := newSelectingCloudCoordinator()
	localCfg := completedLocalCloudTestConfig()
	localCfg.ProfileID = "profile-local-selection"
	localCfg.RelayInstanceID = ""
	localSaved := localCfg
	local, err := connectExistingDeviceToCloud(
		context.Background(),
		paths,
		localCfg,
		localCoordinator,
		false,
		func(value runtimeConfig) error {
			localSaved = value
			return nil
		},
	)
	if err != nil {
		t.Fatalf("local Cloud flow: %v (saved=%+v)", err, localSaved)
	}
	if localCoordinator.localCalls != 1 ||
		localCoordinator.remoteCalls != 0 ||
		local.RoutePolicy != routePolicyAutomatic {
		t.Fatalf("local selection calls=%d/%d result=%+v",
			localCoordinator.localCalls,
			localCoordinator.remoteCalls,
			local,
		)
	}

	remoteCoordinator := newSelectingCloudCoordinator()
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	remoteCoordinator.result.Current.Origin = origin.InputOrigin
	remoteCoordinator.result.Current.CanonicalOrigin = origin.CanonicalOrigin
	remoteCfg := runtimeConfig{
		ProfileID:       "profile-remote-selection",
		ClientInstallID: "install-remote-selection",
	}
	remote, err := connectRemoteToCloud(
		context.Background(),
		paths,
		remoteCfg,
		remoteCoordinator,
		origin,
		func(cloudRemotePairingPrompt) (string, error) {
			return "123456", nil
		},
		false,
		func(runtimeConfig) error { return nil },
	)
	if err != nil {
		t.Fatalf("remote Cloud flow: %v", err)
	}
	if remoteCoordinator.localCalls != 0 ||
		remoteCoordinator.remoteCalls != 1 ||
		remote.RoutePolicy != routePolicyCloud {
		t.Fatalf("remote selection calls=%d/%d result=%+v",
			remoteCoordinator.localCalls,
			remoteCoordinator.remoteCalls,
			remote,
		)
	}
}

func TestCompletedWizardCloudLockContentionLeavesLocalReady(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := successfulCloudCoordinatorForTest()
	installCloudSetupTestSeams(t, coordinator, true, true)
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		t.Fatal("could not acquire competing mutation lock")
	}
	defer release()

	var output strings.Builder
	_, attempted, exit := maybeOfferCloudForCompletedSetup(
		bufio.NewReader(strings.NewReader("y\n")),
		&output,
		paths,
		cfg,
		false,
	)
	if !attempted || exit != 1 {
		t.Fatalf(
			"attempted=%v exit=%d output=%s",
			attempted,
			exit,
			output.String(),
		)
	}
	if coordinator.preflightCalls != 0 || coordinator.addCalls != 0 {
		t.Fatalf(
			"lock contention reached Cloud coordinator: calls=%d/%d",
			coordinator.preflightCalls,
			coordinator.addCalls,
		)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("lock contention changed the working local configuration")
	}
	if !strings.Contains(output.String(), "working local connection was not changed") {
		t.Fatalf("missing local-safety guidance:\n%s", output.String())
	}
}
