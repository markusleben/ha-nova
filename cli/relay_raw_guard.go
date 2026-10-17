package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// Raw `ha-nova relay core|ws` requests that would act on devices, or skip a
// plan's tier, are refused before anything is sent
// (docs/work/2026-10-17-confirmation-hardening-spec.md §2). Raw routes keep
// reads and config surfaces that do not act on devices by themselves.

const (
	rawNextCall   = "run it through `ha-nova call <domain.service> …` (add --response for a service that returns data, --skill ha-nova:<owner> for an owned service), which plans and checks it"
	rawNextEvent  = "to start an automation, run `ha-nova call automation.trigger <automation.x>` (planned and checked like any call), or use the Home Assistant UI"
	rawNextConfig = "use `ha-nova plan write` (create/update) or `ha-nova plan delete`, then `ha-nova apply`"
	rawNextUI     = "do this in the Home Assistant UI"
)

// Core path prefixes no raw write may reach (any method but GET).
var rawCoreWriteDenied = []struct{ prefix, next string }{
	{"/api/services", rawNextCall},
	{"/api/events", rawNextEvent},
	{"/api/webhook", rawNextEvent},
	{"/api/states", "states are set by devices, not by HA NOVA; " + rawNextCall},
	{"/api/intent", rawNextCall},
	{"/api/conversation", "a sentence can control any device; " + rawNextCall},
	{"/api/hassio", rawNextUI},
	{"/api/google_assistant", rawNextUI},
	{"/api/alexa", rawNextUI},
	{"/api/mobile_app", rawNextUI},
	{"/api/cloud", rawNextUI},
	{"/api/config/automation/config", rawNextConfig},
	{"/api/config/script/config", rawNextConfig},
}

// WS types no raw request may send.
var rawWSDenied = map[string]string{
	"call_service":               rawNextCall,
	"execute_script":             rawNextCall,
	"fire_event":                 rawNextEvent,
	"conversation/process":       "a sentence can control any device; " + rawNextCall,
	"assist_pipeline/run":        "a sentence can control any device; " + rawNextCall,
	"backup/restore":             rawNextUI,
	"lovelace/dashboards/delete": "use `ha-nova plan delete --domain dashboard --id <url_path>`, then `ha-nova apply`",
}

// rawRefusedExit is the verbs' "refused" exit (4), never 1: exit 1 reads as
// a connection or pairing problem, and a refusal is neither.
const rawRefusedExit = verbExitRefused

func rawRefusal(what, next string) error {
	return fmt.Errorf("refused raw %s (a safety rule, not a setup or connection problem): %s; nothing was sent", what, next)
}

// decodeObject parses body as a JSON object with UseNumber, so a number the
// Relay's JS parser accepts but Go's float64 would overflow (e.g. 1e1000)
// still parses here instead of failing the guard open.
func decodeObject(body []byte) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var payload map[string]any
	if dec.Decode(&payload) != nil {
		return nil, false
	}
	return payload, true
}

// checkRawRelayRequest is the raw-route guard; body is the request exactly
// as it would be sent to the Relay endpoint.
func checkRawRelayRequest(endpoint string, body []byte) error {
	payload, ok := decodeObject(body)
	if !ok {
		return nil // not a JSON object: the Relay rejects it
	}
	switch endpoint {
	case "core":
		method, _ := payload["method"].(string)
		raw, _ := payload["path"].(string)
		return checkRawCore(strings.ToUpper(strings.TrimSpace(method)), raw)
	case "ws":
		if message, enveloped := payload["message"].(map[string]any); enveloped {
			if err := checkRawWS(message); err != nil {
				return err
			}
		}
		return checkRawWS(payload)
	}
	return nil
}

// checkRawCore compares the path as Home Assistant routes it: the Relay parses
// it as a URL (dropping the #fragment and ?query the same way), so the guard
// drops both before normalizing. Percent-decoding is repeated (the Relay
// decodes up to four rounds), then cleaned and lowercased (broader than HA's
// case-sensitive routes, never narrower).
func checkRawCore(method, raw string) error {
	if strings.ContainsFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f }) { // the URL parse strips these
		return rawRefusal("core path "+strconv.Quote(raw), "the path holds whitespace or control characters")
	}
	pathPart, _, _ := strings.Cut(raw, "#") // the Relay parses as a URL: the fragment is dropped
	pathPart, _, _ = strings.Cut(pathPart, "?")
	decoded := pathPart
	for range 5 {
		next, err := url.PathUnescape(decoded)
		if err != nil {
			return rawRefusal("core path "+raw, "the path is not valid")
		}
		if next == decoded {
			break
		}
		decoded = next
	}
	decoded = strings.ToLower(path.Clean("/" + strings.ReplaceAll(decoded, "\\", "/")))
	if strings.HasPrefix(decoded, "/api/webhook") { // a webhook may accept GET too
		return rawRefusal("webhook call", rawNextEvent)
	}
	if method == http.MethodGet {
		return nil
	}
	for _, denied := range rawCoreWriteDenied {
		if strings.HasPrefix(decoded, denied.prefix) {
			return rawRefusal(method+" "+decoded, denied.next)
		}
	}
	return nil
}

func checkRawWS(message map[string]any) error {
	wsType, _ := message["type"].(string)
	wsType = strings.ToLower(strings.TrimSpace(wsType))
	if next, denied := rawWSDenied[wsType]; denied {
		return rawRefusal("WS "+wsType, next)
	}
	switch {
	case strings.HasPrefix(wsType, "webhook/"):
		return rawRefusal("WS "+wsType, "a webhook ID is a secret and firing a webhook runs its automation untiered; "+rawNextEvent)
	case wsType == "supervisor/api" && !strings.EqualFold(fmt.Sprint(message["method"]), "get"):
		return rawRefusal("Supervisor write", rawNextUI)
	case wsType == "subscribe_events":
		if eventType, _ := message["event_type"].(string); eventType == "" || eventType == "*" || strings.EqualFold(eventType, "call_service") {
			return rawRefusal("subscription to every event or to call_service", "subscribe to the one event type you need")
		}
	case wsType == "subscribe_trigger":
		if trigger, _ := json.Marshal(message["trigger"]); strings.Contains(strings.ToLower(string(trigger)), "call_service") {
			return rawRefusal("trigger subscription on call_service", "watch the entity or event you need instead")
		}
	case wsType == "blueprint/save" && message["allow_override"] == true:
		return rawRefusal("blueprint override (it changes every automation that uses it)", rawNextUI)
	case wsType == "config/entity_registry/update":
		entityID, _ := message["entity_id"].(string)
		if _, setsClass := message["device_class"]; setsClass && strings.HasPrefix(strings.ToLower(entityID), "cover.") {
			return rawRefusal("change of a cover's \"Show as\" class (it decides whether opening needs the one-time code)", rawNextUI)
		}
	}
	return nil
}
