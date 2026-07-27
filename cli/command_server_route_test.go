package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const routeCommandConfig = `{
  "schema_version":3,
  "default_server":"default",
  "servers":{
    "default":{
      "ha_host":"ha",
      "ha_url":"http://ha:8123",
      "relay_base_url":"http://ha:8791",
      "profile_id":"profile-home",
      "route_policy":"local"
    },
    "cabin":{
      "ha_host":"cabin",
      "ha_url":"http://cabin:8123",
      "relay_base_url":"http://cabin:8791",
      "relay_secure_base_url":"https://cabin:18792",
      "relay_spki_pin":"PIN",
      "profile_id":"profile-cabin",
      "relay_instance_id":"relay-cabin",
      "route_policy":"local",
      "future_profile":"keep",
      "cloud":{
        "state":"ready",
        "current":{
          "origin":"https://example.ui.nabu.casa",
          "canonical_origin":"https://example.ui.nabu.casa",
          "oauth_client_id":"http://127.0.0.1:49152/ha-nova",
          "credential_generation":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
          "ha_user_id":"user-1"
        }
      }
    }
  }
}`

func TestServerRouteParsesDocumentedPolicyFirstSyntax(t *testing.T) {
	opts, err := parseServerRouteOptions([]string{"automatic", "--server", "cabin"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Policy != routePolicyAutomatic || !opts.ServerSet || opts.Server != "cabin" {
		t.Fatalf("options = %+v", opts)
	}
}

func TestServerRoutePersistsSelectedProfileAndUnknownFields(t *testing.T) {
	paths := setupServerCommandTest(t, routeCommandConfig)
	exit, out := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"route", "automatic", "--server", "cabin"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d\n%s", exit, out)
	}
	top := readTestConfigTopLevel(t, paths)
	var profiles map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &profiles); err != nil {
		t.Fatal(err)
	}
	if string(profiles["cabin"]["route_policy"]) != `"automatic"` {
		t.Fatalf("route = %s", profiles["cabin"]["route_policy"])
	}
	if string(profiles["cabin"]["profile_id"]) != `"profile-cabin"` {
		t.Fatalf("profile id changed: %s", profiles["cabin"]["profile_id"])
	}
	if string(profiles["cabin"]["future_profile"]) != `"keep"` {
		t.Fatalf("unknown field lost: %s", profiles["cabin"]["future_profile"])
	}
}

func TestServerRouteRejectsCloudWithoutCompletedCloudSetup(t *testing.T) {
	paths := setupServerCommandTest(t, routeCommandConfig)
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	exit, out := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"route", "cloud", "--server", "default"})
	})
	if exit != 1 || !strings.Contains(out, "no completed Home Assistant Cloud setup") {
		t.Fatalf("exit=%d output=%s", exit, out)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("rejected route change touched config")
	}
}

func TestServerRouteRejectsAutomaticWithoutLocalPairing(t *testing.T) {
	config := strings.Replace(routeCommandConfig,
		`"relay_secure_base_url":"https://cabin:18792",`, "", 1)
	config = strings.Replace(config, `"relay_spki_pin":"PIN",`, "", 1)
	paths := setupServerCommandTest(t, config)
	exit, out := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"route", "automatic", "--server", "cabin"})
	})
	if exit != 1 || !strings.Contains(out, "requires both local and Cloud transports") {
		t.Fatalf("exit=%d output=%s", exit, out)
	}
}

func TestServerListShowsCloudReadiness(t *testing.T) {
	paths := setupServerCommandTest(t, routeCommandConfig)
	exit, out := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"list"})
	})
	if exit != 0 {
		t.Fatalf("exit=%d output=%s", exit, out)
	}
	row := serverListRow(t, out, "cabin")
	if len(row) < 6 || row[3] != "local" || row[5] != "ready" {
		t.Fatalf("cabin row = %v", row)
	}
}

func TestServerRouteAllowsFailClosedLocalPolicyForCloudOnlyProfile(t *testing.T) {
	config := strings.Replace(routeCommandConfig, `"relay_base_url":"http://cabin:8791",`, "", 1)
	paths := setupServerCommandTest(t, config)
	exit, out := captureCommandOutput(t, func() int {
		return runServerCommand(paths, []string{"route", "local", "--server", "cabin"})
	})
	if exit != 0 ||
		!strings.Contains(out, "Cloud routing is disabled") ||
		!strings.Contains(out, "will stay offline") {
		t.Fatalf("exit=%d output=%s", exit, out)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, exists := doc.flatProfile("cabin")
	if !exists {
		t.Fatal("cabin profile disappeared")
	}
	if cfg.RoutePolicy != routePolicyLocal {
		t.Fatalf("fail-closed route policy = %q", cfg.RoutePolicy)
	}
}
