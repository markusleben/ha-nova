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
