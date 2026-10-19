package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// `ha-nova try`: the shared demo house at demo.hanova.app, with no Home
// Assistant of one's own. It lives only in demo.json, never in config.json:
// it cannot become the default server, setup/pair/cloud never see it, and it
// is selected only explicitly (HA_NOVA_SERVER=demo or --server demo).

const demoProfileName = "demo"

// Overridden by tests only. The relay URL that connect.json names must share
// this origin, so the public demo token never goes anywhere else.
var demoConnectURL = "https://demo.hanova.app/connect.json"

const (
	demoUnreachableText = "The demo house can't be reached right now. It may be restarting (it resets every hour); try again in a few minutes."
	demoNoticeText      = "This is the demo house: a shared HA NOVA demo home, not the user's own. It resets every hour; never type real names or passwords."
)

type demoProfile struct {
	HAURL      string `json:"ha_url"`
	RelayURL   string `json:"relay_url"`
	RelayToken string `json:"relay_token"`
}

func demoProfilePath(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "demo.json")
}

// loadDemoProfile returns the demo house's config when `ha-nova try` is on;
// ok=false when it is off.
func loadDemoProfile(paths runtimePaths) (runtimeConfig, bool, error) {
	data, err := os.ReadFile(demoProfilePath(paths))
	if errors.Is(err, os.ErrNotExist) {
		return runtimeConfig{}, false, nil
	}
	var demo demoProfile
	if err == nil {
		err = json.Unmarshal(data, &demo)
	}
	if err == nil && (demo.RelayURL == "" || demo.RelayToken == "") {
		err = errors.New("incomplete")
	}
	if err != nil {
		return runtimeConfig{}, true, fmt.Errorf("the demo house connection is broken (%v); run: ha-nova try", err)
	}
	u, _ := url.Parse(demo.HAURL)
	return runtimeConfig{
		HAURL: demo.HAURL, HAHost: u.Hostname(), RelayBaseURL: demo.RelayURL,
		RoutePolicy: routePolicyLocal, demoToken: demo.RelayToken,
	}, true, nil
}

func demoProfileOn(paths runtimePaths) bool {
	_, err := os.Stat(demoProfilePath(paths))
	return err == nil
}

// realDemoProfileExists: a server profile named "demo" made before the name
// was reserved. It stays a real home and keeps the name.
func realDemoProfileExists(paths runtimePaths) bool {
	doc, err := loadConfigDocument(paths.ConfigFile)
	return err == nil && doc.servers != nil && doc.hasProfile(demoProfileName)
}

// demoSelected reports an explicit demo selection on a command that must
// never run against the demo house (setup, pair, cloud, doctor).
func demoSelected(paths runtimePaths, args []string) bool {
	picked := serverSelectionFromEnv()
	for i, arg := range args { // --server beats HA_NOVA_SERVER
		name := strings.TrimLeft(arg, "-")
		if value, ok := strings.CutPrefix(name, "server="); ok && arg != name {
			picked = value
		} else if name == "server" && arg != name && i+1 < len(args) {
			picked = args[i+1]
		}
	}
	return picked == demoProfileName && !realDemoProfileExists(paths)
}

func runTryCommand(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("try", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stop := fs.Bool("stop", false, "disconnect the demo house")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova try [--stop]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}
	if fs.NArg() > 0 {
		printHumanErr("ha-nova try takes no arguments besides --stop")
		return 1
	}
	if *stop {
		return stopDemo(paths)
	}
	if realDemoProfileExists(paths) {
		printHumanErr("One of your servers is already called %q. Rename it first, e.g.: ha-nova server rename demo home2", demoProfileName)
		return 1
	}
	// An uninstall during the fetch must win: nothing is saved after it.
	generation, genErr := readInstallLifecycleGeneration(paths)
	uninstalled, markErr := readUninstallMarker(paths)
	if err := errors.Join(genErr, markErr); err != nil {
		printHumanErr("cannot inspect the HA NOVA install: %s", err)
		return 1
	}
	demo, err := fetchDemoProfile()
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	data, _ := json.MarshalIndent(demo, "", "  ")
	err = ensureSetupLifecycleCurrent(paths, generation, uninstalled)
	if err == nil {
		err = os.MkdirAll(paths.ConfigDir, 0o700)
	}
	if err == nil {
		err = writeFileAtomic(demoProfilePath(paths), append(data, '\n'), 0o600)
	}
	if err != nil {
		printHumanErr("cannot save the demo house connection: %s", err)
		return 1
	}
	if !installClientsForDemo(paths, generation, uninstalled) {
		fmt.Fprint(os.Stdout, `
Demo house connected, but no AI app was found on this computer.
Install Claude Code or Codex, then run: ha-nova try
`)
		return 0
	}
	fmt.Fprint(os.Stdout, `
Demo house connected. It is a shared demo home, not yours: it resets every hour.
Never type real names or passwords there. Your own home is not touched.

Open your AI app (Claude Code, Codex, ...) in a new chat and try:
  "In the demo house, which lights are on?"
  "In the demo house, turn off the kitchen lights."
  "In the demo house, check the automations for problems."

Done?            ha-nova try --stop
Your own home?   ha-nova setup
`)
	return 0
}

func stopDemo(paths runtimePaths) int {
	err := os.Remove(demoProfilePath(paths))
	switch {
	case errors.Is(err, os.ErrNotExist):
		printHumanInfo("The demo house was not connected.")
	case err != nil:
		printHumanErr("cannot remove the demo house connection: %s", err)
		return 1
	default:
		printHumanInfo("Demo house disconnected. Your own home and settings were not touched.")
	}
	return 0
}

// fetchDemoProfile reads connect.json and checks that the relay answers, so a
// broken demo never gets saved. Every failure is one plain line.
func fetchDemoProfile() (demoProfile, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	unreachable := errors.New(demoUnreachableText)
	var demo demoProfile
	if status, body, err := demoGET(ctx, demoConnectURL, ""); err != nil || status != http.StatusOK || json.Unmarshal(body, &demo) != nil {
		return demoProfile{}, unreachable
	}
	origin, _ := url.Parse(demoConnectURL)
	relay, err := url.Parse(demo.RelayURL)
	if err != nil || relay.Scheme != origin.Scheme || relay.Host != origin.Host || relay.RawQuery != "" ||
		demo.RelayToken == "" || len(demo.RelayToken) > 512 || strings.ContainsAny(demo.RelayToken, " \t\r\n") {
		return demoProfile{}, unreachable
	}
	if ha, err := url.Parse(demo.HAURL); err != nil || ha.Host != origin.Host {
		demo.HAURL = origin.Scheme + "://" + origin.Host
	}
	demo.RelayURL = strings.TrimRight(demo.RelayURL, "/")
	if status, _, err := demoGET(ctx, demo.RelayURL+"/health", demo.RelayToken); err != nil || status != http.StatusOK {
		return demoProfile{}, unreachable
	}
	return demo, nil
}

func demoGET(ctx context.Context, target, token string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := demoHTTPClient().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := readAllLimited(resp.Body, 64<<10)
	return resp.StatusCode, body, err
}

// demoHTTPClient: system TLS trust, no redirects, a fresh transport per use
// (relay health applies its own timeouts to it).
func demoHTTPClient() *http.Client {
	return newRelayHTTPClient(defaultRelayConnectTimeoutSeconds, defaultRelayMaxTimeSeconds)
}

// installClientsForDemo adds the skills to the AI apps on this computer when
// none has them yet; a computer already set up keeps its clients untouched.
// false: no AI app was found.
func installClientsForDemo(paths runtimePaths, lifecycle ...[]byte) bool {
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil || len(state.InstalledClients) > 0 {
		return true
	}
	clients, _, err := resolveSetupClients(paths, "all")
	if err != nil || len(clients) == 0 {
		return false
	}
	if err := installClientsAndSaveState(paths, &state, clients, saveState, lifecycle...); err != nil {
		printHumanWarn("HA NOVA could not be added to your AI app: %s. Fix that, then run: ha-nova try", err)
	}
	return true
}
