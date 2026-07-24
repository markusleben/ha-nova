package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// The one-time census ask (docs/reference/census.md). The interactive TTY ask
// (setup complete, update tails, doctor tail) and skill-mediated callout
// (check-update human output) share one state. First responder wins; asked_at
// is stamped before rendering so an aborted prompt never re-asks.

// Injectable TTY probes (pattern: ui_mode.go) so tests can simulate an
// interactive terminal.
var (
	censusStdinIsTTY  = isInteractiveTTY
	censusStdoutIsTTY = stdoutIsInteractiveTTY
)

// censusAskIntro is the approved ask copy. It distinguishes the application
// JSON body from the HTTPS metadata Cloudflare necessarily processes. The
// strict three-action chooser below renders the closing options.
const censusAskIntro = `
  One-time privacy choice

  May this HA NOVA installation contribute to the maintainer's private version
  statistics?
  HA NOVA sends no behavioral or feature-use analytics.
  If you agree, HA NOVA sends the first report now. Further reports are sent
  no sooner than seven days later.
  The message content (JSON) contains only:
      payload schema  ·  random Census installation ID
      HA NOVA version  ·  operating system
      recently observed relay version (when available)
  The random ID only lets the same participating installation count once. It
  is not derived from or reused from a hardware or device identifier, pairing,
  a user, a Relay, or Home Assistant. HA NOVA attaches no device data.
  No usage or Home Assistant data is sent.
  Cloudflare is the hosting provider for the census endpoint. It processes the
  source IP and connection metadata for HTTPS under its privacy policy.
  HA NOVA ingest code does not read or store the source IP.
  Counts are voluntary and self-reported, not verified people or the complete
  installed base.

  Inspect exact JSON: ha-nova census status   Change: ha-nova census on|off
  Details: docs/reference/census.md
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
	installationID, idErr := newCensusInstallationID()
	if idErr != nil {
		return
	}
	if err := mutateCensusState(paths, func(s *censusState) {
		if s.AskedAt != "" {
			return
		}
		if s.InstallationID == "" {
			s.InstallationID = installationID
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
	for {
		choice, err := promptCensusChoice(in, out)
		if err != nil {
			// Aborted prompt: asked stays stamped, answer stays "none".
			return
		}
		switch choice {
		case censusChoiceDetails:
			fmt.Fprintln(out)
			if err := writeCensusStatus(paths, out); err != nil {
				fmt.Fprintf(out, "  Exact data could not be shown: %s. Your consent is unchanged.\n", err)
			}
			fmt.Fprintln(out)
			continue
		case censusChoiceYes:
			applied, applyErr := applyCensusTTYAnswer(paths, askStamp, via, true)
			if applyErr != nil {
				fmt.Fprintf(out, "  Your choice was not saved: %s\n", applyErr)
			} else if !applied {
				fmt.Fprintln(out, "  Your choice was not saved because the census state changed.")
			} else {
				fmt.Fprintln(out, "  Your Yes choice was saved.")
				fmt.Fprintf(out, "  Private maintainer statistics: %s\n", censusStatsURL())
				reportCensusPingResult(
					censusFirstPingAfterYes(paths),
					func(format string, args ...any) { fmt.Fprintf(out, "  "+format+"\n", args...) },
					func(format string, args ...any) { fmt.Fprintf(out, "  Warning: "+format+"\n", args...) },
				)
			}
			return
		case censusChoiceNo:
			applied, applyErr := applyCensusTTYAnswer(paths, askStamp, via, false)
			if applyErr != nil {
				fmt.Fprintf(out, "  Your choice was not saved: %s\n", applyErr)
			} else if !applied {
				fmt.Fprintln(out, "  Your choice was not saved because the census state changed.")
			} else {
				fmt.Fprintln(out, "  Your No choice was saved. This installation will not send census reports.")
				fmt.Fprintln(out, "  Change anytime: ha-nova census on")
			}
			return
		default:
			fmt.Fprintln(out, "  No choice saved. Enter 1, 2, or 3.")
		}
	}
}

type censusChoice int

const (
	censusChoiceNone censusChoice = iota
	censusChoiceYes
	censusChoiceNo
	censusChoiceDetails
)

func promptCensusChoice(in *bufio.Reader, out io.Writer) (censusChoice, error) {
	fmt.Fprintln(out, "  Choose one:")
	fmt.Fprintln(out, "    1. Yes — contribute")
	fmt.Fprintln(out, "    2. No — do not contribute")
	fmt.Fprintln(out, "    3. Show exact data")
	answer, err := promptLineWithOptions(in, out, "Select 1, 2, or 3", "", false)
	if err != nil {
		return censusChoiceNone, err
	}
	switch strings.TrimSpace(answer) {
	case "1":
		return censusChoiceYes, nil
	case "2":
		return censusChoiceNo, nil
	case "3":
		return censusChoiceDetails, nil
	default:
		return censusChoiceNone, nil
	}
}

func applyCensusTTYAnswer(paths runtimePaths, askStamp, via string, enabled bool) (bool, error) {
	answerApplied := false
	if err := mutateCensusState(paths, func(s *censusState) {
		if s.AskedAt != askStamp || s.AskedVia != via || s.Answer != "none" {
			return
		}
		s.Enabled = enabled
		s.ConsentVersion = censusConsentVersion
		s.WithdrawalPending = false
		s.PendingChoiceID = ""
		if enabled {
			s.Answer = "yes"
			s.LastAttemptAt = ""
		} else {
			s.Answer = "no"
		}
		answerApplied = true
	}); err != nil {
		return false, err
	}
	return answerApplied, nil
}

// censusFirstPingAfterYes performs the immediate opt-in report through the same
// locked, stamp-before-send path as the carrier and manual command. Its caller
// decides whether to display or ignore the result.
func censusFirstPingAfterYes(paths runtimePaths) censusPingResult {
	return sendCensusPingOnce(paths)
}

// Skill-mediated ask: most installs never see a TTY tail, so check-update's
// human-readable output (which client hooks and skills read) may carry the
// pending ask. The hidden presentation command stamps the one-time question
// and returns a random choice ID. Yes/No must present that same ID to a
// compare-and-set command, so a stale UI cannot overwrite a newer manual or
// concurrent consent change.
const censusSkillNoticeBlock = `CENSUS ASK PENDING: Present one standalone localized privacy choice after the requested task, but defer it while any other choice or confirmation is active. A deferred notice remains pending. Immediately before a conflict-free presentation, run ha-nova census notice-presented. Render the choice only if its output is CENSUS NOTICE PRESENT followed by a cns-choice ID; if it says CENSUS NOTICE SKIP or the command fails, render nothing. Keep that exact choice ID bound to all three actions in this interaction. Make the census choice the response's only active choice and end the response after its options. Use a native selectable menu when available and the identical numbered fallback otherwise; if the UI requires a default, use No. Ask whether this HA NOVA installation may contribute to the maintainer's private version statistics. If the user agrees, HA NOVA sends the first report now; further reports are sent no sooner than seven days later. The fixed JSON body contains only the payload schema, a dedicated random Census installation ID, HA NOVA version, operating system, and a recently observed Relay version when available. The ID only lets the same participating installation count once. It is not derived from or reused from a hardware or device identifier, pairing, a user, a Relay, or Home Assistant; HA NOVA attaches no device data. No usage or Home Assistant data is sent. Cloudflare is the hosting provider and processes the JSON and source IP/connection metadata of the same HTTPS request under its privacy policy. HA NOVA ingest code does not read or store the source IP. Counts are voluntary, self-reported participating installations, not verified people or the complete installed base. Offer exactly these effects: Yes/contribute -> run ha-nova census choose <choice-id> yes; No/do not contribute -> run ha-nova census choose <choice-id> no; Show exact data -> run ha-nova census status without changing consent, display the literal JSON object verbatim plus the Cloudflare transport disclosure without omitting or renaming fields, then immediately show the same choice again with the same choice ID. Only a selection of the displayed Yes or No effect, including its numbered fallback, changes consent. Missing, dismissed, free-form, or ambiguous input runs nothing. If census status fails, name the error, state that consent is unchanged, and immediately show the same choice again. If a choose command fails, report that the stale choice was not saved and inspect current status; never fall back to unbound census on/off. Details: docs/reference/census.md.`

// maybeEmitCensusSkillNotice prints the pending-ask block on the check-update
// human paths (never --json). Printing does not consume a presentation:
// another active choice may require the skill to defer it.
func maybeEmitCensusSkillNotice(paths runtimePaths) {
	maybeEmitCensusSkillNoticeTo(paths, os.Stdout)
}

func maybeEmitCensusSkillNoticeTo(paths runtimePaths, out io.Writer) bool {
	if BuildChannel == "dev" || censusOptedOutByEnv() || censusLifecycleStopped(paths) {
		return true
	}
	state := loadCensusState(paths)
	if state.AskedAt != "" {
		return true
	}
	// Emission is only delivery to the AI client. The skill claims the one-time
	// prompt only after other active choices are gone.
	fmt.Fprintln(out, censusSkillNoticeBlock)
	return true
}
