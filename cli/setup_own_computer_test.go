package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"
)

func stubOwnComputerAsked(t *testing.T, asked bool) {
	t.Helper()
	saved := ownComputerAskedForSetup
	ownComputerAskedForSetup = func(runtimeConfig) bool { return asked }
	t.Cleanup(func() { ownComputerAskedForSetup = saved })
}

// The owner's sign-in declares this computer theirs only with a yes; every
// other arm sends what it always did.
func TestArmSignInPairingDeclaresTheOwnersComputerOnlyWhenAsked(t *testing.T) {
	secret := strings.Repeat("s", 43)
	for own, want := range map[bool]string{true: `{"own_computer":true}`, false: "{}"} {
		var sent string
		client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(request.Body)
			sent = string(body)
			header := http.Header{}
			header.Set(relayVersionHeader, "0.32.0")
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(
				`{"ok":true,"data":{"pairing_secret":"` + secret + `","expires_in_s":120,"relay_instance_id":"relay-1"}}`))}, nil
		}))
		if _, err := armSignInPairing(context.Background(), client, CloudEndpointPairArm, own); err != nil || sent != want {
			t.Fatalf("own %v: sent %q, %v", own, sent, err)
		}
	}
}

// Asked only where a yes can work: the App says it approves in windows, the
// key isn't in a file, and not on a Mac before its cleanup layer.
func TestOwnComputerAskedOnlyWhereAYesCanWork(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saved := deviceCredentialFileModeForced
	t.Cleanup(func() { deviceCredentialFileModeForced = saved })
	window := 0
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pair/v1/info" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"data":{"protocol_version":"v1","sign_in":true,"window":`+map[int]string{0: "0", 1: "1"}[window]+`}}`)
	}))
	t.Cleanup(relay.Close)
	cfg := runtimeConfig{RelayBaseURL: relay.URL}
	deviceCredentialFileModeForced = false
	if ownComputerAsked(cfg) {
		t.Fatal("an App that doesn't approve in windows: no question")
	}
	window = 1
	if got, want := ownComputerAsked(cfg), runtime.GOOS != "darwin"; got != want {
		t.Fatalf("window 1 on %s: %v", runtime.GOOS, got)
	}
	deviceCredentialFileModeForced = true
	if ownComputerAsked(cfg) {
		t.Fatal("a file key: no question")
	}
	deviceCredentialFileModeForced = false
	if ownComputerAsked(runtimeConfig{RelayBaseURL: "http://127.0.0.1:1"}) {
		t.Fatal("an App that can't be read: no question")
	}
}

func TestAskOwnComputerDeclaresOnlyOnAYes(t *testing.T) {
	for input, want := range map[string]bool{"\n": true, "y\n": true, "Yes\n": true, "no\n": false, "n\n": false, "maybe\n": false, "": false} {
		var out bytes.Buffer
		if got := askOwnComputer(bufio.NewReader(strings.NewReader(input)), &out); got != want || !strings.Contains(out.String(), setupT("setup.own.question")) {
			t.Fatalf("%q: %v, said %q", input, got, out.String())
		}
	}
}

// The terminal asks right after the owner's sign-in and before pairing;
// the arm carries the answer. Not asked: never declared.
func TestHASignInStepAsksBeforePairing(t *testing.T) {
	previousOpen, previousPair, previousResolve, previousArmed := openHASignInIngressForSetup, signInPairForSetup, resolveHASignInOriginForSetup, pairDeviceV2ArmedForSetup
	t.Cleanup(func() {
		openHASignInIngressForSetup, signInPairForSetup, resolveHASignInOriginForSetup, pairDeviceV2ArmedForSetup = previousOpen, previousPair, previousResolve, previousArmed
	})
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) {
		return &url.URL{Scheme: "http", Host: "ha.local"}, nil
	}
	openHASignInIngressForSetup = func(context.Context, io.Writer, string) (haSignInSession, error) {
		return haSignInSession{user: HACurrentUser{Name: "Markus", IsOwner: true}}, nil
	}
	var out bytes.Buffer
	var armed, asked []bool
	pairDeviceV2ArmedForSetup = func(_ context.Context, _ *CloudIngressClient, _ deviceMetadata, own bool) (*provisionedCredential, error) {
		armed = append(armed, own)
		asked = append(asked, strings.Contains(out.String(), setupT("setup.own.question"))) // before the arm
		return &provisionedCredential{}, nil
	}
	signInPairForSetup = func(_ string, provision pairingProvisioner, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo, _ SecretStoreUIPolicy) (string, error) {
		_, err := provision(deviceMetadata{})
		return "device-1", err
	}
	cfg := &runtimeConfig{HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791"}
	for _, test := range []struct {
		asked bool
		input string
		own   bool
	}{{true, "\n\n", true}, {true, "\nno\n", false}, {false, "\n", false}} {
		stubOwnComputerAsked(t, test.asked)
		out.Reset()
		armed, asked = nil, nil
		paired, _, err := runHASignInPairingStep(bufio.NewReader(strings.NewReader(test.input)), &out, runtimePaths{ConfigDir: t.TempDir()}, cfg)
		if !paired || err != nil || len(armed) != 1 || armed[0] != test.own || asked[0] != test.asked {
			t.Fatalf("%+v: paired %v, %v, armed %v asked %v\n%s", test, paired, err, armed, asked, out.String())
		}
	}
}

// The setup page: the sign-in button carries the switch's answer, on by
// default; the arm declares only with it.
func TestSetupPageSignInCarriesTheOwnAnswer(t *testing.T) {
	for _, value := range []string{"own", ""} {
		t.Run(value, func(t *testing.T) {
			withClientRuntimeAvailability(t, map[string]bool{"codex": true})
			setupGoldenHome(t)
			stubOwnComputerAsked(t, true)
			ha := newFakeSignInHA(t)
			pairs := stubSetupPageHome(t, ha.server.URL, true)
			previousArmed := pairDeviceV2ArmedForSetup
			t.Cleanup(func() { pairDeviceV2ArmedForSetup = previousArmed })
			var armed []bool
			pairDeviceV2ArmedForSetup = func(_ context.Context, _ *CloudIngressClient, _ deviceMetadata, own bool) (*provisionedCredential, error) {
				armed = append(armed, own)
				return &provisionedCredential{}, nil
			}
			signInPairForSetup = func(_ string, provision pairingProvisioner, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo, _ SecretStoreUIPolicy) (string, error) {
				*pairs++
				_, err := provision(deviceMetadata{})
				return "device-1", err
			}
			previousIdle := setupPageIdle
			setupPageIdle = 2 * time.Second
			t.Cleanup(func() { setupPageIdle = previousIdle })
			page, stdout, done := startSetupPageFlow(t, "")
			if found := page.waitFor("the found screen", accepting("signin")); !found.Own {
				t.Fatalf("found = %+v", found)
			}
			page.click("signin", value)
			waiting := page.waitFor("the sign-in URL", func(s setupPageState) bool { return s.SignInURL != "" })
			authorize, _ := url.Parse(waiting.SignInURL)
			page.do(http.MethodGet, oauthLoopbackCallbackPath+"?"+signedInQuery(authorize.Query().Get("state")), "", nil)
			page.waitFor("step 3", accepting("finish"))
			setupPageExit(t, done, stdout)
			if len(armed) != 1 || armed[0] != (value == "own") {
				t.Fatalf("%q: armed %v", value, armed)
			}
		})
	}
}
