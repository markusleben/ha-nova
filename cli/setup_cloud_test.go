package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type fakeCloudSetupCoordinator struct {
	available      bool
	preflightErr   error
	addErr         error
	result         cloudSetupResult
	preflightCalls int
	addCalls       int
	request        cloudSetupRequest
	preflightID    string
	afterPersist   func()
}

func (f *fakeCloudSetupCoordinator) Available() bool {
	return f.available
}

func (f *fakeCloudSetupCoordinator) Preflight(_ context.Context, profileID string) error {
	f.preflightCalls++
	f.preflightID = profileID
	return f.preflightErr
}

func (f *fakeCloudSetupCoordinator) AddAwayWithExistingDevice(_ context.Context, request cloudSetupRequest) (cloudSetupResult, error) {
	f.addCalls++
	f.request = request
	if f.addErr != nil {
		return cloudSetupResult{}, f.addErr
	}
	if err := request.PersistPendingMetadata(f.result.Current); err != nil {
		return cloudSetupResult{}, err
	}
	if f.afterPersist != nil {
		f.afterPersist()
	}
	if err := request.AdvancePendingLifecycle(cloudStateTokenStored); err != nil {
		return cloudSetupResult{}, err
	}
	if err := request.AdvancePendingLifecycle(cloudStateCloudVerified); err != nil {
		return cloudSetupResult{}, err
	}
	if err := request.AdvancePendingLifecycle(cloudStateDeviceBoundOrPaired); err != nil {
		return cloudSetupResult{}, err
	}
	return f.result, nil
}

func installCloudSetupTestSeams(t *testing.T, coordinator cloudSetupCoordinator, reusable, promptEligible bool) {
	t.Helper()
	oldCoordinator := cloudCoordinatorForSetup
	oldReusable := reusableLocalDeviceForCloudSetup
	oldPromptEligible := cloudSetupPromptEligible
	cloudCoordinatorForSetup = coordinator
	reusableLocalDeviceForCloudSetup = func(runtimeConfig) (bool, error) { return reusable, nil }
	cloudSetupPromptEligible = func(io.Writer) bool { return promptEligible }
	t.Cleanup(func() {
		cloudCoordinatorForSetup = oldCoordinator
		reusableLocalDeviceForCloudSetup = oldReusable
		cloudSetupPromptEligible = oldPromptEligible
	})
}

func completedLocalCloudTestConfig() runtimeConfig {
	return runtimeConfig{
		HAHost:             "ha",
		HAURL:              "http://ha:8123",
		RelayBaseURL:       "http://ha:8791",
		RelaySecureBaseURL: "https://ha:18792",
		RelaySpkiPin:       "PIN",
		RoutePolicy:        routePolicyLocal,
	}
}

func successfulCloudCoordinatorForTest() *fakeCloudSetupCoordinator {
	return &fakeCloudSetupCoordinator{
		available: true,
		result: cloudSetupResult{
			Current:         cloudMetadataForTest(strings.Repeat("f", 32)),
			RelayInstanceID: "relay-instance-1",
		},
	}
}

func TestCompletedSetupAddsCloudByReusingExistingDevice(t *testing.T) {
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

	var out strings.Builder
	reader := bufio.NewReader(strings.NewReader("y\n"))
	handled, code := maybeHandleInteractiveSetupCurrentState(
		reader, &out, paths, cfg,
		setupState{ConfigOK: true, TokenOK: true, RelayOK: true, WSOK: true, SkillsOK: true},
		false, false,
	)
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d output=%s", handled, code, out.String())
	}
	if coordinator.preflightCalls != 1 || coordinator.addCalls != 1 {
		t.Fatalf("coordinator preflight/add calls = %d/%d", coordinator.preflightCalls, coordinator.addCalls)
	}
	output := out.String()
	orderedCopy := []string{
		"Optional — Away-from-home access",
		"Home Assistant Cloud (Beta) adds a secure fallback",
		"Local access stays preferred.",
		"Your Cloud authorization stays in this computer's native secure storage.",
		"Add Home Assistant Cloud fallback now? [y/N]",
	}
	previous := -1
	for _, want := range orderedCopy {
		index := strings.Index(output, want)
		if index == -1 {
			t.Fatalf("Cloud offer missing %q:\n%s", want, output)
		}
		if index <= previous {
			t.Fatalf("Cloud offer order is unclear at %q:\n%s", want, output)
		}
		previous = index
	}
	for _, want := range []string{
		"Setup complete!",
		"Local:          Ready — preferred",
		"Away from home: Ready — Home Assistant Cloud",
		"Routing:        Automatic — Cloud is used only when local access is unavailable",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("Cloud completion missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Everything is already set up!") {
		t.Fatalf("Cloud completion must not fall back to the generic resume banner:\n%s", output)
	}
	if coordinator.preflightID == "" {
		t.Fatal("secure-storage preflight did not receive the stable profile id")
	}
	if coordinator.request.ProfileName != defaultServerProfileName ||
		coordinator.request.Config.ProfileID == "" {
		t.Fatalf("request = %+v", coordinator.request)
	}

	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RoutePolicy != routePolicyAutomatic || !saved.Cloud.configured() {
		t.Fatalf("saved Cloud state = %+v", saved)
	}
	if saved.Cloud.State != cloudStateReady || saved.Cloud.Pending != nil ||
		saved.RelayInstanceID != "relay-instance-1" {
		t.Fatalf("saved lifecycle = %+v relay=%q", saved.Cloud, saved.RelayInstanceID)
	}
}

func TestCompletedSetupDefaultsPendingHybridResumeToYes(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-resume-default"
	cfg.Cloud = &cloudLifecycleMetadata{
		State: cloudStateAuthorizing,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := successfulCloudCoordinatorForTest()
	installCloudSetupTestSeams(t, coordinator, true, true)

	var output strings.Builder
	updated, attempted, exit := maybeOfferCloudForCompletedSetup(
		bufio.NewReader(strings.NewReader("\n")),
		&output,
		paths,
		cfg,
		false,
	)
	if !attempted || exit != 0 || !updated.Cloud.ready() {
		t.Fatalf(
			"resume attempted=%v exit=%d cloud=%+v output=%s",
			attempted,
			exit,
			updated.Cloud,
			output.String(),
		)
	}
	if coordinator.addCalls != 1 ||
		!strings.Contains(
			output.String(),
			"Resume Home Assistant Cloud setup now?",
		) {
		t.Fatalf(
			"resume calls=%d output=%s",
			coordinator.addCalls,
			output.String(),
		)
	}
}

func TestCompletedSetupPendingMetadataDoesNotAdvanceTokenState(t *testing.T) {
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
	var checkpoint *cloudLifecycleMetadata
	coordinator.afterPersist = func() {
		saved, loadErr := loadConfig(paths)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		checkpoint = saved.Cloud
	}
	installCloudSetupTestSeams(t, coordinator, true, true)

	var out strings.Builder
	_, attempted, code := maybeOfferCloudForCompletedSetup(
		bufio.NewReader(strings.NewReader("y\n")), &out, paths, cfg, false,
	)
	if !attempted || code != 0 {
		t.Fatalf("attempted=%v code=%d output=%s", attempted, code, out.String())
	}
	if checkpoint == nil || checkpoint.State != cloudStateAuthorizing || checkpoint.Pending == nil {
		t.Fatalf("pre-token checkpoint = %+v, want authorizing with pending metadata", checkpoint)
	}
}

func TestCompletedSetupSkipsCloudForServiceHeadlessOrMissingDevice(t *testing.T) {
	for _, test := range []struct {
		name           string
		service        bool
		reusable       bool
		promptEligible bool
	}{
		{"service", true, true, true},
		{"headless", false, true, false},
		{"no reusable device", false, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
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
			installCloudSetupTestSeams(t, coordinator, test.reusable, test.promptEligible)

			var out strings.Builder
			_, attempted, code := maybeOfferCloudForCompletedSetup(
				bufio.NewReader(strings.NewReader("y\n")), &out, paths, cfg, test.service,
			)
			if attempted || code != 0 || coordinator.preflightCalls != 0 || coordinator.addCalls != 0 {
				t.Fatalf("attempted=%v code=%d calls=%d/%d", attempted, code, coordinator.preflightCalls, coordinator.addCalls)
			}
		})
	}
}

func TestCompletedSetupCloudPreflightFailsBeforeOAuth(t *testing.T) {
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
	coordinator.preflightErr = newCloudError(CloudErrSecretStoreLocked, "preflight", errors.New("locked"))
	installCloudSetupTestSeams(t, coordinator, true, true)

	var out strings.Builder
	_, attempted, code := maybeOfferCloudForCompletedSetup(
		bufio.NewReader(strings.NewReader("y\n")), &out, paths, cfg, false,
	)
	if !attempted || code != 1 || coordinator.preflightCalls != 1 || coordinator.addCalls != 0 {
		t.Fatalf("attempted=%v code=%d calls=%d/%d", attempted, code, coordinator.preflightCalls, coordinator.addCalls)
	}
	if !strings.Contains(out.String(), "working local connection was not changed") {
		t.Fatalf("missing local-safety message: %s", out.String())
	}
	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ProfileID == "" || saved.Cloud != nil || saved.RoutePolicy != routePolicyLocal {
		t.Fatalf("preflight failure mutated Cloud routing state: %+v", saved)
	}
}

func TestCompletedSetupRejectsRelayIdentityMismatch(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.RelayInstanceID = "relay-local"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := successfulCloudCoordinatorForTest()
	coordinator.result.RelayInstanceID = "relay-cloud"
	installCloudSetupTestSeams(t, coordinator, true, true)

	var out strings.Builder
	_, attempted, code := maybeOfferCloudForCompletedSetup(
		bufio.NewReader(strings.NewReader("y\n")), &out, paths, cfg, false,
	)
	if !attempted || code != 1 || coordinator.addCalls != 1 {
		t.Fatalf("attempted=%v code=%d addCalls=%d", attempted, code, coordinator.addCalls)
	}
	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RoutePolicy != routePolicyLocal || saved.Cloud == nil ||
		saved.Cloud.State != cloudStateDeviceBoundOrPaired || saved.Cloud.Current != nil {
		t.Fatalf("identity mismatch did not stay fail-closed: %+v", saved)
	}
}

func TestCompletedSetupResumesCommittedCloudCheckpointWithoutOAuth(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-resume"
	cfg.RelayInstanceID = "relay-instance-1"
	metadata := cloudMetadataForTest(strings.Repeat("5", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateCommitted,
		Current: &metadata,
		Pending: &metadata,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	coordinator := successfulCloudCoordinatorForTest()
	installCloudSetupTestSeams(t, coordinator, false, true)

	var out strings.Builder
	_, attempted, code := maybeOfferCloudForCompletedSetup(
		bufio.NewReader(strings.NewReader("")), &out, paths, cfg, false,
	)
	if !attempted || code != 0 || coordinator.preflightCalls != 1 || coordinator.addCalls != 0 {
		t.Fatalf("attempted=%v code=%d calls=%d/%d", attempted, code, coordinator.preflightCalls, coordinator.addCalls)
	}
	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Cloud.ready() || saved.RoutePolicy != routePolicyAutomatic {
		t.Fatalf("resumed config = %+v", saved)
	}
}
