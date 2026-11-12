package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

type updatesFixture struct {
	mapFixture
	components  []string
	repairs     []map[string]any
	backups     any
	manifests   []map[string]any
	traces      []map[string]any
	coreState   string
	repairsFail bool
	supervisor  map[string]any // path → data; a missing path fails upstream
}

func (f updatesFixture) reply(r *http.Request, endpoint string, payload map[string]any) any {
	switch {
	case endpoint == "core" && payload["path"] == "/api/config":
		return coreReply(200, map[string]any{"version": "2026.10.2", "state": firstNonEmpty(f.coreState, "RUNNING"), "components": f.components})
	case endpoint == "supervisor":
		if data, ok := f.supervisor[payload["path"].(string)]; ok {
			return map[string]any{"status": 200, "body": map[string]any{"result": "ok", "data": data}}
		}
		return fakeRelayFailure{code: "UPSTREAM_SUPERVISOR_ERROR", message: "Supervisor unreachable"}
	}
	switch payload["type"] {
	case "repairs/list_issues":
		if f.repairsFail {
			return fakeRelayFailure{code: "UPSTREAM_WS_COMMAND_ERROR", message: "unknown_error"}
		}
		return map[string]any{"issues": append([]map[string]any{}, f.repairs...)}
	case "backup/info":
		return f.backups
	case "manifest/list":
		return f.manifests
	case "trace/list":
		return append([]map[string]any{}, f.traces...)
	}
	return f.mapFixture.reply(r, endpoint, payload)
}

func upd(id, installed, latest string, attrs map[string]any) map[string]any {
	row := st(id, "on", map[string]any{"installed_version": installed, "latest_version": latest, "in_progress": false, "auto_update": false})
	for k, v := range attrs {
		row["attributes"].(map[string]any)[k] = v
	}
	return row
}

func backupInfo(age time.Duration, agents ...string) map[string]any {
	held := map[string]any{}
	for _, agent := range agents {
		held[agent] = map[string]any{"protected": true, "size": 1}
	}
	return map[string]any{"state": "idle", "backups": []map[string]any{{"backup_id": "b1", "date": ago(age), "agents": held}}}
}

// haosHome: Core 2026.10.2 → 2026.11.1 on a healthy HAOS, an App that needs
// the new Core, one that needs a Core not offered yet, the NOVA App, a
// Marketplace integration, and lock firmware.
func haosHome() updatesFixture {
	return updatesFixture{
		mapFixture: mapFixture{
			devices: []map[string]any{{"id": "d_lock", "area_id": nil, "config_entry_id": "e_zwave"}},
			entities: []map[string]any{
				ent("update.home_assistant_core_update", "", "hassio", "", map[string]any{"unique_id": "home_assistant_core_version_latest"}),
				ent("update.mosquitto_update", "", "hassio", "", map[string]any{"unique_id": "core_mosquitto_version_latest"}),
				ent("update.future_update", "", "hassio", "", map[string]any{"unique_id": "future_version_latest"}),
				ent("update.ha_nova_update", "", "hassio", "", map[string]any{"unique_id": "2368fcfa_ha_nova_relay_version_latest"}),
				ent("update.card_update", "", "marketplace", "", map[string]any{"unique_id": "x"}),
				ent("update.lock_firmware", "d_lock", "zwave_js", "e_zwave", map[string]any{"unique_id": "y"}),
				ent("lock.front_door", "d_lock", "zwave_js", "e_zwave", nil),
			},
			entries: []map[string]any{{"entry_id": "e_zwave", "domain": "zwave_js", "state": "loaded"}},
			states: []map[string]any{
				upd("update.home_assistant_core_update", "2026.10.2", "2026.11.1", map[string]any{"friendly_name": "Home Assistant Core"}),
				upd("update.mosquitto_update", "6.4.0", "6.5.0", map[string]any{"friendly_name": "Mosquitto"}),
				upd("update.future_update", "1.0", "2.0", map[string]any{"friendly_name": "Future App"}),
				upd("update.ha_nova_update", "0.13.26", "0.13.27", map[string]any{"friendly_name": "HA NOVA"}),
				upd("update.card_update", "1.0", "1.1", map[string]any{"friendly_name": "Mushroom"}),
				upd("update.lock_firmware", "1.2", "1.3", map[string]any{"friendly_name": "Front door firmware"}),
				st("update.quiet", "off", map[string]any{"installed_version": "1", "latest_version": "1"}),
				st("lock.front_door", "locked", nil),
			},
		},
		components: []string{"hassio", "zwave_js", "sensor.template"},
		backups:    backupInfo(2*time.Hour, "backup.local", "cloud.cloud"),
		supervisor: map[string]any{
			"/resolution/info":                     map[string]any{"unhealthy": []string{}, "unsupported": []string{}},
			"/host/info":                           map[string]any{"disk_free": 20.5},
			"/store/addons/core_mosquitto":         map[string]any{"homeassistant": "2026.11.0"},
			"/store/addons/future":                 map[string]any{"homeassistant": "2026.12.0"},
			"/store/addons/2368fcfa_ha_nova_relay": map[string]any{"homeassistant": nil},
		},
	}
}

type updatesResult struct {
	Ready      string           `json:"ready"`
	Readiness  []map[string]any `json:"readiness"`
	NotChecked []map[string]any `json:"not_checked"`
	Pending    []updateRow      `json:"pending"`
	Order      []string         `json:"order"`
	Core       map[string]any   `json:"core"`
}

func runUpdates(t *testing.T, f updatesFixture, args ...string) (updatesResult, *fakeRelayLog) {
	t.Helper()
	releaseNotesServer(t, nil) // never the network
	paths, log := setupVerbRelayTest(t, f.reply)
	exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, append([]string{"--section", "updates"}, args...)) })
	if exit != 0 {
		t.Fatalf("exit %d: %+v", exit, env)
	}
	return decodeInto[updatesResult](t, env.Result), log
}

func codes(rows []map[string]any) []string {
	var out []string
	for _, row := range rows {
		out = append(out, firstNonEmpty(asString(row["code"]), asString(row["check"])))
	}
	return out
}

func asString(v any) string { s, _ := v.(string); return s }

func TestUpdatesOrderAndAppMinimums(t *testing.T) {
	got, _ := runUpdates(t, haosHome())
	want := []string{"update.home_assistant_core_update", "update.mosquitto_update", "update.ha_nova_update", "update.card_update", "update.lock_firmware"}
	if !slices.Equal(got.Order, want) {
		t.Fatalf("order %v, want %v", got.Order, want)
	}
	if got.Ready != "no" || !slices.Equal(codes(got.Readiness), []string{"APP_NEEDS_NEWER_CORE"}) || len(got.NotChecked) != 0 {
		t.Fatalf("readiness %+v / %+v / %s", got.Readiness, got.NotChecked, got.Ready)
	}
	kinds := map[string]string{}
	for _, row := range got.Pending {
		kinds[row.EntityID] = row.Kind
		if row.EntityID == "update.mosquitto_update" && !row.AfterCore || row.EntityID == "update.future_update" && !row.Blocked {
			t.Fatalf("app minimum not applied: %+v", row)
		}
	}
	if kinds["update.card_update"] != "integration" || kinds["update.lock_firmware"] != "firmware" || kinds["update.ha_nova_update"] != "app" || len(got.Pending) != 6 {
		t.Fatalf("kinds %v", kinds)
	}
	if got.Core["installed"] != "2026.10.2" || got.Core["latest"] != "2026.11.1" {
		t.Fatalf("core %v", got.Core)
	}
}

func TestUpdatesBlockersAndStaleBackup(t *testing.T) {
	f := haosHome()
	delete(f.supervisor, "/store/addons/future")
	f.supervisor["/resolution/info"] = map[string]any{"unhealthy": []string{"docker"}}
	f.supervisor["/host/info"] = map[string]any{"disk_free": 1.4}
	f.states[1]["attributes"].(map[string]any)["in_progress"] = true
	f.backups = backupInfo(30*time.Hour, "backup.local")
	f.repairs = []map[string]any{{"domain": "homeassistant_alerts", "issue_id": "a1", "ignored": false, "translation_placeholders": map[string]any{"title": "**Zigbee** <b>alert</b>"}},
		{"domain": "zha", "issue_id": "z", "ignored": true}}
	got, _ := runUpdates(t, f)
	if got.Ready != "no" || !slices.Equal(codes(got.Readiness), []string{"UPDATE_RUNNING", "SUPERVISOR_UNHEALTHY", "DISK_LOW", "NO_RECENT_BACKUP", "REPAIRS_OPEN"}) {
		t.Fatalf("readiness %+v", got.Readiness)
	}
	if got.Readiness[4]["count"] != float64(1) || strings.ContainsAny(asString(got.Readiness[4]["alerts"].([]any)[0]), "*<>") {
		t.Fatalf("repairs %+v", got.Readiness[4])
	}
	if !slices.Equal(codes(got.NotChecked), []string{"app_minimum"}) || got.Order[0] != "backup" {
		t.Fatalf("gaps %+v order %v", got.NotChecked, got.Order)
	}
}

func TestUpdatesUnreadableSupervisorIsNotChecked(t *testing.T) {
	f := haosHome()
	f.supervisor = map[string]any{}
	f.backups = backupInfo(time.Hour, "backup.local")
	got, _ := runUpdates(t, f)
	if got.Ready != "check" || !slices.Equal(codes(got.NotChecked), []string{"supervisor_health", "disk", "app_minimum", "app_minimum", "app_minimum"}) ||
		!slices.Equal(codes(got.Readiness), []string{"BACKUP_ONLY_ON_DEVICE"}) {
		t.Fatalf("got %+v / %+v", got.Readiness, got.NotChecked)
	}
}

func TestUpdatesWithoutSupervisorSendsNoSupervisorRead(t *testing.T) {
	f := updatesFixture{mapFixture: mapFixture{states: []map[string]any{st("update.quiet", "off", nil)}}, components: []string{"sensor"},
		backups: backupInfo(time.Hour, "cloud.cloud"), supervisor: map[string]any{}}
	got, log := runUpdates(t, f)
	if got.Ready != "yes" || !slices.Equal(codes(got.Readiness), []string{"SUPERVISOR_NOT_APPLICABLE"}) || len(got.Pending) != 0 || len(got.Order) != 0 {
		t.Fatalf("got %+v", got)
	}
	for _, call := range log.all() {
		if call.endpoint == "supervisor" {
			t.Fatalf("Supervisor read without Supervisor: %+v", call.payload)
		}
	}
}

func TestUpdatesMissingFieldsAreNotChecked(t *testing.T) {
	f := haosHome()
	f.supervisor["/host/info"] = map[string]any{}
	f.backups = map[string]any{"state": "idle"}
	got, _ := runUpdates(t, f)
	if !slices.Contains(codes(got.NotChecked), "disk") || !slices.Contains(codes(got.NotChecked), "backups") || got.Order[0] != "backup" {
		t.Fatalf("got %+v / %v", got.NotChecked, got.Order)
	}
}

func TestUpdatesRefusesLimit(t *testing.T) {
	releaseNotesServer(t, nil)
	paths, log := setupVerbRelayTest(t, haosHome().reply)
	exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, []string{"--section", "updates", "--limit", "5"}) })
	if exit == 0 || len(log.all()) != 0 || !strings.Contains(env.Error.Message, "--limit") {
		t.Fatalf("exit %d %+v", exit, env)
	}
}

func TestReadVerbSupervisorReadsAreExact(t *testing.T) {
	for _, path := range []string{"/resolution/info", "/host/info", "/store/addons/core_mosquitto"} {
		if err := checkReadOnlyRequest("supervisor", map[string]any{"method": "GET", "path": path}); err != nil {
			t.Fatalf("%s refused: %v", path, err)
		}
	}
	for _, payload := range []map[string]any{
		{"method": "POST", "path": "/resolution/info"},
		{"method": "GET", "path": "/host/info", "data": map[string]any{}},
		{"method": "GET", "path": "/addons/core_mosquitto/info"},
		{"method": "GET", "path": "/store/addons/self"},
		{"method": "POST", "path": "/store/addons/core_mosquitto/install"},
		{"method": "GET", "path": "/store/addons/core_mosquitto/install"},
		{"method": "GET", "path": "/supervisor/logs"},
		{"method": "GET", "path": "/store/addons/../host/info"},
	} {
		if checkReadOnlyRequest("supervisor", payload) == nil {
			t.Fatalf("allowed %v", payload)
		}
	}
}

func TestHAVersionOrder(t *testing.T) {
	for _, c := range []struct {
		a, b string
		less bool
	}{{"2026.10.2", "2026.11.0", true}, {"2026.11.0b3", "2026.11.0", true}, {"2026.11.0", "2026.11.0b3", false}, {"0.92.0", "2024.1.0", true}, {"2026.9.0", "2026.10.0", true}} {
		if less, ok := haVersionLess(c.a, c.b); !ok || less != c.less {
			t.Fatalf("%s < %s: %v %v", c.a, c.b, less, ok)
		}
	}
	if _, ok := haVersionLess("2026.11", "latest"); ok {
		t.Fatal("unreadable version compared")
	}
	if _, ok := haVersionLess("99999999999999999999.1.0", "2026.11.0"); ok {
		t.Fatal("an overflowing version compared")
	}
}

func TestUpdatesAppMinimumThatIsNoVersionIsNotChecked(t *testing.T) {
	f := haosHome()
	f.supervisor["/store/addons/core_mosquitto"] = map[string]any{"homeassistant": 2026}
	got, _ := runUpdates(t, f)
	if !slices.Contains(codes(got.NotChecked), "app_minimum") {
		t.Fatalf("not checked %+v", got.NotChecked)
	}
}

func TestUpdatesOddAnswersAreNotChecked(t *testing.T) {
	f := haosHome()
	f.components = []string{"zwave_js"} // hassio failed to load; its update entities still say Supervisor runs
	f.supervisor["/store/addons/core_mosquitto"] = map[string]any{"name": "Mosquitto"}
	f.backups = map[string]any{"backups": []map[string]any{{"date": ago(time.Hour), "agents": map[string]any{}}}}
	got, _ := runUpdates(t, f)
	if gaps := codes(got.NotChecked); !slices.Equal(gaps, []string{"backup_running", "backup_copy", "app_minimum"}) || got.Ready != "no" {
		t.Fatalf("gaps %v ready %s readiness %+v", gaps, got.Ready, got.Readiness)
	}
	if slices.Contains(codes(got.Readiness), "SUPERVISOR_NOT_APPLICABLE") {
		t.Fatal("Supervisor's own update entities were ignored")
	}
}
