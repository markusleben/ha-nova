package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Passwords and other secrets never go through a chat (skills/integration-setup
// → Flow steps, skills/health → Repairs). A config, options, subentry, or
// repair flow step that fills a field the flow's last form marked secret
// (formSecretSet), or one whose name says it is (flowStepSecret), is refused
// before it is sent; an apply whose flow now shows such a form tells the
// model to send the user to Home Assistant instead (flowAnswered).

// rawFlowPath: a flow as started (1: its base) or one of its steps.
var rawFlowPath = regexp.MustCompile(`^(/api/(?:config/config_entries/(?:(?:options|subentries)/)?flow|repairs/issues/fix))(/[A-Za-z0-9_-]+)?$`)

// rawFlowSecret: a flow that sets or asks for field, a secret; path is the
// flow step a DELETE cancels.
type rawFlowSecret struct{ field, path string }

func (r rawFlowSecret) Error() string {
	return rawRefusal("flow step that sets "+strconv.Quote(r.field)+" (a password or other secret)", "secrets don't belong in a chat; the user finishes it in Home Assistant ("+r.place()+")").Error()
}

func (r rawFlowSecret) place() string {
	if strings.HasPrefix(r.path, "/api/repairs/") {
		return "Settings > System > Repairs"
	}
	return "Settings > Devices & services"
}

// next: an options step can leave the secret out; any other flow finishes
// in Home Assistant.
func (r rawFlowSecret) next() string {
	never := "Passwords and other secrets don't belong in a chat: never ask the user for " + r.field + " here. "
	if strings.HasPrefix(r.path, "/api/config/config_entries/options/") {
		return never + "Leave " + r.field + " out of the step, or tell them to finish this in Home Assistant under " + r.place() + " and cancel this flow with a plan (core DELETE " + r.path + ")."
	}
	return never + "Tell them to finish this in Home Assistant under " + r.place() +
		". If you started this flow, cancel it with a plan (core DELETE " + r.path + "); leave one Home Assistant started (a discovered device, a reauthentication) as it is: it waits there for them."
}

// flowStepSecret: a flow step POST that sets a field whose name says it is
// a secret (secretFlowField, in a section too: a non-empty string, or a
// number for a PIN, code, key, or password); nil otherwise.
func flowStepSecret(method, path string, body any) error {
	if m := rawFlowPath.FindStringSubmatch(path); method == http.MethodPost && m != nil && m[2] != "" {
		if field := flowSecretSet(body); field != "" {
			return rawFlowSecret{field: field, path: path}
		}
	}
	return nil
}

func flowSecretSet(body any) string {
	fields, _ := body.(map[string]any)
	for _, key := range sortedKeys(fields) {
		name := strings.ToLower(key)
		switch value := fields[key].(type) {
		case map[string]any:
			if field := flowSecretSet(value); field != "" {
				return field
			}
		case string:
			if value != "" && secretFlowField(map[string]any{"name": key}) {
				return key
			}
		case json.Number: // a token or PSK is never a number
			if secretFlowNames[name] || strings.Contains(name, "password") || strings.Contains(name, "passphrase") ||
				slices.ContainsFunc(secretFlowSuffixes, func(suffix string) bool { return strings.HasSuffix(name, suffix) }) {
				return key
			}
		}
	}
	return ""
}

// flowForms: the secret fields of the last form each flow answered a plan's
// apply with (flowAnswered), by flow id: the newest 50 flows.
func flowFormsPath(paths runtimePaths) string {
	return filepath.Join(paths.CacheDir, "flow-forms.json")
}

type flowForm struct {
	Secrets []string  `json:"secrets"`
	At      time.Time `json:"at"`
}

// readFlowForms: no file yet, or one holding null, holds no forms; an
// unreadable one is an error.
func readFlowForms(paths runtimePaths) (map[string]flowForm, error) {
	forms := map[string]flowForm{}
	data, err := os.ReadFile(flowFormsPath(paths))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return forms, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &forms); err != nil {
		return nil, fmt.Errorf("%s: %w", flowFormsPath(paths), err)
	}
	if forms == nil {
		forms = map[string]flowForm{}
	}
	return forms, nil
}

// formSecretSet: a flow step POST that fills a field its flow's last form
// marked secret, in a section too, whatever its name.
func formSecretSet(paths runtimePaths, endpoint string, body []byte) (refusal, err error) {
	payload, _ := decodeObject(body)
	method, _ := payload["method"].(string)
	target, _ := payload["path"].(string)
	target, _, _ = strings.Cut(target, "#")
	target, _, _ = strings.Cut(target, "?")
	m := rawFlowPath.FindStringSubmatch(target)
	if endpoint != "core" || strings.ToUpper(strings.TrimSpace(method)) != http.MethodPost || m == nil || m[2] == "" {
		return nil, nil
	}
	forms, err := readFlowForms(paths)
	if err != nil {
		return nil, err
	}
	fields, _ := payload["body"].(map[string]any)
	scopes := []any{fields}
	for _, value := range fields {
		scopes = append(scopes, value)
	}
	for _, name := range forms[m[2][1:]].Secrets {
		for _, scope := range scopes {
			if value, _ := scope.(map[string]any); value[name] != nil && value[name] != "" {
				return rawFlowSecret{field: name, path: target}, nil
			}
		}
	}
	return nil, nil
}

// flowAnswered keeps the secret fields of the form a flow request at target
// got back (none, or no form: the flow is forgotten) and returns the
// hand-off when it asks for one; unkept says why the forms could not be
// kept (noteFlowForms).
func flowAnswered(paths runtimePaths, target string, data json.RawMessage) (asks *rawFlowSecret, unkept error) {
	m := rawFlowPath.FindStringSubmatch(target)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	body := jsonFields(jsonFields(v)["body"])
	flowID, _ := body["flow_id"].(string)
	if m == nil || err != nil || flowID == "" {
		return nil, nil
	}
	var secrets []string
	if schema, _ := body["data_schema"].([]any); body["type"] == "form" {
		secrets = formSecretFields(schema)
	}
	if len(secrets) > 0 {
		asks = &rawFlowSecret{field: secrets[0], path: m[1] + "/" + flowID}
	}
	forms, err := readFlowForms(paths)
	if err != nil {
		return asks, err
	}
	delete(forms, flowID)
	if len(secrets) > 0 {
		forms[flowID] = flowForm{Secrets: secrets, At: verbNow()}
	}
	for len(forms) > 50 {
		oldest := ""
		for id, form := range forms {
			if oldest == "" || form.At.Before(forms[oldest].At) {
				oldest = id
			}
		}
		delete(forms, oldest)
	}
	out, err := json.Marshal(forms)
	if err == nil {
		err = os.MkdirAll(paths.CacheDir, 0o700)
	}
	if err == nil {
		err = writeFileAtomic(flowFormsPath(paths), out, 0o600)
	}
	return asks, err
}

// noteFlowForms says once in a raw plan's result that its flow steps met
// the name check only, when the flow forms HA NOVA keeps could not be read
// or kept.
func noteFlowForms(s *relaySession, env *verbEnvelope) {
	if s.flowForms == nil || slices.ContainsFunc(env.Notices, func(n verbNotice) bool { return n.Kind == "flow_forms" }) {
		return
	}
	env.Notices = append(env.Notices, verbNotice{Kind: "flow_forms",
		Text: fmt.Sprintf("HA NOVA couldn't read or keep the setup forms it records (%v): flow steps are checked by field names only", s.flowForms)})
}

// formSecretFields: the secret fields of a serialized data_schema, in a
// collapsible section too: one marked secret or a password box, or a text
// field whose name says it is (secretFlowField); a number, toggle, or choice
// is never one by its name.
func formSecretFields(fields []any) []string {
	var names []string
	for _, x := range fields {
		field := jsonFields(x)
		selector := jsonFields(field["selector"])
		_, text := selector["text"]
		name, _ := field["name"].(string)
		switch section, ok := field["schema"].([]any); {
		case ok && field["type"] == "expandable":
			names = append(names, formSecretFields(section)...)
		case name == "":
		case field["secret"] == true || jsonFields(selector["text"])["type"] == "password",
			(field["type"] == "string" || text) && secretFlowField(field):
			names = append(names, name)
		}
	}
	return names
}
