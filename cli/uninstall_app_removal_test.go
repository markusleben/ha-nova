package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func peersOf(others int, legacy bool, chatApps bool) devicePeers {
	pairing := false
	return devicePeers{OtherDevices: &others, PairingOpen: &pairing, LegacyAccess: &legacy, ChatAppFiles: &chatApps}
}

// stubAppRemoval stubs the Relay answers (one per call, the last repeats),
// the official-App and desktop checks, and the browser sign-in, which runs
// prepare against f like the real one (signInErr: the sign-in fails first).
func stubAppRemoval(t *testing.T, f *fakeSupervisor, signInErr error, answers ...func() (devicePeers, error)) (fetches, signIns *int) {
	t.Helper()
	fetches, signIns = new(int), new(int)
	previousFetch, previousPossible, previousOpen, previousResolve, previousTargets :=
		fetchDevicePeersForUninstall, appRemovalSignInPossible, openHASignInForAppRemoval, resolveHASignInOriginForSetup, appRemovalTargetsThisHA
	t.Cleanup(func() {
		fetchDevicePeersForUninstall, appRemovalSignInPossible, openHASignInForAppRemoval, resolveHASignInOriginForSetup, appRemovalTargetsThisHA =
			previousFetch, previousPossible, previousOpen, previousResolve, previousTargets
	})
	appRemovalTargetsThisHA = func(runtimeConfig) bool { return true }
	fetchDevicePeersForUninstall = func(runtimeConfig) (devicePeers, error) {
		*fetches++
		return answers[min(*fetches, len(answers))-1]()
	}
	appRemovalSignInPossible = func(io.Writer, runtimeConfig) bool { return true }
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) {
		return &url.URL{Scheme: "http", Host: "ha.local:8123"}, nil
	}
	openHASignInForAppRemoval = func(_ context.Context, _ io.Writer, _ string, prepare haSignInPrepare) (haSignInSession, error) {
		*signIns++
		owner := haSignInSession{user: HACurrentUser{Name: "Markus", IsOwner: true}}
		if signInErr != nil {
			return owner, signInErr
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := prepare(ctx, f, owner.user); err != nil {
			return owner, err
		}
		// The real sign-in then looks for the App's ingress, which is gone.
		return owner, errSignInAppMissing
	}
	return fetches, signIns
}

func appRemovalPaths(t *testing.T, cfg runtimeConfig) runtimePaths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_CONFIG_DIR", filepath.Join(home, ".config", "ha-nova"))
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

var appPathConfig = runtimeConfig{
	HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791",
	RelaySecureBaseURL: "https://ha.local:8792", RelaySpkiPin: "pin",
}

func TestUninstallAppRemovalOfferedOnlyForTheLastDevice(t *testing.T) {
	unknown := errors.New("route not found")
	for _, tc := range []struct {
		name     string
		answer   func() (devicePeers, error)
		offered  bool
		chatNote bool
	}{
		{"last device", func() (devicePeers, error) { return peersOf(0, false, false), nil }, true, false},
		{"another device", func() (devicePeers, error) { return peersOf(1, false, false), nil }, false, false},
		{"legacy access", func() (devicePeers, error) { return peersOf(0, true, false), nil }, false, false},
		{"pairing under way", func() (devicePeers, error) {
			p, open := peersOf(0, false, false), true
			p.PairingOpen = &open
			return p, nil
		}, false, false},
		{"chat apps", func() (devicePeers, error) { return peersOf(0, false, true), nil }, false, true},
		{"chat apps and another device", func() (devicePeers, error) { return peersOf(1, false, true), nil }, false, false},
		{"unknown chat apps", func() (devicePeers, error) {
			p := peersOf(0, false, false)
			p.ChatAppFiles = nil
			return p, nil
		}, false, false},
		{"unknown device count", func() (devicePeers, error) {
			p := peersOf(0, false, false)
			p.OtherDevices = nil
			return p, nil
		}, false, false},
		{"older Relay", func() (devicePeers, error) { return devicePeers{}, unknown }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubAppRemoval(t, nil, nil, tc.answer)
			offer := uninstallAppRemovalOffer(io.Discard, appRemovalPaths(t, appPathConfig))
			if offer.offered != tc.offered || (offer.note == appRemovalChatAppsLine) != tc.chatNote {
				t.Fatalf("offer = %+v", offer)
			}
		})
	}
}

func TestUninstallAppRemovalNotOfferedWithoutItsPreconditions(t *testing.T) {
	otherHost := appPathConfig
	otherHost.RelaySecureBaseURL = "https://other.local:8792"
	unpinned := appPathConfig
	unpinned.RelaySpkiPin = ""
	for _, tc := range []struct {
		name  string
		cfg   runtimeConfig
		setup func()
	}{
		{"not the official App on this Home Assistant", appPathConfig, func() { appRemovalTargetsThisHA = func(runtimeConfig) bool { return false } }},
		{"no desktop sign-in", appPathConfig, func() { appRemovalSignInPossible = func(io.Writer, runtimeConfig) bool { return false } }},
		{"paired Relay on another host", otherHost, func() {}},
		{"no pinned pairing", unpinned, func() {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetches, _ := stubAppRemoval(t, nil, nil, func() (devicePeers, error) { return peersOf(0, false, false), nil })
			tc.setup()
			if offer := uninstallAppRemovalOffer(io.Discard, appRemovalPaths(t, tc.cfg)); offer.offered || *fetches != 0 {
				t.Fatalf("offer = %+v, Relay asked %d times", offer, *fetches)
			}
		})
	}
}

func TestRemoveNOVAAppRemovesOnlyTheAppAndItsSource(t *testing.T) {
	f := newFakeSupervisor(t)
	f.installedApp("0.10.0", "started", "")
	fetches, _ := stubAppRemoval(t, f, nil, func() (devicePeers, error) { return peersOf(0, false, false), nil })
	var out bytes.Buffer
	if !removeNOVAAppForUninstall(&out, appPathConfig) {
		t.Fatalf("removal failed:\n%s", out.String())
	}
	slug := HAOfficialNOVAAppSlug
	want := []string{"get_config", "get /store/repositories", "get /addons/" + slug + "/info", "post /addons/" + slug + "/uninstall",
		"get /store/addons/" + slug, "delete /store/repositories/2368fcfa", "get /store/repositories"}
	if !slices.Equal(f.calls, want) || *fetches != 1 {
		t.Fatalf("calls = %v, Relay asked %d times", f.calls, *fetches)
	}
	for _, line := range []string{"NOVA App removed from Home Assistant", "NOVA App source removed from Home Assistant"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("missing %q:\n%s", line, out.String())
		}
	}
}

func TestRemoveNOVAAppKeepsGoingWhenOnlyTheSourceStays(t *testing.T) {
	f := newFakeSupervisor(t)
	f.installedApp("0.10.0", "started", "")
	f.deleteRepoFails = true
	stubAppRemoval(t, f, nil, func() (devicePeers, error) { return peersOf(0, false, false), nil })
	var out bytes.Buffer
	if !removeNOVAAppForUninstall(&out, appPathConfig) || !strings.Contains(out.String(), "still lists the NOVA App source") {
		t.Fatalf("output:\n%s", out.String())
	}
}

// Every failure keeps HA NOVA on this computer and says so; nothing past the
// failing step reaches Home Assistant.
func TestRemoveNOVAAppFailsClosed(t *testing.T) {
	alone := func() (devicePeers, error) { return peersOf(0, false, false), nil }
	slug := HAOfficialNOVAAppSlug
	for _, tc := range []struct {
		name      string
		signInErr error
		answers   []func() (devicePeers, error)
		prepare   func(f *fakeSupervisor)
		line      string
		uninstall bool // the uninstall call was made
	}{
		{"sign-in declined", errors.Join(errSignInBrowser, newCloudError(CloudErrOAuthCanceled, "x", nil)), []func() (devicePeers, error){alone}, nil, "Sign-in was canceled.", false},
		{"not the owner", errSignInNotOwner, []func() (devicePeers, error){alone}, nil, "Only the owner can remove the NOVA App.", false},
		{"another device meanwhile", nil, []func() (devicePeers, error){func() (devicePeers, error) { return peersOf(1, false, false), nil }}, nil, "Another computer uses the NOVA App now.", false},
		{"Relay unreachable on the re-check", nil, []func() (devicePeers, error){func() (devicePeers, error) { return devicePeers{}, errors.New("down") }}, nil, "couldn't check whether other computers", false},
		{"chat apps meanwhile", nil, []func() (devicePeers, error){func() (devicePeers, error) { return peersOf(0, false, true), nil }}, nil, "Chat apps are on now", false},
		{"lookalike source", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) {
			f.repos = []map[string]string{{"slug": "2368fcfa", "source": "https://github.com/evil/ha-nova"}}
		}, "Home Assistant lists a different NOVA source. Nothing was removed.", false},
		{"App not installed", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) { f.info["version"] = nil }, "doesn't list the NOVA App as installed", false},
		{"no Supervisor", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) { f.noHassio = true }, "no Apps", false},
		{"another Relay answered", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) {
			f.info["network"] = map[string]any{"8792/tcp": 18792}
		}, "isn't the one this computer is connected to", false},
		{"store answer without installed", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) { f.storeOmitsInstalled = true },
			"didn't confirm that the NOVA App was removed", true},
		{"Supervisor refuses", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) { f.uninstallFails = true }, "couldn't remove the NOVA App; it is still installed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSupervisor(t)
			f.installedApp("0.10.0", "started", "")
			if tc.prepare != nil {
				tc.prepare(f)
			}
			stubAppRemoval(t, f, tc.signInErr, tc.answers...)
			var out bytes.Buffer
			if removeNOVAAppForUninstall(&out, appPathConfig) {
				t.Fatalf("removal claimed success:\n%s", out.String())
			}
			if !strings.Contains(out.String(), tc.line) || !strings.Contains(out.String(), "HA NOVA is still on this computer") {
				t.Fatalf("missing %q:\n%s", tc.line, out.String())
			}
			if got := slices.Contains(f.calls, "post /addons/"+slug+"/uninstall"); got != tc.uninstall {
				t.Fatalf("uninstall call = %v, calls %v", got, f.calls)
			}
			if slices.Contains(f.calls, "delete /store/repositories/2368fcfa") {
				t.Fatalf("source removed after a failure: %v", f.calls)
			}
		})
	}
}

func TestNOVAAppSupervisorKeepsInstallAndRemovalApart(t *testing.T) {
	removal := newNOVAAppSupervisor(newFakeSupervisor(t))
	removal.removal = true
	slug := HAOfficialNOVAAppSlug
	for _, refused := range [][3]string{
		{"post", "/store/addons/" + slug + "/install", "data"},
		{"post", "/addons/" + slug + "/start", ""},
		{"post", "/store/reload", ""},
		{"post", "/store/repositories", "data"},
		{"post", "/addons/" + slug + "/uninstall", "data"}, // no remove_config
		{"get", "/store/addons/" + slug, "data"},
		{"post", "/addons/2368fcfa_other/uninstall", ""},
		{"delete", "/store/repositories/abcdef12", ""},
		{"get", "/jobs/" + fakeJobID, ""},
	} {
		var data map[string]any
		if refused[2] != "" {
			data = map[string]any{"remove_config": true, "background": true, "repository": haNovaRepositoryURL}
		}
		err := removal.call(context.Background(), refused[0], refused[1], data, time.Second, nil)
		if !errors.Is(err, errAppInstallRefusedCall) || !strings.Contains(err.Error(), "removing its App") {
			t.Errorf("%s %s: err = %v, want refused", refused[0], refused[1], err)
		}
	}
}

func TestReadDevicePeersParsesTheRelayAnswer(t *testing.T) {
	answer := `{"ok":true,"data":{"other_devices":0,"pairing_open":false,"legacy_access":false,"chat_app_files":null}}`
	status := http.StatusOK
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/auth/device/peers" || r.Header.Get("Authorization") != "Bearer cred" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(relay.Close)
	peers, err := readDevicePeers(context.Background(), relay.Client(), relay.URL, "cred")
	if err != nil || peers.alone() || peers.chatApps() || *peers.OtherDevices != 0 {
		t.Fatalf("peers = %+v err = %v (unknown chat-app files must not read as alone)", peers, err)
	}
	status = http.StatusNotFound // an older Relay
	if _, err := readDevicePeers(context.Background(), relay.Client(), relay.URL, "cred"); err == nil {
		t.Fatal("a 404 must be unknown")
	}
}

// pairedForAppRemoval adds this computer's device credential and records
// revocations, which fail like a removed App's closed port.
func pairedForAppRemoval(t *testing.T) *int {
	t.Helper()
	service := deviceCredentialServiceForProfile(defaultServerProfileName)
	if err := secretSet(service, testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secretDelete(service) })
	revokes := new(int)
	previous := revokeSelfDeviceV1ForUninstall
	revokeSelfDeviceV1ForUninstall = func(string, string, string) error {
		*revokes++
		return errors.New("connection refused")
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForUninstall = previous })
	return revokes
}

// --yes never asks the Relay or signs in, even where the App could be removed.
func TestRunUninstallYesNeverRemovesTheApp(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {"--yes", "--purge"}} {
		paths := appRemovalPaths(t, appPathConfig)
		pairedForAppRemoval(t)
		fetches, signIns := stubAppRemoval(t, newFakeSupervisor(t), nil, func() (devicePeers, error) { return peersOf(0, false, false), nil })
		exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, args) })
		if exitCode != 0 || *fetches != 0 || *signIns != 0 || !strings.Contains(output, "The NOVA App in Home Assistant. Other computers may still use it.") {
			t.Fatalf("args %v: exit=%d fetches=%d sign-ins=%d\n%s", args, exitCode, *fetches, *signIns, output)
		}
	}
}

type recordingUninstallMenu struct {
	answer string
	spec   setupMenuSpec
	unlock string // the keyring window question's answer ("yes"/"no")
	asked  int    // how often that question came
}

func (r *recordingUninstallMenu) Run(_ io.Writer, spec setupMenuSpec) (string, error) {
	if strings.HasPrefix(spec.Title, "Open the window to unlock") {
		r.asked++
		return r.unlock, nil
	}
	r.spec = spec
	return r.answer, nil
}

// stubUninstallMenu makes runUninstall interactive with an arrow menu that
// answers answer.
func stubUninstallMenu(t *testing.T, answer string) *recordingUninstallMenu {
	t.Helper()
	previousInteractive, previousEnv, previousTTY, previousInput, previousANSI, previousRunner :=
		uninstallInteractive, uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner
	t.Cleanup(func() {
		uninstallInteractive, uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner =
			previousInteractive, previousEnv, previousTTY, previousInput, previousANSI, previousRunner
	})
	menu := &recordingUninstallMenu{answer: answer}
	uninstallInteractive = func() bool { return true }
	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	interactiveSetupMenuRunner = menu
	return menu
}

// The whole interactive path: one menu, the App removal, then the local
// purge. Its revoke against the gone App still runs, but no hint about a
// NOVA page that went with the App, and the removal is said once (lane S4).
func TestRunUninstallRemovesTheAppThenThisComputer(t *testing.T) {
	paths := appRemovalPaths(t, appPathConfig)
	revokes := pairedForAppRemoval(t)
	f := newFakeSupervisor(t)
	f.installedApp("0.10.0", "started", "")
	stubAppRemoval(t, f, nil, func() (devicePeers, error) { return peersOf(0, false, false), nil })
	menu := stubUninstallMenu(t, "app")
	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, nil) })
	if exitCode != 0 || menu.spec.DefaultValue != "purge" || len(menu.spec.Options) != 4 || menu.spec.Options[1].Value != "app" {
		t.Fatalf("exit=%d spec=%+v\n%s", exitCode, menu.spec, output)
	}
	if !slices.Contains(f.calls, "post /addons/"+HAOfficialNOVAAppSlug+"/uninstall") || *revokes != 1 || fileExists(paths.ConfigFile) {
		t.Fatalf("calls=%v revokes=%d config kept=%v\n%s", f.calls, *revokes, fileExists(paths.ConfigFile), output)
	}
	if strings.Count(output, "NOVA App removed from Home Assistant") != 1 || strings.Contains(output, "Removed the NOVA App") ||
		strings.Contains(output, "If the NOVA App is still installed") {
		t.Fatalf("removal said twice, or a moot NOVA-page hint:\n%s", output)
	}
	if strings.Contains(output, "Stays:") || !strings.Contains(output, "  Removed:\n    - The NOVA App in Home Assistant\n") {
		t.Fatalf("the summary must list the removed App, and not as staying:\n%s", output)
	}
}

// A failed App removal stops before anything on this computer is removed.
func TestRunUninstallKeepsThisComputerWhenTheAppStays(t *testing.T) {
	paths := appRemovalPaths(t, appPathConfig)
	revokes := pairedForAppRemoval(t)
	f := newFakeSupervisor(t)
	f.installedApp("0.10.0", "started", "")
	f.uninstallFails = true
	stubAppRemoval(t, f, nil, func() (devicePeers, error) { return peersOf(0, false, false), nil })
	stubUninstallMenu(t, "app")
	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, nil) })
	if exitCode != 1 || *revokes != 0 || !fileExists(paths.ConfigFile) || !strings.Contains(output, "HA NOVA is still on this computer") {
		t.Fatalf("exit=%d revokes=%d config kept=%v\n%s", exitCode, *revokes, fileExists(paths.ConfigFile), output)
	}
}

// Where a full removal can't run, "Remove" is disabled, Enter cancels (it
// removes nothing), and the Relay is not even asked about the App.
func TestRunUninstallMenuWhenAFullRemovalCantRun(t *testing.T) {
	paths := appRemovalPaths(t, appPathConfig)
	pairedForAppRemoval(t)
	cfg := appPathConfig
	cfg.RelayTokenFile = filepath.Join(t.TempDir(), "foreign-token")
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	fetches, _ := stubAppRemoval(t, newFakeSupervisor(t), nil, func() (devicePeers, error) { return peersOf(0, false, false), nil })
	menu := stubUninstallMenu(t, "cancel")
	exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, nil) })
	if exitCode != 0 || *fetches != 0 || menu.spec.DefaultValue != "cancel" || !menu.spec.Options[0].Disabled ||
		len(menu.spec.Options) != 3 || !strings.Contains(output, "Uninstall cancelled") {
		t.Fatalf("exit=%d fetches=%d spec=%+v\n%s", exitCode, *fetches, menu.spec, output)
	}
}
