package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestCloudCheckpointConditionalWritePreservesConcurrentConfigChanges(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(*testing.T, map[string]json.RawMessage)
	}{
		{
			name: "selected profile",
			mutate: func(t *testing.T, top map[string]json.RawMessage) {
				var servers map[string]json.RawMessage
				if err := json.Unmarshal(top["servers"], &servers); err != nil {
					t.Fatal(err)
				}
				var selected map[string]json.RawMessage
				if err := json.Unmarshal(
					servers["default"],
					&selected,
				); err != nil {
					t.Fatal(err)
				}
				selected["external_selected"] = json.RawMessage(`true`)
				var err error
				servers["default"], err = json.Marshal(selected)
				if err != nil {
					t.Fatal(err)
				}
				top["servers"], err = json.Marshal(servers)
				if err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "sibling profile",
			mutate: func(t *testing.T, top map[string]json.RawMessage) {
				var servers map[string]json.RawMessage
				if err := json.Unmarshal(top["servers"], &servers); err != nil {
					t.Fatal(err)
				}
				var sibling map[string]json.RawMessage
				if err := json.Unmarshal(
					servers["cabin"],
					&sibling,
				); err != nil {
					t.Fatal(err)
				}
				sibling["external_sibling"] = json.RawMessage(`true`)
				var err error
				servers["cabin"], err = json.Marshal(sibling)
				if err != nil {
					t.Fatal(err)
				}
				top["servers"], err = json.Marshal(servers)
				if err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "default selection",
			mutate: func(
				_ *testing.T,
				top map[string]json.RawMessage,
			) {
				top["default_server"] = json.RawMessage(`"cabin"`)
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(t, `{
				"schema_version": 3,
				"default_server": "default",
				"client_install_id": "install-checkpoint",
				"servers": {
					"default": {
						"profile_id": "profile-default",
						"route_policy": "local",
						"cloud": {"state": "authorizing"}
					},
					"cabin": {
						"profile_id": "profile-cabin",
						"route_policy": "local"
					}
				}
			}`)
			doc, err := loadConfigDocument(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			profileRaw, err := cloudRecoveryProfileRaw(
				doc,
				defaultServerProfileName,
			)
			if err != nil {
				t.Fatal(err)
			}
			top := readTestConfigTopLevel(t, paths)
			testCase.mutate(t, top)
			if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
				t.Fatal(err)
			}
			changed, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			err = writeCloudLifecycleFieldRaw(
				paths,
				doc,
				defaultServerProfileName,
				profileRaw,
				"recovery_hold",
				json.RawMessage(`{"code":"oauth_outcome_unknown"}`),
			)
			if err == nil {
				t.Fatal("stale checkpoint write succeeded")
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(changed) {
				t.Fatal("stale checkpoint overwrote concurrent config")
			}
		})
	}
}

func TestCloudCheckpointAtomicSwapRejectsExactRaceWindow(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "default",
		"client_install_id": "install-checkpoint",
		"servers": {
			"default": {
				"profile_id": "profile-default",
				"route_policy": "local",
				"cloud": {"state": "authorizing"}
			},
			"cabin": {
				"profile_id": "profile-cabin",
				"route_policy": "local"
			}
		}
	}`)
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	profileRaw, err := cloudRecoveryProfileRaw(
		doc,
		defaultServerProfileName,
	)
	if err != nil {
		t.Fatal(err)
	}
	previousHook := conditionalJSONBeforeSwap
	conditionalJSONBeforeSwap = func(path string) {
		top := readTestConfigTopLevel(t, paths)
		var servers map[string]json.RawMessage
		if err := json.Unmarshal(
			top["servers"],
			&servers,
		); err != nil {
			t.Fatal(err)
		}
		var sibling map[string]json.RawMessage
		if err := json.Unmarshal(
			servers["cabin"],
			&sibling,
		); err != nil {
			t.Fatal(err)
		}
		sibling["cloud"] = json.RawMessage(
			`{"state":"ready"}`,
		)
		servers["cabin"], err = json.Marshal(sibling)
		if err != nil {
			t.Fatal(err)
		}
		top["servers"], err = json.Marshal(servers)
		if err != nil {
			t.Fatal(err)
		}
		conditionalJSONBeforeSwap = func(string) {}
		if err := writeJSONFile(path, top, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		conditionalJSONBeforeSwap = previousHook
	})

	err = writeCloudLifecycleFieldRaw(
		paths,
		doc,
		defaultServerProfileName,
		profileRaw,
		"recovery_hold",
		json.RawMessage(
			`{"code":"cloud_authorization","remediation":"inspect_security"}`,
		),
	)
	if err == nil {
		t.Fatal("racing checkpoint write succeeded")
	}
	after := readTestConfigTopLevel(t, paths)
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(
		after["servers"],
		&servers,
	); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(servers["cabin"]) ||
		!bytes.Contains(servers["cabin"], []byte(`"cloud"`)) {
		t.Fatalf(
			"racing Cloud inventory was lost: %s",
			servers["cabin"],
		)
	}
	if _, err := os.Lstat(
		conditionalJSONTransactionPath(paths.ConfigFile),
	); !os.IsNotExist(err) {
		t.Fatalf("transaction was not cleared: %v", err)
	}
}

func TestConditionalCheckpointRecoversCrashAfterAtomicSwap(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "default",
		"client_install_id": "install-checkpoint",
		"servers": {
			"default": {
				"profile_id": "profile-default",
				"route_policy": "local",
				"cloud": {"state": "authorizing"}
			}
		}
	}`)
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	profileRaw, err := cloudRecoveryProfileRaw(
		doc,
		defaultServerProfileName,
	)
	if err != nil {
		t.Fatal(err)
	}
	previousHook := conditionalJSONAfterSwap
	conditionalJSONAfterSwap = func(string) error {
		return errors.New("simulated power loss")
	}
	err = writeCloudLifecycleFieldRaw(
		paths,
		doc,
		defaultServerProfileName,
		profileRaw,
		"recovery_hold",
		json.RawMessage(
			`{"code":"cloud_authorization","remediation":"inspect_security"}`,
		),
	)
	conditionalJSONAfterSwap = previousHook
	if err == nil {
		t.Fatal("simulated power loss succeeded")
	}
	if _, err := loadConfigDocument(
		paths.ConfigFile,
	); err == nil {
		t.Fatal("reader bypassed pending transaction")
	}
	if err := recoverConditionalJSONTransaction(
		paths.ConfigFile,
	); err != nil {
		t.Fatalf("recover transaction: %v", err)
	}
	recovered, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, ok := recovered.flatProfile(
		defaultServerProfileName,
	)
	if !ok ||
		cfg.Cloud == nil ||
		cfg.Cloud.RecoveryHold == nil {
		t.Fatalf(
			"durable checkpoint was not recovered: %+v raw=%s",
			cfg,
			recovered.source,
		)
	}
}
