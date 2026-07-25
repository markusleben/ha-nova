package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNonInteractiveSetupReusesReadyCloudRoute(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		args   []string
		hybrid bool
		named  bool
	}{
		{name: "Cloud-only target", args: []string{"antigravity", "--non-interactive"}},
		{name: "Cloud-only all", args: []string{"--non-interactive"}},
		{name: "named Cloud-only first profile", args: []string{"antigravity", "--non-interactive"}, named: true},
		{name: "hybrid away", args: []string{"antigravity", "--non-interactive"}, hybrid: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			if testCase.named {
				setServerSelectionOverride("cabin")
			}
			withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("HA_NOVA_NO_BROWSER", "1")
			t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

			relay := httptest.NewServer(http.HandlerFunc(
				func(response http.ResponseWriter, request *http.Request) {
					if request.URL.Path != "/health" ||
						request.Header.Get("Authorization") != "Bearer cloud-device" {
						http.Error(response, "unauthorized", http.StatusUnauthorized)
						return
					}
					response.Header().Set("Content-Type", "application/json")
					fmt.Fprint(
						response,
						`{"ok":true,"data":{"version":"999.0.0","status":"ok","ha_ws_connected":true,"relay_instance_id":"relay-cloud"}}`,
					)
				},
			))
			defer relay.Close()

			paths, err := detectPaths()
			if err != nil {
				t.Fatal(err)
			}
			metadata := cloudMetadataForTest(strings.Repeat("a", 32))
			cfg := runtimeConfig{
				ProfileID:       "profile-cloud",
				RelayInstanceID: "relay-cloud",
				RoutePolicy:     routePolicyCloud,
				Cloud: &cloudLifecycleMetadata{
					State:   cloudStateReady,
					Current: &metadata,
				},
			}
			if testCase.hybrid {
				cfg.HAHost = "192.0.2.10"
				cfg.HAURL = "http://192.0.2.10:8123"
				cfg.RelayBaseURL = "http://192.0.2.10:8791"
				cfg.RelaySecureBaseURL = "https://192.0.2.10:8792"
				cfg.RelaySpkiPin = "saved-pin"
				cfg.RoutePolicy = routePolicyAutomatic
			}
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}

			oldCloud := resolveCloudRelayTransportForCLI
			oldAutomatic := resolveAutomaticRelayTransportForCLI
			selection := func(context.Context, runtimeConfig) (relayTransportSelection, error) {
				return relayTransportSelection{
					BaseURL:    relay.URL,
					Client:     relay.Client(),
					Credential: "cloud-device",
					DeviceMode: true,
					Via:        relayViaCloud,
				}, nil
			}
			resolveCloudRelayTransportForCLI = selection
			resolveAutomaticRelayTransportForCLI = selection
			t.Cleanup(func() {
				resolveCloudRelayTransportForCLI = oldCloud
				resolveAutomaticRelayTransportForCLI = oldAutomatic
			})

			exit, output := captureCommandOutput(t, func() int {
				return runSetup(paths, testCase.args)
			})
			if exit != 0 {
				t.Fatalf("setup exit=%d:\n%s", exit, output)
			}
			if strings.Contains(output, "missing Home Assistant host") ||
				strings.Contains(output, "missing relay auth token") {
				t.Fatalf("Cloud reuse entered legacy local setup:\n%s", output)
			}
			savedState := loadStateOrDefault(paths)
			if !containsClient(savedState.InstalledClients, "antigravity") {
				t.Fatalf("client installation was not persisted: %+v", savedState)
			}
		})
	}
}

func TestNonInteractiveCloudOnlyPendingGivesExactResumePath(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		t.Run(profile, func(t *testing.T) {
			resetServerProfileSelection(t)
			if profile != defaultServerProfileName {
				setServerSelectionOverride(profile)
			}
			withClientRuntimeAvailability(t, map[string]bool{"antigravity": true})
			t.Setenv("HOME", t.TempDir())
			t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
			paths, err := detectPaths()
			if err != nil {
				t.Fatal(err)
			}
			metadata := cloudMetadataForTest(strings.Repeat("b", 32))
			if err := saveConfig(paths, runtimeConfig{
				ProfileID: "profile-cloud",
				Cloud: &cloudLifecycleMetadata{
					State:   cloudStateTokenStored,
					Pending: &metadata,
				},
			}); err != nil {
				t.Fatal(err)
			}
			exit, output := captureCommandOutput(t, func() int {
				return runSetup(
					paths,
					[]string{"antigravity", "--non-interactive"},
				)
			})
			wantResume := "ha-nova cloud add --server " + profile
			wantRemove := "ha-nova cloud remove --server " + profile
			if exit == 0 ||
				!strings.Contains(output, wantResume) ||
				!strings.Contains(output, wantRemove) ||
				!strings.Contains(output, "interactive desktop") {
				t.Fatalf(
					"pending Cloud-only recovery exit=%d:\n%s",
					exit,
					output,
				)
			}
		})
	}
}

func TestNonInteractiveCloudOnlyHoldOffersOnlyExactCleanup(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		t.Run(profile, func(t *testing.T) {
			resetServerProfileSelection(t)
			if profile != defaultServerProfileName {
				setServerSelectionOverride(profile)
			}
			withClientRuntimeAvailability(
				t,
				map[string]bool{"antigravity": true},
			)
			t.Setenv("HOME", t.TempDir())
			t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
			paths, err := detectPaths()
			if err != nil {
				t.Fatal(err)
			}
			if err := saveConfig(paths, runtimeConfig{
				ProfileID: "profile-cloud-held",
				Cloud: &cloudLifecycleMetadata{
					State: cloudStateAuthorizing,
					RecoveryHold: &cloudRecoveryHold{
						Code:        cloudProblemAuthorization,
						Remediation: cloudRemediationSecurityStop,
					},
				},
			}); err != nil {
				t.Fatal(err)
			}

			exit, output := captureCommandOutput(t, func() int {
				return runSetup(
					paths,
					[]string{"antigravity", "--non-interactive"},
				)
			})
			wantRemove := "ha-nova cloud remove --server " + profile
			if exit != 1 ||
				!strings.Contains(output, wantRemove) ||
				strings.Contains(output, "ha-nova cloud unlock") ||
				strings.Contains(output, "ha-nova setup --server") ||
				strings.Contains(output, "ha-nova cloud add") ||
				strings.Contains(output, "ha-nova cloud reconnect") {
				t.Fatalf(
					"held Cloud-only recovery exit=%d:\n%s",
					exit,
					output,
				)
			}
		})
	}
}
