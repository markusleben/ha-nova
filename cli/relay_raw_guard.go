package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
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
		method = strings.ToUpper(strings.TrimSpace(method))
		if err := checkRawCore(method, raw); err != nil {
			return err
		}
		return checkTemplateText(method, raw, payload["body"])
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

// checkTemplateText: a template's output is plain text, where redaction
// cannot find a camera's or image entity's access token (redactSecrets), so
// a render that names one is not sent. A template built to get around this
// is not covered.
func checkTemplateText(method, raw string, body any) error {
	pathPart, _, _ := strings.Cut(raw, "#")
	pathPart, _, _ = strings.Cut(pathPart, "?")
	if method != http.MethodPost || pathPart != "/api/template" {
		return nil
	}
	text, _ := json.Marshal(body)
	lower := strings.ToLower(string(text))
	if strings.Contains(lower, "access_token") || strings.Contains(lower, "entity_picture") {
		return rawRefusal("template naming access_token or entity_picture", "templates can't print a camera's or image's sign-in token; read the entity instead")
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
	case "config/device_registry/remove":
		if device, _ := message["device_id"].(string); device == "" {
			return rawRefusal("device deletion without its device_id", "name the device with its device_id")
		}
	case "subscribe_events":
		eventType, _ := message["event_type"].(string)
		if eventType == "" || eventType == "*" || strings.EqualFold(eventType, "call_service") {
			return rawRefusal("subscription to every event or to call_service", "subscribe to the one event type you need")
		}
		if phoneEventName(eventType) {
			return rawRefusal("subscription to phone notification events (they can carry a phone confirmation's IDs)", rawNextUI)
		}
	case "subscribe_trigger":
		trigger, _ := json.Marshal(message["trigger"])
		text := strings.ToLower(string(trigger))
		// A template can name any event type, so no template in a raw trigger.
		if strings.Contains(text, "call_service") || phoneEventName(text) || strings.Contains(text, "{{") || strings.Contains(text, "{%") {
			return rawRefusal("trigger subscription on call_service, phone notification events, or a template", "watch the entity or event you need instead")
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
		optionsDomain, _ := message["options_domain"].(string)
		_, setsOptionsDomain := message["options_domain"]
		_, setsOptions := message["options"]
		switch {
		case (setsOptions || setsOptionsDomain) && !(optionsDomain == domain && (domain == "sensor" || domain == "number")):
			// Other options hold a lock's or alarm panel's code, voice and
			// cloud exposure (cloud.* disable_2fa), or switch_as_x wiring.
			return rawRefusal("change of "+entityID+"'s options (only a sensor's or number's own display options are changed from here: options_domain must be the entity's domain, sensor or number)", rawNextUI)
		case disables && (domain == "lock" || domain == "alarm_control_panel" || domain == "cover"):
			return rawRefusal("disabling or enabling "+entityID+" (a disabled lock, alarm panel, or cover stops working in Home Assistant, so a door can't be locked or the alarm armed)", rawNextUI)
		case setsClass && strings.HasPrefix(entityID, "cover."):
			return rawRefusal("change of a cover's \"Show as\" class (it decides whether opening needs the one-time code)", rawNextUI)
		case setsClass && strings.HasPrefix(entityID, "button."):
			return rawRefusal("change of a button's class (it decides whether pressing it needs a preview)", rawNextUI)
		}
		// A button rename is checked against the registry (relay_raw_targets.go).
	case "config/device_registry/update":
		device, _ := message["device_id"].(string)
		if _, disables := message["disabled_by"]; disables && device == "" { // the registry check needs the device it names
			return rawRefusal("disabling or enabling a device without its device_id", "name the device with its device_id, or "+rawNextUI)
		}
	}
	return nil
}

// phoneEventName: the Companion app's notification events. Android echoes
// every action ID and the phone's webhook ID in them, so a raw listener could
// learn a phone confirmation's IDs (confirm_phone.go).
func phoneEventName(text string) bool {
	text = strings.ToLower(text)
	return strings.Contains(text, "mobile_app_notification") || strings.Contains(text, "ios.notification")
}

// watchesConfirmation: a config with an event trigger (or wait_for_trigger)
// on call_service, on a phone notification event other than the tap itself,
// or on a templated event type. Such an automation could copy a phone
// confirmation's buttons into a notification with its own text, and the
// owner's genuine tap would confirm what they never read; writing one is
// typed. A listener on the tap itself (mobile_app_notification_action) sees
// the ids only after the decision, when they are dead.
func watchesConfirmation(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		if t["platform"] == "event" || t["trigger"] == "event" {
			var types []string
			switch et := t["event_type"].(type) {
			case string:
				types = []string{et}
			case []any:
				for _, x := range et {
					text, ok := x.(string)
					if !ok {
						return true
					}
					types = append(types, text)
				}
			default:
				return true // missing or unreadable: assume the worst
			}
			for _, et := range types {
				et = strings.ToLower(strings.TrimSpace(et))
				if strings.Contains(et, "call_service") || strings.ContainsAny(et, "{}%") || phoneEventName(et) && et != phoneEventType && et != "ios.notification_action_fired" {
					return true
				}
			}
		}
		for _, x := range t {
			if watchesConfirmation(x) {
				return true
			}
		}
	case []any:
		for _, x := range t {
			if watchesConfirmation(x) {
				return true
			}
		}
	}
	return false
}

// flowStepPath: a GET on a flow step re-runs it (Home Assistant's
// FlowManagerResourceView.get calls async_configure), which can finish the
// flow (a progress or external step) and create an entry, a subentry, or a
// repair's fix.
var flowStepPath = regexp.MustCompile(`^/api/(config/config_entries/((options|subentries)/)?flow|repairs/issues/fix)/`)

// rawRelayRead: the request only reads (a core GET, or WS types listed as
// reads), so a failure after it was sent changed nothing in Home Assistant.
func rawRelayRead(endpoint string, body []byte) bool {
	payload, ok := decodeObject(body)
	if !ok {
		return false
	}
	isRead := func(message map[string]any) bool {
		wsType, _ := message["type"].(string)
		return rawWSAllowed[wsType] == rawWSRead
	}
	switch endpoint {
	case "core":
		method, _ := payload["method"].(string)
		raw, _ := payload["path"].(string)
		pathPart, _, _ := strings.Cut(raw, "#")
		pathPart, _, _ = strings.Cut(pathPart, "?")
		if strings.ToUpper(strings.TrimSpace(method)) != http.MethodGet || !strings.HasPrefix(pathPart, "/") || strings.ContainsAny(pathPart, "%\\") || path.Clean(pathPart) != pathPart { // the Relay roots a relative path
			return false
		}
		return !flowStepPath.MatchString(pathPart)
	case "ws":
		message, hasMessage := payload["message"].(map[string]any)
		if hasMessage && !isRead(message) {
			return false
		}
		_, typed := payload["type"]
		return !typed && hasMessage || isRead(payload)
	}
	return false
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
