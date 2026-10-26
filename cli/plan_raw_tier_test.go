package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Parity item 3 (docs/work/2026-10-25-chat-parity-spec.md): every
// allowlisted raw write plans through kind raw with the tier the skills use
// on a computer, and every allowlisted read is read through ha_read or is a
// read only a plan sends. A new allowlist entry fails here until its tier
// is decided below.
func TestPlanRawTierCoversEveryAllowlistedRequest(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"backups list":                func(map[string]any) any { return []any{map[string]any{"file": "scenes/a.json.gz"}} },
		"config/device_registry/list": func(map[string]any) any { return []any{map[string]any{"id": "demo", "name": "Demo"}} },
	})
	c := startMCP(t, paths)
	mcpInit(c)
	wantTyped := []string{"config/entity_registry/remove", "config/device_registry/remove", "lovelace/resources/create", "lovelace/resources/update", "lovelace/resources/delete",
		"recorder/clear_statistics", "backup/delete", "hacs/repositories/remove", "hacs/repository/download", "hacs/repository/remove",
		"config/auth/create", "config/auth/delete", "assist_pipeline/pipeline/delete", "calendar/event/delete", "blueprint/delete"}
	for _, collection := range rawWSCollections {
		wantTyped = append(wantTyped, collection+"/delete")
	}
	plan := func(endpoint string, payload map[string]any) (int, verbTestEnvelope) {
		items, _ := json.Marshal([]map[string]any{{"endpoint": endpoint, "payload": payload}})
		return planRaw(t, paths, string(items))
	}
	for wsType, what := range rawWSAllowed {
		payload := map[string]any{"type": wsType}
		if wsType == "config/device_registry/remove" {
			payload["device_id"] = "demo"
		}
		if wsType == "subscribe_events" {
			payload["event_type"] = "state_changed"
		}
		body, _ := json.Marshal(payload)
		exit, env := plan("ws", payload)
		switch {
		case what == rawWSRead && mcpPlannedReads[wsType]:
			if exit != verbExitConfirmation || env.Tier != "preview" {
				t.Errorf("%s (plan-only read): exit %d tier %s %+v", wsType, exit, env.Tier, env.Error)
			}
		case what == rawWSRead:
			if exit != verbExitRefused || mcpReadRefusal("ws", body) != nil {
				t.Errorf("%s (read): planned with exit %d, or ha_read refuses it: %v", wsType, exit, mcpReadRefusal("ws", body))
			}
		default:
			want := map[bool]string{true: "typed", false: "preview"}[slices.Contains(wantTyped, wsType)]
			if exit != verbExitConfirmation || env.Tier != want {
				t.Errorf("%s: exit %d tier %s, want %s (%+v)", wsType, exit, env.Tier, want, env.Error)
			}
			chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": payload}}}))
			if code != "" || chat["tier"] != want || chat["status"] != "needs_confirmation" {
				t.Errorf("%s: chat tier differs from CLI: %v", wsType, chat)
			}
			var result rawPlanResult
			_ = json.Unmarshal(env.Result, &result)
			if len(result.Items) != 1 || strings.Contains(result.Items[0].Change, wsType) {
				t.Errorf("%s: no plain line: %+v", wsType, result.Items)
			}
		}
	}
	// One sample path per core write; a new entry needs its own.
	samples := []string{"/api/template", "/api/config/core/check_config", "/api/config/config_entries/flow", "/api/config/config_entries/options/flow",
		"/api/config/config_entries/flow/0123abcd", "/api/config/config_entries/options/flow/0123abcd", "/api/config/config_entries/entry/0123abcd",
		"/api/config/scene/config/evening"}
	typedCore := []string{"DELETE /api/config/config_entries/entry/0123abcd", "DELETE /api/config/scene/config/evening"}
	for _, allowed := range rawCoreWrites {
		matched := false
		for _, sample := range samples {
			if !allowed.path.MatchString(sample) {
				continue
			}
			matched = true
			exit, env := plan("core", map[string]any{"method": allowed.method, "path": sample, "body": map[string]any{"handler": "hue"}})
			want := map[bool]string{true: "typed", false: "preview"}[slices.Contains(typedCore, allowed.method+" "+sample)]
			if exit != verbExitConfirmation || env.Tier != want {
				t.Errorf("%s %s: exit %d tier %s, want %s (%+v)", allowed.method, sample, exit, env.Tier, want, env.Error)
			}
		}
		if !matched {
			t.Errorf("no sample path for %s %s: add one and its tier", allowed.method, allowed.path)
		}
	}
	for _, tc := range []struct{ endpoint, action, tier string }{{"files", "write_file", "preview"}, {"files", "delete_file", "typed"},
		{"backups", "save", "preview"}, {"backups", "delete", "typed"}, {"backups", "prune", "preview"}} {
		exit, env := plan(tc.endpoint, map[string]any{"action": tc.action, "path": "packages/a.yaml", "content": "a: 1", "file": "scenes/a.json.gz",
			"category": "scenes", "name": "auto-a", "data": map[string]any{}})
		if exit != verbExitConfirmation || env.Tier != tc.tier {
			t.Errorf("%s %s: exit %d tier %s, want %s", tc.endpoint, tc.action, exit, env.Tier, tc.tier)
		}
	}
	for key := range rawTypedWS {
		if _, allowed := rawWSAllowed[key]; !allowed {
			t.Errorf("rawTypedWS names %s, which the allowlist does not send", key)
		}
	}
	for key := range rawWSPhrases {
		if _, allowed := rawWSAllowed[key]; !allowed {
			t.Errorf("rawWSPhrases names %s, which the allowlist does not send", key)
		}
	}
	for _, collection := range rawHelperCollections {
		if !slices.Contains(rawWSCollections, collection) {
			t.Errorf("helper collection %s is not on the allowlist", collection)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning sent %v", got)
	}
}

// HA UI parity batch 1 (docs/work/2026-10-26-ha-ui-parity-spec.md): the
// fields that type an account or general-settings change, the integration
// disable that would take a lock out of service, and the accounts whose
// access stays with the Home Assistant UI.
func TestPlanRawUIParityTiersAndGuards(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "lock.front", "config_entry_id": "Elock"}, map[string]any{"entity_id": "light.hall", "config_entry_id": "Ehue"},
				map[string]any{"entity_id": "switch.gate_relay", "config_entry_id": "Esource", "device_id": "dgate"},
				map[string]any{"entity_id": "cover.gate", "config_entry_id": "Ewrapper", "device_id": "dgate", "platform": "switch_as_x"}}
		},
		"config/auth/list": func(map[string]any) any {
			return []any{map[string]any{"id": "owner", "name": "Markus", "is_owner": true}, map[string]any{"id": "sys", "name": "Supervisor", "system_generated": true},
				map[string]any{"id": "nova", "name": "HA NOVA"}, map[string]any{"id": "anna", "name": "Anna"}}
		},
		"auth/current_user": func(map[string]any) any { return map[string]any{"id": "nova"} },
	})
	c := startMCP(t, paths)
	mcpInit(c)
	plan := func(payload string) (int, verbTestEnvelope) {
		return planRaw(t, paths, `[{"endpoint":"ws","payload":`+payload+`}]`)
	}
	for payload, want := range map[string]string{
		`{"type":"config/auth/update","user_id":"anna","name":"Anna B"}`:                                 "preview",
		`{"type":"config/auth/update","user_id":"anna","group_ids":["system-admin"]}`:                    "typed",
		`{"type":"config/auth/update","user_id":"anna","is_active":false}`:                               "typed",
		`{"type":"config/auth/update","user_id":"owner","name":"Markus L"}`:                              "preview",
		`{"type":"config/core/update","location_name":"Home"}`:                                           "preview",
		`{"type":"config/core/update","time_zone":"Europe/Berlin"}`:                                      "typed",
		`{"type":"config/core/update","unit_system":"us_customary","update_units":true}`:                 "typed",
		`{"type":"config_entries/disable","entry_id":"Ehue","disabled_by":"user"}`:                       "preview",
		`{"type":"config_entries/update","entry_id":"Ehue","title":"Hue","pref_disable_polling":true}`:   "preview",
		`{"type":"blueprint/delete","domain":"automation","path":"homeassistant/motion_light.yaml"}`:     "typed",
		`{"type":"repairs/ignore_issue","domain":"hue","issue_id":"old_bridge","ignore":true}`:           "preview",
		`{"type":"homeassistant/expose_new_entities/set","assistant":"conversation","expose_new":false}`: "preview",
	} {
		exit, env := plan(payload)
		if exit != verbExitConfirmation || env.Tier != want {
			t.Errorf("%s: exit %d tier %s, want %s (%+v)", payload, exit, env.Tier, want, env.Error)
		}
		var result rawPlanResult
		_ = json.Unmarshal(env.Result, &result)
		if len(result.Items) != 1 || strings.Contains(result.Items[0].Change, "pref disable") || strings.Contains(result.Items[0].Change, "is active") || strings.Contains(result.Items[0].Change, "group ids") {
			t.Errorf("%s: no plain line: %+v", payload, result.Items)
		}
	}
	for payload, want := range map[string]string{
		`{"type":"config_entries/disable","entry_id":"Elock","disabled_by":"user"}`:          "integration entry Elock, which has lock.front",
		`{"type":"config_entries/disable","entry_id":"Elock","disabled_by":null}`:            "integration entry Elock, which has lock.front",
		`{"type":"config_entries/disable","entry_id":"Esource","disabled_by":"user"}`:        "integration entry Esource, which has cover.gate",
		`{"type":"config_entries/disable","entry_id":"Esource","disabled_by":null}`:          "integration entry Esource, which has cover.gate",
		`{"type":"config_entries/update","entry_id":"Elock","pref_disable_polling":true}`:    "integration entry Elock, which has lock.front",
		`{"type":"config_entries/update","entry_id":"Esource","pref_disable_polling":false}`: "integration entry Esource, which has cover.gate",
		`{"type":"config/auth/update","user_id":"owner","group_ids":["system-users"]}`:       "the owner's account",
		`{"type":"config/auth/update","user_id":"sys","group_ids":["system-admin"]}`:         "a system account",
		`{"type":"config/auth/update","user_id":"nova","group_ids":["system-admin"]}`:        "HA NOVA's own account",
		`{"type":"config/auth/update","user_id":"sys","is_active":false}`:                    "a system account",
		`{"type":"config/auth/update","user_id":"nova","local_only":true}`:                   "HA NOVA's own account",
		`{"type":"config/auth/update","user_id":"gone","group_ids":["system-admin"]}`:        "unknown account gone",
	} {
		exit, env := plan(payload)
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, want) {
			t.Errorf("%s: exit %d %+v, want refusal %q", payload, exit, env.Error, want)
		}
		var stderr string
		stderr = captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", payload}) })
		if exit != verbExitRefused || !strings.Contains(stderr, want) {
			t.Errorf("relay %s: exit %d, stderr %q", payload, exit, stderr)
		}
		var message map[string]any
		_ = json.Unmarshal([]byte(payload), &message)
		chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": message}}}))
		if code != "REFUSED" || !strings.Contains(fmt.Sprint(chat), want) {
			t.Errorf("chat %s: %v, want refusal %q", payload, chat, want)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning or refusals sent %v", got)
	}
}

func TestRawAccountGuardUnreadableSendsNothing(t *testing.T) {
	paths, sent := rawRelay(t, nil) // config/auth/list answers an object, auth/current_user no id
	exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/auth/update","user_id":"anna","group_ids":["system-admin"]}}]`)
	if exit == verbExitConfirmation || env.Plan != nil || env.Error == nil || !strings.Contains(env.Error.Message, "cannot read the accounts") {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("sent %v", got)
	}
}

func TestRawIntegrationDeviceLessWrapperSources(t *testing.T) {
	for _, source := range []string{"switch.other", "switch.plug", ""} {
		t.Run(source, func(t *testing.T) {
			paths, sent := rawRelay(t, map[string]func(map[string]any) any{
				"config/entity_registry/list": func(map[string]any) any {
					return []any{map[string]any{"entity_id": "switch.plug", "config_entry_id": "Eplug"},
						map[string]any{"entity_id": "lock.wrapper", "config_entry_id": "Ewrapper", "platform": "switch_as_x"}}
				},
				"config/entity_registry/get": func(map[string]any) any {
					return map[string]any{"options": map[string]any{"switch_as_x": map[string]any{"entity_id": source}}}
				},
			})
			c := startMCP(t, paths)
			mcpInit(c)
			for _, payload := range []map[string]any{
				{"type": "config_entries/disable", "entry_id": "Eplug", "disabled_by": "user"},
				{"type": "config_entries/disable", "entry_id": "Eplug", "disabled_by": nil},
				{"type": "config_entries/update", "entry_id": "Eplug", "pref_disable_polling": true},
				{"type": "config/entity_registry/update", "entity_id": "switch.plug", "disabled_by": "user"},
			} {
				items := []any{map[string]any{"endpoint": "ws", "payload": payload}}
				body, _ := json.Marshal(items)
				exit, env := planRaw(t, paths, string(body))
				chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": items}))
				if source == "switch.other" {
					if exit != verbExitConfirmation || env.Tier != "preview" || code != "" || chat["tier"] != "preview" {
						t.Fatalf("unrelated wrapper: exit %d env %+v chat %v", exit, env, chat)
					}
				} else if exit != verbExitRefused || code != "REFUSED" {
					t.Fatalf("access source %q: exit %d env %+v chat %v", source, exit, env, chat)
				}
			}
			if got := sent(); len(got) != 0 {
				t.Fatalf("planning sent %v", got)
			}
		})
	}
}
