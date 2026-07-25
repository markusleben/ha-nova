package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func cloudMetadataForTest(generation string) cloudConnectionMetadata {
	return cloudConnectionMetadata{
		Origin:               "https://example.ui.nabu.casa",
		CanonicalOrigin:      "https://example.ui.nabu.casa",
		OAuthClientID:        "http://127.0.0.1:49152/ha-nova",
		CredentialGeneration: generation,
		HAUserID:             "user-1",
	}
}

func TestConfigV3MigratesEveryProfileLocally(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)

	setServerSelectionOverride("cabin")
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RoutePolicy != routePolicyLocal {
		t.Fatalf("legacy route = %q, want local", cfg.RoutePolicy)
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	top := readTestConfigTopLevel(t, paths)
	if string(top["schema_version"]) != "3" {
		t.Fatalf("schema_version = %s, want 3", top["schema_version"])
	}
	var profiles map[string]serverProfileConfig
	if err := json.Unmarshal(top["servers"], &profiles); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for name, profile := range profiles {
		if err := validateProfileID(profile.ProfileID); err != nil {
			t.Fatalf("%s profile id: %v", name, err)
		}
		if ids[profile.ProfileID] {
			t.Fatalf("duplicate profile id %q", profile.ProfileID)
		}
		ids[profile.ProfileID] = true
		if profile.RoutePolicy != routePolicyLocal || profile.Cloud != nil {
			t.Fatalf("%s migrated to unsafe defaults: %+v", name, profile)
		}
	}
}

func TestConfigV3ProfileIdentityImmutableAndCloudNeverMirrored(t *testing.T) {
	resetServerProfileSelection(t)
	metadata := cloudMetadataForTest(strings.Repeat("a", 32))
	cfg := runtimeConfig{
		HAHost:          "ha",
		HAURL:           "http://ha:8123",
		RelayBaseURL:    "http://ha:8791",
		ProfileID:       "profile-fixed",
		RelayInstanceID: "relay-fixed",
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	top := readTestConfigTopLevel(t, paths)
	for _, forbidden := range []string{"profile_id", "relay_instance_id", "route_policy", "cloud"} {
		if _, ok := top[forbidden]; ok {
			t.Fatalf("%s leaked into legacy flat mirror", forbidden)
		}
	}

	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ProfileID = "profile-replaced"
	if err := saveConfig(paths, cfg); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("profile id replacement error = %v", err)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected profile id replacement changed config")
	}
}

func TestConfigV3PreservesUnknownProfileAndCloudFields(t *testing.T) {
	resetServerProfileSelection(t)
	raw := `{
	  "schema_version":3,
	  "default_server":"default",
	  "servers":{
	    "default":{
	      "ha_host":"ha",
	      "ha_url":"http://ha:8123",
	      "relay_base_url":"http://ha:8791",
	      "profile_id":"profile-fixed",
	      "relay_instance_id":"relay-fixed",
	      "route_policy":"cloud",
	      "future_profile":{"keep":true},
	      "cloud":{
	        "state":"ready",
	        "future_lifecycle":"keep",
	        "current":{
	          "origin":"https://example.ui.nabu.casa",
	          "canonical_origin":"https://example.ui.nabu.casa",
	          "oauth_client_id":"http://127.0.0.1:49152/ha-nova",
	          "credential_generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	          "ha_user_id":"user-1",
	          "future_claim":17
	        }
	      }
	    }
	  }
	}`
	paths := writeTestConfigFile(t, raw)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelayBaseURL = "http://ha:9000"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	top := readTestConfigTopLevel(t, paths)
	var profiles map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &profiles); err != nil {
		t.Fatal(err)
	}
	profile := profiles["default"]
	var futureProfile struct {
		Keep bool `json:"keep"`
	}
	if err := json.Unmarshal(profile["future_profile"], &futureProfile); err != nil || !futureProfile.Keep {
		t.Fatalf("unknown profile field lost: %s", profile["future_profile"])
	}
	var cloud map[string]json.RawMessage
	if err := json.Unmarshal(profile["cloud"], &cloud); err != nil {
		t.Fatal(err)
	}
	if string(cloud["future_lifecycle"]) != `"keep"` {
		t.Fatalf("unknown Cloud lifecycle field lost: %s", cloud["future_lifecycle"])
	}
	var current map[string]json.RawMessage
	if err := json.Unmarshal(cloud["current"], &current); err != nil {
		t.Fatal(err)
	}
	if string(current["future_claim"]) != "17" {
		t.Fatalf("unknown current metadata field lost: %s", current["future_claim"])
	}
}

func TestConfigV3CloudLifecycleValidationAndLegacyReadyDefault(t *testing.T) {
	metadata := cloudMetadataForTest(strings.Repeat("b", 32))
	legacy := &cloudLifecycleMetadata{Current: &metadata}
	if err := normalizeCloudLifecycle(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.State != cloudStateReady {
		t.Fatalf("completed legacy state = %q, want ready", legacy.State)
	}

	for _, test := range []struct {
		name  string
		cloud *cloudLifecycleMetadata
	}{
		{"unknown state", &cloudLifecycleMetadata{State: "future", Current: &metadata}},
		{"ambiguous missing state", &cloudLifecycleMetadata{Pending: &metadata}},
		{"ready with pending", &cloudLifecycleMetadata{State: cloudStateReady, Current: &metadata, Pending: &metadata}},
		{"token stored without pending", &cloudLifecycleMetadata{State: cloudStateTokenStored}},
		{
			"verified without user identity",
			&cloudLifecycleMetadata{
				State: cloudStateCloudVerified,
				Pending: &cloudConnectionMetadata{
					Origin:               metadata.Origin,
					CanonicalOrigin:      metadata.CanonicalOrigin,
					OAuthClientID:        metadata.OAuthClientID,
					CredentialGeneration: metadata.CredentialGeneration,
				},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := test.cloud
			if err := normalizeCloudLifecycle(&value); err == nil {
				t.Fatal("expected lifecycle validation error")
			}
		})
	}
}

func TestConfigV3CloudLifecycleStatesRoundTrip(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("2", 32))
	pending := cloudMetadataForTest(strings.Repeat("3", 32))
	cases := []cloudLifecycleMetadata{
		{State: cloudStateAuthorizing},
		{State: cloudStateAuthorizing, Pending: &pending},
		{State: cloudStateTokenStored, Pending: &pending},
		{State: cloudStateCloudVerified, Pending: &pending},
		{State: cloudStateDeviceBoundOrPaired, Pending: &pending},
		{State: cloudStateRollingBack, Current: &current, Pending: &pending},
		{State: cloudStateCommitted, Current: &current, Pending: &current},
		{State: cloudStateRetiringPrevious, Current: &current},
		{State: cloudStateReady, Current: &current},
	}
	for _, lifecycle := range cases {
		t.Run(string(lifecycle.State), func(t *testing.T) {
			raw, err := json.Marshal(lifecycle)
			if err != nil {
				t.Fatal(err)
			}
			var decoded *cloudLifecycleMetadata
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if err := normalizeCloudLifecycle(&decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.State != lifecycle.State {
				t.Fatalf("state = %q, want %q", decoded.State, lifecycle.State)
			}
		})
	}
}

func TestConfigV3RejectsCloudWithoutProfileAndInvalidRelayInstance(t *testing.T) {
	metadata := cloudMetadataForTest(strings.Repeat("c", 32))
	for _, test := range []struct {
		name string
		cfg  runtimeConfig
		want string
	}{
		{
			name: "missing profile id",
			cfg: runtimeConfig{
				RoutePolicy: routePolicyCloud,
				Cloud:       &cloudLifecycleMetadata{State: cloudStateReady, Current: &metadata},
			},
			want: "profile_id",
		},
		{
			name: "invalid relay id",
			cfg: runtimeConfig{
				ProfileID:       "profile-valid",
				RelayInstanceID: " relay\n",
				RoutePolicy:     routePolicyLocal,
			},
			want: "relay_instance_id",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := test.cfg
			if err := validateLoadedRuntimeConfig(&cfg); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestConfigV3LoadsReadyCloudOnlyButRejectsAutomaticWithoutLocal(t *testing.T) {
	resetServerProfileSelection(t)
	metadata := cloudMetadataForTest(strings.Repeat("1", 32))
	cloud := cloudLifecycleMetadata{State: cloudStateReady, Current: &metadata}
	cloudRaw, err := json.Marshal(cloud)
	if err != nil {
		t.Fatal(err)
	}
	config := `{"schema_version":3,"default_server":"default","servers":{"default":{` +
		`"profile_id":"profile-cloud","relay_instance_id":"relay-cloud","route_policy":"cloud","cloud":` +
		string(cloudRaw) + `}}}`
	paths := writeTestConfigFile(t, config)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatalf("ready Cloud-only profile did not load: %v", err)
	}
	if cfg.RelayBaseURL != "" || cfg.RoutePolicy != routePolicyCloud || !cfg.Cloud.configured() {
		t.Fatalf("Cloud-only profile = %+v", cfg)
	}

	config = strings.Replace(config, `"route_policy":"cloud"`, `"route_policy":"automatic"`, 1)
	paths = writeTestConfigFile(t, config)
	if _, err := loadConfig(paths); err == nil || !strings.Contains(err.Error(), "completed local device pairing") {
		t.Fatalf("automatic Cloud-only load error = %v", err)
	}
}

func TestConfigV3RejectsDuplicateProfileIDs(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
	  "schema_version":3,
	  "default_server":"default",
	  "servers":{
	    "default":{"relay_base_url":"http://ha:8791","profile_id":"profile-same","route_policy":"local"},
	    "cabin":{"relay_base_url":"http://cabin:8791","profile_id":"profile-same","route_policy":"local"}
	  }
	}`)
	setServerSelectionOverride("cabin")
	if _, err := loadConfig(paths); err == nil || !strings.Contains(err.Error(), "share profile_id") {
		t.Fatalf("duplicate profile id load error = %v", err)
	}
}

func TestConfigV3DoesNotCarryUnknownCredentialFieldsAcrossGeneration(t *testing.T) {
	existing := json.RawMessage(`{
	  "cloud":{
	    "state":"ready",
	    "current":{
	      "origin":"https://example.ui.nabu.casa",
	      "canonical_origin":"https://example.ui.nabu.casa",
	      "oauth_client_id":"http://127.0.0.1:49152/ha-nova",
	      "credential_generation":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	      "ha_user_id":"user-1",
	      "future_generation_bound_claim":"old"
	    }
	  }
	}`)
	replacement := cloudMetadataForTest(strings.Repeat("4", 32))
	merged, err := mergeServerProfileRaw(existing, serverProfileConfig{
		ProfileID:       "profile-fixed",
		RelayInstanceID: "relay-fixed",
		RoutePolicy:     routePolicyCloud,
		Cloud:           &cloudLifecycleMetadata{State: cloudStateReady, Current: &replacement},
	})
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(merged, &profile); err != nil {
		t.Fatal(err)
	}
	var lifecycle map[string]json.RawMessage
	if err := json.Unmarshal(profile["cloud"], &lifecycle); err != nil {
		t.Fatal(err)
	}
	var current map[string]json.RawMessage
	if err := json.Unmarshal(lifecycle["current"], &current); err != nil {
		t.Fatal(err)
	}
	if _, exists := current["future_generation_bound_claim"]; exists {
		t.Fatal("generation-bound unknown metadata leaked into replacement credentials")
	}
}

func TestConfigV3PromotesUnknownPendingFieldsWithSameGeneration(t *testing.T) {
	generation := strings.Repeat("6", 32)
	existing := json.RawMessage(`{
	  "cloud":{
	    "state":"device_bound_or_paired",
	    "pending":{
	      "origin":"https://example.ui.nabu.casa",
	      "canonical_origin":"https://example.ui.nabu.casa",
	      "oauth_client_id":"http://127.0.0.1:49152/ha-nova",
	      "credential_generation":"` + generation + `",
	      "ha_user_id":"user-1",
	      "future_generation_bound_claim":"new"
	    }
	  }
	}`)
	replacement := cloudMetadataForTest(generation)
	merged, err := mergeServerProfileRaw(existing, serverProfileConfig{
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateCommitted,
			Current: &replacement,
			Pending: &replacement,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Cloud struct {
			Current map[string]json.RawMessage `json:"current"`
		} `json:"cloud"`
	}
	if err := json.Unmarshal(merged, &profile); err != nil {
		t.Fatal(err)
	}
	if string(profile.Cloud.Current["future_generation_bound_claim"]) != `"new"` {
		t.Fatalf("pending extension was not promoted: %s", merged)
	}
}
