package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCloudConnectValidatesConfigBeforePromptResolutionOrStorage(
	t *testing.T,
) {
	invalidSelected := `{
		"schema_version": 3,
		"default_server": "default",
		"servers": {
			"default": {
				"profile_id": "profile-default",
				"relay_base_url": "http://ha:8791",
				"route_policy": "automatic"
			}
		}
	}`
	invalidSibling := `{
		"schema_version": 3,
		"default_server": "default",
		"servers": {
			"default": {
				"profile_id": "profile-default",
				"relay_base_url": "http://ha:8791",
				"route_policy": "local"
			},
			"cabin": {
				"profile_id": "profile-cabin",
				"relay_base_url": "http://cabin:8791",
				"route_policy": "automatic"
			}
		}
	}`
	for _, testCase := range []struct {
		name      string
		raw       string
		args      []string
		reconnect bool
	}{
		{
			name: "add rejects invalid selected before URL prompt",
			raw:  invalidSelected,
		},
		{
			name: "add rejects invalid sibling before URL resolution",
			raw:  invalidSibling,
			args: []string{"--url", productionCloudTestOrigin},
		},
		{
			name: "named add rejects invalid selected before URL prompt",
			raw:  invalidSibling,
			args: []string{"--server", "cabin"},
		},
		{
			name:      "reconnect rejects invalid sibling before URL resolution",
			raw:       invalidSibling,
			args:      []string{"--url", productionCloudTestOrigin},
			reconnect: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			restoreFeature := setCloudFeatureTestBuild(t, true)
			defer restoreFeature()
			paths := setupServerCommandTest(t, testCase.raw)
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			installCloudCommandPromptSession(t, true)
			installCloudCommandCoordinator(t, newSelectingCloudCoordinator())

			promptCalls := 0
			installCloudCommandURLPrompt(t, func(
				context.Context,
			) (CloudOrigin, error) {
				promptCalls++
				return CloudOrigin{
					InputOrigin:     productionCloudTestOrigin,
					CanonicalOrigin: productionCloudTestOrigin,
				}, nil
			})
			resolverCalls := 0
			oldResolver := resolveCanonicalNabuOriginForCloudCommand
			resolveCanonicalNabuOriginForCloudCommand = func(
				context.Context,
				string,
				CloudCNAMEResolver,
			) (CloudOrigin, error) {
				resolverCalls++
				return CloudOrigin{
					InputOrigin:     productionCloudTestOrigin,
					CanonicalOrigin: productionCloudTestOrigin,
				}, nil
			}
			t.Cleanup(func() {
				resolveCanonicalNabuOriginForCloudCommand = oldResolver
			})
			storageCalls := 0
			oldStorageProbe := probeCloudDeviceStorageForSetup
			probeCloudDeviceStorageForSetup = func(
				context.Context,
				SecretStoreUIPolicy,
			) (deviceStorageProbe, error) {
				storageCalls++
				return deviceStorageProbe{}, errors.New(
					"unexpected secure-storage access",
				)
			}
			t.Cleanup(func() {
				probeCloudDeviceStorageForSetup = oldStorageProbe
			})

			exit, output := captureCommandOutput(t, func() int {
				return runCloudConnectCommand(
					paths,
					testCase.args,
					testCase.reconnect,
				)
			})
			if exit != 1 ||
				!strings.Contains(
					output,
					"cannot safely continue Home Assistant Cloud setup",
				) {
				t.Fatalf(
					"invalid config exit=%d output=%s",
					exit,
					output,
				)
			}
			if promptCalls != 0 || resolverCalls != 0 || storageCalls != 0 {
				t.Fatalf(
					"invalid config reached prompt/resolver/storage: %d/%d/%d",
					promptCalls,
					resolverCalls,
					storageCalls,
				)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("invalid Cloud connect changed configuration")
			}
		})
	}
}
