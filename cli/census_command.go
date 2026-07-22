package main

import (
	"fmt"
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
	case "on", "off", "status":
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
	default:
		printHumanErr("Unknown census subcommand: %s", args[0])
		printCensusUsage()
		return 1
	}
}

func printCensusUsage() {
	fmt.Fprintln(os.Stdout, "Usage: ha-nova census <on|off|status>")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "  on      Opt in: count this install (one anonymous ping, at most once a week, no ID).")
	fmt.Fprintln(os.Stdout, "  off     Opt out: nothing is ever sent.")
	fmt.Fprintln(os.Stdout, "  status  Show on/off, the exact bytes that would be sent, and the public stats URL.")
	fmt.Fprintln(os.Stdout, "")
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
	state := loadCensusState(paths)
	if !censusEndpointConfigured() {
		printHumanInfo("Census is on.")
		printHumanWarn("census endpoint not configured in this build — nothing is sent")
		return 0
	}
	printHumanInfo("Census is on — this install counts toward the public numbers at %s", censusStatsURL())
	if censusOptedOutByEnv() {
		printHumanWarn("%s is set — no ping is sent while it stays set.", censusOptOutEnv)
		return 0
	}
	// Same never-send gates as the weekly carrier: dev builds and platforms
	// outside the documented os buckets are not counted.
	if BuildChannel == "dev" || localVersion(paths) == "dev" {
		printHumanWarn("Local dev build — dev builds never ping. Released builds ping on their normal update checks.")
		return 0
	}
	if censusOS() == "" {
		printHumanWarn("This platform is outside the census os buckets (macos/linux/windows) — no ping is sent.")
		return 0
	}
	currentWeek := censusISOWeek(now)
	// Same shared week gate as the carrier — including the clock-rollback
	// clamp, so a future stamp never lets the manual path double-count.
	if !censusWeekSendable(paths, state, currentWeek) {
		printHumanInfo("This week (%s) is already counted — the next ping goes out next ISO week.", currentWeek)
		return 0
	}
	if !claimCensusWeekMarker(paths, currentWeek) {
		// Another process already attempted this week's single send.
		printHumanInfo("A ping for week %s was already attempted — the next one goes out next ISO week.", currentWeek)
		_ = mutateCensusState(paths, func(s *censusState) {
			if s.LastPingWeek < currentWeek {
				s.LastPingWeek = currentWeek
			}
		})
		return 0
	}
	// Immediate first ping: success stamps the week; a failed attempt leaves
	// the week stamp empty, and the install is counted on a later update check.
	payload := censusWireBytes(buildCensusPayload(paths, state, now))
	if err := sendCensusPing(paths, payload); err != nil {
		printHumanWarn("First ping did not go through (%s). This install will be counted on a later update check.", err)
		return 0
	}
	if err := mutateCensusState(paths, func(s *censusState) { s.LastPingWeek = currentWeek }); err != nil {
		printHumanWarn("ping sent, but the week stamp could not be saved: %s", err)
		return 0
	}
	printHumanInfo("First ping sent: %s", payload)
	return 0
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
	printHumanInfo("Census is off — nothing is sent. (There is nothing to delete server-side: pings carry no ID; only aggregate counters exist.)")
	printHumanInfo("Change anytime: ha-nova census on")
	return 0
}

func runCensusStatus(paths runtimePaths) int {
	state := loadCensusState(paths)
	now := censusNow().UTC()
	onOff := "off"
	if state.Enabled {
		onOff = "on"
	}
	printHumanInfo("Census: %s", onOff)
	if state.AskedAt != "" {
		printHumanInfo("Asked: %s (via %s, answer: %s)", state.AskedAt, state.AskedVia, state.Answer)
	} else {
		printHumanInfo("Asked: never")
	}
	printHumanInfo("Cadence: at most one ping per ISO week (UTC), sent during normal update checks")
	if state.LastPingWeek != "" {
		printHumanInfo("Last ping week: %s", state.LastPingWeek)
	} else {
		printHumanInfo("Last ping week: never")
	}
	currentWeek := censusISOWeek(now)
	switch {
	case !state.Enabled:
		printHumanInfo("Next possible ping: none (census is off)")
	case state.LastPingWeek == currentWeek:
		printHumanInfo("Next possible ping: next ISO week (current week %s is already counted)", currentWeek)
	default:
		printHumanInfo("Next possible ping: now (on the next update check, week %s)", currentWeek)
	}
	// The literal wire bytes — not a description of them.
	printHumanInfo("Exact wire payload that would be sent: %s", censusWireBytes(buildCensusPayload(paths, state, now)))
	if censusEndpointConfigured() {
		printHumanInfo("Endpoint: %s", censusPingURL())
		printHumanInfo("Public numbers: %s", censusStatsURL())
	} else {
		printHumanInfo("census endpoint not configured in this build — nothing is sent")
	}
	printHumanInfo("Turn off: ha-nova census off  (or set %s=1)", censusOptOutEnv)
	if censusOptedOutByEnv() {
		printHumanWarn("%s is set — asks and pings are suppressed.", censusOptOutEnv)
	}
	return 0
}
