package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// redactSecrets hides, in every Relay response before an agent, --jq, a file,
// or a snapshot sees it:
//   - what lets anyone act as a Companion phone without a login
//     (confirm_phone.go): its webhook id — the prefix of a mobile_app entity's
//     registry unique_id, and data.webhook_id of the app's notification events
//     (Android sends it; automation traces keep the event) — and, with nonces,
//     a phone confirmation's live action ids. A webhook id lets anyone fire a
//     tap as the phone's user, redirect its pushes, or call services. Other
//     webhook ids (an automation's webhook trigger) stay.
//   - stored codes, passwords, and keys: a lock's or alarm panel's
//     default_code (entity registry options, also in the old values of
//     entity_registry_updated events), the backup encryption password
//     (backup/config/info), the Thread network dataset (otbr/info), an MQTT
//     alarm panel's code in its discovery payload (mqtt/device/debug_info),
//     and the current value of a secret field in a config or options flow
//     form.
//
// A set value becomes a fixed marker, an empty one stays empty, so "is a code
// set" stays readable. Nothing the skills or verbs read needs the values;
// writes that carry a marker back are refused (holdsHiddenValue).
func redactSecrets(body []byte, nonces bool) []byte {
	if !bytes.Contains(body, []byte("mobile_app")) && !bytes.Contains(body, []byte("ios.notification")) &&
		!(nonces && bytes.Contains(body, []byte("HANOVA_"))) && !bytes.Contains(body, []byte("default_code")) &&
		!bytes.Contains(body, []byte("create_backup")) && !bytes.Contains(body, []byte("active_dataset_tlvs")) &&
		!bytes.Contains(body, []byte("data_schema")) && !bytes.Contains(body, []byte("discovery_data")) &&
		!bytes.Contains(body, []byte(`/config"`)) &&
		!bytes.Contains(body, []byte(`\u`)) { // an escaped key spells none of these
		return body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	if err != nil {
		return body
	}
	if _, err := dec.Token(); err != io.EOF { // trailing data: never rewrite a malformed response into a valid one
		return body
	}
	v, changed := redactValue(v, nonces)
	if !changed {
		return body
	}
	out, err := marshalPlain(v)
	if err != nil {
		return body
	}
	return out
}

// jsonObject keeps an object's key order, so a redacted config keeps its
// order (variables run top-down; snapshot verify compares order).
type jsonObject struct {
	keys []string
	vals map[string]any
	dup  bool // the source repeated a key
}

func (o *jsonObject) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := marshalPlain(key)
		v, err := marshalPlain(o.vals[key])
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func marshalPlain(v any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

// decodeOrdered decodes one JSON value; objects become *jsonObject.
func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch tok {
	case json.Delim('{'):
		o := &jsonObject{vals: map[string]any{}}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, _ := keyTok.(string)
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			if _, dup := o.vals[key]; dup {
				o.dup = true // re-encoded, so no hidden duplicate survives
			} else {
				o.keys = append(o.keys, key)
			}
			o.vals[key] = v
		}
		_, err = dec.Token()
		return o, err
	case json.Delim('['):
		list := []any{}
		for dec.More() {
			v, err := decodeOrdered(dec)
			if err != nil {
				return nil, err
			}
			list = append(list, v)
		}
		_, err = dec.Token()
		return list, err
	}
	return tok, nil
}

// jsonFields returns an object's fields, or nil.
func jsonFields(v any) map[string]any {
	if o, ok := v.(*jsonObject); ok {
		return o.vals
	}
	return nil
}

const (
	phoneSecretHidden = "(hidden: a phone's webhook id or confirmation id)"
	secretHidden      = "(hidden: a stored code, password, or key)"
)

// secretFlowNames, secretFlowParts, secretFlowSuffixes: flow form fields
// whose current value is a secret even without a password selector or
// Secret() flag. No bare "_code" suffix: country_code and zip_code are not
// secrets; only string values are hidden, so max_tokens or use_psk stay.
var (
	secretFlowNames = map[string]bool{"code": true, "pin": true, "key": true, "apikey": true, "bindkey": true, "passkey": true,
		"default_code": true, "pin_code": true, "user_code": true, "usercode": true, "installer_code": true, "master_code": true,
		"alarm_code": true, "disarm_code": true, "arm_code": true, "access_code": true}
	secretFlowParts    = []string{"password", "passphrase", "secret", "token", "psk"}
	secretFlowSuffixes = []string{"_key", "_pin"}
)

// hideSet replaces m[key] with the marker when it is set (not null, not "").
func hideSet(m map[string]any, key string) bool {
	if x, ok := m[key]; ok && x != nil && x != "" && x != secretHidden {
		m[key] = secretHidden
		return true
	}
	return false
}

func redactValue(v any, nonces bool) (any, bool) {
	changed := false
	switch t := v.(type) {
	case string:
		if nonces && strings.Contains(t, "HANOVA_") {
			return phoneSecretHidden, true
		}
	case *jsonObject:
		changed = redactObject(t.vals, nonces) || t.dup
	case []any:
		for i, x := range t {
			if redacted, did := redactValue(x, nonces); did {
				t[i], changed = redacted, true
			}
		}
	}
	return v, changed
}

// redactObject hides the secrets in one object and its children.
func redactObject(t map[string]any, nonces bool) bool {
	changed := false
	if t["platform"] == "mobile_app" {
		for _, key := range []string{"unique_id", "previous_unique_id"} {
			if id, ok := t[key].(string); ok && id != "" {
				t[key], changed = phoneSecretHidden, true
			}
		}
	}
	if eventType, ok := t["event_type"].(string); ok && phoneEventName(eventType) {
		if data := jsonFields(t["data"]); data != nil && data["webhook_id"] != nil {
			data["webhook_id"], changed = phoneSecretHidden, true
		}
	}
	changed = hideSet(t, "default_code") || changed
	changed = hideSet(t, "active_dataset_tlvs") || changed
	if backup := jsonFields(t["create_backup"]); backup != nil {
		changed = hideSet(backup, "password") || changed
	}
	if fields, ok := t["data_schema"].([]any); ok { // a config or options flow form
		changed = hideFlowSecrets(fields) || changed
	}
	if payload := jsonFields(jsonFields(t["discovery_data"])["payload"]); payload != nil { // mqtt/device/debug_info: an MQTT alarm panel's code
		changed = hideSet(payload, "code") || changed
	}
	if topic, _ := t["topic"].(string); strings.HasSuffix(topic, "/config") { // a retained discovery message from mqtt/subscribe
		if payload, ok := t["payload"].(string); ok {
			if hidden, did := hideDiscoveryCode(payload); did {
				t["payload"], changed = hidden, true
			}
		}
	}
	for key, x := range t {
		if redacted, did := redactValue(x, nonces); did {
			t[key], changed = redacted, true
		}
	}
	return changed
}

// hideDiscoveryCode hides an MQTT discovery config's code, also in a
// device config's components (cmps); the payload stays a JSON string.
func hideDiscoveryCode(payload string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(payload))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	config := jsonFields(v)
	if err != nil || config == nil {
		return payload, false
	}
	changed := hideSet(config, "code")
	if cmps := jsonFields(config["cmps"]); cmps != nil {
		for _, c := range cmps {
			if component := jsonFields(c); component != nil {
				changed = hideSet(component, "code") || changed
			}
		}
	}
	out, err := marshalPlain(v)
	if !changed || err != nil {
		return payload, false
	}
	return string(out), true
}

// hideFlowSecrets hides the current value of each secret field of a
// serialized data_schema, inside a collapsible section too (HA nests none
// deeper).
func hideFlowSecrets(fields []any) bool {
	changed := false
	for _, x := range fields {
		field := jsonFields(x)
		if field == nil {
			continue
		}
		if section, ok := field["schema"].([]any); ok && field["type"] == "expandable" {
			changed = hideFlowSecrets(section) || changed
			continue
		}
		if !secretFlowField(field) {
			continue
		}
		if _, isString := field["default"].(string); isString {
			changed = hideSet(field, "default") || changed
		}
		if desc := jsonFields(field["description"]); desc != nil {
			if _, isString := desc["suggested_value"].(string); isString {
				changed = hideSet(desc, "suggested_value") || changed
			}
		}
	}
	return changed
}

// secretFlowField: one serialized data_schema field that holds a secret: a
// Secret() field, a password text selector, or a secret field name.
func secretFlowField(m map[string]any) bool {
	if m["secret"] == true {
		return true
	}
	if text := jsonFields(jsonFields(m["selector"])["text"]); text != nil && text["type"] == "password" {
		return true
	}
	name, _ := m["name"].(string)
	name = strings.ToLower(name)
	for _, suffix := range []string{"_id", "_url", "_file", "_filename"} {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	for _, part := range secretFlowParts {
		if strings.Contains(name, part) {
			return true
		}
	}
	for _, suffix := range secretFlowSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return secretFlowNames[name]
}

// hiddenValueNext: what to do instead of writing a hidden marker back,
// which would replace the real value (a lock's code, a password).
const hiddenValueNext = "it holds a value HA NOVA hid, and sending it would replace the real code, password, or key. " +
	"For a lock's or alarm panel's options, leave out options_domain and options entirely; " +
	"for a backup, leave out password (the backup gets none); for a setup or options form, stop and send the user to the Home Assistant UI"

// holdsHiddenValue: the decoded body carries a hidden marker (decoded, so a
// \u escape cannot hide one; UseNumber, so 1e1000 still decodes).
func holdsHiddenValue(body []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return false // not JSON: the Relay rejects it
	}
	plain, err := marshalPlain(v)
	return err != nil || bytes.Contains(plain, []byte(secretHidden)) || bytes.Contains(plain, []byte(phoneSecretHidden))
}
