package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
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
// ("Include this install? [y/N]:") is rendered by the prompt helper.
const censusAskIntro = `
  One-time question

  May HA NOVA include this install in its public census?
  HA NOVA sends no behavioral or feature-use analytics. The census exists because we otherwise
  cannot tell which operating systems and versions need attention first.
  A yes permits one anonymous ping attempt per week while local census state remains intact:
      HA NOVA version  ·  relay version  ·  operating system
  No ID, no IP in HA NOVA storage, nothing about your home. Public totals are
  directional accepted-ping counts, not verified unique installs.

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
	askStamp := ""
	if err := mutateCensusState(paths, func(s *censusState) {
		if s.AskedAt != "" {
			return
		}
		askStamp = censusNow().UTC().Format(time.RFC3339Nano)
		s.AskedAt = askStamp
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
	yes, err := promptYesNoWithOptions(in, out, "Include this install?", false, false)
	if err != nil {
		// Aborted prompt: asked stays stamped, answer stays "none".
		return
	}
	answerApplied := false
	if !yes {
		if err := mutateCensusState(paths, func(s *censusState) {
			if s.AskedAt != askStamp || s.AskedVia != via || s.Answer != "none" {
				return
			}
			s.Answer = "no"
			s.Enabled = false
			answerApplied = true
		}); err != nil || !answerApplied {
			return
		}
		fmt.Fprintln(out, "  This install will not send census pings. Change anytime: ha-nova census on")
		return
	}
	if err := mutateCensusState(paths, func(s *censusState) {
		if s.AskedAt != askStamp || s.AskedVia != via || s.Answer != "none" {
			return
		}
		s.Answer = "yes"
		s.Enabled = true
		answerApplied = true
	}); err != nil || !answerApplied {
		return
	}
	fmt.Fprintln(out, "  Thank you — this install can now contribute an anonymous census ping.")
	fmt.Fprintf(out, "  Public numbers: %s\n", censusStatsURL())
	censusFirstPingAfterYes(paths)
}

// censusFirstPingAfterYes performs the immediate opt-in ping through the same
// locked, stamp-before-send path as the carrier and manual command.
func censusFirstPingAfterYes(paths runtimePaths) {
	_ = sendCensusPingOnce(paths)
}

// Skill-mediated ask: most installs never see a TTY tail, so check-update's
// human-readable output (which client hooks and skills read) may carry the
// pending ask — at most three times ever; the third emission closes the
// question for good (answer=none).
const censusSkillNoticeCap = 3

const censusSkillNoticeBlock = `CENSUS ASK PENDING: One-time question for the user — may HA NOVA include this install in its public census? HA NOVA sends no behavioral or feature-use analytics; the census helps decide which operating systems and versions need attention first. An explicit yes permits one anonymous ping attempt per ISO week while local census state remains intact: HA NOVA version, relay version when recently observed, and operating system. No ID, no IP field, nothing about the home. Public totals are directional accepted-ping counts, not verified unique installs. Details: docs/reference/census.md. If the user explicitly says yes, run: ha-nova census on. If the user explicitly says no, run: ha-nova census off. If the user does not answer, run nothing.`

// maybeEmitCensusSkillNotice prints the pending-ask block on the check-update
// human paths (never --json). The counter is persisted BEFORE printing so a
// crash cannot mint extra emissions.
func maybeEmitCensusSkillNotice(paths runtimePaths) {
	maybeEmitCensusSkillNoticeTo(paths, os.Stdout)
}

func maybeEmitCensusSkillNoticeTo(paths runtimePaths, out io.Writer) bool {
	if BuildChannel == "dev" || censusOptedOutByEnv() {
		return true
	}
	state := loadCensusState(paths)
	if state.AskedAt != "" || state.SkillNotices >= censusSkillNoticeCap {
		return true
	}
	// Serialize the cap across concurrent processes (session-start hooks can
	// fan out several `check-update --quiet` at once). Each contender proposes
	// the next logical slot; the locked mutation accepts it only if the slot is
	// still current. The install-state sentinel prevents a queued writer from
	// recreating state after uninstall.
	claimed := state.SkillNotices + 1
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
		return false
	}
	fmt.Fprintln(out, censusSkillNoticeBlock)
	return true
}
