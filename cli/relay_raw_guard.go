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

// Raw `ha-nova relay core|ws` writes are deny-by-default: only the writes in
// relay_raw_allow.go are sent; every other write, and every WS type not
// listed there, is refused before anything is sent
// (docs/work/2026-10-17-confirmation-hardening-spec.md §2.1). Core GETs stay
// open (webhook calls, backup downloads, and the Supervisor proxy excepted).

const (
	rawNextCall   = "run it through `ha-nova call <domain.service> …` (add --response for a service that returns data, --skill ha-nova:<owner> for an owned service), which plans and checks it"
	rawNextEvent  = "to start an automation, run `ha-nova call automation.trigger <automation.x>` (planned and checked like any call), or use the Home Assistant UI"
	rawNextConfig = "use `ha-nova plan write` (create/update) or `ha-nova plan delete`, then `ha-nova apply`"
	rawNextUI     = "do this in the Home Assistant UI"
	rawNextOther  = "raw writes reach only the config surfaces the skills use; act on a device with `ha-nova call`, change automations, scripts, and dashboards with `ha-nova plan`, and do anything else in the Home Assistant UI"
	rawNextWSType = "only the WebSocket types the skills use are sent raw, reads included; read with `ha-nova get`, `find`, or `history`, act on a device with `ha-nova call`, change automations, scripts, and dashboards with `ha-nova plan`, and do anything else in the Home Assistant UI"
)

// Refusal hints for well-known refused writes (the refusal itself comes from
// the allowlist; these only name the better route).
var rawCoreWriteHints = []struct{ prefix, next string }{
	{"/api/services", rawNextCall},
	{"/api/events", rawNextEvent},
	{"/api/states", "states are set by devices, not by HA NOVA; " + rawNextCall},
	{"/api/intent", rawNextCall},
	{"/api/conversation", "a sentence can control any device; " + rawNextCall},
	{"/api/config/automation/config", rawNextConfig},
	{"/api/config/script/config", rawNextConfig},
}

var rawWSHints = map[string]string{
	"call_service":               rawNextCall,
	"execute_script":             rawNextCall,
	"fire_event":                 rawNextEvent,
	"conversation/process":       "a sentence can control any device; " + rawNextCall,
	"assist_pipeline/run":        "a sentence can control any device; " + rawNextCall,
	"lovelace/config/save":       "use `ha-nova plan write --domain dashboard`, then `ha-nova apply`",
	"lovelace/dashboards/create": "use `ha-nova plan write --domain dashboard --create`, then `ha-nova apply`",
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
		// The Relay sends `message` from a collect_events envelope (a body
		// without `type`), else the body itself; both are checked when present.
		message, hasMessage := payload["message"].(map[string]any)
		if hasMessage {
			if err := checkRawWS(message); err != nil {
				return err
			}
		}
		if _, typed := payload["type"]; typed || !hasMessage {
			return checkRawWS(payload)
		}
	}
	return nil
}

// checkRawCore: a GET is a read; any other method is sent only when its
// exact path is on the allowlist. The allowlisted path must already be in the
// form the Relay forwards (no %-escapes, dot segments, doubled slashes, or
// backslashes), so the path matched is the path Home Assistant routes. The
// hints and the webhook check compare a decoded, cleaned, lowercased path
// (broader than HA's routes, never narrower).
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
	if strings.HasPrefix(decoded, "/api/backup") || strings.HasPrefix(decoded, "/api/hassio") { // a backup download holds every stored credential
		return rawRefusal("backup or Supervisor request", "list backups with WS backup/info; downloads and Supervisor actions stay in the Home Assistant UI")
	}
	if method == http.MethodGet {
		return nil
	}
	canonical := !strings.ContainsAny(pathPart, "%\\") && path.Clean(pathPart) == pathPart
	if canonical {
		for _, allowed := range rawCoreWrites {
			if method == allowed.method && allowed.path.MatchString(pathPart) {
				return nil
			}
		}
	}
	next := rawNextOther
	if !canonical {
		next = "send the plain path (no %-escapes, dot segments, or doubled slashes); " + rawNextOther
	}
	for _, hint := range rawCoreWriteHints {
		if strings.HasPrefix(decoded, hint.prefix) {
			next = hint.next
			break
		}
	}
	return rawRefusal(method+" "+decoded, next)
}

// checkRawWS: the exact type must be on the allowlist (Home Assistant
// dispatches on the exact string, so no trimming or case folding here), then
// the per-type rules below apply.
func checkRawWS(message map[string]any) error {
	wsType, _ := message["type"].(string)
	if _, allowed := rawWSAllowed[wsType]; !allowed {
		folded := strings.ToLower(strings.TrimSpace(wsType))
		next, hinted := rawWSHints[folded]
		switch {
		case hinted:
		case strings.HasPrefix(folded, "webhook/"):
			next = "a webhook ID is a secret and firing a webhook runs its automation untiered; " + rawNextEvent
		default:
			next = rawNextWSType
		}
		return rawRefusal("WS type "+strconv.Quote(wsType), next)
	}
	switch wsType {
	case "subscribe_events":
		if eventType, _ := message["event_type"].(string); eventType == "" || eventType == "*" || strings.EqualFold(eventType, "call_service") {
			return rawRefusal("subscription to every event or to call_service", "subscribe to the one event type you need")
		}
	case "subscribe_trigger":
		if trigger, _ := json.Marshal(message["trigger"]); strings.Contains(strings.ToLower(string(trigger)), "call_service") {
			return rawRefusal("trigger subscription on call_service", "watch the entity or event you need instead")
		}
	case "lovelace/config":
		if message["force"] == true { // re-reads a YAML dashboard from disk, like a reload
			return rawRefusal("lovelace/config with force", "read it without force")
		}
	case "lovelace/resources/create", "lovelace/resources/update":
		// Frontend code runs in every signed-in browser and can call any
		// service: only code already on this Home Assistant (HACS-installed or
		// /config/www, which /files cannot write) is registered raw.
		if url, set := message["url"]; set {
			u, _ := url.(string)
			if !(strings.HasPrefix(u, "/hacsfiles/") || strings.HasPrefix(u, "/local/")) || strings.Contains(u, "..") || strings.ContainsAny(u, "\\") {
				return rawRefusal("dashboard resource from "+strconv.Quote(u), "only /hacsfiles/… and /local/… resources are added from here; add another URL in the Home Assistant UI (Settings > Dashboards > Resources)")
			}
		}
	case "blueprint/save":
		if message["allow_override"] == true {
			return rawRefusal("blueprint override (it changes every automation that uses it)", rawNextUI)
		}
	case "config/entity_registry/update":
		entityID, _ := message["entity_id"].(string)
		entityID = strings.ToLower(entityID)
		_, setsClass := message["device_class"]
		_, disables := message["disabled_by"]
		domain, _ := splitID(entityID)
		switch {
		case disables && (domain == "lock" || domain == "alarm_control_panel" || domain == "cover"):
			return rawRefusal("disabling or enabling "+entityID+" (a disabled lock, alarm panel, or cover no longer marks its device's buttons and switches as needing the one-time code)", rawNextUI)
		case setsClass && strings.HasPrefix(entityID, "cover."):
			return rawRefusal("change of a cover's \"Show as\" class (it decides whether opening needs the one-time code)", rawNextUI)
		case setsClass && strings.HasPrefix(entityID, "button."):
			return rawRefusal("change of a button's class (it decides whether pressing it needs a preview)", rawNextUI)
		}
		// A button rename is checked against the registry (relay_raw_targets.go).
	}
	return nil
}
