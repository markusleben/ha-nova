package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	readRelayAuthTokenForDoctor = readRelayAuthToken
	probePairingV1ForDoctor     = probePairingV1Context
	firstUseRelayNoticeTimeout  = 4 * time.Second
)

func runDoctor(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	autoRepair := fs.Bool("auto-repair", false, "silently reattach drifted clients before reporting")
	quiet := fs.Bool("quiet", false, "suppress info lines; only print warnings, errors, and repair actions")
	server := fs.String("server", "", "server profile name (multi-server installs)")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova doctor [--auto-repair] [--quiet] [--server <name>]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}
	if err := selectServerFlag(fs, *server); err != nil {
		printHumanErr("%s", err)
		return 1
	}
	doctorLifecycleGeneration, lifecycleErr := readInstallLifecycleGeneration(paths)
	if lifecycleErr != nil {
		printHumanErr("HA NOVA couldn't read its install state: %s", lifecycleErr)
		return 1
	}
	doctorConfigSnapshot, doctorHadConfigSnapshot, snapshotErr := readOptionalFile(paths.ConfigFile)
	if snapshotErr != nil {
		printHumanErr("HA NOVA couldn't read its settings file: %s", snapshotErr)
		return 1
	}
	doctorInfo := func(format string, parts ...any) {
		if *quiet {
			return
		}
		printHumanInfo(format, parts...)
	}

	if recovery := inspectWindowsUninstallStatus(paths); recovery.Kind != windowsUninstallStatusKindNone {
		switch recovery.Kind {
		case windowsUninstallStatusKindRunning:
			printHumanErr("%s", recovery.Summary)
			printHumanWarn("HA NOVA is still being removed. Wait for that to finish, then run `ha-nova doctor` again.")
			return 1
		case windowsUninstallStatusKindInterrupted, windowsUninstallStatusKindFailed, windowsUninstallStatusKindCorrupt:
			printHumanErr("%s", recovery.Summary)
			printHumanWarn("To fix: run `%s`.", recovery.RecoveryCommand)
			return 1
		}
	}

	cfg, cfgErr := loadConfig(paths)
	state, stateErr := loadStateOrDefaultChecked(paths)
	if stateErr != nil {
		printHumanErr("%s", stateErr)
		return 1
	}
	status, appBelowFloor := 0, false

	if cfgErr == nil {
		doctorInfo("Settings file found: %s", paths.ConfigFile)
	} else {
		printHumanErr("%s", cfgErr)
		return 1
	}
	// Shared access still works in this release: a warning first, never a
	// failure, and not in --quiet's machine contract.
	if !*quiet {
		legacyCtx, cancelLegacy := context.WithTimeout(context.Background(), legacyNoticeProbeTimeout)
		if legacySharedAccess(cfg) && legacyTokenReadable() &&
			legacyAccessSwitchable(cfg, func(url string) bool { return probePairingV1ForDoctor(legacyCtx, url) }) {
			printHumanWarn("%s", legacyAccessNoticeFor(cfg))
		}
		cancelLegacy()
	}
	// Multi-server installs: name the checked profile so per-server doctor runs
	// (ha-nova doctor --server <name>) are unambiguous.
	if profileName, profileCount := selectedServerProfileStatus(paths); profileCount > 1 || profileName != defaultServerProfileName {
		doctorInfo("Checking server: %s", profileName)
	}

	// Finish a pairing interrupted between activation and promotion (crash or
	// lost response) before any client self-heal mutation.
	resumed, resumeErr := resumeInterruptedPairingForDoctor(
		paths,
		&cfg,
		doctorLifecycleGeneration,
		doctorConfigSnapshot,
		doctorHadConfigSnapshot,
	)
	if resumeErr != nil {
		printPendingActivationResumeError(resumeErr)
		return 1
	}
	if resumed {
		doctorInfo("Finished connecting this computer. It is connected now.")
	}
	// Self-heal client integrations once after a version change (complements
	// --auto-repair below, which only re-attaches drifted clients). Best-effort.
	ensureClientsVerifiedForCurrentVersion(paths)

	// Doctor uses the same route policy as functional commands. Automatic may
	// therefore verify over Cloud while away, but still fails closed on local
	// authentication, pin, or protocol errors.
	doctorRelayCtx, cancelDoctorRelay := context.WithTimeout(
		context.Background(),
		time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)),
	)
	defer cancelDoctorRelay()
	transport, transportErr := selectRelayTransport(doctorRelayCtx, cfg, "", false)
	transportBase, transportClient, transportCred, deviceMode :=
		transport.BaseURL, transport.Client, transport.Credential, transport.DeviceMode
	pairedConfig := cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != ""
	var token string
	if transportErr == nil && deviceMode {
		token = transportCred
		doctorInfo("This computer's key is saved")
	} else if pairedConfig {
		// A paired config whose device transport failed must NOT be masked by a
		// leftover legacy token: report the device problem so the user re-pairs.
		// The direct slot read distinguishes unreadable storage from an absent
		// credential (re-pairing cannot store anything in broken storage).
		if _, exists, credErr := readDeviceCredential(); credErr != nil && isLinuxKeyringError(credErr) {
			printHumanErr("This computer was set up with HA NOVA, but HA NOVA can't read its key. %s", relayAuthTokenProblemMessage(credErr))
			if hint := doctorServiceCredentialRecoveryHint(paths, state, credErr); hint != "" {
				printHumanWarn("%s", hint)
			}
			return 1
		} else if credErr != nil {
			printHumanErr("This computer was set up with HA NOVA, but HA NOVA can't read its key from the password store. Details: %s", credErr)
			if secureStorageSessionProblem(credErr) {
				printHumanWarn("%s. Next step: %s, then run 'ha-nova doctor' there.", secureStorageSessionReason, secureStorageSessionNextStep())
			} else if hint := setupSecureStorageRecoveryHint(credErr); hint != "" {
				printHumanWarn("%s", hint)
			} else {
				printHumanWarn("Unlock this computer's password store, then run 'ha-nova doctor' again.")
			}
			return 1
		} else if exists {
			printHumanErr("%s", relayTransportErrorMessage(transportErr))
			return 1
		}
		printHumanErr("This computer was connected, but its key is gone from the password store.")
		printHumanErr(
			"Connect it again: run '%s'.",
			localRelayRepairCommand(
				activeServerProfile(),
				cfg.RelayBaseURL,
			),
		)
		return 1
	} else if transportErr != nil &&
		effectiveRoutePolicy(cfg.RoutePolicy) != routePolicyLocal {
		if hold := cloudRecoveryHoldProblem(cfg); !cloudHoldIsSecurityStop(cfg) || relayTransportErrorMessage(transportErr) != hold.humanText() {
			printHumanErr("%s", relayTransportErrorMessage(transportErr)) // the hold itself: its steps say it
		}
		printCloudSecurityHold(cfg)
		return 1
	} else if activeServerProfile() != defaultServerProfileName {
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
		printHumanErr("%s", transportErr)
		return 1
	} else {
		tokenErr := transportErr
		if tokenErr == nil {
			token = transportCred
		} else {
			token, tokenErr = readRelayAuthTokenForDoctor()
		}
		if tokenErr == nil && token != "" {
			where := relayAuthTokenStorageLabel()
			if where == "secure storage" {
				where = "this computer's password store"
			}
			doctorInfo("This computer's shared key is saved in %s", where)
		} else {
			printHumanErr("%s", relayAuthTokenProblemMessage(tokenErr))
			if hint := doctorServiceCredentialRecoveryHint(paths, state, tokenErr); hint != "" {
				printHumanWarn("%s", hint)
			}
			if hint := setupSecureStorageRecoveryHint(tokenErr); hint != "" {
				printHumanWarn("%s", hint)
			}
			return 1
		}
	}

	usingCloud := transport.Via == relayViaCloud
	haReachable, haHinted := false, false
	haURLKnown := strings.TrimSpace(cfg.HAURL) != ""
	if usingCloud {
		doctorInfo("Using Home Assistant Link, so HA NOVA skips the home address check")
		haReachable = true
	} else if !haURLKnown {
		// A pair-only setup (`ha-nova pair --relay-url ...`) has no saved HA
		// address yet. The relay's WS state still proves the connection; the
		// direct HA probe is just skipped instead of failing on an empty URL.
		if activeServerProfile() == defaultServerProfileName {
			printHumanWarn("No Home Assistant address saved yet, so HA NOVA skips that check. Run 'ha-nova setup' to finish setting up this computer.")
		} else {
			printHumanWarn("No Home Assistant address saved for this server, so HA NOVA skips that check.")
		}
	} else if err := probeHTTP(cfg.HAURL); err != nil {
		if hint := reachProblem(cfg.HAURL, err); hint != "" {
			printHumanErr("%s", hint)
			haHinted = true
		} else {
			printHumanErr("This computer can't reach Home Assistant at %s. Details: %s", cfg.HAURL, err)
		}
		status = 1
	} else {
		doctorInfo("Home Assistant answers at %s", cfg.HAURL)
		haReachable = true
	}
	// WS state is judged when HA answered directly, or when no address is
	// saved (then the relay's own upstream state is the only — and sufficient —
	// signal). Only a KNOWN-down HA suppresses it: blaming tokens while HA
	// itself is offline would mislead.
	judgeWS := haReachable || !haURLKnown

	healthBase := transportBase
	runReadiness := func() relayReadiness {
		if deviceMode || usingCloud {
			return checkRelayReadinessOverTransportContext(
				doctorRelayCtx,
				transportBase,
				transportClient,
				token,
			)
		}
		return checkRelayReadiness(cfg.RelayBaseURL, token)
	}
	// Over Home Assistant Link, a turned-down computer is Link's to explain.
	printNotAccepted := func() {
		if usingCloud {
			printHumanErr("Home Assistant Link didn't let this computer in. Run 'ha-nova cloud status --server %s'. It says what to do next.", activeServerProfile())
			return
		}
		printHumanErr(
			"The NOVA App doesn't know this computer anymore. Maybe it was removed on the NOVA page. Connect it again: run '%s'.",
			localRelayRepairCommand(activeServerProfile(), cfg.RelayBaseURL),
		)
	}
	readiness := runReadiness()
	if readiness.HealthErr != nil {
		switch {
		case deviceMode && relayHealthIssueLooksLikeRelayAuth(readiness.HealthErr):
			printNotAccepted()
		case haHinted: // the hint above already says why
			printHumanErr("HA NOVA can't reach the NOVA App either.")
		default:
			printHumanErr("HA NOVA couldn't check the NOVA App. Details: %s", readiness.HealthErr)
			printHumanErr("Is the NOVA App running? In Home Assistant: Settings → Apps → NOVA Relay.")
		}
		status = 1
	} else {
		doctorInfo("The NOVA App answers at %s", healthBase)
		if notice := checkRelayVersion(paths, readiness.HealthBody); !notice.empty() {
			// check-update's words ("No Update button yet?", a container's step)
			if !usingCloud { // bounded, as the first-use check; Cloud keeps the floor words
				if full := relayUpdateNoticeWithTimeout(paths, firstUseRelayNoticeTimeout); full.belowFloor || full.kind == humanNoticeKindRelayOutdated {
					notice = full
				}
			}
			printHumanNotice(notice)
			// --quiet is a machine/diagnostic contract: warning-only, never
			// an interactive question that could block or trigger a restart.
			// A guided update that ends verified clears THIS failure — doctor
			// must not exit 1 over a problem the user just fixed.
			fixed := false
			if !*quiet && !usingCloud {
				fixed = maybeOfferGuidedRelayUpdate(paths, notice)
			}
			if !fixed {
				status, appBelowFloor = 1, true
			} else {
				// The relay just restarted: the readiness captured before the
				// update is stale (its WS may still be reconnecting, or fail
				// on the new version) — the checks below must judge the relay
				// that is running NOW.
				readiness = runReadiness()
			}
		} else if !*quiet && !usingCloud {
			// A newer App may be available even while the running Relay remains
			// compatible with min_relay_version. That is an optional update, not
			// a failed doctor check; decline/non-TTY keeps status unchanged.
			if notice := relayAvailableUpdateNotice(cfg, token, parseRelayHealthVersion(readiness.HealthBody)); !notice.empty() {
				printHumanNotice(notice)
				if maybeOfferGuidedRelayUpdate(paths, notice) {
					readiness = runReadiness()
				}
			}
		}
		if judgeWS {
			switch {
			case readiness.WSReady:
				doctorInfo("Connected to Home Assistant")
			case readiness.UpstreamAuthIssue:
				printHumanErr("The NOVA App can't talk to Home Assistant: Home Assistant turned down its login.")
				printHumanErr("Restart or update the NOVA App in Home Assistant. NOVA Relay in your own container? Give it a new Home Assistant token (HA_LLAT).")
				status = 1
			case readiness.RelayAuthIssue:
				if deviceMode {
					printNotAccepted()
				} else {
					printHumanErr(`The NOVA App turned down this computer's shared key. In the NOVA App's Configuration tab, check the "Relay Auth Token" field.`)
				}
				status = 1
			default:
				printHumanErr("The NOVA App isn't connected to Home Assistant yet.")
				printHumanErr("Wait a minute, then run 'ha-nova doctor' again. Still not working? Restart the NOVA App in Home Assistant.")
				status = 1
			}
		}
	}

	clientStatuses, err := configuredClientStatuses(paths, state)
	if err != nil {
		printHumanErr("%s", err)
		status = 1
	} else if len(clientStatuses) > 0 {
		if *autoRepair {
			needsRepair := false
			for _, c := range clientStatuses {
				if c.RuntimeDetected && !c.Attached && !c.Ready {
					needsRepair = true
					break
				}
			}
			if needsRepair {
				for _, outcome := range runClientAutoRepair(paths, clientStatuses) {
					switch {
					case outcome.Repaired:
						printHumanInfo("Fixed HA NOVA in %s", outcome.ClientLabel)
					case outcome.Err != nil:
						printHumanWarn("Couldn't fix HA NOVA in %s: %s", outcome.ClientLabel, outcome.Err)
					}
				}
				if refreshed, refreshErr := configuredClientStatuses(paths, state); refreshErr == nil {
					clientStatuses = refreshed
				}
			}
		}
		installSource := detectInstallSource(paths, state)
		for _, client := range clientStatuses {
			switch {
			case client.Ready:
				doctorInfo("%s is ready", client.Label)
				if client.ID != "claude" {
					break
				}
				if hint := claudeRulesHint(paths); hint != "" {
					doctorInfo("%s", hint)
				}
			case !client.RuntimeDetected:
				printHumanWarn("HA NOVA is set up for %s, but can't find %s on this computer right now", client.Label, client.Label)
				printHumanWarn("%s", doctorClientRepairHint(client, installSource))
				status = 1
			case !client.Attached:
				printHumanWarn("%s doesn't load HA NOVA right now", client.Label)
				printHumanWarn("%s", doctorClientRepairHint(client, installSource))
				status = 1
			}
		}
	}
	if notice := checkForUpdate(paths, false); !notice.empty() && notice.kind != humanNoticeKindUpToDate {
		printHumanNotice(notice)
	}
	installStatus, installStatusErr := buildInstallStatus(paths)
	if installStatusErr != nil {
		printHumanWarn("HA NOVA couldn't check its own files: %s", installStatusErr)
	} else {
		for _, client := range installStatus.Clients {
			if !client.ActiveDrift {
				continue
			}
			printHumanWarn("%s still uses HA NOVA files from an update that didn't finish", client.Label)
			printHumanWarn("%s", doctorClientRepairHint(clientStatus{ID: client.ID, Label: client.Label, RuntimeDetected: client.RuntimeDetected}, installStatus.InstallSource))
			status = 1
		}
		for _, artifact := range installStatus.InactiveArtifacts {
			doctorInfo("Ignoring an old, unused HA NOVA file: %s (%s)", artifact.Path, artifact.Kind)
		}
	}
	if appBelowFloor { // the App notice is plain words; this says it failed
		printHumanErr("To fix: update the NOVA App (see above).")
	}
	if status == 0 {
		doctorInfo("All checks passed")
	}
	return status
}

func doctorClientRepairHint(client clientStatus, installSource string) string {
	setupCommand := doctorClientSetupCommand(client.ID)
	switch {
	case !client.RuntimeDetected:
		return fmt.Sprintf(
			"To fix: install or open %s, then run `%s`.",
			client.Label,
			setupCommand,
		)
	case installSource == installSourceDev:
		return fmt.Sprintf(
			"To fix: run `npm run dev:sync` or `%s`.",
			setupCommand,
		)
	default:
		return fmt.Sprintf("To fix: run `%s`.", setupCommand)
	}
}

func doctorClientSetupCommand(clientID string) string {
	if activeServerProfile() != defaultServerProfileName {
		return fmt.Sprintf(
			"ha-nova setup --server %s %s",
			activeServerProfile(),
			clientID,
		)
	}
	return fmt.Sprintf("ha-nova setup %s", clientID)
}

func fetchRelayHealth(relayBaseURL, token string) ([]byte, error) {
	return fetchRelayHealthWith(httpClient, relayBaseURL, token)
}

func fetchRelayHealthWith(client *http.Client, relayBaseURL, token string) ([]byte, error) {
	return fetchRelayHealthWithContext(context.Background(), client, relayBaseURL, token)
}

func fetchRelayHealthWithContext(ctx context.Context, client *http.Client, relayBaseURL, token string) ([]byte, error) {
	url := strings.TrimRight(relayBaseURL, "/") + "/health"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized ||
		resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := readAllLimited(
		resp.Body,
		maxRelayDiagnosticResponseBytes,
	)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func probeHTTP(url string) error {
	return probeHTTPWithClient(httpClient, url)
}

func probeHTTPWithClient(client *http.Client, url string) error {
	req, err := http.NewRequest("GET", strings.TrimRight(url, "/"), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
