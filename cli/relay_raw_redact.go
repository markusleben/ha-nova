package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

// redactPhoneSecrets hides what lets anyone act as a Companion phone without
// a login (confirm_phone.go): its webhook id — the prefix of a mobile_app
// entity's registry unique_id, and data.webhook_id of the app's notification
// events (Android sends it; automation traces keep the event) — and, with
// nonces, a phone confirmation's live action ids. A webhook id lets anyone
// fire a tap as the phone's user, redirect its pushes, or call services.
// Nothing the skills or verbs read needs either. Other webhook ids (an
// automation's webhook trigger) stay.
func redactPhoneSecrets(body []byte, nonces bool) []byte {
	if !bytes.Contains(body, []byte("mobile_app")) && !bytes.Contains(body, []byte("ios.notification")) && !(nonces && bytes.Contains(body, []byte("HANOVA_"))) {
		return body
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return body
	}
	v, changed := redactPhoneValue(v, nonces)
	if !changed {
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

const phoneSecretHidden = "(hidden: a phone's webhook id or confirmation id)"

func redactPhoneValue(v any, nonces bool) (any, bool) {
	changed := false
	switch t := v.(type) {
	case string:
		if nonces && strings.Contains(t, "HANOVA_") {
			return phoneSecretHidden, true
		}
	case map[string]any:
		if t["platform"] == "mobile_app" {
			for _, key := range []string{"unique_id", "previous_unique_id"} {
				if id, ok := t[key].(string); ok && id != "" {
					t[key], changed = phoneSecretHidden, true
				}
			}
		}
		if eventType, ok := t["event_type"].(string); ok && phoneEventName(eventType) {
			if data, ok := t["data"].(map[string]any); ok && data["webhook_id"] != nil {
				data["webhook_id"], changed = phoneSecretHidden, true
			}
		}
		for key, x := range t {
			if redacted, did := redactPhoneValue(x, nonces); did {
				t[key], changed = redacted, true
			}
		}
	case []any:
		for i, x := range t {
			if redacted, did := redactPhoneValue(x, nonces); did {
				t[i], changed = redacted, true
			}
		}
	}
	return v, changed
}
