package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// One guard per raw Relay endpoint, shared by `ha-nova relay core|ws|files|backups`
// and the chat bridge (docs/work/2026-10-25-chat-parity-spec.md): the bridge
// adds no route the CLI refuses and skips no check the CLI runs.

// guardRawRequest runs every check before a raw request goes to endpoint.
// send is the body to send (backups: codes hidden); refusal is the safety
// rule (exit 4); err is a failed registry read (exit 1). Either way nothing
// was sent. A caller sending several requests runs it right before each.
func guardRawRequest(paths runtimePaths, opts verbOptions, endpoint string, body []byte) (send []byte, refusal, err error) {
	if payload, _ := decodeObject(body); endpoint == "supervisor" && rawSupervisorWrite(payload) { // relay and ha_read: reads only
		return nil, rawRefusal("Supervisor write", "an App install, uninstall, or options change goes through ha-nova plan raw (ha_plan kind raw in chat apps), then ha-nova apply"), nil
	}
	return guardRaw(endpoint, body, func() (error, error) {
		if refusal, err := formSecretSet(paths, nil, endpoint, body); refusal != nil || err != nil {
			return refusal, err
		}
		return checkRawAccessTarget(paths, opts, endpoint, body)
	})
}

// guardRawRequestIn is guardRawRequest on an open session: a raw plan runs
// it at planning and again right before each item is sent (live registry,
// the flow forms an earlier item got back, an App's settings form).
func guardRawRequestIn(s *relaySession, endpoint string, body []byte) (send []byte, refusal, err error) {
	send, refusal, err = guardRaw(endpoint, body, func() (error, error) {
		if refusal, err := formSecretSet(s.paths, s.flowForms, endpoint, body); refusal != nil || err != nil {
			return refusal, err
		}
		return checkRawAccessTargetIn(s, endpoint, body)
	})
	if refusal == nil && err == nil && endpoint == "supervisor" {
		if refusal, err = rawSupervisorFormSecret(s, body); refusal != nil || err != nil {
			return nil, refusal, err
		}
	}
	return send, refusal, err
}

// relayRawChecks runs what only `ha-nova relay` checks, after
// guardRawRequest: it sends a write at once, where a raw plan tiers it and
// ha_read sends none. A todo/item/move is refused: on a shopping list it
// fires shopping_list_updated, and only a plan classifies that event's
// listeners (rawShoppingList), so an automation that unlocks on it would run
// unasked (#928). A write a plan types is refused (relayTyped), and so is
// one whose listeners a plan rates typed (relayListeners, #946). A scene
// save keeps its members (relaySceneKept).
func relayRawChecks(paths runtimePaths, opts verbOptions, endpoint string, body []byte) (refusal, err error) {
	payload, _ := decodeObject(body)
	message, _ := payload["message"].(map[string]any) // a collect_events envelope
	if endpoint == "ws" && (payload["type"] == "todo/item/move" || message["type"] == "todo/item/move") {
		return rawRefusal("todo/item/move", "a reorder can start automations that listen to the list (a shopping list fires shopping_list_updated), and only a plan checks them: "+
			`put [{"endpoint":"ws","payload":<the message itself, without a collect_events envelope>}] in a file, run ha-nova plan raw --file <file>, then ha-nova apply`), nil
	}
	if refusal, err := relayTyped(paths, opts, endpoint, body, payload); refusal != nil || err != nil {
		return refusal, err
	}
	if refusal, err := relayListeners(paths, opts, endpoint, payload, message); refusal != nil || err != nil {
		return refusal, err
	}
	return relayKept(paths, opts, endpoint, body)
}

// relayTyped refuses a write a raw plan rates typed (gateVerdict, the
// Relay gate's own rating): only the user may confirm it, and only a plan
// asks them (docs/work/2026-11-03-relay-refuses-code-writes-spec.md). A
// failed read counts as typed; a session that doesn't open is err.
func relayTyped(paths runtimePaths, opts verbOptions, endpoint string, body []byte, payload map[string]any) (refusal, err error) {
	requests := gateRequests(endpoint, payload) // the core method as the Relay sends it, for the checks after this one too
	var s *relaySession
	defer func() {
		if s != nil {
			s.close()
		}
	}()
	check, err := gateVerdict(endpoint, body, func() (*relaySession, error) {
		if s != nil {
			return s, nil
		}
		opened, err := newRelaySession(paths, opts)
		s = opened
		return opened, err
	})
	switch {
	case err != nil:
		return nil, err
	case check.Verdict == gateRefuse: // fail closed: its own guard passed it, the gate (a number Home Assistant would get as null, a hidden value sent back) doesn't
		return rawRefusal("request", "HA NOVA's gate refuses it as written; send it without the values it names in its own words, or do this in the Home Assistant UI"), nil
	case check.Verdict != gateTyped:
		return nil, nil
	}
	request := requests[check.Item]
	what, _ := request["type"].(string)
	shown := "<this request>"
	switch endpoint {
	case "core":
		target, _ := request["path"].(string)
		what = request["method"].(string) + " " + target
		if _, body := payload["body"]; !body {
			exact, _ := json.Marshal(map[string]any{"method": payload["method"], "path": payload["path"]})
			shown = string(exact)
		}
	case "files", "backups":
		action, _ := request["action"].(string)
		what = endpoint + " " + action
	case "ws":
		if _, envelope := payload["message"].(map[string]any); envelope && check.Item == 0 {
			shown = "<the message itself, without a collect_events envelope>"
		}
	}
	reason := "it needs the user's own confirmation"
	if why := rawWhys[check.Why]; why != nil {
		reason = why("en")
	}
	if _, kept := request["device_trackers"]; check.Why == "person_follows" && request["type"] == "person/update" && !kept {
		reason += " (an update without device_trackers empties them: to keep them, resend the list person/list shows)"
	}
	return rawRefusal(what, strings.ToLower(reason[:1])+reason[1:]+", so only the user may confirm it, and only a plan asks them: "+
		fmt.Sprintf(`put [{"endpoint":%q,"payload":%s}] in a file, run ha-nova plan raw --file <file>, show the user its say, and follow its next_step`, endpoint, shown)), nil
}

// rawLogNext: logs are free text that can hold passwords, keys, and
// addresses no redactor bounds (relay-typed-gate spec §2; Markus,
// 2026-11-07), so the user reads them in Home Assistant. The one log read
// HA NOVA answers is log facts (log_facts.go).
const rawLogNext = "Home Assistant's logs can hold passwords, keys, and addresses, so HA NOVA doesn't read their lines. " +
	"Which integration has been failing since when: read WS {\"type\":\"system_log/list\"} with no other key (ha_read, or ha-nova relay ws), which answers log facts (counts and times). " +
	"For the lines, ask the user to open Settings > System > Logs in Home Assistant (an App's own log: Settings > Apps > the App > Log), " +
	"search for the integration or entity, and tell you the few lines that matter, without passwords, keys, or addresses; never ask for the whole log"

// rawLogRefusal refuses a request that reads a log, nil otherwise.
func rawLogRefusal(endpoint string, payload map[string]any) error {
	if payload == nil || !slices.ContainsFunc(gateRequests(endpoint, payload), func(r map[string]any) bool { return gateLogRead(endpoint, r) }) {
		return nil
	}
	return rawRefusal("log read", rawLogNext)
}

func guardRaw(endpoint string, body []byte, accessTarget func() (refusal, err error)) (send []byte, refusal, err error) {
	if payload, _ := decodeObject(body); rawLogRefusal(endpoint, payload) != nil {
		return nil, rawLogRefusal(endpoint, payload), nil
	}
	switch endpoint {
	case "files": // the Relay decides: file access is off unless the Relay App turns it on
		return body, nil, nil
	case "backups": // a snapshot never stores a code, even one an agent got elsewhere
		return redactSecrets(body, false), nil, nil
	case "supervisor":
		payload, ok := decodeObject(body)
		switch {
		case !ok:
			return nil, rawRefusal("Supervisor request", "send a JSON object {method, path, data?, lines?}"), nil
		case holdsHiddenValue(body):
			return nil, rawRefusal("write", "it holds a value HA NOVA hid; leave that option out and the App keeps its stored value"), nil
		}
		if refusal := checkRawSupervisor(payload); refusal != nil {
			return nil, refusal, nil
		}
		return body, nil, nil
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
	if endpoint == "supervisor" {
		body = redactSupervisorResponse(body)
	}
	return redactSecrets(body, endpoint == "ws" || endpoint == "core")
}

// relayReadFailed words a failed read: its outcome is never unknown.
func relayReadFailed(cause string) string {
	return "READ_FAILED: " + cause + "\nThis was a read: nothing in Home Assistant was changed. Fix the cause, then run it again."
}

func printRelaySendFailure(read bool, baseURL string, err error) {
	switch {
	case !read:
		printErr("%s", relayRequestOutcomeUnknownMessage(baseURL, err))
	case strings.TrimRight(baseURL, "/") == cloudRelayVirtualBaseURL:
		printErr("%s", relayReadFailed("Home Assistant Link could not complete the NOVA Relay request: "+cloudOutcomeUnknownCause(err)))
	default:
		printErr("%s", relayReadFailed(relayConnectErrorMessage(baseURL, err)))
	}
}

func printRelayFailure(read bool, stage string, err error) {
	if read {
		printErr("%s", relayReadFailed(stage+" failed: "+err.Error()))
		return
	}
	printRelayPostRequestError(stage, err)
}

func printRelayHTTPFailure(read bool, status int) {
	if read {
		printErr("%s", relayReadFailed(fmt.Sprintf("Relay returned HTTP %d", status)))
		return
	}
	printRelayHTTPOutcomeUnknown(status)
}
