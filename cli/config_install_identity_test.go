package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSaveRejectsReplacingInstallWideClientIdentity(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)
	setServerSelectionOverride("cabin")
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ClientInstallID = "inst-replacement"

	if err := saveConfig(paths, cfg); err == nil ||
		!strings.Contains(err.Error(), "immutable client_install_id") {
		t.Fatalf("client_install_id replacement error=%v", err)
	}
	top := readTestConfigTopLevel(t, paths)
	if string(top["client_install_id"]) != `"inst-abc"` {
		t.Fatalf("client_install_id changed to %s", top["client_install_id"])
	}
}

func TestLegacyMirrorFollowsLiteralDefaultNotDefaultServer(t *testing.T) {
	// The flat mirror pairs with the machine-wide legacy token in old binaries,
	// so it must always carry the LITERAL default profile — never the profile
	// default_server points at.
	resetServerProfileSelection(t)
	doc, err := parseConfigDocument([]byte(`{"schema_version":2,"default_server":"cabin","servers":{"default":{"relay_base_url":"http://home:8791"},"cabin":{"relay_base_url":"http://cabin:8791"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	top, err := doc.withProfile(
		"cabin",
		runtimeConfig{
			RelayBaseURL: "http://cabin:8791",
			HAHost:       "cabin.local",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var mirror serverProfileConfig
	raw, _ := json.Marshal(top)
	if err := json.Unmarshal(raw, &mirror); err != nil {
		t.Fatal(err)
	}
	if mirror.RelayBaseURL != "http://home:8791" {
		t.Fatalf(
			"mirror relay_base_url = %q, want literal default",
			mirror.RelayBaseURL,
		)
	}

	// Without a literal default profile there is no mirror at all: an old
	// binary honestly reports "not set up yet" instead of pairing the legacy
	// token with a named profile's server.
	fresh := &configDocument{top: map[string]json.RawMessage{}}
	top, err = fresh.withProfile(
		"cabin",
		runtimeConfig{RelayBaseURL: "http://cabin:8791"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := top["relay_base_url"]; ok {
		t.Fatal("no literal default profile: flat mirror must be absent")
	}
}
