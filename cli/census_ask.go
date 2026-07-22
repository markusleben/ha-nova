package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"
)

// The one-time census ask (docs/reference/census.md). Three delivery paths
// share ONE state: the interactive TTY ask (setup complete, update tails,
// doctor tail) and the skill-mediated callout (check-update human output,
// hard-capped at three emissions). First responder wins; asked_at is stamped
// BEFORE the prompt so an aborted prompt never re-asks.

// Injectable TTY probes (pattern: ui_mode.go) so tests can simulate an
// interactive terminal.
var (
	censusStdinIsTTY  = isInteractiveTTY
	censusStdoutIsTTY = stdoutIsInteractiveTTY
)

// censusAskIntro is the approved ask copy. The closing question line
// ("Count this install? [y/N]:") is rendered by the prompt helper.
const censusAskIntro = `
  One-time question

  May HA NOVA count your install? HA NOVA has no telemetry — that stays.
  The flip side: we don't know how many people use it or on which OS,
  and that makes it hard to decide what to build and test first.
  A yes sends one anonymous ping, at most once a week:
      HA NOVA version  ·  relay version  ·  operating system
  No ID, no IP stored, nothing about your home — and the resulting
  numbers are public for everyone.

  Details: docs/reference/census.md   Change anytime: ha-nova census on|off
`

// maybeAskCensus is the TTY entry point for the update and doctor tails.
func maybeAskCensus(paths runtimePaths, via string) {
	askCensusIfEligible(paths, via, bufio.NewReader(os.Stdin), os.Stdout)
}

// askCensusIfEligible runs the one-time interactive ask when every gate
// passes. Non-TTY sessions skip WITHOUT stamping — the question stays open
// for a real terminal later.
func askCensusIfEligible(paths runtimePaths, via string, in *bufio.Reader, out io.Writer) {
	if BuildChannel == "dev" || censusOptedOutByEnv() {
		return
	}
	if !censusStdinIsTTY() || !censusStdoutIsTTY() {
		return
	}
	state := loadCensusState(paths)
	if state.AskedAt != "" {
		return
	}
	// Stamp BEFORE the prompt: Ctrl-C, EOF, or a crash mid-question must never
	// lead to a second ask. If the stamp cannot be written, do not ask at all.
	// Re-check asked-state INSIDE the locked mutation: two interactive entry
	// points racing past the unlocked pre-check must resolve to exactly one
	// prompt — only the process that actually wins the stamp asks.
	wonStamp := false
	if err := mutateCensusState(paths, func(s *censusState) {
		if s.AskedAt != "" {
			return
		}
		s.AskedAt = censusNow().UTC().Format(time.RFC3339)
		s.AskedVia = via
		s.Answer = "none"
		wonStamp = true
	}); err != nil {
		return
	}
	if !wonStamp {
		return
	}
	fmt.Fprint(out, censusAskIntro)
	yes, err := promptYesNoWithOptions(in, out, "Count this install?", false, false)
	if err != nil {
		// Aborted prompt: asked stays stamped, answer stays "none".
		return
	}
	if !yes {
		_ = mutateCensusState(paths, func(s *censusState) {
			s.Answer = "no"
			s.Enabled = false
		})
		fmt.Fprintln(out, "  Not counting this install. Change anytime: ha-nova census on")
		return
	}
	if err := mutateCensusState(paths, func(s *censusState) {
		s.Answer = "yes"
		s.Enabled = true
	}); err != nil {
		return
	}
	fmt.Fprintln(out, "  Thank you — this install now counts.")
	fmt.Fprintf(out, "  Public numbers: %s\n", censusStatsURL())
	censusFirstPingAfterYes(paths)
}

// censusFirstPingAfterYes performs the immediate opt-in ping: success stamps
// the week; a failed attempt leaves the week empty AND releases the claimed
// week marker, so the promised retry on a later update check can claim it
// again.
func censusFirstPingAfterYes(paths runtimePaths) {
	// A placeholder-endpoint build is inert: skip before the week gate and
	// marker so nothing is burned that a configured build could count.
	if !censusEndpointConfigured() {
		return
	}
	if localVersion(paths) == "dev" || censusOS() == "" {
		return
	}
	now := censusNow().UTC()
	currentWeek := censusISOWeek(now)
	state := loadCensusState(paths)
	// Shared week gate incl. the clock-rollback clamp — identical semantics
	// with the carrier and `census on`.
	if !censusWeekSendable(paths, state, currentWeek) {
		return
	}
	if !claimCensusWeekMarker(paths, currentWeek) {
		return
	}
	if sendErr := sendCensusPing(paths, censusWireBytes(buildCensusPayload(paths, state, now))); sendErr == nil {
		_ = mutateCensusState(paths, func(s *censusState) { s.LastPingWeek = currentWeek })
	} else {
		// Same retry contract as `census on`: a failed first ping frees the
		// week so a later update check can attempt it.
		releaseCensusWeekMarker(paths, currentWeek)
	}
}

// Skill-mediated ask: most installs never see a TTY tail, so check-update's
// human-readable output (which client hooks and skills read) may carry the
// pending ask — at most three times ever; the third emission closes the
// question for good (answer=none).
const censusSkillNoticeCap = 3

const censusSkillNoticeBlock = `CENSUS ASK PENDING: One-time question for the user — may HA NOVA count this install? HA NOVA has no telemetry — that stays; but without any numbers it is hard to decide what to build and test first. An explicit yes sends one anonymous ping, at most once a week: HA NOVA version, relay version, operating system. No ID, no IP stored, nothing about the home — and the resulting numbers are public for everyone. Details: docs/reference/census.md. If the user explicitly says yes, run: ha-nova census on. If the user explicitly says no, run: ha-nova census off. If the user does not answer, run nothing.`

// maybeEmitCensusSkillNotice prints the pending-ask block on the check-update
// human paths (never --json). The counter is persisted BEFORE printing so a
// crash cannot mint extra emissions.
func maybeEmitCensusSkillNotice(paths runtimePaths) {
	if BuildChannel == "dev" || censusOptedOutByEnv() {
		return
	}
	state := loadCensusState(paths)
	if state.AskedAt != "" || state.SkillNotices >= censusSkillNoticeCap {
		return
	}
	// Serialize the cap across concurrent processes (session-start hooks can
	// fan out several `check-update --quiet` at once): emission n is guarded
	// by the exclusive marker census-notice-<n> — only the claim winner may
	// increment and print. The mutation then accepts exactly the claimed slot,
	// so a stale reader that re-claims a pruned older slot still does nothing.
	claimed := state.SkillNotices + 1
	if !claimCensusNoticeMarker(paths, claimed) {
		return
	}
	emitted := false
	if err := mutateCensusState(paths, func(s *censusState) {
		if s.AskedAt != "" || s.SkillNotices != claimed-1 {
			return
		}
		s.SkillNotices = claimed
		emitted = true
		if claimed >= censusSkillNoticeCap {
			// Third and final emission: close the question permanently.
			s.AskedAt = censusNow().UTC().Format(time.RFC3339)
			s.AskedVia = "skill"
			s.Answer = "none"
		}
	}); err != nil || !emitted {
		return
	}
	fmt.Fprintln(os.Stdout, censusSkillNoticeBlock)
}

// claimCensusNoticeMarker claims the n-th skill-notice emission slot with the
// same O_EXCL pattern (and pruning) as the weekly ping marker.
func claimCensusNoticeMarker(paths runtimePaths, n int) bool {
	return claimCensusMarker(paths, "census-notice-", strconv.Itoa(n))
}
