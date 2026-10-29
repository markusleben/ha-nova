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
	case kind == "options" && strings.Contains(jsonText(data), `"!secret`):
		return rawRefusal("App option set to a !secret reference", "it would hand a stored secret to the App; set that option in the App's Configuration tab")
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

func jsonText(v any) string {
	text, _ := json.Marshal(v)
	return string(text)
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

// rawSupervisorEveryday: a Supervisor write's everyday line, with the
// request as its detail.
func rawSupervisorEveryday(s *relaySession, payload map[string]any) everydayLine {
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	data, _ := payload["data"].(map[string]any)
	kind, slug := rawSupervisorMatch(method, path)
	line := everydayLine{goal: actionGoal{frame: "app", names: []string{appLabel(rawLang, appStoreName(s, slug), slug)}},
		detail: confirmT(rawLang, "detail.supervisor_raw", "method", sayText(method, false), "path", sayDetail(rawLang, path, detailLimit, true))}
	switch {
	case kind == "install":
		line.goal.verb = "install"
	case kind == "uninstall" && data["remove_config"] == true:
		line.goal.verb, line.more = "uninstall", confirmT(rawLang, "everyday.uninstall_config")
	case kind == "uninstall":
		line.goal.verb, line.more = "uninstall", confirmT(rawLang, "everyday.uninstall")
	case rawSupervisorOptionsKind(data) == "auto_update" && data["auto_update"] == true:
		line.goal.verb = "auto_update_on"
	case rawSupervisorOptionsKind(data) == "auto_update":
		line.goal.verb = "auto_update_off"
	default:
		line.goal.verb, line.more = "app_settings", rawSupervisorSettings(s, slug, data)
	}
	return line
}

// rawSupervisorSettings: what an options write sets, every value that may
// hold a secret hidden.
func rawSupervisorSettings(s *relaySession, slug string, data map[string]any) string {
	options, _ := data["options"].(map[string]any)
	secret, schemaRead := rawSupervisorPasswordOptions(s, slug)
	changes := make([]string, 0, len(options))
	for _, key := range sortedKeys(options) {
		value, err := marshalPlain(options[key])
		switch {
		case !schemaRead || secret[key] || rawSupervisorSecretKey(key) || rawSupervisorHoldsSecret(key, options[key]):
			changes = append(changes, confirmT(rawLang, "everyday.app_setting_secret", "key", sayText(key, true)))
		case err != nil || len(value) > 120:
			changes = append(changes, confirmT(rawLang, "everyday.app_setting_long", "key", sayText(key, true)))
		default:
			changes = append(changes, confirmT(rawLang, "everyday.app_setting", "key", sayText(key, true), "value", sayText(string(value), false)))
		}
	}
	return strings.Join(changes, ", ")
}

// rawSupervisorPasswordOptions: the App's top-level options whose schema marks
// them, or anything nested in them, as a password (the Relay's redactAppInfo).
// read is false when the schema can't be read: then no value is shown.
func rawSupervisorPasswordOptions(s *relaySession, slug string) (secret map[string]bool, read bool) {
	if s == nil {
		return nil, false
	}
	var info struct {
		Status int `json:"status"`
		Body   struct {
			Data struct {
				Schema []json.RawMessage `json:"schema"`
			} `json:"data"`
		} `json:"body"`
	}
	raw, err := s.sendWithin("supervisor", []byte(`{"method":"GET","path":"/addons/`+slug+`/info"}`), relaySessionRequestTimeout)
	if err != nil || json.Unmarshal(raw, &info) != nil || info.Status != 200 || info.Body.Data.Schema == nil {
		return nil, false
	}
	secret = map[string]bool{}
	for _, node := range info.Body.Data.Schema {
		var top struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(node, &top) != nil {
			return nil, false
		}
		var compact bytes.Buffer
		if json.Compact(&compact, node) != nil {
			return nil, false
		}
		secret[top.Name] = bytes.Contains(compact.Bytes(), []byte(`"format":"password"`))
	}
	return secret, true
}

// rawSupervisorSecretKey mirrors the Relay's secretKey: the Relay hides these
// values already; the CLI hides them again in case an older Relay did not.
var rawSupervisorSecretWords = []string{"pass", "pw", "pwd", "key", "keys", "psk", "pin", "code", "codes", "cred", "creds", "llat", "cert", "certs",
	"certificate", "salt", "otp", "totp", "auth", "secret", "secrets", "token", "tokens"}

var (
	rawSupervisorSecretText   = regexp.MustCompile(`password|passwd|passphrase|secret|token|credential|apikey|api_key|private|privkey`)
	rawSupervisorSecretSuffix = regexp.MustCompile(`(pass|pw|pwd|pin|key|code)$`)
	rawSupervisorCamel        = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	rawSupervisorURLUserinfo  = regexp.MustCompile(`(?i)^(\s*[a-z][a-z0-9+.-]*://)[^/?#\s]*@`)
)

func rawSupervisorSecretKey(key string) bool {
	lower := strings.ToLower(rawSupervisorCamel.ReplaceAllString(key, "${1}_${2}"))
	if lower == "error_key" {
		return false
	}
	if rawSupervisorSecretText.MatchString(lower) || rawSupervisorSecretSuffix.MatchString(lower) {
		return true
	}
	return slices.ContainsFunc(strings.FieldsFunc(lower, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') }),
		func(word string) bool { return slices.Contains(rawSupervisorSecretWords, word) })
}

// rawSupervisorHoldsSecret: the option's new value would be hidden on a read
// (a nested secret-like key, an environment entry, a URL's user info).
func rawSupervisorHoldsSecret(key string, value any) bool {
	plain, err := marshalPlain(map[string]any{"body": map[string]any{key: value}})
	return err != nil || !bytes.Equal(redactSupervisorResponse(plain), plain)
}

// redactSupervisorResponse hides, inside every "body" (the Supervisor's own
// reply in the Relay's {status, body}), each value under a secret-like key at
// any depth (booleans and null stay), an environment entry's value, and a
// URL's user info; a log's text is left as it is.
func redactSupervisorResponse(body []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil || dec.More() {
		return body
	}
	var walk func(v any, inBody bool) any
	walk = func(v any, inBody bool) any {
		switch t := v.(type) {
		case map[string]any:
			name, _ := t["name"].(string)
			named := inBody && rawSupervisorSecretKey(name)
			for key, inner := range t {
				_, flag := inner.(bool)
				if inBody && (rawSupervisorSecretKey(key) || named && key == "value") && inner != nil && !flag {
					t[key] = secretHidden
				} else {
					t[key] = walk(inner, inBody || key == "body")
				}
			}
		case []any:
			for i := range t {
				t[i] = walk(t[i], inBody)
			}
		case string:
			if inBody {
				return rawSupervisorURLUserinfo.ReplaceAllString(t, "${1}"+secretHidden+"@")
			}
		}
		return v
	}
	out, err := marshalPlain(walk(v, false))
	if err != nil {
		return body
	}
	return out
}

// errSupervisorViaCloud: Home Assistant Link carries no Supervisor route.
var errSupervisorViaCloud = errors.New("Supervisor access needs HA NOVA's local connection; Home Assistant Link does not carry it. Connect from home (on a computer: --via local); nothing was sent")
