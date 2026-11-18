package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The re-pair is the one cause a Mac with Link shows after the 0.32 update:
// doctor says it once, in plain words, and ends on it, after the AI apps.
// Before, three error lines (one with a technical prefix) and the home
// address's failure buried the step in the middle.
func TestDoctorLinkRePairSaysItOnceAndLast(t *testing.T) {
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	paths, _ := linkDoctorFixture(t, "relay-instance-1", &cloudLifecycleMetadata{State: cloudStateReady, Current: &current})
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir()) // the new slots start empty
	applies, automatic, local := linkRePairApplies, resolveAutomaticRelayTransportForCLI, relayFunctionalTransportForDoctor
	t.Cleanup(func() {
		linkRePairApplies, resolveAutomaticRelayTransportForCLI, relayFunctionalTransportForDoctor = applies, automatic, local
	})
	linkRePairApplies = func() bool { return true }
	resolveAutomaticRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		return relayTransportSelection{}, deviceNeedsRePair(defaultServerProfileName, true)
	}
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return "", nil, "", false, deviceNeedsRePair(defaultServerProfileName, true)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.HAURL = "http://127.0.0.1:9" // away: home doesn't answer
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	code, out := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	if code != 1 || strings.Count(out, "needs to be paired again") != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, unwanted := range []string{"relay auth token", "key is gone", "can't reach Home Assistant", "only through Home Assistant Link"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("follow-on line %q:\n%s", unwanted, out)
		}
	}
	requireLastLine(t, out, "[ha-nova] Next step: run ha-nova setup to pair this computer again (one or two Home Assistant sign-ins, about two minutes).")
	if !strings.Contains(out, "Claude Code is ready") {
		t.Fatalf("the AI apps are still checked:\n%s", out)
	}
}

// A paired computer whose key is gone: one line, no home address check that
// the re-pair explains, and the step last.
func TestDoctorKeyGoneEndsWithTheRePair(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	cfg.HAURL = "http://127.0.0.1:9"
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin = "https://192.168.1.5:8792", "pin"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	code, out := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	if code != 1 || strings.Contains(out, "can't reach Home Assistant") || strings.Count(out, "paired again") != 1 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	requireLastLine(t, out, "[ha-nova] Next step: run ha-nova setup to pair this computer again (one Home Assistant sign-in, about a minute).")
}

// Every command's text for the re-pair is plain words, never the token
// storage's prefix.
func TestRePairErrorReadsPlainly(t *testing.T) {
	message := relayTransportErrorMessage(deviceNeedsRePair(defaultServerProfileName, false))
	if !strings.HasPrefix(message, "This computer needs to be paired again") || strings.Contains(message, "token") {
		t.Fatalf("%q", message)
	}
}

func requireLastLine(t *testing.T, out, want string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != want || len(lines) < 2 || strings.TrimSpace(lines[len(lines)-2]) != "" {
		t.Fatalf("want an empty line, then %q last:\n%s", want, out)
	}
}

// windowAppSession: the App answers /health with window and /gate/v1/me with
// status and body.
func windowAppSession(t *testing.T, window string, status int, body string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"ok":true,"data":{"gate":1,"window":` + window + `}}`))
		case gateMeRoute:
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	saved := legacyCleanupSession
	t.Cleanup(func() { legacyCleanupSession = saved })
	legacyCleanupSession = func(paths runtimePaths, cfg runtimeConfig) (*relaySession, error) {
		ctx, cancel := context.WithCancel(context.Background())
		return relaySessionOn(paths, cfg, relayTransportSelection{BaseURL: server.URL, Client: server.Client(), Credential: "key", DeviceMode: true, Via: relayViaLocal}, ctx, cancel), nil
	}
}

// Setup pairs a Mac again only on the App's own pair_again answer: any other,
// unknown, or unreadable answer, an App without the window, or a computer
// that can't show one is no reason (fail closed).
func TestWindowPairAgainOnlyOnTheAppsAnswer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	saved := macWindowHere
	t.Cleanup(func() { macWindowHere = saved })
	me := func(why string) string { return `{"ok":true,"data":{"window":false,"why":"` + why + `"}}` }
	for _, test := range []struct {
		mac    bool
		window string
		status int
		body   string
		want   bool
	}{
		{true, "1", 200, me("pair_again"), true},
		{false, "1", 200, me("pair_again"), false},
		{true, "0", 200, me("pair_again"), false},
		{true, "1", 200, me("legacy_cleanup_pending"), false},
		{true, "1", 200, me("not_yours"), false},
		{true, "1", 200, me("something_new"), false},
		{true, "1", 500, me("pair_again"), false},
		{true, "1", 200, "not json", false},
	} {
		macWindowHere = func() bool { return test.mac }
		windowAppSession(t, test.window, test.status, test.body)
		if got := windowPairAgain(paths, runtimeConfig{HAURL: "http://ha:8123", RelayBaseURL: "http://ha:8791"}); got != test.want {
			t.Fatalf("%+v: got %v", test, got)
		}
	}
	macWindowHere = func() bool { return true }
	windowAppSession(t, "1", 200, me("pair_again"))
	if windowPairAgain(paths, runtimeConfig{RelayBaseURL: "http://ha:8791"}) { // paired by --relay-url, no address: setup's own flow asks it
		t.Fatal("pairs again without a Home Assistant address")
	}
}

// Doctor names setup last for the two answers setup finishes, and nothing for
// any other.
func TestDoctorWindowStepOnlyForWhatSetupFinishes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	saved := macWindowHere
	t.Cleanup(func() { macWindowHere = saved })
	macWindowHere = func() bool { return true }
	for why, want := range map[string]string{
		"pair_again":             "run ha-nova setup to pair this computer again (",
		"legacy_cleanup_pending": "run ha-nova setup to sign the old version out",
		"on":                     "",
		"something_new":          "",
	} {
		windowAppSession(t, "1", 200, `{"ok":true,"data":{"window":false,"why":"`+why+`"}}`)
		s, err := legacyCleanupSession(paths, runtimeConfig{})
		if err != nil {
			t.Fatal(err)
		}
		var got string
		_, _ = captureCommandOutput(t, func() int { got = doctorWindowStep(s, false); return 0 })
		s.close()
		if (want == "") != (got == "") || !strings.HasPrefix(got, want) {
			t.Fatalf("%s: %q", why, got)
		}
	}
}
