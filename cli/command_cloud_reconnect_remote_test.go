package main

import (
	"strings"
	"testing"
)

func TestCloudReconnectAcceptsExplicitRemoteURL(t *testing.T) {
	resetServerProfileSelection(t)
	flags, err := parseCloudCommandFlags(
		"reconnect",
		[]string{
			"--server",
			"cabin",
			"--url",
			productionCloudTestOrigin,
		},
	)
	if err != nil {
		t.Fatalf("parse reconnect URL: %v", err)
	}
	if flags.server != "cabin" || flags.url != productionCloudTestOrigin {
		t.Fatalf("reconnect flags = %+v", flags)
	}
}

func TestHybridCloudReconnectUsesSavedRemoteOriginAwayFromLAN(t *testing.T) {
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-away"
	cfg.RelayInstanceID = "relay-away"
	current := cloudMetadataForTest(strings.Repeat("f", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	cfg.RoutePolicy = routePolicyAutomatic

	if got := cloudRemoteURLForCommand(
		cfg,
		cloudCommandFlags{},
		true,
	); got != current.Origin {
		t.Fatalf("hybrid reconnect URL = %q want %q", got, current.Origin)
	}
	explicit := "https://explicit.ui.nabu.casa"
	if got := cloudRemoteURLForCommand(
		cfg,
		cloudCommandFlags{url: explicit},
		true,
	); got != explicit {
		t.Fatalf("explicit reconnect URL = %q", got)
	}
	if got := cloudRemoteURLForCommand(
		cfg,
		cloudCommandFlags{},
		false,
	); got != "" {
		t.Fatalf("hybrid initial add unexpectedly selected remote URL %q", got)
	}
}
