package main

import (
	"fmt"
	"testing"
	"time"
)

func TestDiscoverReachableHAHostsReturnsAllInPriorityOrderAndDeduplicatesResolvedHosts(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	originalIPResolve := resolveHostToIPv4ForDiscovery
	t.Cleanup(func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
		resolveHostToIPv4ForDiscovery = originalIPResolve
	})

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		switch input {
		case "saved.local":
			return "http://saved.local:8123", nil
		case "other.local":
			return "https://other.local", nil
		case "10.0.0.1":
			return "http://10.0.0.1:8123", nil
		case "10.0.0.2":
			return "https://10.0.0.2", nil
		case "10.0.0.3":
			return "http://10.0.0.3:8123", nil
		default:
			return "", assertDiscoveryFailure{}
		}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "other.local" }
	collectARPHostsForDiscovery = func() []string { return []string{"10.0.0.3"} }
	resolveHostToIPv4ForDiscovery = func(host string, _ time.Duration) string {
		switch host {
		case "saved.local", "other.local":
			return "10.0.0.1"
		default:
			return ""
		}
	}

	found, fallback := discoverReachableHAHosts(runtimeConfig{
		HAHost:       "saved.local",
		RelayBaseURL: "http://10.0.0.2:8791",
	})
	if fallback != "saved.local" {
		t.Fatalf("fallback = %q, want saved.local", fallback)
	}
	want := []setupDiscoveryCandidate{
		{Host: "10.0.0.1", HAURL: "http://10.0.0.1:8123", Via: "saved.local", Source: "saved Home Assistant address"},
		{Host: "10.0.0.2", HAURL: "https://10.0.0.2", Source: "saved Relay address"},
		{Host: "10.0.0.3", HAURL: "http://10.0.0.3:8123", Source: "local network cache"},
	}
	if len(found) != len(want) {
		t.Fatalf("found = %+v, want %d candidates", found, len(want))
	}
	for idx := range want {
		if found[idx] != want[idx] {
			t.Fatalf("found[%d] = %+v, want %+v", idx, found[idx], want[idx])
		}
	}
}

func TestCollectDiscoveryProbesCapsCandidateCount(t *testing.T) {
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	t.Cleanup(func() {
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
	})
	discoverHAViaMDNSForDiscovery = func() string { return "discovered.local" }
	collectARPHostsForDiscovery = func() []string {
		hosts := make([]string, 20)
		for idx := range hosts {
			hosts[idx] = fmt.Sprintf("10.0.0.%d", idx+1)
		}
		return hosts
	}

	probes := collectDiscoveryProbes(runtimeConfig{
		HAHost:       "saved.local",
		HAURL:        "https://saved-url.example",
		RelayBaseURL: "http://relay.example:8791",
	})
	if len(probes) != setupDiscoveryMaxCandidateCount {
		t.Fatalf("candidate count = %d, want cap %d", len(probes), setupDiscoveryMaxCandidateCount)
	}
	if probes[0].Host != "saved.local" || probes[len(probes)-1].Host != "10.0.0.5" {
		t.Fatalf("bounded probe priority = %+v", probes)
	}
}

func TestDiscoverReachableHAHostsSharesOverallDeadlineAcrossCandidates(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	originalOverall := setupDiscoveryOverallTimeout
	originalProbe := setupDiscoveryMaxProbeTimeout
	t.Cleanup(func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
		setupDiscoveryOverallTimeout = originalOverall
		setupDiscoveryMaxProbeTimeout = originalProbe
	})

	setupDiscoveryOverallTimeout = 50 * time.Millisecond
	setupDiscoveryMaxProbeTimeout = time.Second
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }
	calls := 0
	resolveHAURLBaseWithinTimeoutForDiscovery = func(string, time.Duration) (string, error) {
		calls++
		time.Sleep(10 * time.Millisecond)
		return "", assertDiscoveryFailure{}
	}

	started := time.Now()
	found, _ := discoverReachableHAHosts(runtimeConfig{})
	elapsed := time.Since(started)
	if len(found) != 0 {
		t.Fatalf("found = %+v, want none", found)
	}
	if calls < 2 {
		t.Fatalf("calls = %d, want fair probing beyond the first candidate", calls)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("elapsed = %s, want bounded near %s", elapsed, setupDiscoveryOverallTimeout)
	}
}
