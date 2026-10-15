package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeHA is a small Home Assistant behind the fake Relay: states, both
// registries, stored configs, relations, event listeners, config entries.
type fakeHA struct {
	states        map[string]map[string]any
	compact       []map[string]any
	full          []map[string]any
	configs       map[string]any      // core path → stored config; absent → 404
	unique        map[string]string   // entity_id → registry unique_id
	related       map[string][]string // "entity:<id>" → automations, "area:<id>" → entities
	events        []map[string]any
	entries       []map[string]any
	substitute    any
	fail          map[string]bool     // ws type or core path answered with a Relay error
	hang          map[string]bool     // ws type or core path that never answers
	relatedGroups map[string][]string // entity → groups search/related reports
	aliases       map[string][]string // entity → aliases (get_entries only, as in HA)
	flows         []map[string]any    // config_entries/flow/progress
	services      []map[string]any    // GET /api/services (nil: 404)
	// service answers POST /api/services/<d>/<s> (nil: 200 and no effect);
	// it runs under mu and may change states.
	service func(f *fakeHA, path string, body map[string]any) any
	mu      sync.Mutex
	// Config writes (PR5): configWrite overrides how a POSTed config is
	// stored (nil: verbatim); snapshots records /backups saves.
	configWrite func(f *fakeHA, path string, body map[string]any) any
	snapshots   []map[string]any
	snapshotErr bool
	// deleteStatus: answer DELETE with this status after removing the item
	// (a concurrent removal: Home Assistant says 400 "Resource not found").
	deleteStatus int
}

func newFakeHA() *fakeHA {
	f := &fakeHA{states: map[string]map[string]any{}, configs: map[string]any{}, unique: map[string]string{}, related: map[string][]string{}, fail: map[string]bool{}, hang: map[string]bool{}}
	f.ent("light.kitchen", "hue", "on", map[string]any{"brightness": 255.0, "friendly_name": "Kitchen"})
	f.ent("light.hall", "hue", "off", nil)
	f.ent("lock.front", "zwave_js", "locked", nil)
	return f
}

// ent adds a state and a compact registry row (pl "" = no registry row).
func (f *fakeHA) ent(id, pl, state string, attrs map[string]any) *fakeHA {
	if attrs == nil {
		attrs = map[string]any{}
	}
	f.states[id] = map[string]any{"entity_id": id, "state": state, "attributes": attrs}
	if pl != "" {
		f.compact = append(f.compact, map[string]any{"ei": id, "en": attrs["friendly_name"], "pl": pl, "di": attrs["_di"]})
	}
	return f
}

// script adds script.<object> with a stored sequence under config id <object>.
func (f *fakeHA) script(object string, sequence ...any) *fakeHA {
	f.ent("script."+object, "script", "off", nil)
	f.unique["script."+object] = object
	f.configs["/api/config/script/config/"+object] = map[string]any{"sequence": sequence}
	return f
}

func (f *fakeHA) automation(object, id string, cfg map[string]any) *fakeHA {
	attrs := map[string]any{}
	if id != "" {
		attrs["id"] = id
		f.configs["/api/config/automation/config/"+id] = cfg
	}
	return f.ent("automation."+object, "automation", "on", attrs)
}

func step(action, target string) map[string]any {
	return map[string]any{"action": action, "target": map[string]any{"entity_id": target}}
}

func (f *fakeHA) reply(r *http.Request, endpoint string, p map[string]any) any {
	if f.hang[fmt.Sprint(p["type"])] || f.hang[fmt.Sprint(p["path"])] {
		<-r.Context().Done()
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if reply, handled := f.configReply(r, endpoint, p); handled {
		return reply
	}
	if p["method"] == "POST" {
		if f.fail[fmt.Sprint(p["path"])] {
			return fakeRelayFailure{"UPSTREAM_HTTP_TIMEOUT", "timed out"}
		}
		if f.service == nil {
			return coreReply(200, []any{})
		}
		body, _ := p["body"].(map[string]any)
		return f.service(f, fmt.Sprint(p["path"]), body)
	}
	if endpoint == "ws" {
		wsType := fmt.Sprint(p["type"])
		if f.fail[wsType] {
			return fakeRelayFailure{"UPSTREAM_ERROR", "boom"}
		}
		switch wsType {
		case "config/entity_registry/list_for_display":
			return map[string]any{"entities": f.compact}
		case "config/entity_registry/list":
			return append([]map[string]any{}, f.full...)
		case "config/entity_registry/get":
			for _, row := range f.full {
				if row["entity_id"] == p["entity_id"] && row["disabled_by"] != nil {
					return row
				}
			}
			if uid, ok := f.unique[fmt.Sprint(p["entity_id"])]; ok {
				return map[string]any{"unique_id": uid}
			}
			return fakeRelayFailure{"UPSTREAM_ERROR", "not_found: Entity not found"}
		case "search/related":
			items := f.related[fmt.Sprint(p["item_type"])+":"+fmt.Sprint(p["item_id"])]
			if p["item_type"] == "area" {
				return map[string]any{"entity": items}
			}
			return map[string]any{"automation": items, "group": f.relatedGroups[fmt.Sprint(p["item_id"])]}
		case "config/entity_registry/get_entries":
			entries := map[string]any{}
			ids, _ := p["entity_ids"].([]any)
			for _, id := range ids {
				entries[fmt.Sprint(id)] = map[string]any{"aliases": f.aliases[fmt.Sprint(id)]}
			}
			return entries
		case "config_entries/flow/progress":
			return append([]map[string]any{}, f.flows...)
		case "config_entries/get":
			return append([]map[string]any{}, f.entries...)
		case "blueprint/substitute":
			return f.substitute
		}
		return []any{}
	}
	path := fmt.Sprint(p["path"])
	if f.fail[path] {
		return fakeRelayFailure{"UPSTREAM_ERROR", "boom"}
	}
	switch {
	case path == "/api/services" && f.services != nil:
		return coreReply(200, f.services)
	case path == "/api/events":
		return coreReply(200, append([]map[string]any{}, f.events...))
	case path == "/api/states":
		var all []any
		for _, st := range f.states {
			all = append(all, st)
		}
		return coreReply(200, all)
	case strings.HasPrefix(path, "/api/states/"):
		if st, ok := f.states[strings.TrimPrefix(path, "/api/states/")]; ok {
			return coreReply(200, st)
		}
	case strings.HasPrefix(path, "/api/config/"):
		if cfg, ok := f.configs[path]; ok {
			return coreReply(200, cfg)
		}
	}
	return coreReply(404, map[string]any{"message": "not found"})
}

func runPlanForTest(t *testing.T, f *fakeHA, args ...string) (int, verbTestEnvelope, runtimePaths, *fakeRelayLog) {
	t.Helper()
	paths, log := setupVerbRelayTest(t, f.reply)
	exit, env := runVerbForTest(t, func() int { return runPlanCommand(paths, append([]string{"call"}, args...)) })
	return exit, env, paths, log
}

func reasonCodes(env verbTestEnvelope) string {
	var codes []string
	for _, r := range env.Reasons {
		codes = append(codes, r.Code)
	}
	return strings.Join(codes, ",")
}

// tierCase is one golden row: the plan call, its exit, tier, and reasons.
type tierCase struct {
	name    string
	setup   func(*fakeHA)
	args    []string
	exit    int
	tier    string
	reasons []string // each must be present
	absent  []string // none may be present
	check   func(*testing.T, verbTestEnvelope)
}

func runTierCases(t *testing.T, cases []tierCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHA()
			if tc.setup != nil {
				tc.setup(f)
			}
			exit, env, _, _ := runPlanForTest(t, f, tc.args...)
			codes := reasonCodes(env)
			if exit != tc.exit || env.Tier != tc.tier {
				t.Fatalf("exit %d tier %q (reasons %s, error %+v), want exit %d tier %q", exit, env.Tier, codes, env.Error, tc.exit, tc.tier)
			}
			for _, want := range tc.reasons {
				if !strings.Contains(","+codes+",", ","+want+",") {
					t.Fatalf("reasons %s lack %s", codes, want)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(","+codes+",", ","+unwanted+",") {
					t.Fatalf("reasons %s contain %s", codes, unwanted)
				}
			}
			if tc.check != nil {
				tc.check(t, env)
			}
		})
	}
}
