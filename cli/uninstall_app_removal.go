package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// "Remove, and remove the NOVA App from Home Assistant" in `ha-nova
// uninstall` (docs/work/2026-10-18-onboarding-0.27-spec.md §9). It is a
// choice only when this computer's Relay answers that no other device, no
// shared legacy access, and no chat-app files depend on its App. The owner's
// sign-in then removes only the official App and its source through the
// Supervisor allow-list (ha_supervisor_app.go); every other App and Home
// Assistant's own data stay. Any doubt keeps the App.

const appRemovalChatAppsLine = "The NOVA App stays: chat apps are on. Turn them off in NOVA to remove the App too."

// devicePeers is the Relay's GET /auth/device/peers answer; a missing field
// counts as unknown.
type devicePeers struct {
	OtherDevices *int  `json:"other_devices"`
	LegacyAccess *bool `json:"legacy_access"`
	ChatAppFiles *bool `json:"chat_app_files"`
}

func (p devicePeers) chatApps() bool { return p.ChatAppFiles != nil && *p.ChatAppFiles }

// alone: every answer is known and nothing else uses the App.
func (p devicePeers) alone() bool {
	return p.OtherDevices != nil && *p.OtherDevices == 0 &&
		p.LegacyAccess != nil && !*p.LegacyAccess &&
		p.ChatAppFiles != nil && !*p.ChatAppFiles
}

// Test seams.
var (
	fetchDevicePeersForUninstall = fetchDevicePeers
	appRemovalTargetsThisHA      = haAppInstallTargetsThisHA
	appRemovalSignInPossible     = haSignInPossible
	appRemovalPurgeBlocked       = func(paths runtimePaths) bool { return purgeSecureStorageSessionError(paths) != nil }
	openHASignInForAppRemoval    = openHASignInIngressWith
)

// fetchDevicePeers asks the default profile's Relay on its pinned local port
// with this computer's device credential. A Relay without the route (404),
// Home Assistant Link, or a legacy token is an error: unknown.
func fetchDevicePeers(cfg runtimeConfig) (devicePeers, error) {
	original := activeServerProfile()
	setActiveServerProfile(defaultServerProfileName)
	defer setActiveServerProfile(original)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	selected, err := selectRelayTransport(ctx, cfg, relayViaLocal, true)
	if err != nil {
		return devicePeers{}, err
	}
	if !selected.DeviceMode {
		return devicePeers{}, errors.New("this computer has no device pairing")
	}
	return readDevicePeers(ctx, selected.Client, selected.BaseURL, selected.Credential)
}

func readDevicePeers(ctx context.Context, client *http.Client, baseURL, credential string) (devicePeers, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/auth/device/peers", nil)
	if err != nil {
		return devicePeers{}, err
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err := client.Do(request)
	if err != nil {
		return devicePeers{}, err
	}
	defer response.Body.Close()
	body, err := readAllLimited(response.Body, 64<<10)
	if err != nil {
		return devicePeers{}, err
	}
	if response.StatusCode != http.StatusOK {
		return devicePeers{}, fmt.Errorf("device peers returned %d", response.StatusCode)
	}
	var envelope struct {
		OK   bool        `json:"ok"`
		Data devicePeers `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.OK {
		return devicePeers{}, errors.Join(errors.New("device peers answer is not readable"), err)
	}
	return envelope.Data, nil
}

type uninstallAppOffer struct {
	offered bool
	note    string // one line when the App stays for a reason the owner can change
	cfg     runtimeConfig
}

// uninstallAppRemovalOffer decides before the menu whether removing the App is
// a choice: the official App on this Home Assistant, a desktop sign-in, a
// purge this session can finish, and a Relay that says this is its last device.
func uninstallAppRemovalOffer(out io.Writer, paths runtimePaths) uninstallAppOffer {
	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil || !appRemovalTargetsThisHA(cfg) || !appRemovalSignInPossible(out, cfg) || appRemovalPurgeBlocked(paths) {
		return uninstallAppOffer{}
	}
	peers, err := fetchDevicePeersForUninstall(cfg)
	switch {
	case err != nil:
		return uninstallAppOffer{}
	case peers.chatApps():
		return uninstallAppOffer{note: appRemovalChatAppsLine}
	case !peers.alone():
		return uninstallAppOffer{}
	}
	return uninstallAppOffer{offered: true, cfg: cfg}
}

// removeNOVAAppForUninstall signs the owner in and removes the App. On false
// it has printed what Home Assistant holds; HA NOVA then stays on this
// computer, so a rerun can still ask the Relay.
func removeNOVAAppForUninstall(out io.Writer, cfg runtimeConfig) bool {
	stays := "HA NOVA stays on this computer. Run ha-nova uninstall again, or choose Remove to keep the NOVA App."
	origin, err := resolveHASignInOriginForSetup(context.Background(), cfg.HAURL)
	if err != nil {
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, HACurrentUser{}))
		renderSetupParagraphTight(out, stays)
		return false
	}
	renderSetupParagraph(out,
		"Your browser opens Home Assistant: sign in as the owner.",
		"(Home Assistant asks to give “http://127.0.0.1:…” access: that's this computer.)",
		"HA NOVA then removes only its own App and the App's source.",
	)
	removed := false
	prepare := func(ctx context.Context, socket haWSCaller, user HACurrentUser) error {
		renderSetupSuccessLine(out, "Signed in to Home Assistant as %s (owner)", haSignInUserName(user))
		// Time passed since the menu: ask the Relay again right before.
		peers, err := fetchDevicePeersForUninstall(cfg)
		switch {
		case err != nil:
			return &novaAppInstallError{line: "HA NOVA couldn't check whether other computers use the NOVA App. Nothing was removed.", cause: err}
		case peers.chatApps():
			return &novaAppInstallError{line: appRemovalChatAppsLine}
		case !peers.alone():
			return &novaAppInstallError{line: "Another computer uses the NOVA App now. Nothing was removed."}
		}
		supervisor := newNOVAAppSupervisor(socket)
		supervisor.removal = true
		return supervisor.remove(ctx, out, &removed)
	}
	// After prepare the sign-in looks for the App's ingress and fails: the App
	// is gone by then, so only removed counts.
	session, err := openHASignInForAppRemoval(context.Background(), out, origin.String(), prepare)
	if removed {
		return true
	}
	var failure *novaAppInstallError
	switch {
	case errors.As(err, &failure):
		renderSetupWarningLine(out, "%s", failure.line)
	case errors.Is(err, errAppInstallNoSupervisor):
		renderSetupWarningLine(out, "This Home Assistant has no Apps, so there is no NOVA App to remove.")
	case errors.Is(err, errSignInNotOwner):
		renderSetupWarningLine(out, "Signed in as %s, who is not the Home Assistant owner. Only the owner can remove the NOVA App.", haSignInUserName(session.user))
	default:
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, session.user))
	}
	renderSetupParagraphTight(out, stays)
	return false
}

// remove uninstalls the official App, without remove_config, then its source.
// removed turns true only once Home Assistant reports the App gone.
func (s novaAppSupervisor) remove(ctx context.Context, out io.Writer, removed *bool) error {
	var config struct {
		Components []string `json:"components"`
	}
	if err := s.ws.Call(ctx, "get_config", nil, &config); err != nil {
		return err
	}
	if !slices.Contains(config.Components, "hassio") {
		return errAppInstallNoSupervisor
	}
	listed, err := s.sourceListed(ctx)
	if err != nil {
		return err
	}
	info, err := s.info(ctx)
	if err != nil {
		return err
	}
	if info.Version == "" {
		return &novaAppInstallError{line: "Home Assistant doesn't list the NOVA App as installed. Nothing was removed."}
	}
	_, callErr := runSetupSpinnerWithResult(out, "Removing the NOVA App from Home Assistant...", setupSpinnerDebounce, func() (struct{}, error) {
		return struct{}{}, s.call(ctx, "post", "/addons/"+s.appSlug+"/uninstall", nil, 5*time.Minute, nil)
	})
	after, infoErr := s.info(ctx)
	switch {
	case infoErr != nil:
		return &novaAppInstallError{line: "Home Assistant didn't confirm that the NOVA App was removed; it may still be removing it.", cause: errors.Join(callErr, infoErr)}
	case after.Version != "":
		return &novaAppInstallError{line: "Home Assistant couldn't remove the NOVA App; it is still installed.", cause: callErr}
	}
	*removed = true
	renderSetupSuccessLine(out, "NOVA App removed from Home Assistant")
	if !listed {
		return nil
	}
	deleteErr := s.call(ctx, "delete", "/store/repositories/"+s.repoSlug, nil, time.Minute, nil)
	still, listErr := s.sourceListed(ctx)
	if deleteErr == nil && listErr == nil && !still {
		renderSetupSuccessLine(out, "NOVA App source removed from Home Assistant")
		return nil
	}
	renderSetupWarningLine(out, "Home Assistant still lists the NOVA App source. Remove it in Settings → Apps → App Store → ⋮ → Repositories: %s", haNovaRepositoryURL)
	return nil
}
