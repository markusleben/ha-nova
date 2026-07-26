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
	namedClientRepair := namedClientRepairTransportConfigured(
		cfg,
		target,
	)
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

func namedClientRepairTransportConfigured(
	cfg runtimeConfig,
	target string,
) bool {
	if strings.TrimSpace(target) == "" {
		return false
	}
	localDeviceConfigured :=
		strings.TrimSpace(cfg.RelaySecureBaseURL) != "" &&
			strings.TrimSpace(cfg.RelaySpkiPin) != ""
	cloudDeviceConfigured := cfg.Cloud != nil &&
		cfg.Cloud.ready() &&
		cloudRecoveryHoldProblem(cfg) == nil &&
		!cfg.Cloud.cleanupPending() &&
		cloudRemoteFeatureAvailable()
	return localDeviceConfigured || cloudDeviceConfigured
}

func renderNamedSetupRequestError() {
	profile := activeServerProfile()
	printHumanErr(
		"setup can use named profile %q only for Cloud-only resume and client installation; local pairing is managed with: ha-nova pair --server %s --relay-url http://<ha-host>:8791",
		profile,
		profile,
	)
}
