package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/brutella/dnssd"
)

func TestDiscoverReachableHAHostsUsesSavedRelayHostImmediately(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	t.Cleanup(func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
	})

	discoverHAViaMDNSForDiscovery = func() []setupDiscoveryProbe {
		t.Fatal("mDNS must not run after the saved Relay host succeeds")
		return nil
	}
	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		if input != "192.168.1.5" {
			t.Fatalf("probe input = %q, want saved Relay host", input)
		}
		return "http://192.168.1.5:8123", nil
	}

	found, fallback := discoverReachableHAHosts(runtimeConfig{
		RelayBaseURL: "http://192.168.1.5:8791",
	})
	want := setupDiscoveryCandidate{
		Host:   "192.168.1.5",
		HAURL:  "http://192.168.1.5:8123",
		Source: "saved Relay address",
	}
	if len(found) != 1 || found[0] != want || fallback != "192.168.1.5" {
		t.Fatalf("discovery = (%+v, %q), want ([%+v], %q)", found, fallback, want, "192.168.1.5")
	}
}

func TestCollectDiscoveryProbesNeverEnumeratesNetworkNeighbors(t *testing.T) {
	originalMDNS := discoverHAViaMDNSForDiscovery
	t.Cleanup(func() { discoverHAViaMDNSForDiscovery = originalMDNS })
	discoverHAViaMDNSForDiscovery = func() []setupDiscoveryProbe {
		return []setupDiscoveryProbe{
			{Host: "http://192.168.1.5:8123", Source: "mDNS: Home"},
			{Host: "http://192.168.1.6:8123", Source: "mDNS: Cabin"},
		}
	}

	probes := collectDiscoveryProbes(runtimeConfig{})
	want := []string{
		"http://192.168.1.5:8123",
		"http://192.168.1.6:8123",
	}
	if len(probes) != len(want) {
		t.Fatalf("probes = %+v, want %d official/name candidates", probes, len(want))
	}
	for idx := range want {
		if probes[idx].Host != want[idx] {
			t.Fatalf("probes[%d] = %+v, want host %q", idx, probes[idx], want[idx])
		}
	}
}

func TestDiscoverHAViaMDNSCollectsAllAnnouncedInstances(t *testing.T) {
	originalLookup := lookupDNSSDForDiscovery
	t.Cleanup(func() { lookupDNSSDForDiscovery = originalLookup })
	lookupDNSSDForDiscovery = func(
		ctx context.Context,
		_ string,
		add dnssd.AddFunc,
		_ dnssd.RmvFunc,
	) error {
		add(dnssd.BrowseEntry{Text: map[string]string{
			"uuid":          "8ad0218b813d4a3a8c2ef9ed84f296e8",
			"location_name": "Cabin",
			"internal_url":  "http://192.168.1.6:8123",
		}})
		select {
		case <-time.After(800 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		add(dnssd.BrowseEntry{Text: map[string]string{
			"uuid":          "556851affd194582ad3f150856f13a05",
			"location_name": "Home",
			"internal_url":  "http://192.168.1.5:8123",
		}})
		return nil
	}

	got := discoverHAViaMDNS()
	if len(got) != 2 ||
		got[0].Host != "http://192.168.1.5:8123" ||
		got[0].Source != "mDNS: Home" ||
		got[1].Host != "http://192.168.1.6:8123" ||
		got[1].Source != "mDNS: Cabin" {
		t.Fatalf("discoverHAViaMDNS() = %+v", got)
	}
}

func TestHomeAssistantDNSSDURLRequiresOfficialMetadata(t *testing.T) {
	valid := dnssd.BrowseEntry{Text: map[string]string{
		"uuid":         "556851affd194582ad3f150856f13a05",
		"internal_url": "http://192.168.1.5:8123/",
	}}
	if got := homeAssistantDNSSDURL(valid); got != "http://192.168.1.5:8123" {
		t.Fatalf("homeAssistantDNSSDURL(valid) = %q", got)
	}
	localHTTPSURL := dnssd.BrowseEntry{
		IPs: []net.IP{net.ParseIP("192.168.1.5")},
		Text: map[string]string{
			"uuid":         "556851affd194582ad3f150856f13a05",
			"internal_url": "https://homeassistant.local:9123/",
		},
	}
	if got := homeAssistantDNSSDURL(localHTTPSURL); got != "https://homeassistant.local:9123" {
		t.Fatalf("homeAssistantDNSSDURL(local HTTPS URL) = %q", got)
	}
	localHTTPURL := localHTTPSURL
	localHTTPURL.Text = map[string]string{
		"uuid":         "556851affd194582ad3f150856f13a05",
		"internal_url": "http://homeassistant.local:9123/",
	}
	if got := homeAssistantDNSSDURL(localHTTPURL); got != "http://192.168.1.5:9123" {
		t.Fatalf("homeAssistantDNSSDURL(local HTTP URL) = %q", got)
	}
	fallback := dnssd.BrowseEntry{
		Host: "556851affd194582ad3f150856f13a05.local.",
		Port: 8123,
		IPs:  []net.IP{net.ParseIP("192.168.1.5")},
		Text: map[string]string{
			"uuid":         "556851affd194582ad3f150856f13a05",
			"internal_url": "",
		},
	}
	if got := homeAssistantDNSSDURL(fallback); got != "http://192.168.1.5:8123" {
		t.Fatalf("homeAssistantDNSSDURL(fallback) = %q", got)
	}

	for name, entry := range map[string]dnssd.BrowseEntry{
		"missing uuid": {
			Text: map[string]string{"internal_url": "http://192.168.1.5:8123"},
		},
		"invalid uuid": {
			Text: map[string]string{
				"uuid":         "not-a-home-assistant-uuid",
				"internal_url": "http://192.168.1.5:8123",
			},
		},
		"missing fallback host": {
			Port: 8123,
			Text: map[string]string{"uuid": "556851affd194582ad3f150856f13a05"},
		},
		"mismatched fallback host": {
			Host: "different.local.",
			Port: 8123,
			Text: map[string]string{"uuid": "556851affd194582ad3f150856f13a05"},
		},
		"unsafe URL": {
			Text: map[string]string{
				"uuid":         "556851affd194582ad3f150856f13a05",
				"internal_url": "file:///tmp/home-assistant",
			},
		},
		"URL credentials": {
			Text: map[string]string{
				"uuid":         "556851affd194582ad3f150856f13a05",
				"internal_url": "http://user:secret@192.168.1.5:8123",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := homeAssistantDNSSDURL(entry); got != "" {
				t.Fatalf("homeAssistantDNSSDURL() = %q, want blank", got)
			}
		})
	}
}

func TestSafeDNSSDNameRemovesTerminalControls(t *testing.T) {
	if got := safeDNSSDName(" Home\x1b[31m\n"); got != "Home[31m" {
		t.Fatalf("safeDNSSDName() = %q", got)
	}
}

func TestDiscoverReachableHAHostsPreservesAdvertisedURL(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalIPResolve := resolveHostToIPv4ForDiscovery
	t.Cleanup(func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		resolveHostToIPv4ForDiscovery = originalIPResolve
	})

	discoverHAViaMDNSForDiscovery = func() []setupDiscoveryProbe {
		return []setupDiscoveryProbe{{
			Host:   "http://ha-box.local:9123",
			Source: "mDNS: Home",
		}}
	}
	resolveHostToIPv4ForDiscovery = func(host string, _ time.Duration) string {
		if host != "ha-box.local" {
			t.Fatalf("resolved host = %q", host)
		}
		return "192.168.1.5"
	}
	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		switch input {
		case "http://ha-box.local:9123", "http://192.168.1.5:9123":
			return input, nil
		}
		return "", assertDiscoveryFailure{}
	}

	found, _ := discoverReachableHAHosts(runtimeConfig{})
	if len(found) != 1 ||
		found[0].Host != "192.168.1.5" ||
		found[0].HAURL != "http://192.168.1.5:9123" ||
		found[0].Via != "ha-box.local" ||
		found[0].Source != "mDNS: Home" {
		t.Fatalf("found = %+v", found)
	}
}

func TestDiscoverReachableHAHostsPreservesDistinctPorts(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	t.Cleanup(func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
	})

	discoverHAViaMDNSForDiscovery = func() []setupDiscoveryProbe {
		return []setupDiscoveryProbe{
			{Host: "http://192.168.1.5:8123", Source: "mDNS: Home"},
			{Host: "http://192.168.1.5:9123", Source: "mDNS: Test"},
		}
	}
	resolveHAURLBaseWithinTimeoutForDiscovery = func(
		input string,
		_ time.Duration,
	) (string, error) {
		return strings.TrimRight(input, "/"), nil
	}

	found, _ := discoverReachableHAHosts(runtimeConfig{})
	if len(found) != 2 ||
		found[0].HAURL != "http://192.168.1.5:8123" ||
		found[1].HAURL != "http://192.168.1.5:9123" {
		t.Fatalf("found = %+v", found)
	}
}

func TestDiscoverReachableHAHostsSharesOverallDeadline(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalOverall := setupDiscoveryOverallTimeout
	originalProbe := setupDiscoveryMaxProbeTimeout
	t.Cleanup(func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		setupDiscoveryOverallTimeout = originalOverall
		setupDiscoveryMaxProbeTimeout = originalProbe
	})

	setupDiscoveryOverallTimeout = 50 * time.Millisecond
	setupDiscoveryMaxProbeTimeout = time.Second
	discoverHAViaMDNSForDiscovery = func() []setupDiscoveryProbe { return nil }
	resolveHAURLBaseWithinTimeoutForDiscovery = func(string, time.Duration) (string, error) {
		time.Sleep(10 * time.Millisecond)
		return "", assertDiscoveryFailure{}
	}

	started := time.Now()
	found, _ := discoverReachableHAHosts(runtimeConfig{})
	if elapsed := time.Since(started); len(found) != 0 || elapsed > 100*time.Millisecond {
		t.Fatalf("found = %+v elapsed = %s", found, elapsed)
	}
}

func TestDiscoverReachableHAHostsReservesTimeAfterStaleSavedAddress(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalOverall := setupDiscoveryOverallTimeout
	originalMDNSTimeout := setupDiscoveryMDNSTimeout
	originalReserve := setupDiscoveryCandidateProbeReserve
	originalProbe := setupDiscoveryMaxProbeTimeout
	t.Cleanup(func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		setupDiscoveryOverallTimeout = originalOverall
		setupDiscoveryMDNSTimeout = originalMDNSTimeout
		setupDiscoveryCandidateProbeReserve = originalReserve
		setupDiscoveryMaxProbeTimeout = originalProbe
	})

	setupDiscoveryOverallTimeout = 90 * time.Millisecond
	setupDiscoveryMDNSTimeout = 30 * time.Millisecond
	setupDiscoveryCandidateProbeReserve = 30 * time.Millisecond
	setupDiscoveryMaxProbeTimeout = 80 * time.Millisecond
	discoverHAViaMDNSForDiscovery = func() []setupDiscoveryProbe {
		time.Sleep(setupDiscoveryMDNSTimeout)
		return []setupDiscoveryProbe{{
			Host:   "http://192.168.1.5:8123",
			Source: "mDNS: Home",
		}}
	}
	resolveHAURLBaseWithinTimeoutForDiscovery = func(
		input string,
		timeout time.Duration,
	) (string, error) {
		if input == "http://stale.local:8123" {
			time.Sleep(timeout)
			return "", assertDiscoveryFailure{}
		}
		if input == "http://192.168.1.5:8123" {
			return input, nil
		}
		return "", assertDiscoveryFailure{}
	}

	found, _ := discoverReachableHAHosts(runtimeConfig{
		HAURL: "http://stale.local:8123",
	})
	if len(found) != 1 ||
		found[0].HAURL != "http://192.168.1.5:8123" {
		t.Fatalf("replacement discovery = %+v", found)
	}
}

type assertDiscoveryFailure struct{}

func (assertDiscoveryFailure) Error() string { return "unreachable" }
