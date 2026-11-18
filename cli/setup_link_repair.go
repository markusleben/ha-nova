package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// A Mac profile with Home Assistant Link whose key or Link sign-in the 0.32
// update left behind (spec §5.3) pairs again with one "ha-nova setup
// [--server <name>]", at home or away. Away, a fresh Link sign-in pairs it
// through Link, and the profile goes through Link only until a setup at home
// brings the local route back. At home, setup drops the Link entry, pairs by
// the local sign-in, and adds Link again. The old version's cleanup closes
// setup; no old sign-in is ever copied or moved.

type linkRePairState string

const (
	linkRePairAddLink        linkRePairState = "add_link"         // paired at home: Link comes back next
	linkRePairLinkOnly       linkRePairState = "link_only"        // paired away: Link only until setup runs at home
	linkRePairLinkOnlyHinted linkRePairState = "link_only_hinted" // and doctor named the way back once
)

// The home Relay's short reachability check before the route is chosen.
const homeRelayProbeTimeout = 3 * time.Second

var errLinkKept = errors.New("Home Assistant Link was not removed")

// Test seams.
var (
	linkRePairApplies                                      = func() bool { return pairingCredentialStore() == "keychain" }
	homeRelayAnswers                                       = probePairingV1Context
	linkRePairOwnAskedAway                                 = ownComputerHere
	linkRePairPairingCode   cloudRemotePairingCodeProvider = interactiveRemotePairingCode
	windowPairAgainForSetup                                = windowPairAgain
	macWindowHere                                          = func() bool { return linkRePairApplies() && ownComputerHere() }
	// The removal setup runs for itself: only while setup's snapshot holds,
	// and it keeps the re-pair checkpoint.
	linkRePairRemoveLink = func(paths runtimePaths, lifecycle [][]byte) int {
		return runCloudRemove(paths, cloudCommandFlags{confirmed: true, rePairLifecycle: lifecycle})
	}
)

// linkRePairBlocked: a Link safety hold or an unfinished removal keeps its
// own steps.
func linkRePairBlocked(cfg runtimeConfig) bool {
	return cfg.Cloud != nil && (cloudRecoveryHoldProblem(cfg) != nil || cfg.Cloud.cleanupPending())
}

// linkRePairOffered: a command names setup's re-pair for a missing Link
// sign-in or key only in a state the re-pair takes (cloud_runtime.go).
func linkRePairOffered(cfg runtimeConfig) bool {
	return linkRePairApplies() && cfg.Cloud.ready() && !linkRePairBlocked(cfg)
}

// linkRePairNeeded: setup runs the Link re-pair for this profile.
func linkRePairNeeded(cfg runtimeConfig) bool {
	if !linkRePairApplies() || linkRePairBlocked(cfg) {
		return false
	}
	if cfg.LinkRePair != "" {
		return true
	}
	if !cfg.Cloud.configured() || strings.TrimSpace(cfg.PendingSecureBaseURL) != "" {
		return false
	}
	if legacySharedAccess(cfg) {
		return true
	}
	if _, _, _, _, err := relayFunctionalTransport(cfg); errors.Is(err, errDeviceNeedsRePair) {
		return true
	}
	present, err := linkCredentialsPresent(cfg)
	return cfg.Cloud.ready() && err == nil && !present
}

// linkCredentialsPresent: this version's Link sign-in and key are in their
// slots. No-UI reads of the new slots only; the old items are never read.
func linkCredentialsPresent(cfg runtimeConfig) (bool, error) {
	ctx := context.Background()
	store, err := newCloudSecretStoreForCLI(cfg.ProfileID)
	if err != nil {
		return false, err
	}
	if _, signedIn, err := store.LoadCurrent(ctx, SecretStoreForbidUI); err != nil || !signedIn {
		return false, err
	}
	_, keyed, err := readCredentialSlotWithPolicy(ctx, deviceCredentialServiceForProfile(cfg.profileName()), SecretStoreForbidUI)
	return keyed, err
}

func homeRelayReachable(cfg runtimeConfig) bool {
	if strings.TrimSpace(cfg.RelayBaseURL) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), homeRelayProbeTimeout)
	defer cancel()
	return homeRelayAnswers(ctx, cfg.RelayBaseURL)
}

// windowPairAgain: a Mac profile paired while its NOVA App didn't know the
// window yet (the App answers pair_again) runs the re-pair once more, so
// one-click confirming and the old version's cleanup can finish. An App
// answer that can't be read is no reason.
func windowPairAgain(paths runtimePaths, cfg runtimeConfig) bool {
	if !macWindowHere() || linkRePairBlocked(cfg) || strings.TrimSpace(cfg.HAURL) == "" || strings.TrimSpace(cfg.RelayBaseURL) == "" {
		return false
	}
	why, err := appWindowWhy(paths, cfg)
	return err == nil && why == "pair_again"
}

type linkRePair struct {
	reader    *bufio.Reader
	out       io.Writer
	paths     runtimePaths
	cfg       runtimeConfig
	lifecycle [][]byte
	force     bool // windowPairAgain: pair again although this computer's key works
}

func runLinkRePair(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, lifecycle [][]byte, force bool) int {
	if err := resumeSetupDeviceCredentialRetirement(paths, cfg); err != nil {
		printHumanErr("%s", setupT("setup.error.finish_retirement", "error", err))
		return 1
	}
	if _, err := resumeInteractiveSetupPendingActivation(paths, &cfg, lifecycle...); err != nil {
		printPendingActivationResumeError(err)
		return 1
	}
	r := &linkRePair{reader: reader, out: out, paths: paths, cfg: cfg, lifecycle: lifecycle, force: force}
	switch {
	case homeRelayReachable(cfg):
		return r.atHome()
	case cfg.Cloud == nil && (cfg.LinkRePair == linkRePairAddLink || cfg.LinkRePairOrigin == ""):
		// Paired at home, Link not back yet: Link can't take over that key, so
		// only setup at home finishes. Without a Link address, likewise.
		renderSetupParagraph(out, setupT("setup.relink.home_needed", "command", r.command()))
		return 1
	case (cfg.LinkRePair == linkRePairLinkOnly || cfg.LinkRePair == linkRePairLinkOnlyHinted) && cfg.Cloud.ready():
		present, err := linkCredentialsPresent(cfg)
		if err != nil {
			printHumanErr("%s", cloudProblemForCommandError(err).humanText())
			return 1
		}
		if present { // paired away before: only the cleanup is left (and pairing again waits for home)
			return r.finish("", setupT("setup.relink.link_only", "command", r.command()))
		}
	case force: // a working key pairs again only at home, never by reusing it over Link
		renderSetupParagraph(out, setupT("setup.relink.home_needed", "command", r.command()))
		return 1
	}
	return r.away()
}

// atHome: the local sign-in pairs this computer (unless an earlier run did),
// then Link is added again for the new key. A profile without Link only
// pairs again (windowPairAgain).
func (r *linkRePair) atHome() int {
	_, _, _, device, err := relayFunctionalTransport(r.cfg)
	pair := r.force || err != nil || !device
	link := r.cfg.Cloud != nil || r.cfg.LinkRePair != ""
	var session haSignInSession
	if pair {
		if link {
			renderSetupParagraph(r.out, setupT("setup.relink.home"))
		} else {
			renderSetupParagraph(r.out, setupT("setup.relink.window"))
		}
		own, err := askOwnComputerForSetup(r.reader, r.out, r.cfg)
		if err != nil {
			return r.stopped(err)
		}
		if link && !r.linkUsable() {
			return 1
		}
		origin, err := resolveHASignInOriginForSetup(context.Background(), r.cfg.HAURL)
		if err == nil {
			session, err = openHASignInIngressForSetup(context.Background(), r.out, origin.String())
		}
		if err != nil {
			renderSetupWarningLine(r.out, "%s", haSignInFallbackLine(err, session.user))
			return r.again()
		}
		renderSetupSuccessLine(r.out, "Signed in to Home Assistant as %s (owner)", haSignInUserName(session.user))
		session.own = own
	} else if !r.linkUsable() {
		return 1
	}
	if link {
		if err := r.dropLink(linkRePairAddLink); err != nil {
			return r.failed(err)
		}
	}
	if pair {
		if paired, _, fatal := pairWithHASignInSession(r.reader, r.out, r.paths, &r.cfg, session, r.lifecycle...); !paired || fatal != nil {
			if fatal != nil && !errors.Is(fatal, errPinMismatch) {
				return 1 // its line named the setup step
			}
			return r.again()
		}
	}
	if !link {
		return r.finish(setupT("setup.relink.connected"), "")
	}
	cfg, err := addHybridCloudAfterLocal(r.paths, r.cfg, r.lifecycle...)
	if err != nil {
		return r.failed(err)
	}
	cfg.LinkRePair, cfg.LinkRePairOrigin = "", ""
	if err := saveSetupConfigWithLifecycle(r.paths, cfg, r.lifecycle...); err != nil {
		return r.failed(err)
	}
	r.cfg = cfg
	return r.finish(setupT("setup.relink.home_done"), "")
}

// away: a fresh Link sign-in in the browser, then the owner's pairing over
// Link into this version's slots. The profile goes through Link only.
func (r *linkRePair) away() int {
	coordinator, ok := cloudCoordinatorForSetup.(cloudRemoteSetupCoordinator)
	if !ok {
		printHumanErr("%s", cloudAdapterUnavailableProblem())
		return 1
	}
	if !r.linkUsable() {
		return 1
	}
	ctx, cancel := newInteractiveCloudSetupContext()
	defer cancel()
	saved := cloudOriginFromReconnectConfig(r.cfg)
	if saved == "" {
		saved = r.cfg.LinkRePairOrigin
	}
	origin, err := resolveCanonicalNabuOriginForCloudCommand(ctx, saved, NetCloudCNAMEResolver{})
	if err != nil {
		return r.failed(err)
	}
	renderSetupParagraph(r.out, setupT("setup.relink.away"))
	own := false
	if linkRePairOwnAskedAway() {
		if own, err = askOwnComputer(r.reader, r.out); err != nil {
			return r.stopped(err)
		}
	}
	if err := r.dropLink(linkRePairLinkOnly); err != nil {
		return r.failed(err)
	}
	asked := false
	code := func(prompt cloudRemotePairingPrompt) (string, error) {
		if own && !asked { // the code route can't carry the one-click answer
			renderSetupParagraph(r.out, setupT("setup.relink.own_off"))
		}
		asked = true
		return linkRePairPairingCode(prompt)
	}
	err = withPausableClientMutationLock(r.paths, func(mutation *pausableClientMutationLock) error {
		save := func(value runtimeConfig) error {
			if err := mutation.requireHeld(); err != nil {
				return err
			}
			return saveSetupConfigWithLifecycleUnlocked(r.paths, value, r.lifecycle...)
		}
		var connectErr error
		r.cfg, connectErr = connectRemoteToCloud(withOwnComputer(ctx, own), r.paths, r.cfg, coordinator, origin,
			mutation.pairingCodeProvider(code), false, save, mutation)
		return connectErr
	})
	if err != nil {
		return r.failed(err)
	}
	next := setupT("setup.relink.link_only", "command", r.command())
	r.cfg.LinkRePairOrigin = ""
	if strings.TrimSpace(r.cfg.RelayBaseURL) == "" { // never had a local route: done, as before
		r.cfg.LinkRePair, next = "", ""
	}
	if err := saveSetupConfigWithLifecycle(r.paths, r.cfg, r.lifecycle...); err != nil {
		return r.failed(err)
	}
	return r.finish(setupT("setup.relink.connected"), next)
}

// linkUsable: Link's sign-in can run here, checked before anything changes.
func (r *linkRePair) linkUsable() bool {
	if err := requireCloudRemoteFeatureForSetup(); err != nil {
		printHumanErr("%s", err)
		return false
	}
	if !cloudInteractivePromptSessionForSetup() {
		renderSetupErrorLine(r.out, "%s", setupT("setup.relink.needs_desktop", "command", r.command()))
		return false
	}
	return true
}

// dropLink saves the re-pair checkpoint (next, and the Link address) first,
// then the profile without its Link entry. A sign-in this version stored, or
// a Link setup that didn't finish, goes as "cloud remove" takes it; a sign-in
// only the old version holds stays for the cleanup at the end. Away, the
// local pairing's details go too: its key is gone, and Link carries the
// profile.
func (r *linkRePair) dropLink(next linkRePairState) error {
	cfg := r.cfg
	if origin := cloudOriginFromReconnectConfig(cfg); origin != "" {
		cfg.LinkRePairOrigin = origin
	}
	cfg.LinkRePair = next
	if err := saveSetupConfigWithLifecycle(r.paths, cfg, r.lifecycle...); err != nil {
		return err
	}
	if cfg.Cloud != nil {
		store, err := newCloudSecretStoreForCLI(cfg.ProfileID)
		if err != nil {
			return err
		}
		stored, err := inspectCloudAuthorizationCleanup(context.Background(), runtimeConfig{ProfileID: cfg.ProfileID}, store)
		if err != nil {
			return err
		}
		if stored.hasAuthorization() || !cfg.Cloud.ready() {
			if linkRePairRemoveLink(r.paths, r.lifecycle) != 0 {
				return errLinkKept
			}
			if err := refreshSetupConfigSnapshot(r.paths, r.lifecycle); err != nil {
				return err
			}
			if cfg, err = loadSelectedRuntimeConfigUnchecked(r.paths); err != nil {
				return err
			}
		}
		cfg.Cloud, cfg.RoutePolicy = nil, routePolicyLocal
	}
	if next == linkRePairLinkOnly {
		cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cfg.PendingSecureBaseURL, cfg.PendingSpkiPin = "", "", "", ""
	}
	if err := saveSetupConfigWithLifecycle(r.paths, cfg, r.lifecycle...); err != nil {
		return err
	}
	r.cfg = cfg
	return nil
}

// finish: what setup did, then the old version's cleanup. Its outcome comes
// last; when it couldn't finish, its retry is the only next step.
func (r *linkRePair) finish(done, next string) int {
	if done != "" {
		renderSetupSuccessLine(r.out, "%s", done)
	}
	note := printLegacyLinkCleanup(r.out, r.paths, r.cfg, isInteractiveTTY() && writerSupportsTTYForSetup(r.out))
	if next != "" && note != setupT("setup.cleanup.incomplete", "command", gateCleanupCommand(r.cfg.profileName())) {
		renderSetupParagraph(r.out, next)
	}
	if note != "" {
		renderSetupParagraph(r.out, note)
	}
	return 0
}

func (r *linkRePair) command() string { return rePairNextStep(activeServerProfile(), true) }

// stopped: Back or Exit before anything changed.
func (r *linkRePair) stopped(err error) int {
	if errors.Is(err, errSetupBack) || errors.Is(err, errSetupExit) {
		renderSetupCancelledNote(r.out)
		return 0
	}
	return r.failed(err)
}

func (r *linkRePair) failed(err error) int {
	switch {
	case errors.Is(err, errSetupBack), errors.Is(err, errSetupExit):
		renderSetupCancelledNote(r.out)
	case !errors.Is(err, errLinkKept): // cloud remove said why
		printHumanErr("%s", cloudProblemForCommandError(err).humanText())
	}
	return r.again()
}

// again: the next step after a failure; a Link safety hold names its own.
func (r *linkRePair) again() int {
	if latest, err := loadSelectedRuntimeConfigUnchecked(r.paths); err == nil {
		if problem := cloudRecoveryHoldProblem(latest); problem != nil {
			renderCloudRecoveryGuidance(r.out, latest, problem)
			return 1
		}
	}
	renderSetupParagraph(r.out, setupT("setup.relink.again", "command", r.command()))
	return 1
}

// printHomeRouteHint: doctor at home, once for a profile that paired away
// and whose Link connection is complete.
func printHomeRouteHint(paths runtimePaths, cfg runtimeConfig) {
	if cfg.LinkRePair != linkRePairLinkOnly || !cfg.Cloud.ready() || cloudRecoveryHoldProblem(cfg) != nil {
		return
	}
	if present, err := linkCredentialsPresent(cfg); err != nil || !present || !homeRelayReachable(cfg) {
		return
	}
	printHumanInfo("%s", setupT("setup.relink.home_hint", "command", rePairNextStep(activeServerProfile(), true)))
	err := withClientMutationLock(paths, func() error {
		latest, err := loadConfig(paths)
		if err != nil || latest.LinkRePair != linkRePairLinkOnly {
			return err
		}
		latest.LinkRePair = linkRePairLinkOnlyHinted
		return saveConfig(paths, latest)
	})
	if err != nil {
		printHumanWarn("couldn't note that this hint was shown: %s", err)
	}
}
