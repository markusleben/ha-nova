package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

var readRelayAuthTokenForDoctor = readRelayAuthToken
var probePairingV1ForDoctor = probePairingV1

func runDoctor(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	autoRepair := fs.Bool("auto-repair", false, "silently reattach drifted clients before reporting")
	quiet := fs.Bool("quiet", false, "suppress info lines; only print warnings, errors, and repair actions")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova doctor [--auto-repair] [--quiet]") {
			return 0
		}
		printHumanErr("%s", err)
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
			printHumanWarn("Wait for the background uninstall to finish, then rerun `ha-nova doctor`.")
			return 1
		case windowsUninstallStatusKindInterrupted, windowsUninstallStatusKindFailed, windowsUninstallStatusKindCorrupt:
			printHumanErr("%s", recovery.Summary)
			printHumanWarn("Recovery: run `%s`.", recovery.RecoveryCommand)
			return 1
		}
	}

	// Self-heal client integrations once after a version change (complements
	// --auto-repair below, which only re-attaches drifted clients). Best-effort.
	ensureClientsVerifiedForCurrentVersion(paths)

	cfg, cfgErr := loadConfig(paths)
	state, stateErr := loadStateOrDefaultChecked(paths)
	if stateErr != nil {
		printHumanErr("%s", stateErr)
		return 1
	}
	status := 0

	if cfgErr == nil {
		doctorInfo("Config present: %s", paths.ConfigFile)
	} else {
		printHumanErr("%s", cfgErr)
		return 1
	}
	// Multi-server installs: name the checked profile so per-server doctor runs
	// (HA_NOVA_SERVER=<name> ha-nova doctor) are unambiguous.
	if profileName, profileCount := selectedServerProfileStatus(paths); profileCount > 1 || profileName != defaultServerProfileName {
		doctorInfo("Server profile: %s", profileName)
	}

	// Finish a pairing interrupted between activation and promotion (crash or
	// lost response); best-effort — failures leave the pending slot alone.
	if resumed, resumeErr := resumePendingActivation(&cfg, func(c *runtimeConfig) error { return saveConfig(paths, *c) }); resumeErr == nil && resumed {
		doctorInfo("Resumed the interrupted pairing — this device is connected.")
	}

	// Paired devices authenticate with their own credential over pinned TLS;
	// legacy installs keep the shared relay token. Doctor checks whichever
	// transport this install actually uses.
	transportBase, transportClient, transportCred, deviceMode, transportErr := relayFunctionalTransportForDoctor(cfg)
	pairedConfig := cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != ""
	var token string
	if deviceMode {
		token = transportCred
		doctorInfo("Device credential present (paired securely)")
	} else if pairedConfig {
		// A paired config whose device transport failed must NOT be masked by a
		// leftover legacy token: report the device problem so the user re-pairs.
		// The direct slot read distinguishes unreadable storage from an absent
		// credential (re-pairing cannot store anything in broken storage).
		if _, _, credErr := readDeviceCredential(); credErr != nil {
			printHumanErr("This device is paired, but its device credential could not be read from secure storage: %s", credErr)
			if hint := setupSecureStorageRecoveryHint(credErr); hint != "" {
				printHumanWarn("%s", hint)
			} else {
				printHumanWarn("Unlock or repair secure storage on this machine, then run 'ha-nova doctor' again.")
			}
			return 1
		}
		printHumanErr("This device was paired, but its device credential is missing from secure storage.")
		if profile := activeServerProfile(); profile != defaultServerProfileName {
			// Setup refuses named profiles — their repair path is pair --server.
			printHumanErr("Pair again: run 'ha-nova pair --server %s --relay-url %s' and enter a fresh code from the NOVA page.", profile, cfg.RelayBaseURL)
		} else {
			printHumanErr("Pair again: run 'ha-nova setup' and enter a fresh code from the NOVA page.")
		}
		return 1
	} else if activeServerProfile() != defaultServerProfileName {
		// Non-default profiles are device-credential-only: never check them with
		// the machine-wide legacy token (it belongs to the default profile).
		if transportErr == nil {
			transportErr = fmt.Errorf("server profile %q has no completed device pairing; run: ha-nova pair --server %s --relay-url %s", activeServerProfile(), activeServerProfile(), cfg.RelayBaseURL)
		}
		printHumanErr("%s", transportErr)
		return 1
	} else {
		legacyToken, tokenErr := readRelayAuthTokenForDoctor()
		token = legacyToken
		if tokenErr == nil && token != "" {
			doctorInfo("Relay auth token present in %s", relayAuthTokenStorageLabel())
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

	haReachable := false
	haURLKnown := strings.TrimSpace(cfg.HAURL) != ""
	if !haURLKnown {
		// A pair-only setup (`ha-nova pair --relay-url ...`) has no saved HA
		// address yet. The relay's WS state still proves the connection; the
		// direct HA probe is just skipped instead of failing on an empty URL.
		printHumanWarn("No Home Assistant address saved yet; skipping the direct HA check. Run 'ha-nova setup' to complete this device's setup.")
	} else if err := probeHTTP(cfg.HAURL); err != nil {
		printHumanErr("Home Assistant unreachable: %s", err)
		status = 1
	} else {
		doctorInfo("Home Assistant reachable: %s", cfg.HAURL)
		haReachable = true
	}
	// WS state is judged when HA answered directly, or when no address is
	// saved (then the relay's own upstream state is the only — and sufficient —
	// signal). Only a KNOWN-down HA suppresses it: blaming tokens while HA
	// itself is offline would mislead.
	judgeWS := haReachable || !haURLKnown

	healthBase := cfg.RelayBaseURL
	runReadiness := func() relayReadiness {
		if deviceMode {
			return checkRelayReadinessOverTransport(transportBase, transportClient, token)
		}
		return checkRelayReadiness(cfg.RelayBaseURL, token)
	}
	if deviceMode {
		healthBase = transportBase
	}
	readiness := runReadiness()
	if readiness.HealthErr != nil {
		printHumanErr("Relay health failed: %s", readiness.HealthErr)
		if deviceMode && relayHealthIssueLooksLikeRelayAuth(readiness.HealthErr) {
			printHumanErr("This device's pairing was not accepted (revoked or unknown). Pair again: run 'ha-nova setup'.")
		}
		status = 1
	} else {
		doctorInfo("Relay health reachable: %s/health", healthBase)
		if notice := checkRelayVersion(paths, readiness.HealthBody); !notice.empty() {
			printHumanNotice(notice)
			// --quiet is a machine/diagnostic contract: warning-only, never
			// an interactive question that could block or trigger a restart.
			// A guided update that ends verified clears THIS failure — doctor
			// must not exit 1 over a problem the user just fixed.
			fixed := false
			if !*quiet {
				fixed = maybeOfferGuidedRelayUpdate(paths, notice)
			}
			if !fixed {
				status = 1
			} else {
				// The relay just restarted: the readiness captured before the
				// update is stale (its WS may still be reconnecting, or fail
				// on the new version) — the checks below must judge the relay
				// that is running NOW.
				readiness = runReadiness()
			}
		} else if !*quiet {
			// A newer App may be available even while the running Relay remains
			// compatible with min_relay_version. That is an optional update, not
			// a failed doctor check; decline/non-TTY keeps status unchanged.
			if notice := relayAvailableUpdateNotice(cfg, token); !notice.empty() {
				printHumanNotice(notice)
				if maybeOfferGuidedRelayUpdate(paths, notice) {
					readiness = runReadiness()
				}
			}
		}
		if judgeWS {
			switch {
			case readiness.WSReady:
				if readiness.UsedWSPing {
					doctorInfo("Relay /ws ping succeeded")
				}
				doctorInfo("Connected to Home Assistant")
				// Working legacy install against a pairing-capable relay: point
				// at the passwordless upgrade once, as information — never a
				// failure, and skipped in --quiet's machine contract.
				if !deviceMode && !*quiet && probePairingV1ForDoctor(cfg.RelayBaseURL) {
					printHumanInfo("This relay supports passwordless device pairing. Run 'ha-nova setup' to switch this device to its own secure credential.")
				}
			case readiness.UpstreamAuthIssue:
				printHumanErr("Relay reports degraded upstream WS capability")
				printHumanErr("Relay upstream authentication was rejected; update/restart the App, or replace HA_LLAT for standalone Container/Core")
				status = 1
			case readiness.RelayAuthIssue:
				printHumanErr("Relay reports degraded upstream WS capability")
				if deviceMode {
					printHumanErr("This device's pairing was not accepted (revoked or unknown). Pair again: run 'ha-nova setup'.")
				} else {
					printHumanErr(`The Relay Auth Token field ("relay_auth_token") in NOVA Relay is missing or invalid`)
				}
				status = 1
			default:
				printHumanErr("Relay reports degraded upstream WS capability")
				printHumanErr("Home Assistant WebSocket is not connected yet")
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
						printHumanInfo("Auto-repaired %s attachment", outcome.ClientLabel)
					case outcome.Err != nil:
						printHumanWarn("Auto-repair %s failed: %s", outcome.ClientLabel, outcome.Err)
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
				doctorInfo("%s ready now", client.Label)
			case !client.RuntimeDetected:
				printHumanWarn("%s configured, but client runtime not detected now", client.Label)
				printHumanWarn("%s", doctorClientRepairHint(client, installSource))
				status = 1
			case !client.Attached:
				if client.ID == "claude" {
					printHumanWarn("%s is not attached correctly", client.Label)
				} else {
					printHumanWarn("%s configured, but HA NOVA is not attached", client.Label)
				}
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
		printHumanWarn("Install integrity status unavailable: %s", installStatusErr)
	} else {
		for _, client := range installStatus.Clients {
			if !client.ActiveDrift {
				continue
			}
			printHumanWarn("%s has active install drift: skill files still reference a temporary update backup", client.Label)
			printHumanWarn("%s", doctorClientRepairHint(clientStatus{ID: client.ID, Label: client.Label, RuntimeDetected: client.RuntimeDetected}, installStatus.InstallSource))
			status = 1
		}
		for _, artifact := range installStatus.InactiveArtifacts {
			doctorInfo("Inactive legacy/dev artifact ignored: %s (%s)", artifact.Path, artifact.Kind)
		}
	}
	if status == 0 {
		doctorInfo("Doctor checks passed")
	}
	return status
}

func doctorClientRepairHint(client clientStatus, installSource string) string {
	switch {
	case !client.RuntimeDetected:
		return fmt.Sprintf("Repair: install or reopen %s, then run `ha-nova setup %s`.", client.Label, client.ID)
	case installSource == installSourceDev:
		return fmt.Sprintf("Repair: run `npm run dev:sync` or `ha-nova setup %s`.", client.ID)
	default:
		return fmt.Sprintf("Repair: run `ha-nova setup %s`.", client.ID)
	}
}

func runCheckUpdate(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("check-update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	quiet := fs.Bool("quiet", false, "print only when an update is available; stay silent when current")
	jsonOutput := fs.Bool("json", false, "machine-readable result on stdout")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova check-update [--quiet] [--json]") {
			return 0
		}
		printHumanErr("%s", err)
		return 1
	}

	result := buildUpdateCheckResult(paths)
	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			printHumanErr("%s", err)
			return 1
		}
		return updateCheckExitCode(result)
	}

	// Human/quiet path only (the `--json` branch above stays machine-clean): every
	// client runs `check-update` on first skill use per session, so this is the
	// universal point to self-heal client integrations once after a version change.
	// Best-effort; never blocks the update check.
	ensureClientsVerifiedForCurrentVersion(paths)

	notice := humanNoticeFromUpdateCheckResult(result, *quiet)
	if !notice.empty() {
		printHumanNotice(notice)
	}
	// CLI/skills freshness is only half the answer: the relay in Home
	// Assistant has its own version, and "up to date" would be misleading
	// below min_relay_version or while an App update is pending. Human path
	// only: --quiet is the skill self-update channel whose contract is "UPDATE AVAILABLE or
	// silence" — skill sessions already get the relay warning through the
	// proxy version header. Stderr-only, exit code unchanged, --json above
	// stays machine-clean.
	if !*quiet {
		if relayNotice := relayUpdateNotice(paths); !relayNotice.empty() {
			printHumanNotice(relayNotice)
		}
	}
	if notice.empty() {
		return 0
	}
	return updateCheckExitCode(result)
}

func fetchRelayHealth(relayBaseURL, token string) ([]byte, error) {
	return fetchRelayHealthWith(httpClient, relayBaseURL, token)
}

func fetchRelayHealthWith(client *http.Client, relayBaseURL, token string) ([]byte, error) {
	url := strings.TrimRight(relayBaseURL, "/") + "/health"
	req, err := http.NewRequest("GET", url, nil)
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
	body, err := io.ReadAll(resp.Body)
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
