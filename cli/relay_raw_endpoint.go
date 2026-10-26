package main

import "fmt"

// One guard per raw Relay endpoint, shared by `ha-nova relay core|ws|files|backups`
// and the chat bridge (docs/work/2026-10-25-chat-parity-spec.md): the bridge
// adds no route the CLI refuses and skips no check the CLI runs.

// guardRawRequest runs every check before a raw request goes to endpoint.
// send is the body to send (backups: codes hidden); refusal is the safety
// rule (exit 4); err is a failed registry read (exit 1). Either way nothing
// was sent. A caller sending several requests runs it right before each.
func guardRawRequest(paths runtimePaths, opts verbOptions, endpoint string, body []byte) (send []byte, refusal, err error) {
	return guardRaw(endpoint, body, func() (error, error) { return checkRawAccessTarget(paths, opts, endpoint, body) })
}

// guardRawRequestIn is guardRawRequest on an open session: a raw plan runs
// it at planning and again right before each item is sent (live registry).
func guardRawRequestIn(s *relaySession, endpoint string, body []byte) (send []byte, refusal, err error) {
	return guardRaw(endpoint, body, func() (error, error) { return checkRawAccessTargetIn(s, endpoint, body) })
}

func guardRaw(endpoint string, body []byte, accessTarget func() (refusal, err error)) (send []byte, refusal, err error) {
	switch endpoint {
	case "files": // the Relay decides: file access is off unless the Relay App turns it on
		return body, nil, nil
	case "backups": // a snapshot never stores a code, even one an agent got elsewhere
		return redactSecrets(body, false), nil, nil
	case "core", "ws":
	default:
		return nil, nil, fmt.Errorf("unknown Relay endpoint %q; nothing was sent", endpoint)
	}
	if !rawRelayRead(endpoint, body) && holdsHiddenValue(body) { // first: the clearest words for a marker carried back
		return nil, rawRefusal("write", hiddenValueNext), nil
	}
	if refusal := checkRawRelayRequest(endpoint, body); refusal != nil {
		return nil, refusal, nil
	}
	if refusal, err := accessTarget(); refusal != nil || err != nil {
		return nil, refusal, err
	}
	return body, nil, nil
}

// redactRawResponse hides stored codes and phone webhook ids in every
// response (an old snapshot from backups too), before --jq, which could pick
// one out; live phone confirmation ids only on ws/core (a file's or yaml
// snapshot's whole content is one string).
func redactRawResponse(endpoint string, body []byte) []byte {
	return redactSecrets(body, endpoint == "ws" || endpoint == "core")
}
