package main

import (
	"os"
	"strings"
	"testing"
)

func TestConfigV3NormalizesEmptyCloudLifecycleForRuntime(t *testing.T) {
	cfg := runtimeConfig{
		RoutePolicy: routePolicyLocal,
		Cloud:       &cloudLifecycleMetadata{},
	}
	if err := validateLoadedRuntimeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud != nil {
		t.Fatalf("empty Cloud lifecycle was not normalized away: %+v", cfg.Cloud)
	}
}

func TestConfigV3RejectsCommittedMetadataMismatch(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("7", 32))
	pending := current
	pending.HAUserID = "different-user"
	cloud := &cloudLifecycleMetadata{
		State:   cloudStateCommitted,
		Current: &current,
		Pending: &pending,
	}
	if err := normalizeCloudLifecycle(&cloud); err == nil {
		t.Fatal("committed lifecycle accepted different metadata under one generation")
	}
}

func TestConfigV3PersistsSetupGeneratedIdentityDuringLegacyMigration(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)
	setServerSelectionOverride("cabin")

	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProfileID != "" {
		t.Fatalf("legacy profile unexpectedly had an identity: %q", cfg.ProfileID)
	}
	if err := ensureProfileIdentityForSetup(paths, &cfg); err != nil {
		t.Fatal(err)
	}
	generated := cfg.ProfileID
	if generated == "" {
		t.Fatal("setup did not generate a profile identity")
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("persist setup-generated profile identity: %v", err)
	}

	reloaded, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ProfileID != generated {
		t.Fatalf(
			"persisted profile identity = %q, want generated %q",
			reloaded.ProfileID,
			generated,
		)
	}
}

func TestConfigV3RefusesToOverwriteFutureSchema(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version":4,
		"default_server":"default",
		"servers":{
			"default":{
				"profile_id":"profile-future",
				"route_policy":"local",
				"relay_base_url":"http://ha:8791"
			}
		}
	}`)
	if _, err := loadConfig(paths); err == nil ||
		!strings.Contains(err.Error(), "newer than this HA NOVA build") {
		t.Fatalf("future schema load error = %v", err)
	}
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		ProfileID:    "profile-future",
		RoutePolicy:  routePolicyLocal,
		RelayBaseURL: "http://changed:8791",
	}
	if err := saveConfig(paths, cfg); err == nil ||
		!strings.Contains(err.Error(), "newer than this HA NOVA build") {
		t.Fatalf("future schema save error = %v", err)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("future schema document was overwritten")
	}
}

func TestConfigV3RefusesToOverwriteUnreadableDocument(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":3,"servers":`)
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(
		paths,
		runtimeConfig{
			ProfileID:    "profile-repair",
			RoutePolicy:  routePolicyLocal,
			RelayBaseURL: "http://ha:8791",
		},
	); err == nil || !strings.Contains(err.Error(), "read existing server configuration") {
		t.Fatalf("unreadable config save error = %v", err)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("unreadable config document was overwritten")
	}
}

func TestConfigV3RejectsNullDocumentShapes(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "null document", raw: `null`},
		{name: "null servers map", raw: `{"schema_version":3,"servers":null}`},
		{
			name: "null sibling profile",
			raw: `{
				"schema_version":3,
				"default_server":"default",
				"servers":{
					"default":{
						"profile_id":"profile-default",
						"route_policy":"local",
						"relay_base_url":"http://ha:8791"
					},
					"cabin":null
				}
			}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(t, test.raw)
			if _, err := loadConfig(paths); err == nil {
				t.Fatalf("null config shape loaded: %s", test.raw)
			}
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if err := saveConfig(
				paths,
				runtimeConfig{
					ProfileID:    "profile-default",
					RoutePolicy:  routePolicyLocal,
					RelayBaseURL: "http://changed:8791",
				},
			); err == nil {
				t.Fatalf("null config shape was writable: %s", test.raw)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("null config shape was overwritten")
			}
		})
	}
}
