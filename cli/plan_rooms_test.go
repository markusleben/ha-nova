package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

// roomsFake serves wowHome with live device areas: area writes change them,
// and fail answers the n-th area write instead (it may apply it first).
type roomsFake struct {
	mu     sync.Mutex
	f      mapFixture
	writes []string // "device=area" in order
	fail   func(n int, device, area string) (any, bool)
}

func newRoomsFake() *roomsFake { return &roomsFake{f: wowHome()} }

func (r *roomsFake) setArea(device string, area any) {
	for _, d := range r.f.devices {
		if d["id"] == device {
			d["area_id"] = area
		}
	}
}

func (r *roomsFake) area(device string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.f.devices {
		if d["id"] == device {
			s, _ := d["area_id"].(string)
			return s
		}
	}
	return "?"
}

func (r *roomsFake) reply(req *http.Request, endpoint string, p map[string]any) any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if endpoint == "ws" && p["type"] == deviceAreaRequest {
		device, area := fmt.Sprint(p["device_id"]), p["area_id"]
		r.writes = append(r.writes, fmt.Sprintf("%s=%v", device, area))
		if r.fail != nil {
			if out, applied := r.fail(len(r.writes), device, fmt.Sprint(area)); out != nil {
				if applied {
					r.setArea(device, area)
				}
				return out
			}
		}
		r.setArea(device, area)
		return map[string]any{"id": device, "area_id": area}
	}
	return r.f.reply(req, endpoint, p)
}

func (r *roomsFake) areaWrites() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.writes)
}

type roomsHarness struct {
	*writeHarness
	r *roomsFake
}

// newRoomsHarness: planRooms asserts that planning wrote nothing.
func newRoomsHarness(t *testing.T, r *roomsFake) *roomsHarness {
	fastVerify(t)
	paths, log := setupFakeRelay(t, r.reply, true)
	return &roomsHarness{&writeHarness{t: t, paths: paths, log: log}, r}
}

func (h *roomsHarness) planRooms() verbTestEnvelope {
	h.t.Helper()
	exit, env := h.run(runPlanCommand, "rooms")
	if exit != 3 || env.Tier != "preview" || env.Plan == nil || env.Plan.Confirm != "natural" {
		h.t.Fatalf("plan rooms: exit %d %+v %+v", exit, env, env.Error)
	}
	if w := h.r.areaWrites(); len(w) != 0 {
		h.t.Fatalf("planning wrote %v", w)
	}
	return env
}

var roomsOriginal = map[string]string{"d_guest": "", "d_kitchen": "", "d_radio": ""}

func (h *roomsHarness) assertAreas(want map[string]string) {
	h.t.Helper()
	for device, area := range want {
		if got := h.r.area(device); got != area {
			h.t.Errorf("%s in %q, want %q (writes %v)", device, got, area, h.r.areaWrites())
		}
	}
}

func TestPlanRoomsPreviewsAndSavesOnlyOnApply(t *testing.T) {
	h := newRoomsHarness(t, newRoomsFake())
	env := h.planRooms()
	res := decodeInto[roomsResult](t, env.Result)
	var got []string
	for _, c := range res.Changes {
		got = append(got, c.DeviceID+"→"+c.AreaID)
	}
	if strings.Join(got, ",") != "d_guest→guest,d_kitchen→kitchen,d_radio→kitchen" {
		t.Fatalf("changes %v", got)
	}
	if !strings.Contains(env.Say, "Put 3 devices in rooms: Guest Bedroom ← “Guest Bedroom Lamp”; Kitchen ← “Kitchen Lights” and “Küchenradio”.") ||
		!strings.Contains(env.NextStep, "only after the user's own reply approves it") {
		t.Errorf("say %q next %q", env.Say, env.NextStep)
	}
	h.assertAreas(roomsOriginal)

	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || applied.Status != "verified" || applied.Undo == nil || applied.Undo.Domain != roomsUndoDomain {
		t.Fatalf("apply: exit %d %+v %+v", exit, applied, applied.Error)
	}
	// what was saved is exactly what was previewed
	if saved := decodeInto[roomsResult](t, applied.Result); fmt.Sprint(saved) != fmt.Sprint(res) {
		t.Errorf("saved %v previewed %v", saved, res)
	}
	h.assertAreas(map[string]string{"d_guest": "guest", "d_kitchen": "kitchen", "d_radio": "kitchen"})
	if exit, again := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 4 || again.Error.Code != "PLAN_NOT_FOUND" {
		t.Errorf("second apply: exit %d %+v", exit, again.Error)
	}

	// undo: plan revert picks the rooms record and plans the way back
	exit, undo := h.run(runPlanCommand, "revert")
	if exit != 3 || undo.Tier != "preview" || !strings.Contains(undo.Say, "no room ← ") {
		t.Fatalf("revert plan: exit %d %+v %+v", exit, undo, undo.Error)
	}
	if exit, back := h.run(runApplyCommand, "--plan", undo.Plan.ID); exit != 0 || back.Status != "verified" {
		t.Fatalf("revert apply: exit %d %+v", exit, back.Error)
	}
	h.assertAreas(roomsOriginal)
	if w := h.r.areaWrites(); len(w) != 6 || w[3] != "d_guest=<nil>" {
		t.Errorf("writes %v", w)
	}
}

// A rename or a room change after the preview is drift: nothing is written.
func TestPlanRoomsDriftSendsNothing(t *testing.T) {
	for name, change := range map[string]func(r *roomsFake){
		"moved":   func(r *roomsFake) { r.setArea("d_radio", "bedroom") },
		"renamed": func(r *roomsFake) { r.f.devices[1]["name_by_user"] = "Radio" },
		"area gone": func(r *roomsFake) {
			r.f.areas = slices.DeleteFunc(r.f.areas, func(a map[string]any) bool { return a["area_id"] == "guest" })
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newRoomsHarness(t, newRoomsFake())
			env := h.planRooms()
			h.r.mu.Lock()
			change(h.r)
			h.r.mu.Unlock()
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			if exit == 0 || applied.Error == nil || len(h.r.areaWrites()) != 0 {
				t.Fatalf("exit %d %+v writes %v", exit, applied.Error, h.r.areaWrites())
			}
		})
	}
}

// An injected failure at each write: every earlier write is put back, the
// result says nothing was saved, and no undo record is left behind.
func TestPlanRoomsRollsBackAFailureAtEachWrite(t *testing.T) {
	for failAt := 1; failAt <= 3; failAt++ {
		for _, unknown := range []bool{false, true} {
			t.Run(fmt.Sprintf("write %d unknown %v", failAt, unknown), func(t *testing.T) {
				r := newRoomsFake()
				r.fail = func(n int, _, _ string) (any, bool) {
					switch {
					case n != failAt:
						return nil, false
					case unknown: // it landed, but the answer never came
						return fakeRelayFailure{"UPSTREAM_TIMEOUT", "timed out"}, true
					}
					return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "invalid area"}, false
				}
				h := newRoomsHarness(t, r)
				env := h.planRooms()
				exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
				if exit != verbExitUnverified || applied.Status != "failed" || applied.Error.Code != "ROOMS_NOT_SAVED" ||
					!strings.Contains(applied.Error.Message, "nothing was saved") || applied.Undo != nil {
					t.Fatalf("exit %d %+v undo %+v", exit, applied.Error, applied.Undo)
				}
				h.assertAreas(roomsOriginal)
				rollbacks := failAt - 1
				if unknown {
					rollbacks++
				}
				if w := r.areaWrites(); len(w) != failAt+rollbacks {
					t.Errorf("writes %v", w)
				}
				if _, err := selectServerUndoSnapshot(h.paths, "", "", ""); err == nil {
					t.Error("an undo record for nothing saved")
				}
			})
		}
	}
}

// When putting one back fails too, the result names exactly what stayed
// changed, and the undo record holds exactly those devices.
func TestPlanRoomsNamesWhatStayedWhenRollbackFails(t *testing.T) {
	r := newRoomsFake()
	r.fail = func(n int, device, area string) (any, bool) {
		if n == 3 || (n > 3 && device == "d_guest") {
			return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "refused"}, false
		}
		return nil, false
	}
	h := newRoomsHarness(t, r)
	env := h.planRooms()
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != verbExitUnverified || applied.Error.Code != "ROOMS_PARTIAL" ||
		!strings.Contains(applied.Error.Message, "“Guest Bedroom Lamp” → Guest Bedroom") || strings.Contains(applied.Error.Message, "Kitchen Lights") {
		t.Fatalf("exit %d %+v", exit, applied.Error)
	}
	h.assertAreas(map[string]string{"d_guest": "guest", "d_kitchen": "", "d_radio": ""})
	snap, err := selectServerUndoSnapshot(h.paths, "", "", "")
	compact := func(raw []byte) string { var b bytes.Buffer; _ = json.Compact(&b, raw); return b.String() }
	if err != nil || compact(snap.BeforeConfig) != `{"d_guest":""}` || compact(snap.ExpectedAfter) != `{"d_guest":"guest"}` {
		t.Fatalf("undo record %s %s %v", snap.BeforeConfig, snap.ExpectedAfter, err)
	}
	r.fail = nil
	exit, undo := h.run(runPlanCommand, "revert")
	if exit != 3 {
		t.Fatalf("revert: %d %+v", exit, undo.Error)
	}
	if exit, _ := h.run(runApplyCommand, "--plan", undo.Plan.ID); exit != 0 {
		t.Fatalf("revert apply %d", exit)
	}
	h.assertAreas(roomsOriginal)
}

// The write allowlist takes exactly one device's area, nothing else.
func TestDeviceAreaWriteAllowlist(t *testing.T) {
	for _, p := range []map[string]any{
		{"type": deviceAreaRequest, "device_id": "d1", "area_id": "kitchen"},
		{"type": deviceAreaRequest, "device_id": "d1", "area_id": nil},
	} {
		if err := checkWriteRequest("ws", p); err != nil {
			t.Errorf("%v: %v", p, err)
		}
	}
	for _, p := range []map[string]any{
		{"type": deviceAreaRequest, "device_id": "d1", "area_id": "kitchen", "name_by_user": "x"},
		{"type": deviceAreaRequest, "device_id": "d1", "disabled_by": "user", "area_id": nil},
		{"type": deviceAreaRequest, "device_id": "d1"},
		{"type": deviceAreaRequest, "device_id": "", "area_id": "kitchen"},
		{"type": deviceAreaRequest, "device_id": "d1", "area_id": ""},
		{"type": deviceAreaRequest, "device_id": "d1", "area_id": 3},
		{"type": "config/entity_registry/update", "entity_id": "light.x", "area_id": "kitchen"},
	} {
		if checkWriteRequest("ws", p) == nil {
			t.Errorf("allowed %v", p)
		}
	}
}

// A template plan is a normal automation create: preview first, saved and
// read back only by apply.
func TestPlanTemplateSavesOnlyOnApply(t *testing.T) {
	f := newFakeHA()
	f.ent("person.someone", "", "home", nil)
	f.full = []map[string]any{{"entity_id": "lock.front", "platform": "zwave_js", "area_id": "hall"}, {"entity_id": "light.hall", "platform": "hue", "area_id": "hall"}}
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "template", templateDoorLight)
	if exit != 3 || env.Tier != "preview" || env.Config == nil || env.Config.Op != "write" || len(h.writes()) != 0 {
		t.Fatalf("plan: exit %d %+v %+v writes %v", exit, env, env.Error, h.writes())
	}
	m := decodeInto[templateMatch](t, env.Result)
	if m.Template != templateDoorLight || m.Uses[0].EntityID != "lock.front" || m.Uses[1].EntityID != "light.hall" ||
		!strings.Contains(string(env.Config.Body), `"to":"unlocked"`) {
		t.Fatalf("template %+v body %s", m, env.Config.Body)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || applied.Status != "verified" || len(h.writes()) != 1 {
		t.Fatalf("apply: exit %d %+v writes %v", exit, applied.Error, h.writes())
	}
	if saved := fmt.Sprintf("%s", f.configs[env.Config.Path]); !strings.Contains(saved, `"alias":"Light on when lock.front is unlocked after dark"`) {
		t.Errorf("saved %s", saved)
	}
	// nothing fits: refused, no plan
	if exit, none := h.run(runPlanCommand, "template", templateBatteryReminder); exit != 4 || none.Error.Code != "TEMPLATE_NO_MATCH" || none.Plan != nil {
		t.Errorf("no match: exit %d %+v", exit, none)
	}
}

// A template whose plan would need a code is never offered or stored.
func TestPlanTemplateNeverStoresATypedPlan(t *testing.T) {
	f := newFakeHA()
	f.full = []map[string]any{{"entity_id": "lock.front", "platform": "zwave_js", "area_id": "hall"}, {"entity_id": "light.hall", "platform": "hue", "area_id": "hall"}}
	f.fail["config/entity_registry/list_for_display"] = true // an unreadable registry is typed (fail closed)
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "template", templateDoorLight)
	if exit != 4 || env.Error == nil || env.Error.Code != "TEMPLATE_NOT_OFFERED" || env.Plan != nil || len(h.planFiles()) != 0 {
		t.Fatalf("exit %d %+v plan %+v files %v", exit, env.Error, env.Plan, h.planFiles())
	}
}
