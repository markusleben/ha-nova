package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
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

// next: the setup isn't finished; an options step can leave the secret
// out, any other flow's step is completed in Home Assistant.
func (r rawFlowSecret) next() string {
	never := "The setup isn't finished: its next step needs " + r.field + ", a password or other secret, and secrets don't belong in a chat: never ask the user for " + r.field + " here. "
	if strings.HasPrefix(r.path, "/api/config/config_entries/options/") {
		return never + "Leave " + r.field + " out of the step, or tell them to complete that step in Home Assistant under " + r.place() + " and cancel this flow with a plan (core DELETE " + r.path + ")."
	}
	return never + "Tell them to complete that step in Home Assistant under " + r.place() +
		". If you started this flow, cancel it with a plan (core DELETE " + r.path + ") so they start it there; leave one Home Assistant started (a discovered device, a reauthentication) as it is: it waits there for them."
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
		if section, ok := fields[key].(map[string]any); ok {
			if field := flowSecretSet(section); field != "" {
				return field
			}
		} else if secretFlowValue(key, fields[key]) {
			return key
		}
	}
	return ""
}

// secretFlowValue: value, set under key, is a secret: a non-empty string
// whose name says so (secretFlowField) or a URL with a password, or a
// number for a PIN, code, key, or password.
func secretFlowValue(key string, value any) bool {
	name := strings.ToLower(key)
	switch value := value.(type) {
	case string:
		text := strings.TrimSpace(strings.TrimLeftFunc(urlDropped.Replace(value), func(r rune) bool { return r <= ' ' })) // as urlsplit reads it
		password := secretURLCutPassword.MatchString(text)
		if u, err := url.Parse(text); err != nil {
			password = password || secretURLPassword.MatchString(text) // one Go can't parse: mqtt://u:p#ss@broker
		} else if !password {
			_, password = u.User.Password() // rtsp://admin:hunter2@camera, whatever the field's name
		}
		return value != "" && (password || secretFlowField(map[string]any{"name": key}))
	case json.Number: // a token or PSK is never a number
		return secretFlowNames[name] || strings.Contains(name, "password") || strings.Contains(name, "passphrase") ||
			slices.ContainsFunc(secretFlowSuffixes, func(suffix string) bool { return strings.HasSuffix(name, suffix) })
	}
	return false
}

// flowFormFile: a file that keeps the secret fields of a form a flow
// answered a plan's apply with at a time (flowAnswered). Each answer gets
// its own file and the flow's newest counts, so two applies at once never
// write over each other's, and a prune by the time in the name never
// removes a newer file than it saw.
func flowFormFile(paths runtimePaths, flowID string, at time.Time) string {
	return filepath.Join(flowFormDir(paths), fmt.Sprintf("%s.%020d.json", flowID, at.UnixNano()))
}

// flowFormDir holds every flow's files; uninstall removes it.
func flowFormDir(paths runtimePaths) string { return filepath.Join(paths.CacheDir, "flow-forms") }

// flowFormTime: the time in the name of a flow's file, or of a temporary
// file writing one; false for any other name.
func flowFormTime(name string) (time.Time, bool) {
	name, _, _ = strings.Cut(name, ".tmp-")
	stem, isForm := strings.CutSuffix(name, ".json")
	_, at, _ := strings.Cut(stem, ".")
	nanos, err := strconv.ParseInt(at, 10, 64)
	return time.Unix(0, nanos), isForm && err == nil
}

// flowFormFiles: a flow's files, oldest first; none without the folder.
func flowFormFiles(paths runtimePaths, flowID string) ([]string, error) {
	dir := flowFormDir(paths)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	var files []string
	for _, entry := range entries {
		if name := entry.Name(); strings.HasPrefix(name, flowID+".") && strings.HasSuffix(name, ".json") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files, err
}

// secretURLPassword: a URL with user info and a password that url.Parse
// refuses (a #, /, {, space, or non-ASCII letter in the password), read as
// broadly as Home Assistant's urlsplit, which keeps spaces there.
// secretURLCutPassword: one Go parses as a host and port because the
// password has a # or ? after its digits, or starts with /
// (https://admin:2024#pw@host), which the stored URL still holds.
// urlDropped: what urlsplit removes before it reads a URL.
var (
	secretURLPassword    = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://[^/?#]*:[^@]*@`)
	secretURLCutPassword = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://[^/?#@]*:(?:[0-9]*[#?]|/)[^/?#@]*@`)
	urlDropped           = strings.NewReplacer("\t", "", "\r", "", "\n", "")
)

// flowIDPattern: a flow id as Home Assistant makes it, safe as a file name.
var flowIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// readFlowForm: the paths of a flow's secret fields in its newest file (a
// section's name, then the field's); none without a file or with one
// holding null; an unreadable file is an error.
func readFlowForm(paths runtimePaths, flowID string) ([][]string, error) {
	files, err := flowFormFiles(paths, flowID)
	if err != nil || len(files) == 0 {
		return nil, err
	}
	data, err := os.ReadFile(files[len(files)-1])
	switch {
	case errors.Is(err, fs.ErrNotExist): // pruned since: untouched for 30 days
		return nil, nil
	case err != nil:
		return nil, err
	}
	var secrets [][]string
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("%s: %w", files[len(files)-1], err)
	}
	return secrets, nil
}

// formSecretSet: a flow step POST that fills a field its flow's last form
// marked secret, in that field's section, whatever its name. Forms it can't
// read stop the step, and so does a form this plan couldn't keep (unkept,
// by flow step path): nothing is planned or sent.
func formSecretSet(paths runtimePaths, unkept map[string]error, endpoint string, body []byte) (refusal, err error) {
	payload, _ := decodeObject(body)
	method, _ := payload["method"].(string)
	raw, _ := payload["path"].(string)
	target, _ := relayCorePath(raw)
	m := rawFlowPath.FindStringSubmatch(target)
	if endpoint != "core" || strings.ToUpper(strings.TrimSpace(method)) != http.MethodPost || m == nil || m[2] == "" {
		return nil, nil
	}
	if why := unkept[target]; why != nil {
		return rawRefusal("flow step", fmt.Sprintf("HA NOVA couldn't keep the setup form this flow answered with (%v), so this plan sends it nothing more; "+
			"the setup isn't finished: the user completes this step in Home Assistant under %s", why, rawFlowSecret{path: target}.place())), nil
	}
	secrets, err := readFlowForm(paths, m[2][1:])
	if err != nil {
		return nil, fmt.Errorf("cannot read the setup forms HA NOVA keeps (%w), so nothing was sent; the setup isn't finished: the user completes this step in Home Assistant under %s",
			err, rawFlowSecret{path: target}.place())
	}
	for _, field := range secrets {
		value := payload["body"]
		for _, key := range field {
			inner, _ := value.(map[string]any)
			value = inner[key]
		}
		if len(field) > 0 && value != nil && value != "" {
			return rawFlowSecret{field: field[len(field)-1], path: target}, nil
		}
	}
	return nil, nil
}

// flowAnswered keeps the secret fields of the form a flow request at the
// core path raw got back in a newer file (none: null, once the flow has a
// file) and returns the flow's step path and the hand-off when that form
// asks for one. A flow that ends (create_entry, abort) or is cancelled
// (DELETE) loses its files. unkept says why they could not be kept
// (noteFlowForms).
func flowAnswered(paths runtimePaths, method, raw string, data json.RawMessage) (flow string, asks *rawFlowSecret, unkept error) {
	target, _ := relayCorePath(raw)
	m := rawFlowPath.FindStringSubmatch(target)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	body := jsonFields(jsonFields(v)["body"])
	flowID, _ := body["flow_id"].(string)
	switch {
	case m != nil && m[2] != "" && strings.EqualFold(method, http.MethodDelete):
		flowID, body = m[2][1:], map[string]any{"type": "abort"}
	case m == nil || err != nil || !flowIDPattern.MatchString(flowID):
		return "", nil, nil
	}
	flow = m[1] + "/" + flowID
	files, err := flowFormFiles(paths, flowID)
	var secrets [][]string
	switch schema, _ := body["data_schema"].([]any); body["type"] {
	case "create_entry", "abort":
		for _, file := range files {
			if gone := os.Remove(file); gone != nil && !errors.Is(gone, fs.ErrNotExist) {
				err = gone
			}
		}
		return flow, nil, err
	case "form":
		secrets = formSecretFields(schema)
	}
	if len(secrets) > 0 {
		asks = &rawFlowSecret{field: secrets[0][len(secrets[0])-1], path: flow}
	} else if err == nil && len(files) == 0 {
		return flow, nil, nil
	}
	at := time.Now()
	if len(files) > 0 {
		if newest, named := flowFormTime(filepath.Base(files[len(files)-1])); named && !at.After(newest) { // a clock set back never outranks a newer form
			at = newest.Add(time.Nanosecond)
		}
	}
	file := flowFormFile(paths, flowID, at)
	out, err := json.Marshal(secrets)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(file), 0o700)
	}
	if err == nil {
		err = writeFileAtomic(file, out, 0o600)
	}
	if err == nil {
		err = pruneFlowForms(filepath.Dir(file))
	}
	return flow, asks, err
}

// pruneFlowForms removes the files written more than 30 days ago, and the
// temporary files a write left behind an hour ago, by the time in their
// names.
func pruneFlowForms(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		kept := 30 * 24 * time.Hour
		if strings.Contains(entry.Name(), ".tmp-") {
			kept = time.Hour
		}
		if at, named := flowFormTime(entry.Name()); !named || time.Since(at) < kept {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// relayFlowAnswered is flowAnswered for ha-nova relay core, on the reply
// before redaction: the request was sent, so a form it can't keep is said.
func relayFlowAnswered(paths runtimePaths, method, target string, reply []byte) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(reply, &envelope) != nil {
		return
	}
	if _, _, unkept := flowAnswered(paths, strings.TrimSpace(method), target, envelope.Data); unkept != nil {
		printWarn("HA NOVA couldn't keep the setup form this flow answered with (%v): its next step can't be checked against that form", unkept)
	}
}

// noteFlowForms says once in a raw plan's result that a flow's form could
// not be kept: the plan sent that flow nothing more (formSecretSet).
func noteFlowForms(s *relaySession, env *verbEnvelope) {
	if len(s.flowForms) == 0 || slices.ContainsFunc(env.Notices, func(n verbNotice) bool { return n.Kind == "flow_forms" }) {
		return
	}
	env.Notices = append(env.Notices, verbNotice{Kind: "flow_forms",
		Text: fmt.Sprintf("HA NOVA couldn't keep the setup form a flow answered with (%v): this plan sent that flow nothing more, and a later plan can't check its steps against that form",
			s.flowForms[slices.Min(slices.Collect(maps.Keys(s.flowForms)))])})
}

// formSecretFields: the paths of a serialized data_schema's secret fields
// (in a collapsible section, its name first): one marked secret or a
// password box, or a text field whose name says it is (secretFlowField); a
// number, toggle, or choice is never one by its name.
func formSecretFields(fields []any) [][]string {
	var found [][]string
	for _, x := range fields {
		field := jsonFields(x)
		selector := jsonFields(field["selector"])
		_, text := selector["text"]
		name, _ := field["name"].(string)
		switch section, ok := field["schema"].([]any); {
		case name == "":
		case ok && field["type"] == "expandable":
			for _, inner := range formSecretFields(section) {
				found = append(found, append([]string{name}, inner...))
			}
		case field["secret"] == true || jsonFields(selector["text"])["type"] == "password",
			(field["type"] == "string" || text) && secretFlowField(field):
			found = append(found, []string{name})
		}
	}
	return found
}
