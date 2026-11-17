package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

// boundHome: callHome plus MAR-209's shape, a watchdog on every call_service
// (no filter) whose script opens, closes, and positions covers it picks by
// template. sequence replaces the script's steps.
func boundHome(t *testing.T, sequence ...any) (*writeHarness, *fakeHA) {
	t.Helper()
	h, f := callHome(t)
	if sequence == nil {
		sequence = []any{step("cover.open_cover", "{{ covers }}"), step("cover.close_cover", "{{ covers }}"),
			map[string]any{"action": "cover.set_cover_position", "target": map[string]any{"entity_id": "{{ covers }}"}, "data": map[string]any{"position": "{{ position }}"}}}
	}
	f.script("check_group", sequence...)
	listen(f, "rolladen_watchdog", map[string]any{}, map[string]any{"action": "script.check_group"})
	return h, f
}

// gateLight: the App's check of the same light call.
func gateLight(t *testing.T, h *writeHarness) gateCheck {
	t.Helper()
	var s *relaySession
	defer func() {
		if s != nil {
			s.close()
		}
	}()
	body := []byte("{\"method\":\"POST\",\"path\":\"/api/services/light/turn_off\",\"body\":{\"entity_id\":\"light.kitchen\"}}")
	check, err := gateVerdict("core", body, func() (*relaySession, error) {
		opened, err := newRelaySession(h.paths, verbOptions{})
		s = opened
		return opened, err
	})
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	return check
}

// D6: a templated target is rated by every entity of its domain; only when
// each rates below risky, every read succeeded, every call stays bounded, and
// nothing runs later does the light call stay ordinary, in the CLI and in the
// App's check alike (both pinned: parity alone could repeat one wrong answer).
func TestUnresolvedTargetBound(t *testing.T) {
	templated := func(action string) map[string]any { return step(action, "{{ covers }}") }
	for _, tc := range []struct {
		name     string
		sequence []any
		setup    func(*fakeHA)
		want     string // "" ordinary, else a reason the typed plan carries
	}{
		{name: "every cover is a blind"},
		{name: "a garage cover", want: reasonAccessCover, setup: func(f *fakeHA) {
			f.ent("cover.garage", "zwave_js", "closed", map[string]any{"device_class": "garage", "supported_features": 15.0})
		}},
		{name: "a cover without a class", want: reasonCoverClassUnknown, setup: func(f *fakeHA) {
			f.ent("cover.attic", "zwave_js", "closed", map[string]any{"supported_features": 15.0})
		}},
		{name: "a template cover classed blind", want: reasonTemplateAction, setup: func(f *fakeHA) {
			f.ent("cover.terrace", "template", "open", map[string]any{"device_class": "blind", "supported_features": 15.0})
		}},
		{name: "a YAML cover without unique_id", want: reasonUnregisteredActuator, setup: func(f *fakeHA) {
			f.ent("cover.yaml_shutter", "", "open", map[string]any{"device_class": "shutter", "supported_features": 15.0})
		}},
		{name: "an unresolved script target that reaches an unlock", want: reasonAccessLock, sequence: []any{templated("script.turn_on")}, setup: func(f *fakeHA) {
			f.script("door", step("lock.unlock", "lock.front"))
		}},
		{name: "a delay before the call", want: reasonUnresolvedTarget, sequence: []any{map[string]any{"delay": "00:00:05"}, templated("cover.open_cover")}},
		{name: "a wait before the call", want: reasonUnresolvedTarget, sequence: []any{map[string]any{"wait_template": "{{ is_state('light.hall', 'on') }}"}, templated("cover.open_cover")}},
		{name: "a reload before the call", want: reasonUnresolvedTarget, sequence: []any{step("homeassistant.reload_config_entry", "light.hall"), templated("cover.open_cover")}},
		{name: "a delay elsewhere in the cascade", want: reasonUnresolvedTarget, setup: func(f *fakeHA) {
			f.automation("kitchen_later", "kitchen_later", map[string]any{
				"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.kitchen"}},
				"actions":  []any{map[string]any{"delay": 60.0}, step("light.turn_on", "light.hall")},
			})
			f.related["entity:light.kitchen"] = []string{"automation.kitchen_later"}
		}},
		{name: "a consumer that fires only after for:", want: reasonUnresolvedTarget, setup: func(f *fakeHA) {
			f.automation("kitchen_held", "kitchen_held", map[string]any{
				"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.kitchen", "to": "off", "for": "00:05:00"}},
				"actions":  []any{step("light.turn_on", "light.hall")},
			})
			f.related["entity:light.kitchen"] = []string{"automation.kitchen_held"}
		}},
		{name: "a purpose-specific trigger that holds for a time", want: reasonUnresolvedTarget, setup: func(f *fakeHA) {
			f.automation("kitchen_dark", "kitchen_dark", map[string]any{
				"triggers": []any{map[string]any{"trigger": "light.turned_off", "target": map[string]any{"entity_id": "light.kitchen"}, "options": map[string]any{"for": map[string]any{"minutes": 5.0}}}},
				"actions":  []any{step("light.turn_on", "light.hall")},
			})
			f.related["entity:light.kitchen"] = []string{"automation.kitchen_dark"}
		}},
		{name: "a time trigger the cascade sets", want: reasonUnresolvedTarget, setup: func(f *fakeHA) {
			f.ent("input_datetime.alarm", "input_datetime", "07:00:00", map[string]any{"has_time": true})
			f.manifests = append(f.manifests, map[string]any{"domain": "input_datetime", "is_built_in": true})
			f.automation("set_alarm", "set_alarm", map[string]any{
				"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.kitchen"}},
				"actions":  []any{step("input_datetime.set_datetime", "input_datetime.alarm")},
			})
			f.automation("alarm", "alarm", map[string]any{
				"triggers": []any{map[string]any{"trigger": "time", "at": "input_datetime.alarm"}},
				"actions":  []any{step("light.turn_on", "light.hall")},
			})
			f.related["entity:light.kitchen"] = []string{"automation.set_alarm"}
			f.related["entity:input_datetime.alarm"] = []string{"automation.alarm"}
		}},
		{name: "a remote that holds the run before the call", want: reasonUnresolvedTarget, setup: func(f *fakeHA) {
			f.ent("remote.tv", "zwave_js", "on", nil)
		}, sequence: []any{map[string]any{"action": "remote.send_command", "target": map[string]any{"entity_id": "remote.tv"}, "data": map[string]any{"command": "up", "num_repeats": 20.0, "delay_secs": 2.0}}, templated("cover.open_cover")}},
		{name: "calls elsewhere aren't bounded", want: reasonUnresolvedTarget, setup: func(f *fakeHA) {
			f.config["components"] = append(f.config["components"].([]string), "alert")
		}},
		{name: "a service whose domain has no entities", want: reasonUnresolvedTarget, sequence: []any{templated("recorder.purge_entities")}, setup: func(f *fakeHA) {
			f.services = append(f.defaultServices(), map[string]any{"domain": "recorder", "services": map[string]any{"purge_entities": map[string]any{"target": map[string]any{}}}})
		}},
		{name: "a homeassistant service of any domain", want: reasonUnresolvedTarget, sequence: []any{templated("homeassistant.turn_on")}},
		{name: "an unknown payload", want: reasonUnresolvedTarget, sequence: []any{map[string]any{"action": "cover.open_cover", "target": map[string]any{"entity_id": "{{ covers }}"}, "data": "{{ payload }}"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, f := boundHome(t, tc.sequence...)
			if tc.setup != nil {
				tc.setup(f)
			}
			_, env := h.run(runPlanCommand, "call", "light.turn_off", "light.kitchen")
			if !slices.Contains(consumerList(env), "automation.rolladen_watchdog") {
				t.Fatalf("tier %q (reasons %s), consumers %v; want the watchdog reached", env.Tier, reasonCodes(env), consumerList(env))
			}
			gate := gateLight(t, h)
			if tc.want == "" {
				if env.Tier != "ordinary" || env.Plan == nil || env.Plan.Confirm != "none" || slices.Contains(reasonList(env), reasonUnresolvedTarget) || gate.Verdict != gatePass {
					t.Fatalf("tier %q (reasons %s), gate %+v; want ordinary, no confirmation, and a gate pass", env.Tier, reasonCodes(env), gate)
				}
				return
			}
			if env.Tier != "typed" || !slices.Contains(reasonList(env), tc.want) || gate.Verdict != gateTyped {
				t.Fatalf("tier %q (reasons %s), gate %+v; want typed by %s, held by the gate", env.Tier, reasonCodes(env), gate, tc.want)
			}
		})
	}
}

// The candidates come from one state list; when it can't be read there are
// none, and the read itself is typed. (In a plan the automation scan reads
// that list first, so a failure types the plan before any bound.)
func TestUnresolvedTargetBoundUnreadStates(t *testing.T) {
	e := &tierEngine{s: &relaySession{ctx: context.Background()}, bounding: true, complete: true, seen: map[string]bool{},
		servicesRead: true, services: map[string]serviceSpec{"cover.open_cover": {listed: true, target: true}},
		stateList: func() (map[string]json.RawMessage, error) { return nil, errors.New("unreadable") }}
	if got := e.boundCandidates("cover.open_cover", true, false, "script.check_group"); got != nil || e.level != tierTyped || len(e.reasons) != 1 || e.reasons[0].Code != reasonScanFailed {
		t.Fatalf("candidates %v, level %v, reasons %+v; want none and a typed failed read", got, e.level, e.reasons)
	}
}

// A saved automation runs later, against entities that may not exist yet:
// its templated cover target keeps today's rating.
func TestUnresolvedTargetBoundNeverInADraft(t *testing.T) {
	_, f := boundHome(t)
	env := planRoutine(t, f, map[string]any{"alias": "Routine", "triggers": []any{}, "actions": []any{step("cover.open_cover", "{{ covers }}")}})
	if env.Tier != "typed" || !slices.Contains(reasonList(env), reasonUnresolvedTarget) {
		t.Fatalf("a draft with a templated cover target: tier %q (reasons %s); want typed", env.Tier, reasonCodes(env))
	}
}
