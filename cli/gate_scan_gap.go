package main

// gateDisagrees: the App does not hold a change this computer rates typed,
// so nothing is sent. The App keeps the last word. An App this CLI already
// accepts (at or above min_relay_version, or a version it could not read)
// is not told to update; an older one is. registry: this computer asked
// because it could not read the entity registry.
func gateDisagrees(s *relaySession, registry bool) *verbFailure {
	next := "Tell the user nothing changed: HA NOVA on this computer and the safety check in Home Assistant don't agree, so nothing was sent. Plan the change again; if the same thing happens, they make it in Home Assistant itself."
	switch {
	case registry && relayBehind(s):
		next = "Tell the user nothing changed: this computer couldn't read the entity registry, so it held the change for a person, and the safety check in Home Assistant did not. Update HA NOVA on this computer (ha-nova update) and the NOVA App in Home Assistant, then plan it again."
	case registry:
		next = "Tell the user nothing changed: this computer couldn't read the entity registry, so it held the change for a person, and the safety check in Home Assistant did not. Plan the change again; if the same thing happens, they make it in Home Assistant itself."
	case relayBehind(s):
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

func registryReadFailed(reasons []tierReason) bool {
	for _, r := range reasons {
		if r.Code == reasonScanFailed && r.Detail == "registry" {
			return true
		}
	}
	return false
}
