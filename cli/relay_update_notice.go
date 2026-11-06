package main

import "fmt"

func relayAvailableUpdateNotice(cfg config, token, running string) humanNotice {
	candidate, _ := resolveRelayUpdateCandidate(cfg, token)
	return relayAvailableUpdateNoticeFromCandidate(candidate, running)
}

// running is the version the Relay itself reports. Right after an App update
// through the Supervisor, the update entity's installed_version can still
// show the old one: the running Relay wins, so a done update isn't offered.
func relayAvailableUpdateNoticeFromCandidate(candidate relayUpdateCandidate, running string) humanNotice {
	if _, err := parseReleaseVersion(running); err == nil {
		candidate.InstalledVersion = running
	}
	if !candidate.updateAvailable() {
		return humanNotice{}
	}
	return humanNotice{
		level: humanNoticeNote, appUpdate: true,
		kind:    humanNoticeKindRelayUpdateAvailable,
		message: fmt.Sprintf("The NOVA App in Home Assistant has an update: %s → %s. %s", candidate.InstalledVersion, candidate.LatestVersion, relayAppUpdatePath),
	}
}

// doctorFloorNotice: doctor's words for an App below what this HA NOVA
// needs. At home, check-update's words ("No Update button yet?", a
// container's step), bounded as the first-use check. Over Home Assistant
// Link, which reaches only the App, the floor words and the step that makes
// Home Assistant list a newer App.
func doctorFloorNotice(paths runtimePaths, notice humanNotice, usingCloud, diagnosticOnly bool) humanNotice {
	if usingCloud {
		notice.message += relayCheckForUpdatesLine
		return notice
	}
	if diagnosticOnly {
		return notice
	}
	if full := relayUpdateNoticeWithTimeout(paths, firstUseRelayNoticeTimeout); full.belowFloor || full.kind == humanNoticeKindRelayOutdated {
		return full
	}
	return notice
}
