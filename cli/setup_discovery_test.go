package main

import (
	"testing"
	"time"
)

func TestDetectDefaultHAHostPrefersReachableRelayHost(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		if input == "192.168.1.20" {
			return "http://192.168.1.20:8123", nil
		}
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }

	cfg := runtimeConfig{
		RelayBaseURL: "http://192.168.1.20:8791",
	}
	got := detectDefaultHAHost(cfg)
	if got != "192.168.1.20" {
		t.Fatalf("detectDefaultHAHost() = %q, want %q", got, "192.168.1.20")
	}
}

func TestDetectDefaultHAHostFallsBackToMDNSBeforeHomeassistantLocal(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		if input == "ha-box.local" {
			return "http://ha-box.local:8123", nil
		}
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "ha-box.local" }
	collectARPHostsForDiscovery = func() []string { return nil }

	got := detectDefaultHAHost(runtimeConfig{})
	if got != "ha-box.local" {
		t.Fatalf("detectDefaultHAHost() = %q, want %q", got, "ha-box.local")
	}
}

func TestDetectDefaultHAHostLeavesDefaultBlankWhenNothingWasConfirmed(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(string, time.Duration) (string, error) {
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }

	got := detectDefaultHAHost(runtimeConfig{})
	if got != "" {
		t.Fatalf("detectDefaultHAHost() = %q, want blank", got)
	}
}

func TestDetectDefaultHAHostTreatsReachableHomeassistantLocalAsConfirmed(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	originalIPResolve := resolveHostToIPv4ForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
		resolveHostToIPv4ForDiscovery = originalIPResolve
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		if input == "homeassistant.local" {
			return "http://homeassistant.local:8123", nil
		}
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }
	resolveHostToIPv4ForDiscovery = func(string, time.Duration) string { return "" }

	host, via, discovered := detectDefaultHAHostChoice(runtimeConfig{})
	if host != "homeassistant.local" || via != "" || !discovered {
		t.Fatalf("detectDefaultHAHostChoice() = (%q, %q, %v), want (%q, \"\", true)", host, via, discovered, "homeassistant.local")
	}
}

func TestDetectDefaultHAHostChoicePrefersResolvedIPForMDNSName(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	originalIPResolve := resolveHostToIPv4ForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
		resolveHostToIPv4ForDiscovery = originalIPResolve
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		switch input {
		case "homeassistant.local":
			return "http://homeassistant.local:8123", nil
		case "192.168.1.5":
			return "http://192.168.1.5:8123", nil
		}
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }
	resolveHostToIPv4ForDiscovery = func(host string, _ time.Duration) string {
		if host == "homeassistant.local" {
			return "192.168.1.5"
		}
		return ""
	}

	host, via, discovered := detectDefaultHAHostChoice(runtimeConfig{})
	if host != "192.168.1.5" || via != "homeassistant.local" || !discovered {
		t.Fatalf("detectDefaultHAHostChoice() = (%q, %q, %v), want (%q, %q, true)", host, via, discovered, "192.168.1.5", "homeassistant.local")
	}
}

func TestDetectDefaultHAHostChoiceKeepsMDNSNameWhenResolvedIPDoesNotProbe(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	originalIPResolve := resolveHostToIPv4ForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
		resolveHostToIPv4ForDiscovery = originalIPResolve
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		if input == "homeassistant.local" {
			return "http://homeassistant.local:8123", nil
		}
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }
	resolveHostToIPv4ForDiscovery = func(string, time.Duration) string { return "192.168.1.66" }

	host, via, discovered := detectDefaultHAHostChoice(runtimeConfig{})
	if host != "homeassistant.local" || via != "" || !discovered {
		t.Fatalf("detectDefaultHAHostChoice() = (%q, %q, %v), want (%q, \"\", true)", host, via, discovered, "homeassistant.local")
	}
}

func TestDetectDefaultHAHostChoiceDoesNotResolveIPForNonMDNSHost(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	originalIPResolve := resolveHostToIPv4ForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
		resolveHostToIPv4ForDiscovery = originalIPResolve
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		if input == "ha.example.lan" {
			return "http://ha.example.lan:8123", nil
		}
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }
	resolveHostToIPv4ForDiscovery = func(string, time.Duration) string {
		t.Fatal("resolveHostToIPv4ForDiscovery must not be called for non-.local hosts")
		return ""
	}

	host, via, discovered := detectDefaultHAHostChoice(runtimeConfig{HAHost: "ha.example.lan"})
	if host != "ha.example.lan" || via != "" || !discovered {
		t.Fatalf("detectDefaultHAHostChoice() = (%q, %q, %v), want (%q, \"\", true)", host, via, discovered, "ha.example.lan")
	}
}

func TestDetectDefaultHAHostUsesResolveFallbackVariants(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		if input == "ha-box.local" {
			return "https://ha-box.local", nil
		}
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }

	got := detectDefaultHAHost(runtimeConfig{HAHost: "ha-box.local"})
	if got != "ha-box.local" {
		t.Fatalf("detectDefaultHAHost() = %q, want %q", got, "ha-box.local")
	}
}

func TestDetectDefaultHAHostIncludesARPFallbackCandidates(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, _ time.Duration) (string, error) {
		if input == "192.168.1.77" {
			return "http://192.168.1.77:8123", nil
		}
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return []string{"192.168.1.77"} }

	got := detectDefaultHAHost(runtimeConfig{})
	if got != "192.168.1.77" {
		t.Fatalf("detectDefaultHAHost() = %q, want %q", got, "192.168.1.77")
	}
}

func TestDetectDefaultHAHostChoiceSharesOverallTimeoutAcrossCandidates(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	originalTimeout := setupDiscoveryOverallTimeout
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
		setupDiscoveryOverallTimeout = originalTimeout
	}()

	setupDiscoveryOverallTimeout = 20 * time.Millisecond
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }

	calls := 0
	resolveHAURLBaseWithinTimeoutForDiscovery = func(input string, timeout time.Duration) (string, error) {
		calls++
		time.Sleep(timeout + 5*time.Millisecond)
		return "", assertDiscoveryFailure{}
	}

	host, _, discovered := detectDefaultHAHostChoice(runtimeConfig{})
	if host != "" || discovered {
		t.Fatalf("detectDefaultHAHostChoice() = (%q, %v), want blank false result", host, discovered)
	}
	if calls < 2 {
		t.Fatalf("calls = %d, want fair probing beyond the first candidate", calls)
	}
}

func TestDetectDefaultHAHostChoiceFallsBackToSavedAddressWhenNoHostWasConfirmed(t *testing.T) {
	originalResolve := resolveHAURLBaseWithinTimeoutForDiscovery
	originalMDNS := discoverHAViaMDNSForDiscovery
	originalARP := collectARPHostsForDiscovery
	defer func() {
		resolveHAURLBaseWithinTimeoutForDiscovery = originalResolve
		discoverHAViaMDNSForDiscovery = originalMDNS
		collectARPHostsForDiscovery = originalARP
	}()

	resolveHAURLBaseWithinTimeoutForDiscovery = func(string, time.Duration) (string, error) {
		return "", assertDiscoveryFailure{}
	}
	discoverHAViaMDNSForDiscovery = func() string { return "" }
	collectARPHostsForDiscovery = func() []string { return nil }

	host, _, discovered := detectDefaultHAHostChoice(runtimeConfig{HAURL: "http://saved-ha.local:8123"})
	if host != "saved-ha.local" || discovered {
		t.Fatalf("detectDefaultHAHostChoice() = (%q, %v), want (%q, false)", host, discovered, "saved-ha.local")
	}
}

func TestParseARPHostsSupportsUnixAndWindowsOutput(t *testing.T) {
	got := parseARPHosts(`
? (192.168.1.77) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]
  Internet Address      Physical Address      Type
  192.168.1.88          aa-bb-cc-dd-ee-11     dynamic
`)

	want := []string{"192.168.1.77", "192.168.1.88"}
	if len(got) != len(want) {
		t.Fatalf("parseARPHosts() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("parseARPHosts()[%d] = %q, want %q", idx, got[idx], want[idx])
		}
	}
}

func TestParseARPHostsIgnoresWindowsInterfaceHeaderAddresses(t *testing.T) {
	got := parseARPHosts(`
Interface: 192.168.1.10 --- 0x6
  Internet Address      Physical Address      Type
  192.168.1.88          aa-bb-cc-dd-ee-11     dynamic
  192.168.1.89          aa-bb-cc-dd-ee-12     dynamic
`)

	want := []string{"192.168.1.88", "192.168.1.89"}
	if len(got) != len(want) {
		t.Fatalf("parseARPHosts() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for idx := range want {
		if got[idx] != want[idx] {
			t.Fatalf("parseARPHosts()[%d] = %q, want %q", idx, got[idx], want[idx])
		}
	}
}

func TestParseMDNSBrowseInstanceReturnsHomeAssistantInstance(t *testing.T) {
	output := `Browsing for _home-assistant._tcp.local
DATE: ---Sun 15 Mar 2026---
20:45:31.464  ...STARTING...
Timestamp     A/R    Flags  if Domain               Service Type         Instance Name
20:45:31.465  Add        2  15 local.               _home-assistant._tcp. Zuhause
`

	got := parseMDNSBrowseInstance(output)
	if got != "Zuhause" {
		t.Fatalf("parseMDNSBrowseInstance() = %q, want %q", got, "Zuhause")
	}
}

func TestParseMDNSBrowseInstanceAcceptsIndentedBrowseRows(t *testing.T) {
	output := `Browsing for _home-assistant._tcp.local
DATE: ---Mon 16 Mar 2026---
 9:17:14.163  ...STARTING...
Timestamp     A/R    Flags  if Domain               Service Type         Instance Name
 9:17:14.164  Add        2  14 local.               _home-assistant._tcp. Zuhause
`

	got := parseMDNSBrowseInstance(output)
	if got != "Zuhause" {
		t.Fatalf("parseMDNSBrowseInstance() = %q, want %q", got, "Zuhause")
	}
}

func TestParseMDNSLookupHostReturnsInternalURLHost(t *testing.T) {
	output := `Lookup Zuhause._home-assistant._tcp.local
DATE: ---Sun 15 Mar 2026---
20:43:55.300  Zuhause._home-assistant._tcp.local. can be reached at 556851affd194582ad3f150856f13a05.local.:8123 (interface 15)
 location_name=Zuhause uuid=556851affd194582ad3f150856f13a05 version=2026.3.1 external_url= internal_url=http://192.168.1.5:8123 base_url=http://192.168.1.5:8123 requires_api_password=True
`

	got := parseMDNSLookupHost(output)
	if got != "192.168.1.5" {
		t.Fatalf("parseMDNSLookupHost() = %q, want %q", got, "192.168.1.5")
	}
}

func TestDiscoverHAViaMDNSKeepsLookupOutputAfterTimeoutStyleCompletion(t *testing.T) {
	originalBrowse := runMDNSBrowseForDiscovery
	originalLookup := runMDNSLookupForDiscovery
	originalAvailable := mdnsAvailableForDiscovery
	originalPlatform := setupDiscoveryPlatformOS
	defer func() {
		runMDNSBrowseForDiscovery = originalBrowse
		runMDNSLookupForDiscovery = originalLookup
		mdnsAvailableForDiscovery = originalAvailable
		setupDiscoveryPlatformOS = originalPlatform
	}()

	setupDiscoveryPlatformOS = "darwin"
	mdnsAvailableForDiscovery = func() bool { return true }
	runMDNSBrowseForDiscovery = func() (string, error) {
		return `Browsing for _home-assistant._tcp.local
20:45:31.465  Add        2  15 local.               _home-assistant._tcp. Zuhause
`, nil
	}
	runMDNSLookupForDiscovery = func(instance string) (string, error) {
		if instance != "Zuhause" {
			t.Fatalf("instance = %q, want %q", instance, "Zuhause")
		}
		return `Lookup Zuhause._home-assistant._tcp.local
 location_name=Zuhause internal_url=http://192.168.1.5:8123 base_url=http://192.168.1.5:8123
`, nil
	}

	got := discoverHAViaMDNS()
	if got != "192.168.1.5" {
		t.Fatalf("discoverHAViaMDNS() = %q, want %q", got, "192.168.1.5")
	}
}

func TestParseAvahiBrowseHostReturnsInternalURLHost(t *testing.T) {
	output := `+ enp0s31f6 IPv4 Home Assistant _home-assistant._tcp local
= enp0s31f6 IPv4 Home Assistant _home-assistant._tcp local
   hostname = [homeassistant.local]
   address = [fe80::1234]
   address = [192.168.1.55]
   port = [8123]
   txt = ["internal_url=http://192.168.1.55:8123" "base_url=http://192.168.1.55:8123"]
`

	got := parseAvahiBrowseHost(output)
	if got != "192.168.1.55" {
		t.Fatalf("parseAvahiBrowseHost() = %q, want %q", got, "192.168.1.55")
	}
}

func TestDiscoverHAViaMDNSUsesAvahiBrowseOnLinux(t *testing.T) {
	originalBrowse := runMDNSBrowseForDiscovery
	originalLookup := runMDNSLookupForDiscovery
	originalAvailable := mdnsAvailableForDiscovery
	originalPlatform := setupDiscoveryPlatformOS
	defer func() {
		runMDNSBrowseForDiscovery = originalBrowse
		runMDNSLookupForDiscovery = originalLookup
		mdnsAvailableForDiscovery = originalAvailable
		setupDiscoveryPlatformOS = originalPlatform
	}()

	setupDiscoveryPlatformOS = "linux"
	mdnsAvailableForDiscovery = func() bool { return true }
	runMDNSBrowseForDiscovery = func() (string, error) {
		return `= enp0s31f6 IPv4 Home Assistant _home-assistant._tcp local
   hostname = [homeassistant.local]
   address = [192.168.1.56]
   port = [8123]
`, nil
	}
	runMDNSLookupForDiscovery = func(instance string) (string, error) {
		t.Fatalf("runMDNSLookupForDiscovery called with %q; avahi-browse output should be self-contained", instance)
		return "", nil
	}

	got := discoverHAViaMDNS()
	if got != "192.168.1.56" {
		t.Fatalf("discoverHAViaMDNS() = %q, want %q", got, "192.168.1.56")
	}
}

type assertDiscoveryFailure struct{}

func (assertDiscoveryFailure) Error() string { return "unreachable" }
