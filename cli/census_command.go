package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

// `ha-nova census on|off|status` — the user-facing switch for the opt-in
// census (docs/reference/census.md, PRIVACY.md).

func runCensusCommand(paths runtimePaths, args []string) int {
	if len(args) == 0 {
		printCensusUsage()
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		printCensusUsage()
		return 0
	case "on", "off", "status", "notice-presented":
		for _, arg := range args[1:] {
			if arg == "--help" || arg == "-h" {
				printCensusUsage()
				return 0
			}
			printHumanErr("census %s takes no arguments (got %q)", args[0], arg)
			return 1
		}
	case "choose":
		if len(args) != 3 {
			printHumanErr("census choose requires <choice-id> <yes|no>")
			return 1
		}
	}
	switch args[0] {
	case "on":
		return runCensusOn(paths)
	case "off":
		return runCensusOff(paths)
	case "status":
		return runCensusStatus(paths)
	case "notice-presented":
		return runCensusNoticePresented(paths)
	case "choose":
		return runCensusChoose(paths, args[1], args[2])
	default:
		printHumanErr("Unknown census subcommand: %s", args[0])
		printCensusUsage()
		return 1
	}
}

func printCensusUsage() {
	fmt.Fprintln(os.Stdout, "Usage: ha-nova census <on|off|status>")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "  on      Opt in: send one installation report now, then no sooner than seven days later.")
	fmt.Fprintln(os.Stdout, "  off     Opt out, stop new reports, and request deletion of this installation record.")
	fmt.Fprintln(os.Stdout, "  status  Show on/off, the exact application JSON bytes, and the private stats URL.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Cloudflare is the hosting provider for the census endpoint and processes source IP and connection metadata for HTTPS delivery under its privacy policy.")
	fmt.Fprintln(os.Stdout, "HA NOVA ingest code does not read or store the source IP. A dedicated random Census ID lets one participating installation count once.")
	fmt.Fprintln(os.Stdout, "No flags. Opt-out env var: HA_NOVA_NO_CENSUS=1. Details: docs/reference/census.md")
}

func runCensusOn(paths runtimePaths) int {
	installationID, err := ensureCensusInstallationID(paths)
	if err != nil {
		printHumanErr("cannot establish census installation id: %s", err)
		return 1
	}
	now := censusNow().UTC()
	if err := mutateCensusState(paths, func(s *censusState) {
		if !s.Enabled {
			s.LastAttemptAt = ""
		}
		s.Enabled = true
		s.Answer = "yes"
		s.ConsentVersion = censusConsentVersion
		s.InstallationID = installationID
		s.WithdrawalPending = false
		s.PendingChoiceID = ""
		if s.AskedAt == "" {
			s.AskedAt = now.Format(time.RFC3339)
			s.AskedVia = "command"
		}
	}); err != nil {
		printHumanErr("cannot save census state: %s", err)
		return 1
	}
	if censusEndpointConfigured() {
		printHumanInfo("Census is on — eligible reports contribute to the private maintainer statistics at %s", censusStatsURL())
		printHumanInfo("Counts are voluntary, self-reported participating installations, not verified people or the complete installed base.")
	} else {
		printHumanInfo("Census is on.")
	}
	// The shared coordinator re-checks consent and cadence under the same lock as
	// `census off`, stamps before the request, and never retries ambiguity.
	reportCensusPingResult(sendCensusPingOnce(paths), printHumanInfo, printHumanWarn)
	return 0
}

func reportCensusPingResult(result censusPingResult, info, warn func(string, ...any)) {
	switch {
	case result.Skipped == censusPingSkipEndpoint:
		warn("census endpoint not configured in this build — nothing is sent")
	case result.Skipped == censusPingSkipEnv:
		warn("%s is set — no report is sent while it stays set.", censusOptOutEnv)
	case result.Skipped == censusPingSkipDev:
		warn("Local dev build — dev builds never report. Released builds report on their normal update checks.")
	case result.Skipped == censusPingSkipOS:
		warn("This platform is outside the census OS buckets (macos/linux/windows) — no report is sent.")
	case result.Skipped == censusPingSkipCadence:
		info("A report was attempted less than seven days ago — nothing is sent yet.")
	case result.Skipped == censusPingSkipDisabled:
		info("Census was turned off before the first report — nothing was sent.")
	case !result.Attempted && result.Err != nil:
		warn("Cannot reserve the next census report: %s", result.Err)
	case !result.Attempted:
		info("No census report was eligible to send.")
	case result.Err != nil:
		warn("Report result was not confirmed (%s). It will wait seven days before another attempt.", result.Err)
	default:
		info("Installation report sent: %s", result.Payload)
	}
}

func runCensusOff(paths runtimePaths) int {
	result, err := disableAndWithdrawCensus(paths, true)
	if err != nil {
		printHumanErr("cannot save census state: %s", err)
		return 1
	}
	printHumanInfo("Census is off — no new installation reports will be sent.")
	switch {
	case result.Confirmed:
		printHumanInfo("The server-side installation record was deleted.")
	case result.Attempted && result.Err != nil:
		printHumanWarn("Server-side deletion was not confirmed: %s. Without new reports, the record expires automatically.", result.Err)
	default:
		printHumanInfo("No server-side deletion request was needed or possible; without new reports, any existing record expires automatically.")
	}
	printHumanInfo("Change anytime: ha-nova census on")
	return 0
}

func runCensusChoose(paths runtimePaths, choiceID, answer string) int {
	if !censusChoiceIDPattern.MatchString(choiceID) || (answer != "yes" && answer != "no") {
		printHumanErr("invalid census choice ID or answer")
		return 1
	}
	applied := false
	if err := mutateCensusState(paths, func(state *censusState) {
		if state.PendingChoiceID != choiceID ||
			state.AskedVia != "skill" ||
			state.Answer != "none" {
			return
		}
		state.PendingChoiceID = ""
		state.ConsentVersion = censusConsentVersion
		state.WithdrawalPending = false
		state.Enabled = answer == "yes"
		state.Answer = answer
		if state.Enabled {
			state.LastAttemptAt = ""
		}
		applied = true
	}); err != nil {
		printHumanErr("cannot save census choice: %s", err)
		return 1
	}
	if !applied {
		printHumanErr("this census choice is stale; current consent was not changed")
		return 1
	}
	if answer == "no" {
		printHumanInfo("Your No choice was saved. This installation will not send census reports.")
		printHumanInfo("Change anytime: ha-nova census on")
		return 0
	}
	printHumanInfo("Your Yes choice was saved.")
	printHumanInfo("Private maintainer statistics: %s", censusStatsURL())
	reportCensusPingResult(sendCensusPingOnce(paths), printHumanInfo, printHumanWarn)
	return 0
}

func runCensusStatus(paths runtimePaths) int {
	lines, err := censusStatusLines(paths)
	if err != nil {
		printHumanErr("cannot render census status: %s", err)
		return 1
	}
	for _, line := range lines {
		printHumanInfo("%s", line)
	}
	return 0
}

func censusStatusLines(paths runtimePaths) ([]string, error) {
	if _, err := ensureCensusInstallationID(paths); err != nil {
		return nil, err
	}
	state := loadCensusState(paths)
	now := censusNow().UTC()
	onOff := "off"
	if state.Enabled {
		onOff = "on"
	}
	lines := []string{fmt.Sprintf("Census: %s", onOff)}
	if state.AskedAt != "" {
		lines = append(lines, fmt.Sprintf("Asked: %s (via %s, answer: %s)", state.AskedAt, state.AskedVia, state.Answer))
	} else {
		lines = append(lines, "Asked: never")
	}
	lines = append(lines, "Cadence: reports are attempted no sooner than seven days apart")
	if state.LastAttemptAt != "" {
		lines = append(lines, fmt.Sprintf("Last attempted: %s", state.LastAttemptAt))
	} else {
		lines = append(lines, "Last attempted: never")
	}
	next, hasNext := censusNextAttemptAt(state)
	switch {
	case !state.Enabled:
		lines = append(lines, "Next possible report: none (census is off)")
	case hasNext && next.IsZero():
		lines = append(lines, "Next possible report: unavailable (stored attempt timestamp is invalid; sending stays disabled)")
	case hasNext && now.Before(next):
		lines = append(lines, fmt.Sprintf("Next possible report: %s", next.Format(time.RFC3339)))
	default:
		lines = append(lines, "Next possible report: now (on the next update check)")
	}
	// The literal application-body bytes — not a description of them.
	body := censusApplicationJSONBytes(buildCensusPayload(paths, state, now))
	if len(body) == 0 {
		return nil, fmt.Errorf("empty application JSON body")
	}
	lines = append(lines,
		fmt.Sprintf("Exact application JSON body: %s", body),
		"HTTPS hosting: Cloudflare hosts the census endpoint and processes the source IP and connection metadata under its privacy policy.",
		"HA NOVA ingest code does not read the source IP; application storage does not store it.",
		"The random Census installation ID is used only to count this participating installation once; it is not derived from or reused from hardware/device identifiers, pairing, a user, a Relay, or Home Assistant. HA NOVA attaches no device data.",
	)
	if censusEndpointConfigured() {
		lines = append(lines,
			fmt.Sprintf("Endpoint: %s", censusPingURL()),
			fmt.Sprintf("Private maintainer statistics: %s", censusStatsURL()),
			"Counts are voluntary, self-reported participating installations, not verified people or the complete installed base.",
		)
	} else {
		lines = append(lines, "census endpoint not configured in this build — nothing is sent")
	}
	lines = append(lines, fmt.Sprintf("Turn off: ha-nova census off  (or set %s=1)", censusOptOutEnv))
	if censusOptedOutByEnv() {
		lines = append(lines, fmt.Sprintf("%s is set — asks, reports, and withdrawals are suppressed.", censusOptOutEnv))
	}
	if state.WithdrawalPending {
		lines = append(lines, "Server-side deletion is not confirmed; run `ha-nova census off` again to retry.")
	}
	return lines, nil
}

func writeCensusStatus(paths runtimePaths, out io.Writer) error {
	lines, err := censusStatusLines(paths)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Fprintf(out, "  %s\n", line)
	}
	return nil
}

func runCensusNoticePresented(paths runtimePaths) int {
	if BuildChannel == "dev" || censusOptedOutByEnv() || censusLifecycleStopped(paths) {
		fmt.Fprintln(os.Stdout, "CENSUS NOTICE SKIP")
		return 0
	}
	installationID, err := newCensusInstallationID()
	if err != nil {
		printHumanErr("cannot establish census installation id: %s", err)
		return 1
	}
	choiceID, err := newCensusChoiceID()
	if err != nil {
		printHumanErr("cannot establish census choice ID: %s", err)
		return 1
	}
	presented := false
	if err := mutateCensusState(paths, func(state *censusState) {
		if state.AskedAt != "" {
			return
		}
		if state.InstallationID == "" {
			state.InstallationID = installationID
		}
		state.SkillNotices = 0
		state.SkillPresentations = 1
		state.AskedAt = censusNow().UTC().Format(time.RFC3339Nano)
		state.AskedVia = "skill"
		state.Answer = "none"
		state.PendingChoiceID = choiceID
		presented = true
	}); err != nil {
		printHumanErr("cannot record census notice presentation: %s", err)
		return 1
	}
	if !presented {
		fmt.Fprintln(os.Stdout, "CENSUS NOTICE SKIP")
		return 0
	}
	fmt.Fprintf(os.Stdout, "CENSUS NOTICE PRESENT %s\n", choiceID)
	return 0
}
