package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Deltas: what a call plan changes on each frozen target, before → after,
// for the preview and its say.

var serviceAfterState = map[string]string{
	"turn_on": "on", "turn_off": "off", "lock": "locked", "unlock": "unlocked", "open": "open",
	"open_cover": "open", "close_cover": "closed", "alarm_disarm": "disarmed", "alarm_trigger": "triggered",
}

// planDeltas renders before → after per frozen target: brightness in %,
// unavailable/unknown warnings. This is the rule's source since the
// service-call skill moved to `ha-nova call` (formerly its state-delta rules).
func planDeltas(service string, frozen []string, data map[string]any, states map[string]*entityState) []planDelta {
	var deltas []planDelta
	for _, id := range frozen {
		st := states[id]
		if st == nil {
			continue
		}
		// The state a target ends in follows the service it really gets
		// (homeassistant.turn_on on a cover opens it); scenes and scripts
		// have no on/off end state.
		domain, _ := splitID(id)
		_, name := splitID(effectiveService(service, domain))
		after := serviceAfterState[name]
		if strings.HasPrefix(name, "alarm_arm_") {
			after = "armed_" + strings.TrimPrefix(name, "alarm_arm_")
		}
		if domain == "scene" || domain == "script" || (name == "turn_on" || name == "turn_off") && !onOffDomains[domain] {
			after = ""
		}
		warning := map[string]string{"unavailable": "Device is offline or unreachable.", "unknown": "State not yet known; the call may still work."}[st.State]
		if after != "" || warning != "" {
			deltas = append(deltas, planDelta{EntityID: id, Field: "state", Before: st.State, After: after, Warning: warning})
		}
		for _, key := range sortedKeys(data) {
			d := planDelta{EntityID: id, Field: key, Before: deltaValue(beforeValue(id, key, st)), After: deltaValue(data[key])}
			switch key {
			case "brightness_pct":
				d.Field, d.Before, d.After = "brightness", brightnessPct(st), d.After+"%"
			case "brightness":
				d.Before, d.After = brightnessPct(st), brightnessPct(&entityState{State: "on", Attributes: map[string]any{"brightness": data[key]}})
			case "position", "tilt_position":
				d.Before, d.After = withPercent(d.Before), withPercent(d.After)
			}
			deltas = append(deltas, d)
		}
	}
	return deltas
}

// stateBackedField: a service field whose current value IS the entity state
// (select option, number/text value, climate hvac_mode). Verification, the
// drift basis, and deltas all read it from st.State.
func stateBackedField(domain, key string) bool {
	switch key {
	case "option":
		return domain == "select" || domain == "input_select"
	case "value":
		return domain == "number" || domain == "input_number" || domain == "text" || domain == "input_text"
	case "hvac_mode":
		return domain == "climate"
	}
	return false
}

// beforeValue is the current value of the field a call sets on entityID.
func beforeValue(entityID, key string, st *entityState) any {
	if domain, _ := splitID(entityID); stateBackedField(domain, key) {
		if st == nil {
			return nil
		}
		return st.State
	}
	return st.attr(deltaAttribute(key))
}

// deltaAttribute names the state attribute a service field changes.
func deltaAttribute(field string) string {
	switch field {
	case "brightness_pct":
		return "brightness"
	case "position":
		return "current_position"
	case "tilt_position":
		return "current_tilt_position"
	}
	return field
}

// brightnessPct shows HA's 0–255 as %; a light that is off counts as 0%,
// a missing value on a light that is on is "unknown".
func brightnessPct(st *entityState) string {
	n, isNumber := st.attr("brightness").(float64)
	switch {
	case isNumber:
		return fmt.Sprintf("%d%%", int(math.Round(n/255*100)))
	case st != nil && st.State == "off":
		return "0%"
	}
	return "unknown"
}

func withPercent(v string) string {
	if v == "" {
		return "unknown"
	}
	return v + "%"
}

func deltaValue(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}
