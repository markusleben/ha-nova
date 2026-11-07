package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// An App settings change never sets a password or other secret HA NOVA
// recognizes (skills/updates/apps.md → Settings), as a setup or repair flow
// step doesn't (relay_raw_flow.go): one set by its name or a URL's password
// is refused in checkRawSupervisor, and one that fills an option the App's
// own form marks a password, read live, at each check of a raw plan
// (guardRawRequestIn). The user enters it in the App's settings instead.

// rawSupervisorFormSecret: an options change that fills an option the App's
// own form marks a password, read live before each check (a raw plan's
// planning and apply); a form that can't be read sends nothing.
func rawSupervisorFormSecret(s *relaySession, body []byte) (refusal, err error) {
	payload, _ := decodeObject(body)
	method, _ := payload["method"].(string)
	path, _ := payload["path"].(string)
	data, _ := payload["data"].(map[string]any)
	kind, slug := rawSupervisorMatch(method, path)
	if kind != "options" || rawSupervisorOptionsKind(data) != "options" {
		return nil, nil
	}
	form, err := rawSupervisorForm(s, slug)
	if err != nil {
		return nil, errors.New("cannot read the App's stored options, so nothing was sent: " + err.Error())
	}
	return rawAppOptionSecret(data["options"], form), nil
}

// rawAppOptionSecret refuses App options that set a password or other secret
// (appOptionSecret; form: the App's, nil when unknown): the user enters it
// in Home Assistant, never in a chat.
func rawAppOptionSecret(options any, form []any) error {
	field := appOptionSecret("", options, map[string]any{"schema": form})
	if field == "" {
		return nil
	}
	return rawRefusal("App options change that sets "+strconv.Quote(field)+" (a password or other secret)", "secrets don't belong in a chat; set that option in the App's Configuration tab")
}

// appOptionSecret: the option, at any depth and in lists too, that value
// sets to a secret: one its name says is (appOptionNamedSecret; an
// environment entry, {name, value} or NAME=value, by its name), or a value
// for a field node, its place in the App's form, marks format: password.
// "" for none.
func appOptionSecret(key string, value any, node map[string]any) string {
	form, _ := node["schema"].([]any)
	switch value := value.(type) {
	case map[string]any:
		for _, name := range sortedKeys(value) {
			var field map[string]any
			for _, x := range form {
				if f, _ := x.(map[string]any); f["name"] == name {
					field = f
				}
			}
			named := name
			if entry, _ := value["name"].(string); name == "value" && entry != "" { // {name: DB_PASSWORD, value: …}
				named = entry
			}
			if found := appOptionSecret(named, value[name], field); found != "" {
				return found
			}
		}
	case []any:
		for _, item := range value {
			if found := appOptionSecret(key, item, node); found != "" {
				return found
			}
		}
	case string, json.Number:
		text, _ := value.(string)
		entry := appOptionEnvEntry.FindStringSubmatch(text)
		switch {
		case node["format"] == "password" && value != "", appOptionNamedSecret(key, value):
			return key
		case entry != nil && appOptionNamedSecret(entry[1], entry[2]): // lego_env: ["NETCUP_API_PASSWORD=…"]
			return entry[1]
		}
	}
	return ""
}

var (
	appOptionPassName = regexp.MustCompile(`(pass|pw|pwd)$`)
	appOptionEnvEntry = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
)

// appOptionNamedSecret: secretFlowValue by an App option's name, camelCase
// split first (eurodns_apiKey reads as eurodns_api_key), and a string under
// a name ending in pass, pw, or pwd (he_pass, dbpass, mqtt_pwd), as the
// preview hides them (rawSupervisorSecretKey).
func appOptionNamedSecret(key string, value any) bool {
	name := strings.ToLower(rawSupervisorCamel.ReplaceAllString(key, "${1}_${2}"))
	text, _ := value.(string)
	return secretFlowValue(name, value) || text != "" && appOptionPassName.MatchString(name)
}

// rawSupervisorForm: the App's options form as the Supervisor serializes it
// (/addons/<slug>/info → schema), read live; nil for an App whose options
// have none (schema: false in its config, which the Supervisor sends as null).
func rawSupervisorForm(s *relaySession, slug string) ([]any, error) {
	var info struct {
		Status int `json:"status"`
		Body   struct {
			Data struct {
				Schema json.RawMessage `json:"schema"`
			} `json:"data"`
		} `json:"body"`
	}
	request, _ := json.Marshal(map[string]string{"method": "GET", "path": "/addons/" + slug + "/info"})
	raw, err := s.sendWithin("supervisor", request, relaySessionRequestTimeout)
	if err == nil {
		err = json.Unmarshal(raw, &info)
	}
	switch {
	case err != nil:
		return nil, err
	case info.Status != 200:
		return nil, fmt.Errorf("the Supervisor answered HTTP %d", info.Status)
	case info.Body.Data.Schema == nil:
		return nil, errors.New("the App's info holds no schema")
	}
	var form []any
	if err := json.Unmarshal(info.Body.Data.Schema, &form); err != nil {
		return nil, err
	}
	return form, nil
}
