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
	default:
		printHumanErr("Unknown census subcommand: %s", args[0])
		printCensusUsage()
		return 1
	}
}

func printCensusUsage() {
	fmt.Fprintln(os.Stdout, "Usage: ha-nova census <on|off|status>")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "  on      Opt in: allow one identifier-free JSON-body attempt per ISO week while local state remains intact.")
	fmt.Fprintln(os.Stdout, "  off     Opt out: after success, no new ping can start.")
	fmt.Fprintln(os.Stdout, "  status  Show on/off, the exact application JSON bytes, and the public stats URL.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Cloudflare is the hosting provider for the census endpoint and processes source IP and connection metadata for HTTPS delivery under its privacy policy.")
	fmt.Fprintln(os.Stdout, "HA NOVA Worker code does not read the source IP; application storage and public statistics do not store it.")
	fmt.Fprintln(os.Stdout, "No flags. Opt-out env var: HA_NOVA_NO_CENSUS=1. Details: docs/reference/census.md")
}

func runCensusOn(paths runtimePaths) int {
	now := censusNow().UTC()
	if err := mutateCensusState(paths, func(s *censusState) {
		s.Enabled = true
		s.Answer = "yes"
		if s.AskedAt == "" {
			s.AskedAt = now.Format(time.RFC3339)
			s.AskedVia = "command"
		}
	}); err != nil {
		printHumanErr("cannot save census state: %s", err)
		return 1
	}
	if censusEndpointConfigured() {
		printHumanInfo("Census is on — eligible pings contribute to the public aggregates at %s", censusStatsURL())
		printHumanInfo("Public totals are directional accepted-ping counts, not verified unique installs.")
	} else {
		printHumanInfo("Census is on.")
	}
	// The shared coordinator re-checks consent and week under the same lock as
	// `census off`, stamps before the request, and never retries ambiguity.
	reportCensusPingResult(sendCensusPingOnce(paths), printHumanInfo, printHumanWarn)
	return 0
}

func reportCensusPingResult(result censusPingResult, info, warn func(string, ...any)) {
	switch {
	case result.Skipped == censusPingSkipEndpoint:
		warn("census endpoint not configured in this build — nothing is sent")
	case result.Skipped == censusPingSkipEnv:
		warn("%s is set — no ping is sent while it stays set.", censusOptOutEnv)
	case result.Skipped == censusPingSkipDev:
		warn("Local dev build — dev builds never ping. Released builds ping on their normal update checks.")
	case result.Skipped == censusPingSkipOS:
		warn("This platform is outside the census os buckets (macos/linux/windows) — no ping is sent.")
	case result.Skipped == censusPingSkipWeek:
		info("This week already has a recorded ping attempt — the next can run next week.")
	case result.Skipped == censusPingSkipDisabled:
		info("Census was turned off before the first ping — nothing was sent.")
	case !result.Attempted && result.Err != nil:
		warn("Cannot reserve this week's census ping: %s", result.Err)
	case !result.Attempted:
		info("No census ping was eligible to send.")
	case result.Err != nil:
		warn("Ping result was not confirmed (%s). It will not retry this week, avoiding a possible duplicate.", result.Err)
	default:
		info("First ping sent: %s", result.Payload)
	}
}

func runCensusOff(paths runtimePaths) int {
	if err := mutateCensusState(paths, func(s *censusState) {
		s.Enabled = false
		s.Answer = "no"
		if s.AskedAt == "" {
			s.AskedAt = censusNow().UTC().Format(time.RFC3339)
			s.AskedVia = "command"
		}
	}); err != nil {
		printHumanErr("cannot save census state: %s", err)
		return 1
	}
	printHumanInfo("Census is off — nothing is sent. (There is nothing to delete server-side: application payloads carry no installation, device, or user ID; only aggregate counters exist.)")
	printHumanInfo("Change anytime: ha-nova census on")
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
	lines = append(lines, "Cadence: one recorded attempt per ISO week (UTC) while local census state remains intact")
	if state.LastPingWeek != "" {
		lines = append(lines, fmt.Sprintf("Last attempted week: %s", state.LastPingWeek))
	} else {
		lines = append(lines, "Last attempted week: never")
	}
	currentWeek := censusISOWeek(now)
	switch {
	case !state.Enabled:
		lines = append(lines, "Next possible ping: none (census is off)")
	case state.LastPingWeek == currentWeek:
		lines = append(lines, fmt.Sprintf("Next possible ping: next ISO week (current week %s already has a recorded attempt)", currentWeek))
	case state.LastPingWeek > currentWeek:
		lines = append(lines, fmt.Sprintf("Next possible ping: after recorded week %s (local clock is currently in %s)", state.LastPingWeek, currentWeek))
	default:
		lines = append(lines, fmt.Sprintf("Next possible ping: now (on the next update check, week %s)", currentWeek))
	}
	// The literal application-body bytes — not a description of them.
	body := censusApplicationJSONBytes(buildCensusPayload(paths, state, now))
	if len(body) == 0 {
		return nil, fmt.Errorf("empty application JSON body")
	}
	lines = append(lines,
		fmt.Sprintf("Exact application JSON body: %s", body),
		"HTTPS hosting: Cloudflare hosts the census endpoint and processes the source IP and connection metadata under its privacy policy.",
		"HA NOVA Worker code does not read the source IP; application storage and public statistics do not store it.",
	)
	if censusEndpointConfigured() {
		lines = append(lines,
			fmt.Sprintf("Endpoint: %s", censusPingURL()),
			fmt.Sprintf("Public numbers: %s", censusStatsURL()),
			"Public totals are directional accepted-ping counts, not verified unique installs.",
		)
	} else {
		lines = append(lines, "census endpoint not configured in this build — nothing is sent")
	}
	lines = append(lines, fmt.Sprintf("Turn off: ha-nova census off  (or set %s=1)", censusOptOutEnv))
	if censusOptedOutByEnv() {
		lines = append(lines, fmt.Sprintf("%s is set — asks and pings are suppressed.", censusOptOutEnv))
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
	presented := 0
	if err := mutateCensusState(paths, func(state *censusState) {
		if state.AskedAt != "" || state.SkillNotices >= censusSkillNoticeCap {
			return
		}
		state.SkillNotices++
		presented = state.SkillNotices
		if presented == censusSkillNoticeCap {
			state.AskedAt = censusNow().UTC().Format(time.RFC3339)
			state.AskedVia = "skill"
			state.Answer = "none"
		}
	}); err != nil {
		printHumanErr("cannot record census notice presentation: %s", err)
		return 1
	}
	if presented == 0 {
		fmt.Fprintln(os.Stdout, "CENSUS NOTICE SKIP")
		return 0
	}
	fmt.Fprintf(os.Stdout, "CENSUS NOTICE PRESENT %d/%d\n", presented, censusSkillNoticeCap)
	return 0
}
