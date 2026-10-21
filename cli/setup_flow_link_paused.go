package main

import (
	"fmt"
	"strings"
)

// Link paused at the start: a plain interactive `ha-nova setup` found a Home
// Assistant Link setup that isn't finished (and no safety hold). Setup goes
// on: it checks the existing connection and installs or refreshes skills,
// but it changes no credentials and never touches the Link data. A step that
// would need new credentials (pairing, a token, secure-storage repair, a
// pending activation or retirement, the service token file move) stops
// with the exact Link commands instead.

// linkPausedStopsBefore: the steps that may write credentials.
func linkPausedStopsBefore(step setupStepID) bool {
	switch step {
	case setupStepSecureStorageRecovery, setupStepHost, setupStepRelayInstall, setupStepToken, setupStepPairing:
		// Host too: a new address would no longer match the Link data.
		return true
	}
	return false
}

// stopWhileLinkPaused ends the run before it would change credentials.
func (f *setupFlow) stopWhileLinkPaused() setupStepResult {
	fmt.Fprint(f.ui.termOut(), f.linkPausedOutcome().LinkSteps)
	f.linkPausedShown = true
	f.ui.show(setupView{Blocks: []setupBlock{setupParagraph(setupText("setup.link.paused_stop"))}})
	return setupDone(1)
}

// showLinkPausedOutcome is the paused end screen.
func (f *setupFlow) showLinkPausedOutcome() {
	f.ui.outcome(f.linkPausedOutcome())
	f.linkPausedShown = true
}

// linkPausedNeedsCredentials: an interrupted activation or retirement
// would finish now, and both change credentials.
func (f *setupFlow) linkPausedNeedsCredentials() (bool, error) {
	if strings.TrimSpace(f.cfg.PendingSecureBaseURL) != "" && strings.TrimSpace(f.cfg.PendingSpkiPin) != "" {
		return true, nil
	}
	return deviceCredentialRetirementCheckpointExistsForProfile(f.paths, activeServerProfile())
}

// linkPausedOutcome is the paused end screen with this server's commands.
func (f *setupFlow) linkPausedOutcome() setupOutcome {
	legacy := legacySharedAccess(f.cfg) && strings.TrimSpace(f.existingToken) != ""
	return setupOutcome{Kind: setupOutcomeLinkPaused, LinkSteps: setupCloudPausedSteps(f.paths, f.cfg, legacy)}
}
