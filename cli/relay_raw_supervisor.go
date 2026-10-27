package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The Supervisor endpoint (docs/work/2026-10-26-supervisor-role-spec.md →
// Decisions): the App holds hassio_role: manager, and the Relay's POST
// /supervisor forwards only the routes in its own table
// (nova/src/http/handlers/supervisor-allowlist.ts). This is the same table,
// checked before anything is sent; tests/http/supervisor-allowlist-parity.test.ts
// keeps the two equal. `ha-nova relay supervisor` and ha_read send its reads;
// its writes go through `ha-nova plan raw` only.

var rawSupervisorRoutes = []struct{ method, path, kind string }{
	{"GET", "/info", "read"},
	{"GET", "/os/info", "read"},
	{"GET", "/host/info", "read"},
	{"GET", "/network/info", "read"},
	{"GET", "/hardware/info", "read"},
	{"GET", "/resolution/info", "read"},
	{"GET", "/core/info", "read"},
	{"GET", "/supervisor/info", "read"},
	{"GET", "/addons", "read"},
	{"GET", "/addons/{slug}/info", "read"},
	{"GET", "/store", "read"},
	{"GET", "/store/addons", "read"},
	{"GET", "/store/addons/{slug}", "read"},
	{"GET", "/host/disks/default/usage", "read"},
	{"GET", "/os/datadisk/list", "read"},
	{"GET", "/host/logs/boots", "read"},
	{"GET", "/host/logs/identifiers", "read"},
	{"GET", "/supervisor/logs", "logs"},
	{"GET", "/core/logs", "logs"},
	{"GET", "/host/logs", "logs"},
	{"GET", "/host/logs/identifiers/{identifier}", "logs"},
	{"GET", "/addons/{slug}/logs", "logs"},
	{"POST", "/store/addons/{slug}/install", "install"},
	{"POST", "/addons/{slug}/uninstall", "uninstall"},
	{"POST", "/addons/{slug}/options", "options"},
}

var rawSupervisorPatterns = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(rawSupervisorRoutes))
	for i, route := range rawSupervisorRoutes {
		pattern := strings.ReplaceAll(route.path, "{slug}", "([A-Za-z0-9][A-Za-z0-9_-]{0,63})")
		pattern = strings.ReplaceAll(pattern, "{identifier}", "[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}")
		out[i] = regexp.MustCompile("^" + pattern + "$")
	}
	return out
}()

const rawSupervisorMaxLines = 1000

const rawNextSupervisor = "HA NOVA reaches only the Supervisor reads and App changes its skills use (ha-nova:updates → Apps); do anything else in Home Assistant (Settings > Apps, Settings > System)"

// rawSupervisorMatch: the listed route's kind and App slug for the exact
// method and raw path ("" when not listed). The path is matched as sent.
func rawSupervisorMatch(method, path string) (kind, slug string) {
	for i, route := range rawSupervisorRoutes {
		m := rawSupervisorPatterns[i].FindStringSubmatch(path)
		if route.method != method || m == nil {
			continue
		}
		if len(m) > 1 {
			slug = m[1]
		}
		if strings.EqualFold(slug, "self") {
			return "", ""
		}
		return route.kind, slug
	}
	return "", ""
}

// checkRawSupervisor: the request must be one the Relay would forward, with
// exactly the body its route takes; a write to HA NOVA's own App is refused.
func checkRawSupervisor(payload map[string]any) error {
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	for key := range payload {
		if !slices.Contains([]string{"method", "path", "data", "lines"}, key) {
			return rawRefusal("Supervisor request field "+strconv.Quote(key), "send {method, path, data?, lines?}")
		}
	}
	kind, slug := rawSupervisorMatch(method, path)
	if kind == "" {
		return rawRefusal("Supervisor request "+method+" "+strconv.Quote(path), rawNextSupervisor)
	}
	data, hasData := payload["data"].(map[string]any)
	if _, set := payload["data"]; set && !hasData {
		return rawRefusal("Supervisor request data", "data must be an object")
	}
	_, hasLines := payload["lines"]
	switch kind {
	case "read":
		if hasData || hasLines {
			return rawRefusal("Supervisor read with data or lines", "a read takes only method and path")
		}
		return nil
	case "logs":
		lines, err := strconv.Atoi(jsonNumberText(payload["lines"]))
		if hasData || hasLines && (err != nil || lines < 1 || lines > rawSupervisorMaxLines) {
			return rawRefusal("Supervisor log read", "a log read takes lines, an integer from 1 to "+strconv.Itoa(rawSupervisorMaxLines))
		}
		return nil
	}
	if hasLines {
		return rawRefusal("Supervisor write with lines", "lines applies to log reads")
	}
	if strings.Contains(strings.ToLower(slug), "ha_nova_relay") {
		return rawRefusal("change of HA NOVA's own App", "HA NOVA would lose its way into Home Assistant; do this in Home Assistant (Settings > Apps)")
	}
	switch _, removes := data["remove_config"].(bool); {
	case kind == "install" && len(data) != 0:
		return rawRefusal("App install with data", "an install takes no data")
	case kind == "uninstall" && (len(data) > 1 || len(data) == 1 && !removes):
		return rawRefusal("App uninstall data", "an uninstall takes only {remove_config: true|false}")
	case kind == "options" && rawSupervisorOptionsKind(data) == "":
		return rawRefusal("App options change", "send exactly {options: {…the keys to change}} or {auto_update: true|false}; the Relay keeps every other stored option")
	}
	return nil
}

// rawSupervisorOptionsKind: "options" or "auto_update" for the two bodies an
// options write takes, else "".
func rawSupervisorOptionsKind(data map[string]any) string {
	if len(data) != 1 {
		return ""
	}
	if _, ok := data["auto_update"].(bool); ok {
		return "auto_update"
	}
	if options, ok := data["options"].(map[string]any); ok && options != nil {
		return "options"
	}
	return ""
}

func jsonNumberText(v any) string {
	if n, ok := v.(json.Number); ok {
		return n.String()
	}
	return "x"
}

// rawSupervisorRead: the payload is a listed Supervisor read or log read.
func rawSupervisorRead(payload map[string]any) bool {
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	kind, _ := rawSupervisorMatch(method, path)
	return kind == "read" || kind == "logs"
}

// rawSupervisorWrite: the payload is a listed Supervisor write.
func rawSupervisorWrite(payload map[string]any) bool {
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	kind, _ := rawSupervisorMatch(method, path)
	return kind == "install" || kind == "uninstall" || kind == "options"
}

// rawSupervisorTier: every App install, removal, and options change takes
// the one-time code (App code, or options that can hold credentials and
// code); an auto-update toggle is previewed.
func rawSupervisorTier(payload map[string]any) (tierLevel, string) {
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	data, _ := payload["data"].(map[string]any)
	switch kind, _ := rawSupervisorMatch(method, path); {
	case kind == "install":
		return tierTyped, "This installs an App: new code that runs on your Home Assistant host"
	case kind == "uninstall":
		return tierTyped, "This removes an App, and whatever it serves stops working"
	case kind == "options" && rawSupervisorOptionsKind(data) == "options":
		return tierTyped, "This changes an App's settings, which can hold passwords and run code"
	}
	return tierPreview, ""
}

// rawSupervisorLine: a Supervisor write's plain preview line.
func rawSupervisorLine(payload map[string]any) (verb, object string) {
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	data, _ := payload["data"].(map[string]any)
	kind, slug := rawSupervisorMatch(method, path)
	app := "the App " + sayText(slug, true)
	switch {
	case kind == "install":
		return "Install", app
	case kind == "uninstall" && data["remove_config"] == true:
		return "Uninstall", app + " and delete its settings folder"
	case kind == "uninstall":
		return "Uninstall", app
	case rawSupervisorOptionsKind(data) == "auto_update" && data["auto_update"] == true:
		return "Turn on", "automatic updates for " + app
	case rawSupervisorOptionsKind(data) == "auto_update":
		return "Turn off", "automatic updates for " + app
	}
	options, _ := data["options"].(map[string]any)
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return "Change", "the settings " + sayText(strings.Join(keys, ", "), true) + " of " + app
}

// rawSupervisorSecretKey mirrors the Relay's secretKey: the Relay hides these
// values already; the CLI hides them again in case an older Relay did not.
var rawSupervisorSecretWords = []string{"pass", "pw", "pwd", "key", "keys", "psk", "pin", "llat", "cert", "certs",
	"certificate", "salt", "otp", "totp", "auth", "secret", "secrets", "token", "tokens"}

var rawSupervisorSecretText = regexp.MustCompile(`password|passwd|passphrase|secret|token|credential|apikey|api_key|private|privkey`)

func rawSupervisorSecretKey(key string) bool {
	lower := strings.ToLower(key)
	if rawSupervisorSecretText.MatchString(lower) {
		return true
	}
	return slices.ContainsFunc(strings.FieldsFunc(lower, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }),
		func(word string) bool { return slices.Contains(rawSupervisorSecretWords, word) })
}

// redactSupervisorResponse hides every value under a secret-like key, at any
// depth (booleans and null stay); a log's text is left as it is.
func redactSupervisorResponse(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil || dec.More() {
		return body
	}
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			for key, inner := range t {
				if _, flag := inner.(bool); rawSupervisorSecretKey(key) && inner != nil && !flag {
					t[key] = secretHidden
				} else {
					t[key] = walk(inner)
				}
			}
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
		}
		return v
	}
	out, err := marshalPlain(walk(v))
	if err != nil {
		return body
	}
	return out
}

// errSupervisorViaCloud: Home Assistant Link carries no Supervisor route.
var errSupervisorViaCloud = errors.New("Supervisor access needs HA NOVA's local connection; Home Assistant Link does not carry it. Run it at home (--via local); nothing was sent")
