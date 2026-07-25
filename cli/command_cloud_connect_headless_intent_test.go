package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloudConnectHeadlessValidatesIntentBeforeDesktopGuidance(
	t *testing.T,
) {
	ready := completedLocalCloudTestConfig()
	ready.ProfileID = "profile-headless-ready"
	ready.RelayInstanceID = "relay-headless-ready"
	ready.RelaySecureBaseURL = ""
	ready.RelaySpkiPin = ""
	ready.RoutePolicy = routePolicyCloud
	metadata := cloudMetadataForTest(strings.Repeat("f", 32))
	ready.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &metadata,
	}
	notConfigured := runtimeConfig{
		ProfileID:    "profile-headless-local",
		RelayBaseURL: "http://ha:8791",
		RoutePolicy:  routePolicyLocal,
	}
	for _, testCase := range []struct {
		name      string
		cfg       runtimeConfig
		reconnect bool
		want      string
	}{
		{
			name: "ready add",
			cfg:  ready,
			want: "already configured",
		},
		{
			name:      "local-only reconnect",
			cfg:       notConfigured,
			reconnect: true,
			want:      "not configured",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			restoreFeature := setCloudFeatureTestBuild(t, true)
			defer restoreFeature()
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			if err := saveConfig(paths, testCase.cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			installCloudCommandPromptSession(t, false)
			installCloudCommandCoordinator(
				t,
				newSelectingCloudCoordinator(),
			)

			exit, output := captureCommandOutput(t, func() int {
				return runCloudConnectCommand(
					paths,
					nil,
					testCase.reconnect,
				)
			})
			if exit != 1 ||
				!strings.Contains(output, testCase.want) ||
				strings.Contains(
					output,
					"requires an interactive desktop session",
				) {
				t.Fatalf(
					"headless intent exit=%d output=%s",
					exit,
					output,
				)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("headless intent preflight changed configuration")
			}
		})
	}
}

func TestCloudConnectHeadlessMissingStatePreflightDoesNotCreateProfile(
	t *testing.T,
) {
	for _, testCase := range []struct {
		name           string
		raw            string
		args           []string
		reconnect      bool
		want           string
		wantConfigFile bool
	}{
		{
			name:      "missing config reconnect",
			reconnect: true,
			want:      "HA NOVA is not set up yet",
		},
		{
			name: "explicit missing profile add",
			raw: `{
				"schema_version": 3,
				"default_server": "default",
				"servers": {
					"default": {
						"profile_id": "profile-default",
						"relay_base_url": "http://ha:8791",
						"route_policy": "local"
					}
				}
			}`,
			args:           []string{"--server", "cabin"},
			want:           "requires an interactive desktop session",
			wantConfigFile: true,
		},
		{
			name: "explicit missing profile with invalid sibling",
			raw: `{
				"schema_version": 3,
				"default_server": "default",
				"servers": {
					"default": {
						"profile_id": "profile-default",
						"relay_base_url": "http://ha:8791",
						"route_policy": "local"
					},
					"broken": {
						"profile_id": "profile-broken",
						"relay_base_url": "http://broken:8791",
						"route_policy": "automatic"
					}
				}
			}`,
			args:           []string{"--server", "cabin"},
			want:           "cannot safely continue Home Assistant Cloud setup",
			wantConfigFile: true,
		},
		{
			name: "missing config add",
			want: "requires an interactive desktop session",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			restoreFeature := setCloudFeatureTestBuild(t, true)
			defer restoreFeature()
			var paths runtimePaths
			if testCase.wantConfigFile {
				paths = setupServerCommandTest(t, testCase.raw)
			} else {
				dir := t.TempDir()
				paths = runtimePaths{
					ConfigDir:  dir,
					ConfigFile: filepath.Join(dir, "config.json"),
				}
			}
			var before []byte
			if testCase.wantConfigFile {
				var err error
				before, err = os.ReadFile(paths.ConfigFile)
				if err != nil {
					t.Fatal(err)
				}
			}
			installCloudCommandPromptSession(t, false)
			installCloudCommandCoordinator(
				t,
				newSelectingCloudCoordinator(),
			)

			exit, output := captureCommandOutput(t, func() int {
				return runCloudConnectCommand(
					paths,
					testCase.args,
					testCase.reconnect,
				)
			})
			if exit != 1 || !strings.Contains(output, testCase.want) {
				t.Fatalf(
					"headless missing state exit=%d output=%s",
					exit,
					output,
				)
			}
			if testCase.name == "missing config reconnect" &&
				strings.Contains(
					output,
					"requires an interactive desktop session",
				) {
				t.Fatalf(
					"missing reconnect showed desktop guidance: %s",
					output,
				)
			}
			if testCase.name ==
				"explicit missing profile with invalid sibling" &&
				strings.Contains(
					output,
					"requires an interactive desktop session",
				) {
				t.Fatalf(
					"invalid sibling showed desktop guidance: %s",
					output,
				)
			}
			if testCase.wantConfigFile {
				after, err := os.ReadFile(paths.ConfigFile)
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != string(before) {
					t.Fatal(
						"headless missing-profile add changed configuration",
					)
				}
				doc, err := loadConfigDocument(paths.ConfigFile)
				if err != nil {
					t.Fatal(err)
				}
				if doc.hasProfile("cabin") {
					t.Fatal("headless preflight created the missing profile")
				}
				return
			}
			if _, err := os.Stat(paths.ConfigFile); !errors.Is(
				err,
				os.ErrNotExist,
			) {
				t.Fatalf(
					"headless preflight created config file: %v",
					err,
				)
			}
		})
	}
}
