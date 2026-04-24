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

func runDoctor(paths runtimePaths, _ []string) int {
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

	cfg, cfgErr := loadConfig(paths)
	token, tokenErr := readRelayAuthTokenForDoctor()
	state, stateErr := loadStateOrDefaultChecked(paths)
	if stateErr != nil {
		printHumanErr("%s", stateErr)
		return 1
	}
	status := 0

	if cfgErr == nil {
		printHumanInfo("Config present: %s", paths.ConfigFile)
	} else {
		printHumanErr("%s", cfgErr)
		return 1
	}

	if tokenErr == nil && token != "" {
		printHumanInfo("Relay auth token present in secure storage")
	} else {
		printHumanErr("%s", relayAuthTokenProblemMessage(tokenErr))
		if hint := setupSecureStorageRecoveryHint(tokenErr); hint != "" {
			printHumanWarn("%s", hint)
		}
		return 1
	}

	if err := probeHTTP(cfg.HAURL); err != nil {
		printHumanErr("Home Assistant unreachable: %s", err)
		status = 1
	} else {
		printHumanInfo("Home Assistant reachable: %s", cfg.HAURL)
	}
	haReachable := status == 0

	readiness := checkRelayReadiness(cfg.RelayBaseURL, token)
	if readiness.HealthErr != nil {
		printHumanErr("Relay health failed: %s", readiness.HealthErr)
		status = 1
	} else {
		printHumanInfo("Relay health reachable: %s/health", cfg.RelayBaseURL)
		if notice := checkRelayVersion(paths, readiness.HealthBody); !notice.empty() {
			printHumanNotice(notice)
			status = 1
		}
		if haReachable {
			switch {
			case readiness.WSReady:
				if readiness.UsedWSPing {
					printHumanInfo("Relay /ws ping succeeded")
				}
				printHumanInfo("Connected to Home Assistant")
			case readiness.LLATIssue:
				printHumanErr("Relay reports degraded upstream WS capability")
				printHumanErr(`The Home Assistant Access Token field ("ha_llat") in NOVA Relay is missing or invalid`)
				status = 1
			case readiness.RelayAuthIssue:
				printHumanErr("Relay reports degraded upstream WS capability")
				printHumanErr(`The Relay Auth Token field ("relay_auth_token") in NOVA Relay is missing or invalid`)
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
		installSource := detectInstallSource(paths, state)
		for _, client := range clientStatuses {
			switch {
			case client.Ready:
				printHumanInfo("%s ready now", client.Label)
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
	if status == 0 {
		printHumanInfo("Doctor checks passed")
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
	quiet := fs.Bool("quiet", false, "quiet")
	jsonOutput := fs.Bool("json", false, "json")
	if err := fs.Parse(args); err != nil {
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

	notice := humanNoticeFromUpdateCheckResult(result, *quiet)
	if notice.empty() {
		return 0
	}
	printHumanNotice(notice)
	return updateCheckExitCode(result)
}

func fetchRelayHealth(relayBaseURL, token string) ([]byte, error) {
	url := strings.TrimRight(relayBaseURL, "/") + "/health"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
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
