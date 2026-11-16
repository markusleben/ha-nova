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

func TestSignInPredecessorIsRetiredOnlyWhenTheOwnerAgreesToItsName(t *testing.T) {
	const old = `,"previous":{"device_id":"dev-old","name":"Markus Mac\u001b[2J"}`
	for name, test := range map[string]struct {
		defaultPaired bool
		previous      string
		reader        *bufio.Reader
		want, says    string
	}{
		"no other profile here, still asked": {previous: old, reader: bufio.NewReader(strings.NewReader("\n")), want: "dev-old", says: "setup.retire.question"},
		"no other profile here, no terminal": {previous: old, says: "setup.retire.listed"},
		"another profile paired, Enter":      {defaultPaired: true, previous: old, reader: bufio.NewReader(strings.NewReader("\n")), want: "dev-old", says: "setup.retire.question"},
		"another profile paired, no":         {defaultPaired: true, previous: old, reader: bufio.NewReader(strings.NewReader("no\n")), says: "setup.retire.kept"},
		"input ends before an answer: kept":  {previous: old, reader: bufio.NewReader(strings.NewReader("")), says: "setup.retire.kept"},
		"an App that names nothing":          {defaultPaired: true, reader: bufio.NewReader(strings.NewReader("\n"))},
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

// A named profile whose key is gone gets the sign-in re-pair everywhere: in
// doctor, and as its own failure code for agents (verbs and MCP).
func TestNamedProfileWithoutItsKeyIsToldToPairAgainBySignIn(t *testing.T) {
	paths := setupServerCommandTest(t, predecessorConfig(true))
	setServerSelectionOverride("home")
	_, output := captureCommandOutput(t, func() int { return runDoctor(paths, []string{"--quiet"}) })
	if !strings.Contains(output, "run 'ha-nova setup --server home'") || strings.Contains(output, "pair --server") {
		t.Fatalf("doctor:\n%s", output)
	}
	_, err := newRelaySession(paths, verbOptions{})
	var failure *verbFailure
	if !errors.As(err, &failure) || failure.code != "PAIR_AGAIN" || !strings.Contains(failure.message, "ha-nova setup --server home") {
		t.Fatalf("session: %#v", err)
	}
}

// The machine-wide relay token belongs to the default profile: setup for
// another server never reads it, so it can't be assessed or sent there.
func TestNamedSetupNeverReadsTheDefaultProfilesToken(t *testing.T) {
	resetServerProfileSelection(t)
	setActiveServerProfile("home")
	saved := readRelayAuthTokenForSetup
	t.Cleanup(func() { readRelayAuthTokenForSetup = saved })
	readRelayAuthTokenForSetup = func() (string, error) {
		t.Error("the default profile's token was read for a named profile")
		return "token", nil
	}
	f := &setupFlow{}
	if result := f.checkTokenStorage(); result.done || f.savedTokenBeforeSetup != "" {
		t.Fatalf("result %+v, saved %q", result, f.savedTokenBeforeSetup)
	}
}

func TestMCPPassesThePairAgainStepThrough(t *testing.T) {
	next := "Nothing was sent. Tell the user this computer needs to be paired again once."
	envelope := map[string]any{"ok": false, "error": map[string]any{"code": "PAIR_AGAIN", "message": "x", "next": next}}
	if got := nextStep(envelope, nil); got != next {
		t.Fatalf("next_step = %q", got)
	}
}

// No path of a named profile's setup reaches the default profile's shared
// key: the token step refuses and persistence never writes one.
func TestNamedSetupNeverWritesTheSharedKey(t *testing.T) {
	paths := setupServerCommandTest(t, predecessorConfig(false))
	setServerSelectionOverride("home")
	setActiveServerProfile("home")
	if result := (&setupFlow{paths: paths, ui: setupTextUI{out: io.Discard}}).stepToken(); !result.done {
		t.Fatal("the token step must refuse a named profile")
	}
	if err := persistInteractiveSetupStateUnlocked(paths, runtimeConfig{}, &installState{}, "", false, "pasted"); err == nil || !strings.Contains(err.Error(), "never changes it") {
		t.Fatal("persistence wrote the shared key for a named profile")
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
