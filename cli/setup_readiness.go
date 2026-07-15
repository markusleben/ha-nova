package main

type relayReadiness struct {
	HealthBody     []byte
	RelayReachable bool
	WSReady        bool
	UsedWSPing     bool
	LLATIssue      bool
	RelayAuthIssue bool
	HealthErr      error
	WSPingErr      error
	WSPingResponse relayWSPingResponse
}

var fetchRelayHealthForReadiness = fetchRelayHealth
var probeRelayWSPingForReadiness = probeRelayWSPing

func checkRelayReadiness(relayBaseURL, token string) relayReadiness {
	return checkRelayReadinessWithProbes(relayBaseURL, token, fetchRelayHealthForReadiness, probeRelayWSPingForReadiness, false)
}

func checkRelayReadinessWithProbes(
	relayBaseURL, token string,
	fetchHealth func(string, string) ([]byte, error),
	probeWSPing func(string, string) (relayWSPingResponse, error),
	forceWSPing bool,
) relayReadiness {
	body, err := fetchHealth(relayBaseURL, token)
	if err != nil {
		return relayReadiness{HealthErr: err}
	}

	readiness := relayReadiness{
		HealthBody:     body,
		RelayReachable: true,
	}
	if relayHealthWSConnected(body) && !forceWSPing {
		readiness.WSReady = true
		return readiness
	}

	readiness.UsedWSPing = true
	resp, err := probeWSPing(relayBaseURL, token)
	readiness.WSPingResponse = resp
	readiness.WSPingErr = err
	if err == nil && relayWSPingOK(resp) {
		readiness.WSReady = true
		return readiness
	}
	if err == nil {
		readiness.LLATIssue = relayWSPingIssueIsLLAT(resp)
		readiness.RelayAuthIssue = relayWSPingIssueIsRelayAuth(resp)
	}
	return readiness
}
