package main

import (
	"encoding/json"
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
