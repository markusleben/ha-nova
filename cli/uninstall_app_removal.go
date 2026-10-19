package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
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

const appRemovalChatAppsLine = "To remove the NOVA App too: Cancel, turn off chat apps in NOVA, try again."

// devicePeers is the Relay's GET /auth/device/peers answer; a missing field
// counts as unknown.
type devicePeers struct {
	OtherDevices *int  `json:"other_devices"`
	PairingOpen  *bool `json:"pairing_open"` // a code or sign-in pairing is under way
	LegacyAccess *bool `json:"legacy_access"`
	ChatAppFiles *bool `json:"chat_app_files"`
}

func (p devicePeers) chatApps() bool { return p.ChatAppFiles != nil && *p.ChatAppFiles }

// noOtherAccess: no other device, no pairing under way, and no legacy shared
// access, all known.
func (p devicePeers) noOtherAccess() bool {
	return p.OtherDevices != nil && *p.OtherDevices == 0 && p.PairingOpen != nil && !*p.PairingOpen &&
		p.LegacyAccess != nil && !*p.LegacyAccess
}

// alone: every answer is known and nothing else uses the App.
func (p devicePeers) alone() bool {
	return p.noOtherAccess() && p.ChatAppFiles != nil && !*p.ChatAppFiles
}

// Test seams.
var (
	fetchDevicePeersForUninstall = fetchDevicePeers
	appRemovalTargetsThisHA      = haAppInstallTargetsThisHA
	appRemovalSignInPossible     = haSignInPossible
	openHASignInForAppRemoval    = openHASignInIngressWith
)

// fetchDevicePeers asks the default profile's Relay on its pinned local port
// with this computer's device credential. A Relay without the route (404),
// Home Assistant Link, or a legacy token is an error: unknown.
func fetchDevicePeers(cfg runtimeConfig) (devicePeers, error) {
	original := activeServerProfile()
	setActiveServerProfile(defaultServerProfileName)
	defer setActiveServerProfile(original)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

// uninstallPurgeBlocked names why a full removal would stop in this session
// ("" when it can run): no desktop secure storage, or settings the purge
// refuses (a foreign relay token file, unreadable profiles).
func uninstallPurgeBlocked(paths runtimePaths) string {
	if purgeSecureStorageSessionError(paths) != nil {
		return "needs this computer's own desktop session"
	}
	_, _, release, err := pinnedPurgeTargets(paths)
	if err != nil {
		return "HA NOVA's saved settings need a fix first"
	}
	release()
	return ""
}

// appRemovalSecurePort is the pinned Relay's port when it runs on this Home
// Assistant's host, else 0. The App's own port mapping must match it, so the
// Relay that answered is the App that gets removed.
func appRemovalSecurePort(cfg runtimeConfig) int {
	secure, err := url.Parse(strings.TrimSpace(cfg.RelaySecureBaseURL))
	ha, haErr := url.Parse(strings.TrimSpace(cfg.HAURL))
	if err != nil || haErr != nil || strings.TrimSpace(cfg.RelaySpkiPin) == "" ||
		secure.Hostname() == "" || !strings.EqualFold(secure.Hostname(), ha.Hostname()) {
		return 0
	}
	port, err := strconv.Atoi(secure.Port())
	if err != nil || port < 1 || port > 65535 {
		return 0
	}
	return port
}

type uninstallAppOffer struct {
	offered bool
	note    string // one line when the App stays for a reason the owner can change
	cfg     runtimeConfig
}

// uninstallAppRemovalOffer decides before the menu whether removing the App is
// a choice: the official App on this Home Assistant, a desktop sign-in, and a
// Relay that says this is its last device. The caller asks only when a purge
// can run in this session.
func uninstallAppRemovalOffer(out io.Writer, paths runtimePaths) uninstallAppOffer {
	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil || !appRemovalTargetsThisHA(cfg) || appRemovalSecurePort(cfg) == 0 ||
		!appRemovalSignInPossible(out, cfg) {
		return uninstallAppOffer{}
	}
	// Link cleanup runs before the removal and can retire this computer's
	// credential, which the re-check inside the sign-in needs.
	if cloudTargets, err := collectCloudPurgeTargets(paths.ConfigFile); err != nil || len(cloudTargets) > 0 {
		return uninstallAppOffer{}
	}
	peers, err := runSetupSpinnerWithResult(out, "Checking whether other computers use the NOVA App...", setupSpinnerDebounce, func() (devicePeers, error) {
		return fetchDevicePeersForUninstall(cfg)
	})
	switch {
	case err != nil || !peers.noOtherAccess():
		return uninstallAppOffer{}
	case peers.chatApps():
		return uninstallAppOffer{note: appRemovalChatAppsLine}
	case !peers.alone():
		return uninstallAppOffer{}
	}
	return uninstallAppOffer{offered: true, cfg: cfg}
}

// removeNOVAAppForUninstall signs the owner in and removes the App. On false
// it has printed what Home Assistant holds and stops before the local removal,
// so a rerun can still ask the Relay.
func removeNOVAAppForUninstall(out io.Writer, cfg runtimeConfig) bool {
	stays := "HA NOVA is still on this computer. Run ha-nova uninstall again; pick “Remove” there to leave the NOVA App in Home Assistant."
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
			return &novaAppInstallError{line: "Chat apps are on now, so the NOVA App stays. Turn them off in NOVA, then try again."}
		case !peers.alone():
			return &novaAppInstallError{line: "Another computer uses the NOVA App now. Nothing was removed."}
		}
		supervisor := newNOVAAppSupervisor(socket)
		supervisor.removal = true
		return supervisor.remove(ctx, out, appRemovalSecurePort(cfg), &removed)
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
// The App must map the secure port this computer is paired on. removed turns
// true only once Home Assistant's store lists the App as not installed.
func (s novaAppSupervisor) remove(ctx context.Context, out io.Writer, securePort int, removed *bool) error {
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
	if mapped := info.Network[novaAppSecurePortKey]; mapped == nil || *mapped != securePort || securePort == 0 {
		return &novaAppInstallError{line: "The NOVA App in Home Assistant isn't the one this computer is connected to. Nothing was removed."}
	}
	if err := ctx.Err(); err != nil { // stopped before anything changed
		return errors.Join(errSignInStopped, err)
	}
	_, callErr := runSetupSpinnerWithResult(out, "Removing the NOVA App from Home Assistant...", setupSpinnerDebounce, func() (struct{}, error) {
		return struct{}{}, s.call(ctx, "post", "/addons/"+s.appSlug+"/uninstall", nil, 5*time.Minute, nil)
	})
	// Supervisor keeps going after a Ctrl+C, so the check gets its own time.
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var store supervisorAppInfo
	storeErr := s.call(checkCtx, "get", "/store/addons/"+s.appSlug, nil, 10*time.Second, &store)
	switch {
	case storeErr != nil || store.Slug != s.appSlug || store.Installed == nil: // no answer is no proof
		return &novaAppInstallError{line: "Home Assistant didn't confirm that the NOVA App was removed. Check Settings → Apps: if NOVA Relay is gone, run ha-nova uninstall and pick “Remove”.", cause: errors.Join(callErr, storeErr)}
	case *store.Installed && errors.Is(callErr, context.Canceled):
		return &novaAppInstallError{line: "Stopped. Home Assistant may still be removing the NOVA App; check Settings → Apps.", cause: callErr}
	case *store.Installed:
		return &novaAppInstallError{line: "Home Assistant couldn't remove the NOVA App; it is still installed.", cause: callErr}
	}
	*removed = true
	renderSetupSuccessLine(out, "NOVA App removed from Home Assistant")
	if !listed {
		return nil
	}
	deleteErr := s.call(checkCtx, "delete", "/store/repositories/"+s.repoSlug, nil, time.Minute, nil)
	still, listErr := s.sourceListed(checkCtx)
	if deleteErr == nil && listErr == nil && !still {
		renderSetupSuccessLine(out, "NOVA App source removed from Home Assistant")
		return nil
	}
	renderSetupWarningLine(out, "Home Assistant still lists the NOVA App source. Remove it in Settings → Apps → App Store → ⋮ → Repositories: %s", haNovaRepositoryURL)
	return nil
}
