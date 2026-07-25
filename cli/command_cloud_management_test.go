package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingCloudCommandCoordinator struct {
	entered        chan struct{}
	release        chan struct{}
	enteredOnce    sync.Once
	preflightCalls atomic.Int32
	addCalls       atomic.Int32
}

func newBlockingCloudCommandCoordinator() *blockingCloudCommandCoordinator {
	return &blockingCloudCommandCoordinator{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *blockingCloudCommandCoordinator) Available() bool {
	return true
}

func (c *blockingCloudCommandCoordinator) Preflight(
	ctx context.Context,
	_ string,
) error {
	c.preflightCalls.Add(1)
	c.enteredOnce.Do(func() { close(c.entered) })
	select {
	case <-c.release:
		return errors.New("test preflight stopped")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *blockingCloudCommandCoordinator) AddAwayWithExistingDevice(
	context.Context,
	cloudSetupRequest,
) (cloudSetupResult, error) {
	c.addCalls.Add(1)
	return cloudSetupResult{}, errors.New("unexpected add")
}

func (c *blockingCloudCommandCoordinator) AddRemoteWithPairing(
	context.Context,
	cloudRemoteSetupRequest,
) (cloudSetupResult, error) {
	c.addCalls.Add(1)
	return cloudSetupResult{}, errors.New("unexpected remote add")
}

func installCloudCommandCoordinator(
	t *testing.T,
	coordinator cloudSetupCoordinator,
) {
	t.Helper()
	old := cloudCoordinatorForSetup
	cloudCoordinatorForSetup = coordinator
	t.Cleanup(func() { cloudCoordinatorForSetup = old })
}

func useCloudCommandTTY(t *testing.T) {
	t.Helper()
	output, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	info, err := output.Stat()
	if err != nil {
		output.Close()
		t.Fatal(err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		output.Close()
		t.Skip("the platform null device is not reported as a character device")
	}
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	oldInputTTY := uiInputSupportsTTY
	os.Stdout = output
	os.Stderr = output
	uiInputSupportsTTY = func() bool { return true }
	oldPromptSession := cloudInteractivePromptSessionForSetup
	cloudInteractivePromptSessionForSetup = func() bool { return true }
	t.Cleanup(func() {
		cloudInteractivePromptSessionForSetup = oldPromptSession
		uiInputSupportsTTY = oldInputTTY
		os.Stdout = oldStdout
		os.Stderr = oldStderr
		output.Close()
	})
}

func cloudCommandLockTestConfig(reconnect bool) runtimeConfig {
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-lock"
	cfg.RelayInstanceID = "relay-lock"
	if reconnect {
		metadata := cloudMetadataForTest(strings.Repeat("d", 32))
		cfg.Cloud = &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		}
		cfg.RoutePolicy = routePolicyAutomatic
	}
	return cfg
}

func TestCloudAddAndReconnectHoldWholeFlowMutationLock(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		args      []string
		reconnect bool
	}{
		{name: "add", args: nil},
		{name: "reconnect", reconnect: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			if err := saveConfig(
				paths,
				cloudCommandLockTestConfig(testCase.reconnect),
			); err != nil {
				t.Fatal(err)
			}
			coordinator := newBlockingCloudCommandCoordinator()
			installCloudCommandCoordinator(t, coordinator)
			useCloudCommandTTY(t)
			t.Cleanup(func() {
				select {
				case <-coordinator.release:
				default:
					close(coordinator.release)
				}
			})

			firstDone := make(chan int, 1)
			go func() {
				firstDone <- runCloudConnectCommand(
					paths,
					testCase.args,
					testCase.reconnect,
				)
			}()
			select {
			case <-coordinator.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("first Cloud command did not reach its locked preflight")
			}
			checkpoint, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}

			if exit := runCloudConnectCommand(
				paths,
				testCase.args,
				testCase.reconnect,
			); exit != 1 {
				t.Fatalf("second Cloud command exit = %d, want 1", exit)
			}
			if got := coordinator.preflightCalls.Load(); got != 1 {
				t.Fatalf("second command reached coordinator: preflight calls=%d", got)
			}
			if got := coordinator.addCalls.Load(); got != 0 {
				t.Fatalf("second command reached add flow: calls=%d", got)
			}
			afterSecond, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(afterSecond) != string(checkpoint) {
				t.Fatal("second command mutated config while first flow held the lock")
			}

			close(coordinator.release)
			if exit := <-firstDone; exit != 1 {
				t.Fatalf("first stopped command exit = %d, want 1", exit)
			}
		})
	}
}

func TestCloudCommandFlagsAndHelpStayCommandScoped(t *testing.T) {
	resetServerProfileSelection(t)
	options, err := parseCloudCommandFlags(
		"add",
		[]string{"--server", "cabin", "--url", "https://unit.ui.nabu.casa"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if options.server != "cabin" ||
		options.url != "https://unit.ui.nabu.casa" {
		t.Fatalf("add options = %+v", options)
	}
	for _, testCase := range []struct {
		command string
		args    []string
	}{
		{command: "status", args: []string{"--url", "https://unit.ui.nabu.casa"}},
		{command: "add", args: []string{"--json"}},
		{command: "reconnect", args: []string{"--yes"}},
		{command: "remove", args: []string{"extra"}},
		{command: "status", args: []string{"--server", " cabin"}},
	} {
		if _, err := parseCloudCommandFlags(
			testCase.command,
			testCase.args,
		); err == nil {
			t.Fatalf(
				"cloud %s accepted invalid args %v",
				testCase.command,
				testCase.args,
			)
		}
	}

	exit, output := captureCommandOutput(t, func() int {
		return runCloudCommand(runtimePaths{}, []string{"--help"})
	})
	if exit != 0 {
		t.Fatalf("cloud help exit=%d output=%s", exit, output)
	}
	for _, command := range []string{"add", "status", "unlock", "reconnect", "remove"} {
		if !strings.Contains(output, command) {
			t.Fatalf("cloud help omitted %q:\n%s", command, output)
		}
	}
}

func TestCloudStatusJSONUsesNoUISecretReads(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	deviceCredential := validCredential(71)
	if err := writeDeviceCredential(deviceCredential); err != nil {
		t.Fatal(err)
	}

	server := newProductionCloudProtocolServer(t)
	defer server.Close()
	mux, ok := server.Config.Handler.(*http.ServeMux)
	if !ok {
		t.Fatal("production Cloud test server does not use a ServeMux")
	}
	mux.HandleFunc(productionCloudTestIngressRoot+"/health", func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		cookie, cookieErr := request.Cookie("ingress_session")
		if cookieErr != nil ||
			cookie.Value != strings.Repeat("a", 128) ||
			request.Header.Get("Authorization") != "Bearer "+deviceCredential {
			http.Error(response, "invalid health request", http.StatusUnauthorized)
			return
		}
		response.Header().Set(relayVersionHeader, "1.2.3")
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(
			`{"ok":true,"data":{"relay_instance_id":"relay-1"}}`,
		))
	})
	client := productionCloudMappedClient(t, server)
	oldStore := newCloudSecretStoreForCLI
	oldHTTP := cloudHTTPClientForCLI
	newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
		if profileID != "profile-1" {
			t.Fatalf("secret-store profile = %q", profileID)
		}
		return store, nil
	}
	cloudHTTPClientForCLI = client
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
		cloudHTTPClientForCLI = oldHTTP
	})
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	if exit != 0 {
		t.Fatalf("cloud status exit=%d output=%s", exit, output)
	}
	var summary struct {
		Status      string      `json:"status"`
		Server      string      `json:"server"`
		RoutePolicy routePolicy `json:"route_policy"`
		Origin      string      `json:"origin"`
		UserBound   bool        `json:"user_bound"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("status JSON=%q: %v", output, err)
	}
	if summary.Status != "ready" ||
		summary.Server != defaultServerProfileName ||
		summary.RoutePolicy != routePolicyCloud ||
		summary.Origin != productionCloudTestOrigin ||
		!summary.UserBound {
		t.Fatalf("status summary = %+v", summary)
	}
	if strings.Contains(output, current.RefreshToken) ||
		strings.Contains(output, deviceCredential) {
		t.Fatal("status output exposed a stored credential")
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
	backend.mu.Lock()
	defer backend.mu.Unlock()
	for index, operation := range backend.operations {
		if operation != "get" {
			t.Fatalf("ordinary Cloud status secret-store operation %d = %q, want read-only get", index, operation)
		}
	}
}

func TestCloudStatusNotConfiguredReturnsTypedRecovery(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(paths, completedLocalCloudTestConfig()); err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudProblemNotConfigured)) ||
		!strings.Contains(output, string(cloudRemediationAddAccess)) {
		t.Fatalf("unconfigured status exit=%d output=%s", exit, output)
	}
}
