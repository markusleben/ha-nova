package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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
		name := strings.ToLower(key)
		switch value := fields[key].(type) {
		case map[string]any:
			if field := flowSecretSet(value); field != "" {
				return field
			}
		case string:
			var password bool
			if u, err := url.Parse(strings.TrimSpace(value)); err == nil {
				_, password = u.User.Password() // rtsp://admin:hunter2@camera, whatever the field's name
			}
			if value != "" && (password || secretFlowField(map[string]any{"name": key})) {
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
// read stop the step: nothing is planned or sent.
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

// flowAnswered keeps the secret fields of the form a flow request at target
// got back in a newer file (none: null, once the flow has a file) and
// returns the flow's step path and the hand-off when that form asks for one.
// A flow that ends (create_entry, abort) or is cancelled (DELETE) loses its
// files. unkept says why they could not be kept (noteFlowForms).
func flowAnswered(paths runtimePaths, method, target string, data json.RawMessage) (flow string, asks *rawFlowSecret, unkept error) {
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

// noteFlowForms says once in a raw plan's result that a flow's form could
// not be kept, so its later steps are checked by field names only.
func noteFlowForms(s *relaySession, env *verbEnvelope) {
	if s.flowForms == nil || slices.ContainsFunc(env.Notices, func(n verbNotice) bool { return n.Kind == "flow_forms" }) {
		return
	}
	env.Notices = append(env.Notices, verbNotice{Kind: "flow_forms",
		Text: fmt.Sprintf("HA NOVA couldn't keep the setup form a flow answered with (%v): that flow's later steps are checked by field names only", s.flowForms)})
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
