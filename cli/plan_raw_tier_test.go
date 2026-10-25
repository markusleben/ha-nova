package main

import (
	"encoding/json"
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
	paths, sent := rawRelay(t, nil)
	wantTyped := []string{"config/entity_registry/remove", "lovelace/resources/create", "lovelace/resources/update", "lovelace/resources/delete",
		"recorder/clear_statistics", "backup/delete", "hacs/repositories/remove", "hacs/repository/download", "hacs/repository/remove",
		"config/auth/create", "config/auth/delete", "assist_pipeline/pipeline/delete", "calendar/event/delete"}
	for _, collection := range rawWSCollections {
		wantTyped = append(wantTyped, collection+"/delete")
	}
	plan := func(endpoint string, payload map[string]any) (int, verbTestEnvelope) {
		items, _ := json.Marshal([]map[string]any{{"endpoint": endpoint, "payload": payload}})
		return planRaw(t, paths, string(items))
	}
	for wsType, what := range rawWSAllowed {
		payload := map[string]any{"type": wsType}
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
		{"backups", "save", "preview"}, {"backups", "delete", "typed"}, {"backups", "prune", "typed"}} {
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
