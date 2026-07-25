package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPairExplicitRelayURLFailsClosedOnInvalidConfigBeforeMigration(
	t *testing.T,
) {
	tests := []struct {
		name       string
		config     string
		wantOutput string
		extraArgs  []string
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
						"relay_base_url": "http://old:8791",
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
						"relay_base_url": "http://old:8791",
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
						"relay_base_url": "http://old:8791",
						"route_policy": "local",
						"cloud": {"state": "ready"}
					}
				}
			}`,
			wantOutput: "ready cloud lifecycle requires only current metadata",
		},
		{
			name: "invalid sibling during explicit profile creation",
			config: `{
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
			}`,
			wantOutput: "ready cloud lifecycle requires only current metadata",
			extraArgs:  []string{"--server", "lake"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			withDeviceStorageTestHome(t)
			resetKeyringDeviceSlots(t)
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(t, testCase.config)
			current := validCredential(101)
			if err := writeDeviceCredential(current); err != nil {
				t.Fatal(err)
			}

			originalPair := runSecurePairingForPairCmd
			pairCalls := 0
			runSecurePairingForPairCmd = func(
				_, _ string,
				_ *runtimeConfig,
				_ func(*runtimeConfig) error,
				_ pairingClientInfo,
			) (string, error) {
				pairCalls++
				return "unexpected", nil
			}
			t.Cleanup(func() {
				runSecurePairingForPairCmd = originalPair
			})

			args := []string{
				"--relay-url", "http://new:8791",
				"--code", "123456",
				"--credential-store", "file",
			}
			args = append(args, testCase.extraArgs...)
			exit, output := captureCommandOutput(t, func() int {
				return runPairCommand(paths, args)
			})
			if exit != 1 ||
				!strings.Contains(output, testCase.wantOutput) ||
				!strings.Contains(output, "no code was used") ||
				pairCalls != 0 {
				t.Fatalf(
					"pair exit=%d calls=%d output=%s",
					exit,
					pairCalls,
					output,
				)
			}
			if deviceFileBackendMarkerExists() {
				t.Fatal("invalid config changed the credential backend")
			}
			got, exists, err := readCredentialSlot(
				deviceCredentialServiceForProfile(defaultServerProfileName),
			)
			if err != nil || !exists || got != current {
				t.Fatalf(
					"credential changed before config validation: got=%q exists=%v err=%v",
					got,
					exists,
					err,
				)
			}
		})
	}
}

func TestPairExplicitRelayURLKeepsSafeFreshAndIncompleteSetupPaths(
	t *testing.T,
) {
	tests := []struct {
		name          string
		config        string
		missing       bool
		wantInstallID string
	}{
		{
			name:    "no config yet",
			missing: true,
		},
		{
			name:          "valid incomplete default profile",
			config:        `{"schema_version":1,"client_install_id":"install-existing"}`,
			wantInstallID: "install-existing",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			withDeviceStorageTestHome(t)
			resetKeyringDeviceSlots(t)
			resetServerProfileSelection(t)
			var paths runtimePaths
			if testCase.missing {
				dir := t.TempDir()
				paths = runtimePaths{
					ConfigDir:  dir,
					ConfigFile: filepath.Join(dir, "config.json"),
				}
			} else {
				paths = writeTestConfigFile(t, testCase.config)
			}

			originalPair := runSecurePairingForPairCmd
			runSecurePairingForPairCmd = func(
				bootstrapURL, _ string,
				cfg *runtimeConfig,
				_ func(*runtimeConfig) error,
				_ pairingClientInfo,
			) (string, error) {
				if bootstrapURL != "http://new:8791" ||
					cfg.RelayBaseURL != bootstrapURL ||
					cfg.ClientInstallID != testCase.wantInstallID {
					t.Fatalf(
						"recovered pair config URL=%q cfg=%+v",
						bootstrapURL,
						*cfg,
					)
				}
				return "device-safe", nil
			}
			t.Cleanup(func() {
				runSecurePairingForPairCmd = originalPair
			})

			exit, output := captureCommandOutput(t, func() int {
				return runPairCommand(paths, []string{
					"--relay-url", "http://new:8791",
					"--code", "123456",
				})
			})
			if exit != 0 || !strings.Contains(output, "Paired securely") {
				t.Fatalf("safe pair exit=%d output=%s", exit, output)
			}
		})
	}
}
