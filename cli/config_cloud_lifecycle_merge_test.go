package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestCloudLifecycleMergeKeysCoverEveryKnownJSONField(t *testing.T) {
	known := make(map[string]bool)
	lifecycleType := reflect.TypeOf(cloudLifecycleMetadata{})
	for index := 0; index < lifecycleType.NumField(); index++ {
		tag := lifecycleType.Field(index).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			t.Fatalf(
				"Cloud lifecycle field %s has no mergeable JSON name",
				lifecycleType.Field(index).Name,
			)
		}
		known[name] = true
	}
	for _, name := range cloudLifecycleFieldKeys {
		if !known[name] {
			t.Fatalf("Cloud lifecycle merge key %q is not a known field", name)
		}
		delete(known, name)
	}
	if len(known) != 0 {
		t.Fatalf("Cloud lifecycle fields missing from merge keys: %v", known)
	}
}

func TestConfigCloudLifecycleCheckpointsCanBeDurablyCleared(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	pending := cloudMetadataForTest(strings.Repeat("a", 32))
	deviceID := strings.Repeat("d", 22)
	cfg := runtimeConfig{
		RelayBaseURL: "http://ha:8791",
		ProfileID:    "profile-checkpoint-clear",
		RoutePolicy:  routePolicyLocal,
		Cloud: &cloudLifecycleMetadata{
			State:                    cloudStateCloudVerified,
			Pending:                  &pending,
			DeviceActivationStarted:  true,
			DeviceActivationDeviceID: deviceID,
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	addUnknownCloudLifecycleFieldForTest(t, paths)

	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	request := newCloudSetupRequest(
		&cfg,
		func(value runtimeConfig) error {
			return saveConfig(paths, value)
		},
	)
	if err := request.ClearDeviceActivation(); err != nil {
		t.Fatal(err)
	}
	assertCloudLifecycleFieldsAbsentForTest(
		t,
		paths,
		"device_activation_started",
		"device_activation_device_id",
	)

	cfg, err = loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Cloud.State = cloudStateDeviceBoundOrPaired
	cfg.Cloud.DeviceActivationStarted = true
	cfg.Cloud.DeviceActivationDeviceID = deviceID
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	result := cloudSetupResult{
		Current:         pending,
		RelayInstanceID: "relay-checkpoint-clear",
	}
	cfg, err = commitCloudConnection(
		context.Background(),
		cfg,
		result,
		successfulCloudCoordinatorForTest(),
		func(value runtimeConfig) error {
			return saveConfig(paths, value)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertCloudLifecycleFieldsAbsentForTest(
		t,
		paths,
		"device_activation_started",
		"device_activation_device_id",
	)
	if reloaded, loadErr := loadConfig(paths); loadErr != nil {
		t.Fatalf("committed Cloud config did not reload: %v", loadErr)
	} else {
		cfg = reloaded
	}

	cfg.Cloud.DeviceRevocationCompleted = &cloudDeviceRevocationCheckpoint{
		CurrentDeviceID: deviceID,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cloudLifecycleFieldsForTest(
		t,
		paths,
	)["device_revocation_completed"]; !ok {
		t.Fatal("device revocation checkpoint was not persisted")
	}
	cleared, err := currentCloudDeviceRevocationConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, cleared); err != nil {
		t.Fatal(err)
	}
	assertCloudLifecycleFieldsAbsentForTest(
		t,
		paths,
		"device_revocation_completed",
	)
	if _, err := loadConfig(paths); err != nil {
		t.Fatalf("cleared revocation config did not reload: %v", err)
	}

	cleared.Cloud = nil
	cleared.RelayInstanceID = ""
	cleared.RoutePolicy = routePolicyLocal
	if err := saveConfig(paths, cleared); err != nil {
		t.Fatal(err)
	}
	assertCloudLifecycleFieldsAbsentForTest(
		t,
		paths,
		cloudLifecycleFieldKeys...,
	)
	if reloaded, err := loadConfig(paths); err != nil {
		t.Fatalf("removed Cloud config did not reload: %v", err)
	} else if reloaded.Cloud != nil {
		t.Fatalf("removed Cloud lifecycle was restored: %+v", reloaded.Cloud)
	}
}

func addUnknownCloudLifecycleFieldForTest(
	t *testing.T,
	paths runtimePaths,
) {
	t.Helper()
	top := readTestConfigTopLevel(t, paths)
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(servers[defaultServerProfileName], &profile); err != nil {
		t.Fatal(err)
	}
	var cloud map[string]json.RawMessage
	if err := json.Unmarshal(profile["cloud"], &cloud); err != nil {
		t.Fatal(err)
	}
	cloud["future_lifecycle"] = json.RawMessage(`{"keep":true}`)
	var err error
	profile["cloud"], err = json.Marshal(cloud)
	if err != nil {
		t.Fatal(err)
	}
	servers[defaultServerProfileName], err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	top["servers"], err = json.Marshal(servers)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertCloudLifecycleFieldsAbsentForTest(
	t *testing.T,
	paths runtimePaths,
	fields ...string,
) {
	t.Helper()
	cloud := cloudLifecycleFieldsForTest(t, paths)
	for _, field := range fields {
		if _, ok := cloud[field]; ok {
			t.Fatalf("cleared Cloud lifecycle field %q remained: %s", field, cloud[field])
		}
	}
	var future struct {
		Keep bool `json:"keep"`
	}
	if err := json.Unmarshal(cloud["future_lifecycle"], &future); err != nil ||
		!future.Keep {
		t.Fatalf(
			"unknown Cloud lifecycle field was not preserved: %s",
			cloud["future_lifecycle"],
		)
	}
}

func cloudLifecycleFieldsForTest(
	t *testing.T,
	paths runtimePaths,
) map[string]json.RawMessage {
	t.Helper()
	top := readTestConfigTopLevel(t, paths)
	var profiles map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &profiles); err != nil {
		t.Fatal(err)
	}
	var cloud map[string]json.RawMessage
	if err := json.Unmarshal(
		profiles[defaultServerProfileName]["cloud"],
		&cloud,
	); err != nil {
		t.Fatal(err)
	}
	return cloud
}
