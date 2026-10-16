package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeLovelace is Home Assistant's dashboard store behind the fake Relay.
type fakeLovelace struct {
	entries        []map[string]any
	configs        map[string]any // url_path ("" = unlisted default) → config; absent → config_not_found
	resources      []map[string]any
	reads          int    // lovelace/config reads so far
	editAt         int    // before answering this read, the UI edits the dashboard
	editPath       string // which dashboard that edit changes
	rejectAll      bool   // every save is refused by Home Assistant
	landThenReject bool   // save/delete: applied in memory, then answered with an error
	failReadsFrom  int    // lovelace/config reads from this one on fail (0: never)
	sent           []map[string]any
}

func (f *fakeHA) lovelaceReply(endpoint string, p map[string]any) (any, bool) {
	l := f.lovelace
	wsType := fmt.Sprint(p["type"])
	if l == nil || endpoint != "ws" || !strings.HasPrefix(wsType, "lovelace/") {
		return nil, false
	}
	urlPath, _ := p["url_path"].(string)
	notFound := fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "HA rejected '" + wsType + "': config_not_found: No config found."}
	switch wsType {
	case "lovelace/dashboards/list":
		return append([]map[string]any{}, l.entries...), true
	case "lovelace/resources":
		return append([]map[string]any{}, l.resources...), true
	case "lovelace/config":
		l.reads++
		if l.failReadsFrom > 0 && l.reads >= l.failReadsFrom {
			return fakeRelayFailure{"UPSTREAM_ERROR", "boom"}, true
		}
		if l.reads == l.editAt {
			l.configs[l.editPath] = map[string]any{"views": []any{map[string]any{"title": "Edited in the UI"}}}
		}
		if cfg, ok := l.configs[urlPath]; ok {
			return cfg, true
		}
		return notFound, true
	}
	l.sent = append(l.sent, p)
	switch wsType {
	case "lovelace/config/save":
		for _, e := range l.entries {
			if e["url_path"] == urlPath && e["mode"] == "yaml" {
				return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "HA rejected 'lovelace/config/save': error: Not supported"}, true
			}
		}
		if l.rejectAll {
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "HA rejected 'lovelace/config/save': error: invalid"}, true
		}
		l.configs[urlPath] = p["config"]
		if l.landThenReject {
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "HA rejected 'lovelace/config/save': error: disk full"}, true
		}
		return nil, true
	case "lovelace/dashboards/create":
		l.entries = append(l.entries, map[string]any{"id": strings.ReplaceAll(urlPath, "-", "_"), "url_path": urlPath, "title": p["title"], "mode": "storage", "require_admin": false, "show_in_sidebar": true})
		return l.entries[len(l.entries)-1], true
	case "lovelace/dashboards/delete":
		for i, e := range l.entries {
			if e["id"] == p["dashboard_id"] {
				delete(l.configs, fmt.Sprint(e["url_path"]))
				l.entries = append(l.entries[:i], l.entries[i+1:]...)
				if l.landThenReject {
					return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "HA rejected 'lovelace/dashboards/delete': error: storage"}, true
				}
				return nil, true
			}
		}
		return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "HA rejected 'lovelace/dashboards/delete': not_found: Unable to find"}, true
	}
	return nil, false
}

func homeDashboard() map[string]any {
	return map[string]any{"title": "Home", "views": []any{
		map[string]any{"title": "Living room", "path": "living", "cards": []any{
			map[string]any{"type": "tile", "entity": "light.kitchen"},
			map[string]any{"type": "custom:mushroom-light-card", "entity": "light.hall"},
		}},
		map[string]any{"title": "Doors", "type": "sections", "sections": []any{
			map[string]any{"type": "grid", "cards": []any{map[string]any{"type": "heading", "heading": "Locks"}, map[string]any{"type": "tile", "entity": "lock.front"}}},
		}},
	}}
}

func dashboardFake() *fakeHA {
	f := newFakeHA()
	f.full = append(f.full, map[string]any{"entity_id": "light.kitchen", "platform": "hue"}, map[string]any{"entity_id": "light.hall", "platform": "hue"},
		map[string]any{"entity_id": "lock.front", "platform": "zwave_js"}, map[string]any{"entity_id": "light.attic", "platform": "hue", "disabled_by": "user"})
	f.lovelace = &fakeLovelace{
		entries: []map[string]any{
			{"id": "home_main", "url_path": "home-main", "title": "Home", "mode": "storage", "require_admin": false, "show_in_sidebar": true},
			{"id": "", "url_path": "yaml-board", "title": "YAML board", "mode": "yaml"},
		},
		configs:   map[string]any{"home-main": homeDashboard(), "yaml-board": map[string]any{"views": []any{}}},
		resources: []map[string]any{{"id": "r1", "type": "module", "url": "/hacsfiles/lovelace-mushroom/mushroom.js?hacstag=1"}},
	}
	return f
}

func editedHome() map[string]any {
	cfg := homeDashboard()
	views := cfg["views"].([]any)
	living := views[0].(map[string]any)
	living["cards"] = []any{map[string]any{"type": "tile", "entity": "light.kitchen", "color": "amber"}, living["cards"].([]any)[1],
		map[string]any{"type": "tile", "entity": "light.attic"}}
	cfg["views"] = append([]any{views[1], views[0]}, map[string]any{"title": "Garden", "cards": []any{map[string]any{"type": "tile", "entity": "light.hall"}}})
	return cfg
}

func (h *writeHarness) lovelaceSaves() []string {
	var out []string
	for _, p := range h.f.lovelace.sent {
		out = append(out, fmt.Sprint(p["type"]))
	}
	return out
}

func TestDashboardWritePreviewsCardLevelDiffSnapshotsSavesOnceAndReverts(t *testing.T) {
	h := newWriteHarness(t, dashboardFake())
	exit, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	if exit != 3 || env.Tier != "preview" || env.Plan.Confirm != "natural" || env.Config.Body != nil {
		t.Fatalf("plan: exit %d env %+v", exit, env)
	}
	diff := strings.Join(env.Diff, "\n")
	for _, want := range []string{
		`| View "Living room" › card 1 (tile light.kitchen) › color | — | amber |`,
		`| View "Living room" › card 3 | — | tile light.attic |`,
		`| View "Garden" | — | masonry view, 1 card |`,
		`| View order | View "Living room", View "Doors" | View "Doors", View "Living room" |`,
	} {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff lacks %s:\n%s", want, diff)
		}
	}
	if strings.Contains(diff, `"views"`) || !strings.Contains(string(env.Result), `"entity_id":"light.attic","detail":"disabled (by user)","new":true`) {
		t.Fatalf("diff dumps JSON or issues miss the disabled entity: %s\n%s", diff, env.Result)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || applied.Status != "verified" || applied.Undo == nil || applied.Undo.Domain != "dashboard" || applied.Snapshot == "" {
		t.Fatalf("apply: exit %d env %+v", exit, applied)
	}
	if got := strings.Join(h.lovelaceSaves(), ","); got != "lovelace/config/save" {
		t.Fatalf("writes %s", got)
	}
	if snap := h.f.snapshots[0]; snap["category"] != "dashboards" || snap["name"] != "auto-home-main" || canonicalJSON(mustJSON(snap["data"])) != canonicalJSON(mustJSON(homeDashboard())) {
		t.Fatalf("snapshot %+v", snap)
	}
	exit, revert := h.run(runPlanCommand, "revert")
	if exit != 3 || revert.Config.Op != "revert" || revert.Config.Domain != "dashboard" {
		t.Fatalf("revert plan: exit %d env %+v", exit, revert)
	}
	if exit, done := h.run(runApplyCommand, "--plan", revert.Plan.ID); exit != 0 || done.Status != "verified" {
		t.Fatalf("revert apply: exit %d env %+v", exit, done)
	}
	if canonicalJSON(mustJSON(h.f.lovelace.configs["home-main"])) != canonicalJSON(mustJSON(homeDashboard())) {
		t.Fatalf("revert did not restore: %v", h.f.lovelace.configs["home-main"])
	}
}

func homeBase() string { return digestOf(mustJSON(homeDashboard())) }

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func TestDashboardUIEditBeforeApplyIsDriftAndSavesNothing(t *testing.T) {
	for _, editAt := range []int{2, 3} { // 2: before apply's recompute; 3: during the snapshot, before the save
		t.Run(fmt.Sprint(editAt), func(t *testing.T) {
			f := dashboardFake()
			f.lovelace.editAt, f.lovelace.editPath = editAt, "home-main"
			h := newWriteHarness(t, f)
			_, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			if exit != 2 || applied.Error.Code != "PLAN_DRIFT" || len(h.lovelaceSaves()) != 0 {
				t.Fatalf("exit %d env %+v saves %v", exit, applied.Error, h.lovelaceSaves())
			}
			if title := f.lovelace.configs["home-main"].(map[string]any)["views"].([]any)[0].(map[string]any)["title"]; title != "Edited in the UI" {
				t.Fatalf("the UI edit was overwritten: %v", title)
			}
		})
	}
}

func TestDashboardRefusalsSendNothing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		exit int
		code string
	}{
		{"yaml is read-only", []string{"--id", "yaml-board", "--base", "x"}, 4, "DASHBOARD_NOT_STORAGE"},
		{"unknown url_path", []string{"--id", "nope-board", "--base", "x"}, 5, "NOT_FOUND"},
		{"a stale base", []string{"--id", "home-main", "--base", digestOf(mustJSON(map[string]any{"views": []any{}}))}, 2, "PLAN_DRIFT"},
		{"create over an existing one", []string{"--id", "home-main", "--create", "--title", "Copy"}, 4, "CONFIG_ID_IN_USE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHarness(t, dashboardFake())
			exit, env := h.run(runPlanCommand, append([]string{"write", "--domain", "dashboard", "--file", draftFile(t, homeDashboard())}, tc.args...)...)
			if exit != tc.exit || env.Error == nil || env.Error.Code != tc.code || len(h.lovelaceSaves()) != 0 {
				t.Fatalf("exit %d env %+v", exit, env.Error)
			}
		})
	}
	h := newWriteHarness(t, dashboardFake())
	draft := homeDashboard()
	draft["views"] = append(draft["views"].([]any), map[string]any{"cards": []any{map[string]any{"type": "tile", "entity": "light.guessed"}}})
	exit, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, draft))
	if exit != 4 || env.Error.Code != "UNKNOWN_ENTITY" || !strings.Contains(env.Error.Message, "light.guessed") {
		t.Fatalf("guessed entity: exit %d env %+v", exit, env.Error)
	}
	for _, bad := range [][]string{{"--id", "x-y", "--create"}, {"--id", "home-main", "--title", "T"}, {"--id", "home", "--create", "--title", "T"}, {"--id", "a b"}, {"--id", "home-main"}, {}} {
		if exit, env := h.run(runPlanCommand, append([]string{"write", "--domain", "dashboard", "--file", draftFile(t, draft)}, bad...)...); exit != 1 || env.Error.Code != "USAGE" {
			t.Fatalf("%v: exit %d", bad, exit)
		}
	}
}

func TestDashboardCreateMakesTheShellThenSavesWithoutSnapshot(t *testing.T) {
	h := newWriteHarness(t, dashboardFake())
	exit, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "new-board", "--create", "--title", "New board", "--icon", "mdi:home", "--require-admin", "--hide-sidebar", "--file", draftFile(t, homeDashboard()))
	if exit != 3 || env.Tier != "preview" || !strings.Contains(strings.Join(env.Diff, "\n"), `| View "Doors" | — | sections view, 1 section, 2 cards |`) {
		t.Fatalf("plan: exit %d env %+v", exit, env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || applied.Status != "verified" || applied.Undo != nil || len(h.f.snapshots) != 0 {
		t.Fatalf("apply: exit %d env %+v", exit, applied)
	}
	if got := strings.Join(h.lovelaceSaves(), ","); got != "lovelace/dashboards/create,lovelace/config/save" || h.f.lovelace.sent[0]["icon"] != "mdi:home" ||
		h.f.lovelace.sent[0]["require_admin"] != true || h.f.lovelace.sent[0]["show_in_sidebar"] != false {
		t.Fatalf("writes %s %v", got, h.f.lovelace.sent)
	}
}

func TestDashboardDeleteIsTypedAndSnapshotsShellAndConfig(t *testing.T) {
	h := newWriteHarness(t, dashboardFake())
	exit, env := h.run(runPlanCommand, "delete", "--domain", "dashboard", "--id", "home-main")
	if exit != 3 || env.Tier != "typed" || !fourDigits.MatchString(env.Plan.Code) || !strings.Contains(env.Say, env.Plan.Code) {
		t.Fatalf("plan: exit %d env %+v", exit, env)
	}
	if exit, _ := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 4 || len(h.lovelaceSaves()) != 0 {
		t.Fatalf("no code: exit %d", exit)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if exit != 0 || applied.Status != "verified" || h.lovelaceSaves()[0] != "lovelace/dashboards/delete" || h.f.lovelace.sent[0]["dashboard_id"] != "home_main" {
		t.Fatalf("apply: exit %d env %+v sent %v", exit, applied, h.f.lovelace.sent)
	}
	data := h.f.snapshots[0]["data"].(map[string]any)
	if data["shell"].(map[string]any)["url_path"] != "home-main" || canonicalJSON(mustJSON(data["config"])) != canonicalJSON(mustJSON(homeDashboard())) {
		t.Fatalf("snapshot %+v", data)
	}
}

func TestDashboardRejectedSaveIsFailedWithoutCheckpointAndSnapshotFailureSavesNothing(t *testing.T) {
	f := dashboardFake()
	f.lovelace.rejectAll = true
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 6 || applied.Status != "failed" || applied.Error.Code != "CONFIG_REJECTED" || applied.Undo != nil ||
		len(applied.Verification) != 1 || applied.Verification[0].Observed != "read-back differs" {
		t.Fatalf("rejected: exit %d env %+v", exit, applied)
	}
	f.lovelace.rejectAll, f.snapshotErr = false, true
	_, env = h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	exit, applied = h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 1 || applied.Error.Code != "SNAPSHOT_FAILED" || len(h.lovelaceSaves()) != 1 {
		t.Fatalf("snapshot failure: exit %d env %+v saves %v", exit, applied.Error, h.lovelaceSaves())
	}
	if exit, applied = h.run(runApplyCommand, "--plan", env.Plan.ID, "--no-snapshot"); exit != 0 || applied.Status != "verified" {
		t.Fatalf("--no-snapshot after the failure: exit %d env %+v", exit, applied)
	}
}

func TestDashboardSaveTimeoutIsUnknownAndStaysRevertible(t *testing.T) {
	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	f := dashboardFake()
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	f.hang["lovelace/config/save"] = true
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 7 || !applied.OutcomeUnknown || applied.Undo == nil {
		t.Fatalf("exit %d env %+v", exit, applied)
	}
}

func TestDashboardRevertRefusesAfterAnEditInTheUI(t *testing.T) {
	f := dashboardFake()
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	h.run(runApplyCommand, "--plan", env.Plan.ID)
	f.lovelace.configs["home-main"] = map[string]any{"views": []any{}}
	if exit, revert := h.run(runPlanCommand, "revert", "--target", "home-main", "--domain", "dashboard"); exit != 2 || revert.Error.Code != "REVERT_DRIFT" {
		t.Fatalf("exit %d env %+v", exit, revert.Error)
	}
}

func TestDashboardUnlistedDefaultSavesWithoutURLPath(t *testing.T) {
	f := dashboardFake()
	f.lovelace.configs[""] = homeDashboard()
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "lovelace", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 || applied.Status != "verified" {
		t.Fatalf("exit %d env %+v", exit, applied)
	}
	if _, has := h.f.lovelace.sent[0]["url_path"]; has {
		t.Fatalf("the default dashboard save named a url_path: %v", h.f.lovelace.sent[0])
	}
}

func TestGetDashboardsSweepReportsBrokenCards(t *testing.T) {
	f := dashboardFake()
	cfg := homeDashboard()
	cards := cfg["views"].([]any)[0].(map[string]any)
	cards["cards"] = append(cards["cards"].([]any), map[string]any{"type": "custom:button-card", "entity": "sensor.gone"},
		map[string]any{"type": "vertical-stack", "cards": []any{map[string]any{"type": "entities", "entities": []any{"light.attic", map[string]any{"entity": "light.kitchen"}}}}},
		map[string]any{"type": "button", "tap_action": map[string]any{"action": "perform-action", "perform_action": "light.turn_on", "target": map[string]any{"entity_id": "light.ghost"}}})
	f.lovelace.configs["home-main"] = cfg
	paths, _ := setupVerbRelayTest(t, f.reply)
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"--dashboards"}) })
	if exit != 0 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	var result dashboardsResult
	_ = json.Unmarshal(env.Result, &result)
	var got []string
	for _, d := range result.Dashboards {
		for _, issue := range d.Issues {
			got = append(got, d.URLPath+" "+issue.Kind+" "+issue.EntityID+issue.CardType)
		}
	}
	want := "home-main missing_entity sensor.gone|home-main card_type_unconfirmed custom:button-card|home-main missing_entity light.attic|home-main missing_entity light.ghost"
	if strings.Join(got, "|") != want || len(result.Dashboards) != 3 || result.Dashboards[0].Mode != "empty" {
		t.Fatalf("issues %s\ndashboards %+v", strings.Join(got, "|"), result.Dashboards)
	}
	exit, env = runVerbForTest(t, func() int { return runGetCommand(paths, []string{"--dashboard", "home-main"}) })
	if exit != 0 || !strings.Contains(string(env.Result), `"section 1 › card 2: tile lock.front"`) || !strings.Contains(string(env.Result), `"custom:mushroom-light-card":"/hacsfiles/lovelace-mushroom/mushroom.js?hacstag=1"`) {
		t.Fatalf("one dashboard: exit %d %s", exit, env.Result)
	}
}

func TestProvidedByMatchesResourceFileAndFolderNames(t *testing.T) {
	resources := []lovelaceResource{{URL: "/hacsfiles/lovelace-mushroom/mushroom.js?hacstag=1"}, {URL: "/hacsfiles/mini-graph-card/mini-graph-card-bundle.js"},
		{URL: "/hacsfiles/lovelace-layout-card/layout-card.js"}, {URL: "/local/community/Bubble-Card/bubble-card.js"}, {URL: "/local/my.js"}}
	for cardType, provided := range map[string]bool{"custom:mushroom-light-card": true, "custom:mini-graph-card": true, "custom:masonry-layout": true,
		"custom:layout-card": true, "custom:bubble-card": true, "custom:button-card": false, "custom:local-card": false, "custom:mushroom": true, "custom:mushroomx-card": false} {
		if got := providedBy(cardType, resources) != ""; got != provided {
			t.Fatalf("%s: provided %v, want %v", cardType, got, provided)
		}
	}
}

func TestDashboardWriteAllowlistIsExact(t *testing.T) {
	ok := []map[string]any{
		{"type": "lovelace/config/save", "url_path": "a-b", "config": map[string]any{}},
		{"type": "lovelace/config/save", "config": map[string]any{}},
		{"type": "lovelace/dashboards/create", "url_path": "a-b", "title": "T"},
		{"type": "lovelace/dashboards/create", "url_path": "a-b", "title": "T", "icon": "mdi:x"},
		{"type": "lovelace/dashboards/delete", "dashboard_id": "a_b"},
	}
	refused := []map[string]any{
		{"type": "lovelace/config/save", "url_path": "a-b"},
		{"type": "lovelace/config/save", "url_path": "a-b", "config": map[string]any{}, "force": true},
		{"type": "lovelace/config/delete", "url_path": "a-b"},
		{"type": "lovelace/dashboards/update", "dashboard_id": "a_b", "title": "T"},
		{"type": "lovelace/dashboards/create", "url_path": "a-b", "title": "T", "mode": "yaml"},
		{"type": "lovelace/dashboards/delete", "dashboard_id": ""},
		{"type": "lovelace/resources/create", "res_type": "module", "url": "/x.js"},
	}
	for _, p := range ok {
		if err := checkWriteRequest("ws", p); err != nil {
			t.Fatalf("%v refused: %v", p, err)
		}
		if checkReadOnlyRequest("ws", p) == nil {
			t.Fatalf("%v passes the read allowlist", p)
		}
	}
	for _, p := range refused {
		if checkWriteRequest("ws", p) == nil {
			t.Fatalf("%v allowed", p)
		}
	}
	if checkWriteRequest("backups", map[string]any{"action": "save", "category": "dashboards", "name": "auto-home", "data": map[string]any{}}) != nil {
		t.Fatal("dashboard snapshot refused")
	}
}

func TestDashboardDeleteOfTheListedDefaultNamesAPlainWriteToRestore(t *testing.T) {
	f := dashboardFake()
	f.lovelace.entries = append(f.lovelace.entries, map[string]any{"id": "lovelace", "url_path": "lovelace", "title": "Overview", "mode": "storage", "require_admin": true, "show_in_sidebar": true})
	f.lovelace.configs["lovelace"] = homeDashboard()
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "delete", "--domain", "dashboard", "--id", "lovelace")
	_, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if !strings.Contains(applied.Restore, "--id lovelace --base") || strings.Contains(applied.Restore, "--create") {
		t.Fatalf("restore %q", applied.Restore)
	}
	_, env = h.run(runPlanCommand, "delete", "--domain", "dashboard", "--id", "home-main")
	if _, applied = h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); !strings.Contains(applied.Restore, `--create --title "Home"`) {
		t.Fatalf("restore %q", applied.Restore)
	}
}

func TestDashboardVerifyComparesNumbersByValue(t *testing.T) {
	f := dashboardFake()
	h := newWriteHarness(t, f)
	path := filepath.Join(t.TempDir(), "d.json")
	_ = os.WriteFile(path, []byte(`{"views":[{"title":"Gauge","cards":[{"type":"gauge","entity":"light.kitchen","min":1.50}]}]}`), 0o600)
	_, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", path)
	// The fake decodes the sent config, so it serves 1.5 for the draft's 1.50.
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 || applied.Status != "verified" {
		t.Fatalf("exit %d env %+v", exit, applied)
	}
}

func TestDashboardDiffShowsNonListShapesAsValues(t *testing.T) {
	rows := strings.Join(dashboardDiff(map[string]any{"views": []any{map[string]any{"title": "A", "cards": "x"}}},
		map[string]any{"views": []any{map[string]any{"title": "A", "cards": "y"}}}), "\n")
	if !strings.Contains(rows, `| View "A" › cards | x | y |`) {
		t.Fatalf("rows %s", rows)
	}
	rows = strings.Join(dashboardDiff(map[string]any{"views": "a"}, map[string]any{"views": "b"}), "\n")
	if !strings.Contains(rows, "| Dashboard › views | a | b |") {
		t.Fatalf("rows %s", rows)
	}
	if checkReadOnlyRequest("ws", map[string]any{"type": "lovelace/config", "url_path": "a-b", "force": true}) == nil {
		t.Fatal("a forced lovelace/config read passed the read allowlist")
	}
}

func TestDashboardErrorsAreReportedByWhatHomeAssistantHoldsAfterwards(t *testing.T) {
	// A save that lands in memory and is then answered with an error.
	f := dashboardFake()
	f.lovelace.landThenReject = true
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 6 || applied.Status != "unverified" || !strings.Contains(applied.Error.Message, "yet it now serves the planned config") ||
		applied.Undo == nil || applied.BeforeConfig == nil {
		t.Fatalf("landed save: exit %d env %+v", exit, applied)
	}
	// A delete that lands and is then answered with an error keeps its restore.
	_, env = h.run(runPlanCommand, "delete", "--domain", "dashboard", "--id", "home-main")
	exit, applied = h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if exit != 6 || !strings.Contains(applied.Error.Message, "the dashboard is gone") || applied.Restore == "" {
		t.Fatalf("landed delete: exit %d env %+v", exit, applied)
	}
	// A rejected save whose read-back fails is unknown, never "nothing was saved".
	f = dashboardFake()
	f.lovelace.rejectAll, f.lovelace.failReadsFrom = true, 4 // plan, recompute, pre-save re-read succeed
	h = newWriteHarness(t, f)
	_, env = h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, editedHome()))
	if exit, applied = h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 7 || !applied.OutcomeUnknown || applied.Undo == nil {
		t.Fatalf("unread rejection: exit %d env %+v", exit, applied)
	}
	// A create whose content save is rejected names the empty dashboard.
	f = dashboardFake()
	f.lovelace.rejectAll = true
	h = newWriteHarness(t, f)
	_, env = h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "new-board", "--create", "--title", "New", "--file", draftFile(t, homeDashboard()))
	if exit, applied = h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 6 || !strings.Contains(applied.Error.Message, "exists, but it is empty") {
		t.Fatalf("rejected create: exit %d env %+v", exit, applied.Error)
	}
}

func TestDashboardEmptyDeleteRestoresFromAnEmptyObject(t *testing.T) {
	f := dashboardFake()
	delete(f.lovelace.configs, "home-main")
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "delete", "--domain", "dashboard", "--id", "home-main")
	if _, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); !strings.Contains(applied.Restore, "a file holding {}") {
		t.Fatalf("restore %q", applied.Restore)
	}
}

func TestDashboardShapesBareBadgesHeaderCardsAndMissingYAML(t *testing.T) {
	f := dashboardFake()
	cfg := homeDashboard()
	doors := cfg["views"].([]any)[1].(map[string]any)
	doors["badges"] = []any{"sensor.gone_badge"}
	doors["header"] = map[string]any{"layout": "center", "card": map[string]any{"type": "markdown", "content": "Hi"}}
	f.lovelace.configs["home-main"] = cfg
	delete(f.lovelace.configs, "yaml-board")
	paths, _ := setupVerbRelayTest(t, f.reply)
	_, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"--dashboards"}) })
	got := string(env.Result)
	for _, want := range []string{`"entity_id":"sensor.gone_badge"`, `"url_path":"yaml-board","title":"YAML board","mode":"yaml"`, `"kind":"config_missing"`, `"cards":5`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	h := newWriteHarness(t, dashboardFake())
	draft := homeDashboard()
	draft["views"].([]any)[0].(map[string]any)["badges"] = []any{"sensor.guessed_badge"}
	if exit, plan := h.run(runPlanCommand, "write", "--domain", "dashboard", "--id", "home-main", "--base", homeBase(), "--file", draftFile(t, draft)); exit != 4 || plan.Error.Code != "UNKNOWN_ENTITY" {
		t.Fatalf("guessed bare badge: exit %d %+v", exit, plan.Error)
	}
}

func shapeConfig(t *testing.T, raw string) map[string]any {
	t.Helper()
	cfg, err := configObjectFromBytes([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// Shapes from the review: every literal entity reference is found, actions
// and templates are not, statistics are never judged as entities.
func TestDashboardIssuesCoverEveryCardShape(t *testing.T) {
	present := func(id string) string {
		if id == "light.real" {
			return ""
		}
		return "not in Home Assistant"
	}
	cfg := shapeConfig(t, `{"views":[{"type":"sections","sections":[
	 {"type":"grid","visibility":[{"condition":"state","entity":"light.guess1"}],"cards":[{"type":"heading","heading":"x","badges":[{"type":"entity","entity":"sensor.guess3"}]}]},
	 {"type":"grid","visibility":[{"condition":"state","entity":"light.real"}],"cards":[]}]},
	 {"cards":[{"type":"picture","image_entity":"image.guess"},{"type":"conditional","conditions":[{"entity":"light.guess4"}],"card":{"type":"tile","entity":"light.Guess5"}},
	  {"type":"glance","entities":["light.guess6","all"]},{"type":"button","tap_action":{"action":"perform-action","perform_action":"light.turn_on","target":{"entity_id":"all"}}},
	  {"type":"statistic","entity":"sensor.old_energy"},{"type":"markdown","content":"{{ states('sensor.tpl') }}"}]}]}`)
	var got []string
	for _, issue := range dashboardIssues(cfg, present, nil) {
		got = append(got, issue.EntityID)
	}
	if want := "light.guess1,sensor.guess3,image.guess,light.guess4,light.guess5,light.guess6"; strings.Join(got, ",") != want {
		t.Fatalf("issues %v, want %s", got, want)
	}
	if summary := viewSummary(asMap(shapeConfig(t, `{"strategy":{"type":"custom:foo"}}`))); summary != "custom:foo strategy view" {
		t.Fatalf("strategy view summary %q", summary)
	}
}

func TestDashboardDiffRecursesAndPairsRenamedViews(t *testing.T) {
	cases := []struct{ before, after, want string }{
		{`{"views":[{"cards":[{"type":"grid","cards":[{"type":"tile","entity":"light.a"},{"type":"tile","entity":"light.c"}]}]}]}`,
			`{"views":[{"cards":[{"type":"grid","cards":[{"type":"tile","entity":"light.a"},{"type":"tile","entity":"light.z"}]}]}]}`,
			`| View 1 › card 1 (grid (2 cards)) › card 2 (tile light.z) › entity | light.c | light.z |`},
		{`{"views":[{"title":"Home","cards":[{"type":"tile","entity":"light.a"},{"type":"tile","entity":"light.b"}]}]}`,
			`{"views":[{"title":"House","cards":[{"type":"tile","entity":"light.a"},{"type":"tile","entity":"lock.front"}]}]}`,
			`| View "House" › card 2 (tile lock.front) › entity | light.b | lock.front |`},
		{`{"views":[{"cards":[{"type":"conditional","card":{"type":"tile","entity":"light.a"}}]}]}`,
			`{"views":[{"cards":[{"type":"conditional","card":{"type":"tile","entity":"lock.front"}}]}]}`,
			`| View 1 › card 1 (conditional) › card (tile lock.front) › entity | light.a | lock.front |`},
		{`{"views":[]}`, `{"views":[{"title":"New","cards":[{"type":"tile","entity":"light.a"}]}]}`,
			`| View "New" › card 1 | — | tile light.a |`},
		{`{"views":[{"cards":[{"type":"tile","entity":"light.a"}]}]}`, `{"views":[{"cards":[{"type":"tile","entity":"light.a","name":null}]}]}`,
			`| View 1 › card 1 (tile light.a) › name | — | null |`},
	}
	for _, tc := range cases {
		if rows := strings.Join(dashboardDiff(shapeConfig(t, tc.before), shapeConfig(t, tc.after)), "\n"); !strings.Contains(rows, tc.want) {
			t.Fatalf("want %s in:\n%s", tc.want, rows)
		}
	}
}

func TestGetDashboardsSweepGoesOnPastAnUnreadableDashboard(t *testing.T) {
	f := dashboardFake()
	f.lovelace.configs["yaml-board"] = "views: [broken" // a config that is not an object
	paths, _ := setupVerbRelayTest(t, f.reply)
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"--dashboards"}) })
	if exit != 0 || !strings.Contains(string(env.Result), `"url_path":"yaml-board"`) || !strings.Contains(string(env.Result), `"error":`) ||
		!strings.Contains(string(env.Result), `"url_path":"home-main"`) {
		t.Fatalf("exit %d %s", exit, env.Result)
	}
	if exit, _ := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"--dashboard", "yaml-board"}) }); exit == 0 {
		t.Fatal("a single unreadable dashboard read must fail")
	}
}

// Sibling sweep: an automation delete whose outcome is unknown still names
// its restore, since the delete may have landed.
func TestConfigDeleteWithUnknownOutcomeKeepsItsRestore(t *testing.T) {
	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
	fastVerify(t)
	hangDelete := false
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		if hangDelete && p["method"] == "DELETE" {
			<-r.Context().Done()
			return nil
		}
		return f.reply(r, endpoint, p)
	}, true)
	h := &writeHarness{t: t, f: f, paths: paths, log: log}
	_, env := h.run(runPlanCommand, "delete", "automation.night")
	hangDelete = true
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if exit != 7 || applied.Snapshot == "" || applied.Restore == "" {
		t.Fatalf("exit %d env %+v", exit, applied)
	}
}
