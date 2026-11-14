package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// A NOVA App update that something in the home reacts to (an automation
// listening for service calls, say) rates typed, like an unlock. The guided
// update then asks the way every typed change asks: it plans the install as
// `ha-nova plan call` does and runs `ha-nova apply` in process, so the
// plan's own channel asks the person (the window on this computer and the
// linked phone, a gated App's ask on the phone or in Home Assistant, else a
// four-digit code typed here), and apply checks the home again right before
// it sends. Nothing typed is decided in this file.

func runTypedGuidedRelayUpdate(paths runtimePaths, route *relayUpdateRoute, candidate relayUpdateCandidate, in *bufio.Reader, out io.Writer) bool {
	planned, _ := runSetupSpinnerWithResult(out, "Preparing the confirmation...", setupSpinnerDebounce, func() (guidedVerbResult, error) {
		return runGuidedVerb(paths, io.Discard, planVerb, "call", "update.install", candidate.EntityID, "--set", "backup=true", "--skill", "ha-nova:updates"), nil
	})
	if planned.env.Plan != nil && planned.env.Tier != tierTyped.String() { // it rated typed a moment ago
		fmt.Fprintln(out, guidedHomeChanged)
		return false
	}
	for round := 0; ; round++ {
		args, ok := askTypedGuidedRelayUpdate(planned, in, out)
		if !ok {
			return false
		}
		current, _ := route.candidate()
		if !current.updateAvailable() || !current.guidedInstallReady() || !current.samePreview(candidate) {
			fmt.Fprintln(out, "The update changed, or HA NOVA couldn't check it again. Nothing was installed. Run the check again.")
			return false
		}
		applied := runGuidedVerb(paths, out, applyVerb, args...)
		switch {
		case applied.exit == verbExitConfirmation && round == 0: // the window couldn't open: its new plan asks on the phone or with a code
			planned = applied
			continue
		case applied.exit == verbExitDrift:
			fmt.Fprintln(out, guidedHomeChanged)
			return false
		case applied.exit == verbExitUnverified && applied.env.Error != nil && (applied.env.Error.Code == "SERVICE_REJECTED" || applied.env.Error.Code == "SERVICE_ERROR"):
			fmt.Fprintf(out, "Home Assistant didn't start the update.\n%s\n", relayUpdateManualPath)
			return false
		case applied.exit != verbExitOK && applied.exit != verbExitUnverified && applied.exit != verbExitUnknown: // nothing was sent
			fmt.Fprintf(out, "Nothing was installed (%s).\n%s\n", applied.why(), relayUpdateManualPath)
			return false
		}
		// Sent: the App restarts mid-call, so a lost or unverified answer is
		// the expected shape of success; polling decides, as on the preview path.
		fmt.Fprintln(out, "The NOVA App update started, with a backup first. The App restarts while it installs.")
		return finishGuidedRelayUpdate(paths, route, current, out)
	}
}

const guidedHomeChanged = "Something in your home changed since HA NOVA checked the update. Nothing was installed. Run the check again."

// askTypedGuidedRelayUpdate shows a typed plan's confirmation (its why, and
// where it asks) and returns apply's arguments: a window, phone, or gated
// App plan asks when apply runs; a code plan's code is shown and typed here.
func askTypedGuidedRelayUpdate(planned guidedVerbResult, in *bufio.Reader, out io.Writer) ([]string, bool) {
	plan := planned.env.Plan
	if plan == nil || !planIDPattern.MatchString(plan.ID) || planned.env.Say == "" {
		fmt.Fprintf(out, "HA NOVA couldn't prepare the confirmation, so it installs nothing (%s).\n%s\n", planned.why(), relayUpdateManualPath)
		return nil, false
	}
	const lead = "This update needs your confirmation first, the same way an unlock does:"
	if plan.Channel != channelChat {
		fmt.Fprintf(out, "%s\n%s\n", lead, planned.env.Say)
		return []string{"--plan", plan.ID}, true
	}
	head, lang, found := sayCodeHead(planned.env.Say, plan.Code)
	if !found {
		fmt.Fprintf(out, "HA NOVA couldn't prepare the confirmation, so it installs nothing.\n%s\n", relayUpdateManualPath)
		return nil, false
	}
	head = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(head), sayChatNote(lang))) // the AI's note: nobody else reads this terminal
	fmt.Fprintf(out, "%s\n%s\n%s %s\n", lead, head, sayCodeLead(lang), plan.Code)
	code, err := promptLineFromReader(in, out, "Code (Enter skips)", "")
	if err != nil || strings.TrimSpace(code) == "" {
		fmt.Fprintln(out, "Nothing was installed.")
		return nil, false
	}
	return []string{"--plan", plan.ID, "--confirm", code}, true
}

// guidedVerbResult is one verb's exit code and JSON envelope.
type guidedVerbResult struct {
	exit int
	env  verbEnvelope
}

// why: the verb's own words for what stopped it.
func (r guidedVerbResult) why() string {
	if r.env.Error != nil && r.env.Error.Message != "" {
		return r.env.Error.Message
	}
	return fmt.Sprintf("no clear answer, exit %d", r.exit)
}

// runGuidedVerb runs one verb as `ha-nova <verb> <args>` would on this
// command's server, with a CLI command's budget from now (an ask's time
// counts from here, not from the start of `ha-nova update`). Progress lines
// (the window's or phone's wait) go to progress.
func runGuidedVerb(paths runtimePaths, progress io.Writer, verb func(*verbCall, runtimePaths, []string) int, args ...string) guidedVerbResult {
	var envelope callBuffer
	now := time.Now()
	call := &verbCall{out: &envelope, err: progress, start: now, deadline: now.Add(cliCheckBudget), server: activeServerProfile()}
	result := guidedVerbResult{exit: verb(call, paths, args)}
	_ = json.Unmarshal([]byte(envelope.String()), &result.env) // unreadable: no plan and no error, so nothing goes on
	return result
}

// finishGuidedRelayUpdate waits for the App to restart on the new version
// after the install went out, and checks that it works.
func finishGuidedRelayUpdate(paths runtimePaths, route *relayUpdateRoute, candidate relayUpdateCandidate, out io.Writer) bool {
	version, err := waitForRelayVersion(paths, route, candidate.LatestVersion)
	if errors.Is(err, errSharedAccessEnded) {
		fmt.Fprintf(out, "The NOVA App is updated. %s\n", sharedAccessEndedFor(route.cfg, activeServerProfile()))
		return false
	}
	if err != nil {
		fmt.Fprintf(out, "The NOVA App didn't show the new version within %s.\n%s\n", relayUpdatePollTimeout, relayUpdateManualPath)
		return false
	}
	if !waitForRelayUpdateCompletion(route, candidate) {
		fmt.Fprintf(out, "The NOVA App runs v%s now, but Home Assistant didn't say the update was done within %s.\n%s\n", version, relayUpdatePollTimeout, relayUpdateManualPath)
		return false
	}
	readiness, readinessVersion := verifyUpdatedRelayReadiness(paths, route, candidate.LatestVersion)
	if !readiness {
		fmt.Fprintf(out, "The NOVA App runs v%s now, but HA NOVA couldn't check that it reaches Home Assistant.\n%s\n", version, relayUpdateManualPath)
		return false
	}
	version = readinessVersion
	fmt.Fprintf(out, "The NOVA App is updated and works: v%s.\n", version)
	if ha, err := chatAppsReadHA(paths); err == nil && ha.loaded { // ChatGPT keeps the tools it took until a Refresh (#923)
		fmt.Fprintln(out, chatGPTRefreshLine)
	}
	return true
}
