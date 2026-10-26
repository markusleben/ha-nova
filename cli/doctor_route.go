package main

import (
	"context"
	"errors"
	"fmt"
)

// doctorRoute is how doctor reaches the NOVA App this run. unchecked: it
// can't, and the reason is printed, so the App check is skipped. failed:
// doctor printed something to fix.
type doctorRoute struct {
	transport  relayTransportSelection
	token      string
	deviceMode bool
	unchecked  bool
	failed     bool
}

// selectDoctorRoute picks the route functional commands use. The choice
// never ends doctor: when the saved route fails, doctor says why and checks
// the home connection instead; when the key can't be read, it skips the App
// check (docs/work/2026-10-26-link-leftover-setup-doctor-spec.md).
func selectDoctorRoute(ctx context.Context, paths runtimePaths, cfg runtimeConfig, state installState, info func(string, ...any)) doctorRoute {
	var route doctorRoute
	stop := func() doctorRoute {
		route.unchecked, route.failed = true, true
		return route
	}
	transport, transportErr := selectRelayTransport(ctx, cfg, "", false)
	var localCredential *localRelayCredentialError
	if transportErr != nil && effectiveRoutePolicy(cfg.RoutePolicy) != routePolicyLocal &&
		!errors.As(transportErr, &localCredential) { // a key-read problem: the home route has it too
		printDoctorRouteProblem(cfg, transportErr)
		route.failed = true
		if remoteOnlyCloudSetup(cfg) {
			printHumanErr("This computer reaches Home Assistant only through Home Assistant Link, so HA NOVA can't check the NOVA App now.")
			return stop()
		}
		info("Checking the connection at home instead.")
		transport, transportErr = resolveRelayTransport(ctx, resolveLocalRelayTransportForCLI, cfg)
	}
	route.transport = transport
	if transportErr == nil && transport.DeviceMode {
		route.token, route.deviceMode = transport.Credential, true
		info("This computer's key is saved")
		return route
	}
	if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		// A paired config whose device transport failed must NOT be masked by a
		// leftover legacy token: report the device problem so the user re-pairs.
		// The direct slot read distinguishes unreadable storage from an absent
		// credential (re-pairing cannot store anything in broken storage).
		if _, exists, credErr := readDeviceCredential(); credErr != nil && isLinuxKeyringError(credErr) {
			printHumanErr("This computer was set up with HA NOVA, but HA NOVA can't read its key. %s", relayAuthTokenProblemMessage(credErr))
			if hint := doctorServiceCredentialRecoveryHint(paths, state, credErr); hint != "" {
				printHumanWarn("%s", hint)
			}
			return stop()
		} else if credErr != nil {
			printHumanErr("This computer was set up with HA NOVA, but HA NOVA can't read its key from the password store. Details: %s", credErr)
			if secureStorageSessionProblem(credErr) {
				printHumanWarn("%s. Next step: %s, then run 'ha-nova doctor' there.", secureStorageSessionReason, secureStorageSessionNextStep())
			} else if hint := setupSecureStorageRecoveryHint(credErr); hint != "" {
				printHumanWarn("%s", hint)
			} else {
				printHumanWarn("Unlock this computer's password store, then run 'ha-nova doctor' again.")
			}
			return stop()
		} else if exists {
			printHumanErr("%s", doctorTransportErrorMessage(transportErr))
			return stop()
		}
		printHumanErr("This computer was connected, but its key is gone from the password store.")
		printHumanErr(
			"Connect it again: run '%s'.",
			localRelayRepairCommand(
				activeServerProfile(),
				cfg.RelayBaseURL,
			),
		)
		return stop()
	}
	if activeServerProfile() != defaultServerProfileName {
		// Non-default profiles are device-credential-only: never check them with
		// the machine-wide legacy token (it belongs to the default profile).
		if transportErr == nil {
			transportErr = fmt.Errorf(
				"server %q isn't connected yet. Run: %s",
				activeServerProfile(),
				localRelayRepairCommand(
					activeServerProfile(),
					cfg.RelayBaseURL,
				),
			)
		}
		printHumanErr("%s", doctorErrorText(transportErr))
		return stop()
	}
	token, tokenErr := transport.Credential, transportErr
	if tokenErr != nil {
		token, tokenErr = readRelayAuthTokenForDoctor()
	}
	if tokenErr != nil || token == "" {
		printHumanErr("%s", relayAuthTokenProblemMessage(tokenErr))
		if hint := doctorServiceCredentialRecoveryHint(paths, state, tokenErr); hint != "" {
			printHumanWarn("%s", hint)
		}
		if hint := setupSecureStorageRecoveryHint(tokenErr); hint != "" {
			printHumanWarn("%s", hint)
		}
		return stop()
	}
	where := relayAuthTokenStorageLabel()
	if where == "secure storage" {
		where = "this computer's password store"
	}
	info("This computer's shared key is saved in %s", where)
	route.token = token
	return route
}

// printDoctorRouteProblem says in plain words why the saved route fails.
func printDoctorRouteProblem(cfg runtimeConfig, err error) {
	var preflight *localRelayPreflightError
	switch {
	case errors.Is(err, errRelayHealthIdentityMismatch):
		printHumanErr("%s", relayIdentityMismatchText(activeServerProfile(), cfg.RelayBaseURL))
	case errors.As(err, &preflight):
		printHumanErr("HA NOVA's safety check of the NOVA App failed, so HA NOVA commands stop before they reach it. Details: %s", preflight.Error())
	default:
		message := relayTransportErrorMessage(err)
		if hold := cloudRecoveryHoldProblem(cfg); cloudHoldIsSecurityStop(cfg) && message == hold.humanText() {
			return // the hold's own steps follow (printDoctorLinkLine)
		}
		printHumanErr("%s", message)
	}
}

// doctorTransportErrorMessage: relayTransportErrorMessage without sending
// the reader to doctor, which is running.
func doctorTransportErrorMessage(err error) string {
	var localCredential *localRelayCredentialError
	if errors.As(err, &localCredential) {
		return localCredential.doctorText()
	}
	return relayTransportErrorMessage(err)
}

func doctorErrorText(err error) string {
	var localCredential *localRelayCredentialError
	if errors.As(err, &localCredential) {
		return localCredential.doctorText()
	}
	return err.Error()
}

// printDoctorLinkLine warns when the saved Home Assistant Link isn't ready
// or is held, with the way out. It never fails doctor.
func printDoctorLinkLine(cfg runtimeConfig) {
	if cfg.Cloud == nil || printCloudSecurityHold(cfg) {
		return
	}
	profile := selectedCloudCommandProfile()
	remove := cloudProfileCommandFor("remove", profile)
	if cloudRecoveryHoldProblem(cfg) != nil {
		printHumanWarn("Home Assistant Link is paused on this computer.")
		if cloudRecoveryHoldNeedsUnlockForConfig(cfg) {
			printHumanWarn("First let HA NOVA check this computer's password store: %s", cloudProfileCommandFor("unlock", profile))
		}
		if remove != "" {
			printHumanWarn("To remove: %s", remove)
		}
		return
	}
	if cfg.Cloud.ready() {
		return
	}
	if remoteOnlyCloudSetup(cfg) {
		printHumanWarn("Home Assistant Link isn't finished on this computer.")
	} else {
		printHumanWarn("Home Assistant Link isn't finished on this computer. HA NOVA still works at home without it.")
	}
	if profile == "" {
		return
	}
	if !cfg.Cloud.cleanupPending() && cloudRemoteFeatureAvailable() {
		printHumanWarn("To finish: %s (you sign in to Home Assistant once)", cloudResumeCommandFor(cfg, profile))
	}
	printHumanWarn("To remove: %s", remove)
}
