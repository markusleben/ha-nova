package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
		"config/auth/create", "config/auth/delete", "assist_pipeline/pipeline/delete", "calendar/event/delete", "blueprint/delete", "config_entries/subentries/delete", "labs/update", "recorder/import_statistics"}
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
		"/api/config/config_entries/subentries/flow", "/api/config/config_entries/subentries/flow/0123abcd", "/api/repairs/issues/fix", "/api/repairs/issues/fix/0123abcd",
		"/api/config/scene/config/evening"}
	typedCore := []string{"DELETE /api/config/config_entries/entry/0123abcd", "DELETE /api/config/scene/config/evening"}
	for _, allowed := range rawCoreWrites {
		matched := false
		for _, sample := range samples {
			if !allowed.path.MatchString(sample) {
				continue
			}
			matched = true
			payload := map[string]any{"method": allowed.method, "path": sample, "body": map[string]any{"handler": "hue"}}
			if sample == "/api/config/config_entries/subentries/flow" {
				payload["body"] = map[string]any{"handler": []any{"Ehue", "device"}}
			}
			exit, env := plan("core", payload)
			want := map[bool]string{true: "typed", false: "preview"}[slices.Contains(typedCore, allowed.method+" "+sample)]
			if exit != verbExitConfirmation || env.Tier != want {
				t.Errorf("%s %s: exit %d tier %s, want %s (%+v)", allowed.method, sample, exit, env.Tier, want, env.Error)
			}
			chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "core", "payload": payload}}}))
			if code != "" || chat["tier"] != want || chat["status"] != "needs_confirmation" {
				t.Errorf("%s %s: chat tier differs from CLI: %v", allowed.method, sample, chat)
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

func TestPlanRawBatch2FlowParity(t *testing.T) {
	paths, sent := rawRelay(t, nil)
	c := startMCP(t, paths)
	mcpInit(c)
	for _, target := range []string{"/api/repairs/issues/fix", "/api/repairs/issues/fix/f1", "/api/config/config_entries/subentries/flow", "/api/config/config_entries/subentries/flow/f1"} {
		for _, method := range []string{"POST", "GET", "DELETE"} {
			if method != "POST" && !strings.HasSuffix(target, "/f1") {
				continue
			}
			for _, typed := range []bool{false, true} {
				payload := map[string]any{"method": method, "path": target}
				if method == "POST" {
					payload["body"] = map[string]any{}
					if target == "/api/config/config_entries/subentries/flow" {
						payload["body"] = map[string]any{"handler": []any{"Ehue", "device"}}
					} else if target == "/api/repairs/issues/fix" {
						payload["body"] = map[string]any{"handler": "hue", "issue_id": "old_bridge"}
					}
				}
				items := []any{map[string]any{"endpoint": "core", "payload": payload, "typed": typed}}
				body, _ := json.Marshal(items)
				exit, env := planRaw(t, paths, string(body))
				chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": items}))
				want := map[bool]string{false: "preview", true: "typed"}[typed]
				if exit != verbExitConfirmation || env.Tier != want || code != "" || chat["tier"] != want {
					t.Errorf("%s %s typed=%v: exit %d CLI %+v chat %v", method, target, typed, exit, env, chat)
				}
				if mcpReadRefusal("core", mustJSON(payload)) == nil || rawRelayRead("core", mustJSON(payload)) {
					t.Errorf("%s %s was treated as an ordinary read", method, target)
				}
			}
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning sent %v", got)
	}
}

func TestRawSubentryAccessGuard(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "lock.front", "config_entry_id": "Elock"}}
		},
	})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	c := startMCP(t, paths)
	mcpInit(c)
	for _, item := range []map[string]any{
		{"endpoint": "ws", "payload": map[string]any{"type": "config_entries/subentries/delete", "entry_id": "Elock", "subentry_id": "S1"}},
		{"endpoint": "core", "payload": map[string]any{"method": "POST", "path": "/api/config/config_entries/subentries/flow", "body": map[string]any{"handler": []any{"Elock", "device"}}}},
	} {
		body, _ := json.Marshal([]any{item})
		exit, env := planRaw(t, paths, string(body))
		chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{item}}))
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "lock.front") || code != "REFUSED" {
			t.Errorf("exit %d CLI %+v chat %v", exit, env, chat)
		}
		payload, _ := json.Marshal(item["payload"])
		_, refusal, err := guardRawRequestIn(s, item["endpoint"].(string), payload)
		if refusal == nil && err == nil {
			t.Errorf("raw request bypassed access guard: %s", body)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("refused requests sent %v", got)
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

func TestPlanRawPollingDisruptionSayAndPrompt(t *testing.T) {
	paths, sent := rawRelay(t, nil)
	for _, fields := range []string{`"pref_disable_polling":true`, `"pref_disable_polling":false`, `"title":"New name"`, `"pref_disable_new_entities":true`} {
		for _, typed := range []bool{false, true} {
			exit, env := planRaw(t, paths, fmt.Sprintf(`[{"endpoint":"ws","typed":%t,"payload":{"type":"config_entries/update","entry_id":"Ehue",%s}}]`, typed, fields))
			if exit != verbExitConfirmation || env.Tier != map[bool]string{false: "preview", true: "typed"}[typed] || env.Plan == nil {
				t.Fatalf("%s: exit %d env %+v", fields, exit, env)
			}
			plan, err := readPlanFile(filepath.Join(planDir(paths), env.Plan.ID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Contains(fields, "pref_disable_polling")
			texts := map[string]string{"say": env.Say}
			if typed {
				texts["prompt"] = plan.Prompt
			} else if plan.Prompt != "" {
				t.Errorf("preview plan has a typed confirmation prompt: %s", plan.Prompt)
			}
			for name, text := range texts {
				if strings.Contains(text, "changing polling reloads this integration when loaded, briefly making its devices and entities unavailable") != want {
					t.Errorf("%s %s: unexpected disruption disclosure: %s", fields, name, text)
				}
			}
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning sent %v", got)
	}
}

// HA UI parity batch 3a (docs/work/2026-10-26-ha-ui-parity-spec.md): backup
// settings are previewed with their values and typed when they set a
// retention (it deletes older backups); the encryption key, per-location
// encryption, the first automatic setup, partial blocks Home Assistant would
// reset, and backups that would leave unencrypted are refused on every
// route; Labs and statistics imports are typed; usage-data sharing is previewed.
func TestPlanRawBatch3aTiersAndBackupKeyGuard(t *testing.T) {
	for _, keySaved := range []bool{false, true} {
		t.Run(fmt.Sprint("key saved ", keySaved), func(t *testing.T) { batch3aTiersAndGuards(t, keySaved) })
	}
}

func batch3aTiersAndGuards(t *testing.T, keySaved bool) {
	password := any(nil)
	if keySaved {
		password = secretHidden
	}
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"backup/config/info": func(map[string]any) any {
			return map[string]any{"config": map[string]any{"create_backup": map[string]any{"password": password}}}
		},
	})
	c := startMCP(t, paths)
	mcpInit(c)
	plan := func(payload string) (int, verbTestEnvelope) {
		return planRaw(t, paths, `[{"endpoint":"ws","payload":`+payload+`}]`)
	}
	remote := `{"type":"backup/config/update","create_backup":{"agent_ids":["backup.local","onedrive.01ABC"]}}`
	planned := map[string]struct{ tier, line string }{
		`{"type":"backup/config/update","schedule":{"days":[],"recurrence":"daily","time":"04:45"}}`:                                                           {"preview", "schedule days (), schedule recurrence “daily”, schedule time “04:45”"},
		`{"type":"backup/config/update","retention":{"copies":3,"days":null}}`:                                                                                 {"typed", "retention copies 3, retention days cleared; automatic backups beyond the retention are deleted for good"},
		`{"type":"backup/config/update","agents":{"backup.local":{"retention":{"copies":2,"days":null}}}}`:                                                     {"typed", "agents backup.local retention copies 2"},
		`{"type":"backup/config/update","agents":{"backup.local":{"retention":null}},"create_backup":{"agent_ids":["backup.local"],"include_database":false}}`: {"typed", "create backup agent ids ('backup.local'), create backup include database false"},
		`{"type":"labs/update","domain":"frontend","preview_feature":"winter_mode","enabled":true}`:                                                            {"typed", "on the Labs feature “winter_mode” of “frontend”"},
		`{"type":"labs/update","domain":"frontend","preview_feature":"winter_mode","enabled":false,"create_backup":false}`:                                     {"typed", "off the Labs feature"},
		`{"type":"recorder/import_statistics","metadata":{"statistic_id":"nova_test:import","source":"nova_test","name":"Import","has_sum":true,"mean_type":0,"unit_class":"energy","unit_of_measurement":"kWh"},"stats":[{"start":"2026-10-25T10:00:00+00:00","sum":1},{"start":"2026-10-25T11:00:00+00:00","sum":2}]}`: {"typed", "2 rows into the long-term statistic “nova_test:import” from “2026-10-25T10:00:00+00:00” to “2026-10-25T11:00:00+00:00”"},
		`{"type":"analytics/preferences","preferences":{"base":true,"usage":false}}`:      {"preview", "about this installation: base true, usage false"},
		`{"type":"backup/generate","agent_ids":["hassio.local"],"include_database":true}`: {"preview", "Create a backup"},
	}
	refused := map[string]string{
		`{"type":"backup/config/update","create_backup":{"password":"hunter2"}}`:                                              "backup encryption password",
		`{"type":"backup/config/update","create_backup":{"password":null}}`:                                                   "backup encryption password",
		`{"type":"backup/config/update","schedule":{"recurrence":"daily"},"create_backup":{"name":"x","password":"hunter2"}}`: "backup encryption password",
		`{"type":"backup/config/update","agents":{"cloud.cloud":{"protected":false}}}`:                                        "backup encryption password",
		`{"type":"backup/config/update","agents":{"backup.local":{"retention":null},"cloud.cloud":{"protected":true}}}`:       "backup encryption password",
		`{"type":"backup/config/update","agents":{"cloud.cloud":false}}`:                                                      "backup encryption password",
		`{"type":"backup/config/update","agents":["cloud.cloud"]}`:                                                            "backup encryption password",
		`{"type":"backup/config/update","create_backup":"password"}`:                                                          "backup encryption password",
		`{"type":"backup/config/update","automatic_backups_configured":true}`:                                                 "first automatic-backup setup",
		`{"type":"backup/config/update","schedule":{"time":"05:00"}}`:                                                         "partial schedule or retention",
		`{"type":"backup/config/update","schedule":{}}`:                                                                       "partial schedule or retention",
		`{"type":"backup/config/update","retention":{"copies":2}}`:                                                            "partial schedule or retention",
		`{"type":"backup/config/update","agents":{"backup.local":{"retention":{"days":7}}}}`:                                  "partial schedule or retention",
		`{"type":"backup/generate","agent_ids":["backup.local"],"password":"hunter2"}`:                                        "its own password or to a remote location",
		`{"type":"backup/generate","agent_ids":["hassio.local","hassio.nas"]}`:                                                "its own password or to a remote location",
		`{"type":"labs/update","domain":"frontend","preview_feature":"winter_mode","enabled":true,"create_backup":true}`:      "Labs change with create_backup",
	}
	if keySaved {
		planned[remote] = struct{ tier, line string }{"preview", "onedrive.01ABC"}
	} else {
		refused[remote] = "remote backup location without a saved encryption key"
	}
	for payload, want := range planned {
		exit, env := plan(payload)
		var result rawPlanResult
		_ = json.Unmarshal(env.Result, &result)
		if exit != verbExitConfirmation || env.Tier != want.tier || len(result.Items) != 1 || !strings.Contains(result.Items[0].Change, want.line) {
			t.Errorf("%s: exit %d tier %s, want %s with %q (%+v %+v)", payload, exit, env.Tier, want.tier, want.line, result.Items, env.Error)
		}
		var message map[string]any
		_ = json.Unmarshal([]byte(payload), &message)
		chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": message}}}))
		if code != "" || chat["tier"] != want.tier {
			t.Errorf("chat %s: %v, want tier %s", payload, chat, want.tier)
		}
	}
	for payload, want := range refused {
		exit, env := plan(payload)
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, want) || strings.Contains(env.Error.Message, "hunter2") {
			t.Errorf("%s: exit %d %+v, want refusal %q", payload, exit, env.Error, want)
		}
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", payload}) })
		if exit != verbExitRefused || !strings.Contains(stderr, want) {
			t.Errorf("relay %s: exit %d, stderr %q", payload, exit, stderr)
		}
		if payload != remote { // the static guard; the remote check reads live
			envelope := `{"message":` + payload + `,"collect_events":{"until_type":"result"}}`
			if err := checkRawRelayRequest("ws", []byte(envelope)); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("collect_events envelope %s: %v", payload, err)
			}
		}
		var message map[string]any
		_ = json.Unmarshal([]byte(payload), &message)
		chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": message}}}))
		if code != "REFUSED" || !strings.Contains(fmt.Sprint(chat), want) {
			t.Errorf("chat %s: %v, want refusal %q", payload, chat, want)
		}
	}
	for _, read := range []string{`{"type":"labs/list"}`, `{"type":"analytics"}`, `{"type":"backup/config/info"}`} {
		if !rawRelayRead("ws", []byte(read)) || mcpReadRefusal("ws", []byte(read)) != nil {
			t.Errorf("%s is not an ordinary read", read)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning or refusals sent %v", got)
	}
}
