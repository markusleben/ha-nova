package main

import (
	"bytes"
	"encoding/json"
)

// redactPhoneWebhooks hides Companion-app webhook ids in a raw Relay
// response. A mobile_app entity's registry unique_id starts with its phone's
// webhook id, and that id alone lets anyone act as the phone without a
// login: fire a confirming tap as its user, redirect its pushes, or call
// services (confirm_phone.go). Nothing the skills read raw needs it.
func redactPhoneWebhooks(body []byte) []byte {
	if !bytes.Contains(body, []byte("mobile_app")) {
		return body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil || !redactPhoneValue(v) {
		return body
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return body
	}
	return bytes.TrimRight(out.Bytes(), "\n")
}

func redactPhoneValue(v any) bool {
	changed := false
	switch t := v.(type) {
	case map[string]any:
		if t["platform"] == "mobile_app" {
			for _, key := range []string{"unique_id", "previous_unique_id"} {
				if id, ok := t[key].(string); ok && id != "" {
					t[key], changed = "(hidden: holds the phone's webhook id)", true
				}
			}
		}
		for _, x := range t {
			changed = redactPhoneValue(x) || changed
		}
	case []any:
		for _, x := range t {
			changed = redactPhoneValue(x) || changed
		}
	}
	return changed
}
