package main

import (
	"errors"
	"fmt"
)

// A plan this computer rates typed only because a read failed, while the
// App's check finishes that read and does not hold the change
// (GATE_DISAGREES). The two checks disagree on a coverage gap, not on an
// action the App would refuse to hold: an unlock, a delete, a YAML actuator
// the registry actually listed. This computer then asks in its own window
// and sends; the App forwards the bytes, which it rates below typed.
// Anything else still sends nothing (gateDisagrees). A chat app has no
// window here and keeps the refusal: a code the model can see is not a
// stand-in.

var errGateScanGap = errors.New("the App does not hold a change typed only by a failed read")

// scanGapTyped: every reason is a failed read (SCAN_FAILED, LIMIT_CUT) or a
// code that never raises above preview, so the typed tier is the gap.
// UNREGISTERED_ACTUATOR counts only beside a failed registry read: with no
// compact registry, every actuator looks like YAML without an id, which is
// the same gap. This path runs only after the App returned pass, so an
// actuator the App could read stays on the App's ask. A registry that was
// read makes that code a real finding and this returns false.
func scanGapTyped(reasons []tierReason) bool {
	registryFailed, gap := false, false
	for _, r := range reasons {
		if r.Code == reasonScanFailed || r.Code == reasonLimitCut {
			gap = true
		}
		if r.Code == reasonScanFailed && r.Detail == "registry" {
			registryFailed = true
		}
	}
	if !gap {
		return false
	}
	for _, r := range reasons {
		switch r.Code {
		case reasonScanFailed, reasonLimitCut:
		case reasonUnregisteredActuator:
			if !registryFailed {
				return false
			}
		case reasonConfigWrite, reasonConfigRevert, reasonConfigRestore,
			reasonIntegrationSvc, reasonNoRegistryRow, reasonStoredMembers,
			reasonManager, reasonServiceNotFound, reasonSceneUnread,
			reasonBatch, reasonExpanded, reasonAreaAssign:
		default:
			return false
		}
	}
	return true
}

// gateDisagrees: the App does not hold a change this computer rates typed,
// and it is not the scan-gap case confirmScanGap asks for. An App this CLI
// already accepts (at or above min_relay_version, or a version it could not
// read) is not told to update; an older one is.
func gateDisagrees(s *relaySession) *verbFailure {
	next := "Tell the user nothing changed: HA NOVA on this computer and the safety check in Home Assistant don't agree, so nothing was sent. Plan the change again; if the same thing happens, they make it in Home Assistant itself."
	if relayBehind(s) {
		next = "Tell the user nothing changed. Update HA NOVA on this computer (ha-nova update) and the NOVA App in Home Assistant, then plan it again."
	}
	return gateStop("refused", "GATE_DISAGREES", "HA NOVA's safety check in Home Assistant doesn't hold this change for a person, but this computer does; nothing was sent", next)
}

// relayBehind: the App's version from /health is older than min_relay_version.
// Unknown (no version, unreadable version file) is not behind: the message
// must not tell a current App to update.
func relayBehind(s *relaySession) bool {
	if s == nil || s.relayVersion == "" {
		return false
	}
	v, err := readVersionJSON(s.paths.VersionFile)
	if err != nil || v.MinRelayVersion == "" {
		return false
	}
	cmp, err := compareReleaseVersions(s.relayVersion, v.MinRelayVersion)
	return err == nil && cmp < 0
}

// confirmScanGap asks in the window on this computer. The App will not hold
// the bytes, so the linked phone is not asked through it, and a missing
// window sends nothing (no code in its place). Confirmed: nil, and
// humanWhere records the window.
func confirmScanGap(s *relaySession, plan storedPlan) error {
	if windowOff(s.paths) || !windowAvailable() {
		return gateDisagrees(s)
	}
	lang := textLang(plan)
	text := confirmText(plan, false)
	if _, fits := windowFits(lang, text); !fits {
		return gateDisagrees(s)
	}
	wait := s.askWait()
	if wait <= 0 { // the check already used the time this call has to ask
		return gateStop("refused", "CONFIRM_UNAVAILABLE", "checking this change took the time this call has to ask; nothing was sent",
			"Tell the user nothing changed. They can ask again; if it keeps happening, they make the change in Home Assistant itself.")
	}
	if wait > cliWindowWait {
		wait = cliWindowWait
	}
	fmt.Fprintln(s.call.stderr(), "The safety check in Home Assistant does not hold this change, so the linked phone is not asked. A confirmation window opened on this computer — confirm there.")
	switch askWindow(planAsks, planWindow(lang, text, false, confirmT(lang, "window.confirm")), wait, s.call.stderr()) {
	case windowConfirmed:
		humanWhere = channelWindow
		return nil
	case windowCancelled:
		return gateStop("declined", "CONFIRM_DECLINED", "the confirmation window was cancelled; nothing was sent",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case windowFailed:
		return gateDisagrees(s)
	default:
		return gateStop("cancelled", "CONFIRM_CANCELLED", "the confirmation window on this computer was closed; nothing was sent",
			"Tell the user nothing changed. The linked phone was not asked. Run ha-nova apply --plan "+plan.ID+" again only after they say they still want it; the window on this computer opens again.")
	}
}
