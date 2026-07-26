package main

import "strings"

func namedSetupRequestAllowed(
	cfg runtimeConfig,
	retirementPending bool,
	target string,
	serviceMode bool,
	host string,
	haURL string,
	relayURL string,
	relayToken string,
) bool {
	if activeServerProfile() == defaultServerProfileName {
		return true
	}
	namedCloudRecoverySetup := cfg.Cloud != nil &&
		(cloudRecoveryHoldProblem(cfg) != nil ||
			!cfg.Cloud.ready() ||
			!cloudRemoteFeatureAvailable())
	namedClientRepair := strings.TrimSpace(target) != ""
	return (remoteOnlyCloudSetup(cfg) ||
		namedCloudRecoverySetup ||
		namedClientRepair ||
		retirementPending) &&
		!serviceMode &&
		strings.TrimSpace(host) == "" &&
		strings.TrimSpace(haURL) == "" &&
		strings.TrimSpace(relayURL) == "" &&
		strings.TrimSpace(relayToken) == ""
}

func renderNamedSetupRequestError() {
	profile := activeServerProfile()
	printHumanErr(
		"setup can use named profile %q only for Cloud-only resume and client installation; local pairing is managed with: ha-nova pair --server %s --relay-url http://<ha-host>:8791",
		profile,
		profile,
	)
}
