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
	return devicePeers{OtherDevices: &others, LegacyAccess: &legacy, ChatAppFiles: &chatApps}
}

// stubAppRemoval stubs the Relay answers (one per call, the last repeats),
// the desktop and purge checks, and the browser sign-in, which runs prepare
// against f like the real one (nil f: the sign-in fails with signInErr).
func stubAppRemoval(t *testing.T, f *fakeSupervisor, signInErr error, answers ...func() (devicePeers, error)) (fetches, signIns *int) {
	t.Helper()
	fetches, signIns = new(int), new(int)
	previous := []any{fetchDevicePeersForUninstall, appRemovalSignInPossible, appRemovalPurgeBlocked, openHASignInForAppRemoval, resolveHASignInOriginForSetup, appRemovalTargetsThisHA}
	t.Cleanup(func() {
		fetchDevicePeersForUninstall = previous[0].(func(runtimeConfig) (devicePeers, error))
		appRemovalSignInPossible = previous[1].(func(io.Writer, runtimeConfig) bool)
		appRemovalPurgeBlocked = previous[2].(func(runtimePaths) bool)
		openHASignInForAppRemoval = previous[3].(func(context.Context, io.Writer, string, haSignInPrepare) (haSignInSession, error))
		resolveHASignInOriginForSetup = previous[4].(func(context.Context, string) (*url.URL, error))
		appRemovalTargetsThisHA = previous[5].(func(runtimeConfig) bool)
	})
	appRemovalTargetsThisHA = func(runtimeConfig) bool { return true }
	fetchDevicePeersForUninstall = func(runtimeConfig) (devicePeers, error) {
		*fetches++
		answer := answers[min(*fetches, len(answers))-1]
		return answer()
	}
	appRemovalSignInPossible = func(io.Writer, runtimeConfig) bool { return true }
	appRemovalPurgeBlocked = func(runtimePaths) bool { return false }
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
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

var appPathConfig = runtimeConfig{HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791"}

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
		{"chat apps", func() (devicePeers, error) { return peersOf(0, false, true), nil }, false, true},
		{"unknown chat apps", func() (devicePeers, error) {
			p := peersOf(0, false, false)
			p.ChatAppFiles = nil
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
	for _, tc := range []struct {
		name  string
		cfg   runtimeConfig
		setup func()
	}{
		{"not the official App on this Home Assistant", appPathConfig, func() { appRemovalTargetsThisHA = func(runtimeConfig) bool { return false } }},
		{"no desktop sign-in", appPathConfig, func() { appRemovalSignInPossible = func(io.Writer, runtimeConfig) bool { return false } }},
		{"purge can't finish here", appPathConfig, func() { appRemovalPurgeBlocked = func(runtimePaths) bool { return true } }},
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
		"get /addons/" + slug + "/info", "delete /store/repositories/2368fcfa", "get /store/repositories"}
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
		{"chat apps meanwhile", nil, []func() (devicePeers, error){func() (devicePeers, error) { return peersOf(0, false, true), nil }}, nil, appRemovalChatAppsLine, false},
		{"lookalike source", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) {
			f.repos = []map[string]string{{"slug": "2368fcfa", "source": "https://github.com/evil/ha-nova"}}
		}, "Home Assistant lists a different NOVA source. Nothing was removed.", false},
		{"App not installed", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) { f.info["version"] = nil }, "doesn't list the NOVA App as installed", false},
		{"no Supervisor", nil, []func() (devicePeers, error){alone}, func(f *fakeSupervisor) { f.noHassio = true }, "no Apps", false},
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
			if !strings.Contains(out.String(), tc.line) || !strings.Contains(out.String(), "HA NOVA stays on this computer") {
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
	answer := `{"ok":true,"data":{"other_devices":0,"legacy_access":false,"chat_app_files":null}}`
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

// --yes never asks the Relay or signs in, even where the App could be removed.
func TestRunUninstallYesNeverRemovesTheApp(t *testing.T) {
	for _, args := range [][]string{{"--yes"}, {"--yes", "--purge"}} {
		paths := appRemovalPaths(t, appPathConfig)
		t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
		t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(paths.ConfigDir, ".test-relay-auth-token"))
		fetches, signIns := stubAppRemoval(t, newFakeSupervisor(t), nil, func() (devicePeers, error) { return peersOf(0, false, false), nil })
		exitCode, output := captureCommandOutput(t, func() int { return runUninstall(paths, args) })
		if exitCode != 0 || *fetches != 0 || *signIns != 0 || !strings.Contains(output, "The NOVA App in Home Assistant stays.") {
			t.Fatalf("args %v: exit=%d fetches=%d sign-ins=%d\n%s", args, exitCode, *fetches, *signIns, output)
		}
	}
}
