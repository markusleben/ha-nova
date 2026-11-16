package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const predecessorTestPin = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func predecessorConfig(defaultPaired bool) string {
	def := `{"profile_id":"profile-default","route_policy":"local","relay_base_url":"http://other:8791"}`
	if defaultPaired {
		def = `{"profile_id":"profile-default","route_policy":"local","relay_base_url":"http://other:8791","relay_secure_base_url":"https://other:8792","relay_spki_pin":"` + predecessorTestPin + `"}`
	}
	return `{"schema_version":3,"client_install_id":"inst-plain","default_server":"default","servers":{"default":` + def +
		`,"home":{"profile_id":"profile-home","route_policy":"local","relay_base_url":"http://ha:8791","relay_secure_base_url":"https://ha:8792","relay_spki_pin":"` + predecessorTestPin + `"}}}`
}

// predecessorArm answers the lookup arm and records what it was sent.
func predecessorArm(t *testing.T, previous string, sent *string) *CloudIngressClient {
	t.Helper()
	return newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		*sent = string(body)
		header := http.Header{}
		header.Set(relayVersionHeader, "0.32.0")
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(
			`{"ok":true,"data":{"pairing_secret":"` + strings.Repeat("s", 43) + `","expires_in_s":120,"relay_instance_id":"relay-1"` + previous + `}}`))}, nil
	}))
}

func TestSignInPredecessorIsRetiredOnlyWhenNothingHereUsesItOrTheOwnerAgrees(t *testing.T) {
	const old = `,"previous":{"device_id":"dev-old","name":"Markus Mac\u001b[2J"}`
	for name, test := range map[string]struct {
		defaultPaired bool
		previous      string
		reader        *bufio.Reader
		want, says    string
	}{
		"no other profile paired: no question": {previous: old, want: "dev-old"},
		"another profile paired, Enter":        {defaultPaired: true, previous: old, reader: bufio.NewReader(strings.NewReader("\n")), want: "dev-old", says: "setup.retire.question"},
		"another profile paired, no":           {defaultPaired: true, previous: old, reader: bufio.NewReader(strings.NewReader("no\n")), says: "setup.retire.kept"},
		"another profile paired, no terminal":  {defaultPaired: true, previous: old, says: "setup.retire.listed"},
		"an App that names nothing":            {defaultPaired: true, reader: bufio.NewReader(strings.NewReader("\n"))},
	} {
		paths := setupServerCommandTest(t, predecessorConfig(test.defaultPaired))
		setServerSelectionOverride("home")
		cfg, err := loadConfig(paths)
		if err != nil {
			t.Fatal(err)
		}
		var sent string
		var out bytes.Buffer
		got, err := signInPredecessorToRetire(test.reader, &out, paths, cfg, predecessorArm(t, test.previous, &sent))
		if err != nil || got != test.want || sent != `{"previous_install_id":"inst-plain"}` {
			t.Errorf("%s: got %q, %v; sent %s", name, got, err, sent)
		}
		if test.says != "" && !strings.Contains(out.String(), strings.SplitN(setupT(test.says, "name", "Markus Mac[2J"), "“", 2)[0]) {
			t.Errorf("%s: said %q, want %s", name, out.String(), test.says)
		}
		if strings.ContainsRune(out.String(), 0x1b) {
			t.Errorf("%s: a Relay-supplied name reached the terminal unfiltered: %q", name, out.String())
		}
	}
}

func TestSignInPredecessorIsNeverLookedUpForTheDefaultProfile(t *testing.T) {
	paths := setupServerCommandTest(t, predecessorConfig(false))
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("the default profile already pairs under the plain id: nothing to look up")
		return nil, errors.New("unexpected")
	}))
	if got, err := signInPredecessorToRetire(nil, io.Discard, paths, cfg, client); got != "" || err != nil {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestArmSignInPairingRefusesAMalformedPredecessor(t *testing.T) {
	var sent string
	_, err := armSignInPairingRequest(context.Background(), predecessorArm(t, `,"previous":{"device_id":"","name":"x"}`, &sent), CloudEndpointPairArm, signInArmRequest{PreviousInstallID: "inst-plain"})
	if err == nil {
		t.Fatal("an empty predecessor id must be refused")
	}
}

func TestDeviceNeedsRePairNamesTheProfilesSetupCommand(t *testing.T) {
	for profile, want := range map[string]string{"": "run: ha-nova setup", defaultServerProfileName: "run: ha-nova setup", "home": "run: ha-nova setup --server home"} {
		err := deviceNeedsRePair(profile)
		if !errors.Is(err, errDeviceNeedsRePair) || !strings.HasSuffix(err.Error(), want) {
			t.Errorf("%q: %v", profile, err)
		}
	}
}

// After the 0.32 update a Mac's new key slots start empty: `ha-nova update`
// names the one next step instead of a generic App-check failure.
func TestUpdateOfferNamesTheRePairWhenThePairedKeyIsGone(t *testing.T) {
	paths := setupServerCommandTest(t, predecessorConfig(false))
	setServerSelectionOverride("home")
	checked := false
	saved := relayUpdateNoticeForUpdate
	t.Cleanup(func() { relayUpdateNoticeForUpdate = saved })
	relayUpdateNoticeForUpdate = func(context.Context, runtimePaths) humanNotice { checked = true; return humanNotice{} }
	_, output := captureCommandOutput(t, func() int { offerRelayUpdateAfterUpdate(paths); return 0 })
	if checked || !strings.Contains(output, "This computer needs to be paired again") || !strings.Contains(output, "run: ha-nova setup --server home") {
		t.Fatalf("checked=%v output:\n%s", checked, output)
	}
}

func TestNamedLocalRePairAllowedOnlyForAPairedLocalProfileWithoutOverrides(t *testing.T) {
	resetServerProfileSelection(t)
	paired := runtimeConfig{RelaySecureBaseURL: "https://ha:8792", RelaySpkiPin: predecessorTestPin}
	if namedLocalRePairAllowed(paired, false) {
		t.Fatal("the default profile has its own setup path")
	}
	setActiveServerProfile("home")
	for name, test := range map[string]struct {
		cfg       runtimeConfig
		service   bool
		overrides []string
		want      bool
	}{
		"paired local":        {cfg: paired, want: true},
		"never paired":        {cfg: runtimeConfig{RelayBaseURL: "http://ha:8791"}},
		"Link":                {cfg: runtimeConfig{RelaySecureBaseURL: "https://ha:8792", RelaySpkiPin: predecessorTestPin, Cloud: &cloudLifecycleMetadata{}}},
		"service mode":        {cfg: paired, service: true},
		"an address override": {cfg: paired, overrides: []string{"", "http://other:8123"}},
	} {
		if got := namedLocalRePairAllowed(test.cfg, test.service, test.overrides...); got != test.want {
			t.Errorf("%s: %v, want %v", name, got, test.want)
		}
	}
}
