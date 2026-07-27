package main

import (
	"os"
	"strings"
	"testing"
)

func TestNamedSetupRejectsInvalidConfigBeforeCredentialMutation(
	t *testing.T,
) {
	tests := []struct {
		name       string
		config     string
		wantOutput string
	}{
		{
			name: "future schema",
			config: `{
				"schema_version": 4,
				"default_server": "default",
				"client_install_id": "install-future",
				"servers": {
					"default": {
						"profile_id": "profile-default",
						"relay_base_url": "http://default:8791",
						"route_policy": "local"
					},
					"cabin": {
						"profile_id": "profile-cabin",
						"relay_base_url": "http://cabin:8791",
						"route_policy": "local"
					}
				}
			}`,
			wantOutput: "schema_version 4 is newer",
		},
		{
			name: "duplicate profile identities",
			config: `{
				"schema_version": 3,
				"default_server": "default",
				"client_install_id": "install-duplicate",
				"servers": {
					"default": {
						"profile_id": "profile-shared",
						"relay_base_url": "http://default:8791",
						"route_policy": "local"
					},
					"cabin": {
						"profile_id": "profile-shared",
						"relay_base_url": "http://cabin:8791",
						"route_policy": "local"
					}
				}
			}`,
			wantOutput: "share profile_id",
		},
		{
			name: "invalid Cloud lifecycle",
			config: `{
				"schema_version": 3,
				"default_server": "default",
				"client_install_id": "install-invalid-cloud",
				"servers": {
					"default": {
						"profile_id": "profile-default",
						"relay_base_url": "http://default:8791",
						"route_policy": "local"
					},
					"cabin": {
						"profile_id": "profile-cabin",
						"relay_base_url": "http://cabin:8791",
						"route_policy": "local",
						"cloud": {"state": "ready"}
					}
				}
			}`,
			wantOutput: "ready cloud lifecycle requires only current metadata",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			withDeviceStorageTestHome(t)
			resetKeyringDeviceSlots(t)
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(t, testCase.config)
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			current := validCredential(102)
			if err := secretSet(
				deviceCredentialServiceForProfile(defaultServerProfileName),
				current,
			); err != nil {
				t.Fatal(err)
			}

			exit, output := captureCommandOutput(t, func() int {
				return runSetup(paths, []string{
					"hermes",
					"--server", "cabin",
					"--service",
					"--non-interactive",
					"--host", "new-ha",
					"--relay-token", "token",
				})
			})
			if exit != 1 ||
				!strings.Contains(output, testCase.wantOutput) {
				t.Fatalf("setup exit=%d output=%s", exit, output)
			}
			if deviceFileBackendMarkerExists() {
				t.Fatal("rejected named setup changed the credential backend")
			}
			got, exists, err := readCredentialSlot(
				deviceCredentialServiceForProfile(defaultServerProfileName),
			)
			if err != nil || !exists || got != current {
				t.Fatalf(
					"default credential changed: got=%q exists=%v err=%v",
					got,
					exists,
					err,
				)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("rejected named setup changed config.json")
			}
		})
	}
}

func TestSetupExplicitMissingDefaultPreservesInstallIdentity(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "cabin",
		"client_install_id": "install-shared",
		"servers": {
			"cabin": {
				"profile_id": "profile-cabin",
				"relay_base_url": "http://cabin:8791",
				"route_policy": "local"
			}
		}
	}`)
	setServerSelectionOverride(defaultServerProfileName)
	_, loadErr := loadConfig(paths)
	if loadErr == nil || !strings.Contains(loadErr.Error(), "unknown") {
		t.Fatalf("missing default load error=%v", loadErr)
	}

	cfg, err := recoverSetupConfigAfterLoadError(paths, loadErr)
	if err != nil || cfg.ClientInstallID != "install-shared" {
		t.Fatalf("recovered setup config=%+v err=%v", cfg, err)
	}
}

func TestSetupRejectsInvalidSiblingBeforeCredentialMutation(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "default",
		"client_install_id": "install-invalid-sibling",
		"servers": {
			"default": {
				"profile_id": "profile-default",
				"relay_base_url": "http://default:8791",
				"route_policy": "local"
			},
			"cabin": {
				"profile_id": "profile-cabin",
				"relay_base_url": "http://cabin:8791",
				"route_policy": "local",
				"cloud": {"state": "ready"}
			}
		}
	}`)
	current := validCredential(103)
	if err := writeDeviceCredential(current); err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{
			"hermes",
			"--service",
			"--non-interactive",
			"--host", "new-ha",
			"--relay-token", "token",
		})
	})
	if exit != 1 ||
		!strings.Contains(
			output,
			"ready cloud lifecycle requires only current metadata",
		) {
		t.Fatalf("setup exit=%d output=%s", exit, output)
	}
	if deviceFileBackendMarkerExists() {
		t.Fatal("invalid sibling changed the credential backend")
	}
	got, exists, err := readDeviceCredential()
	if err != nil || !exists || got != current {
		t.Fatalf(
			"credential changed: got=%q exists=%v err=%v",
			got,
			exists,
			err,
		)
	}
}
