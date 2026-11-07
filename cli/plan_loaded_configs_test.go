package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A check reads every other automation as Home Assistant loaded it (WS
// automation/config, #1025): what runs, and never queued behind the
// stored-config view. Only the edited automation's own config is the stored
// one.

// watchHome: automation.watch reacts to the presence switch the draft
// "leave" turns off. Stored, it turns on a light; loaded, it unlocks the
// front door (its file was edited and not reloaded): the check weighs what
// runs.
func watchHome() *fakeHA {
	f := newFakeHA()
	f.ent("input_boolean.presence", "input_boolean", "on", map[string]any{"friendly_name": "Presence"})
	trigger := []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.presence", "to": "off"}}
	f.automation("watch", "w1", map[string]any{"triggers": trigger, "actions": []any{step("light.turn_on", "light.kitchen")}})
	f.loaded = map[string]any{"automation.watch": map[string]any{"id": "w1", "triggers": trigger, "actions": []any{step("lock.unlock", "lock.front")}}}
	f.related["entity:input_boolean.presence"] = []string{"automation.watch"}
	before := leaveDraft()
	before["actions"] = []any{}
	f.storeConfig("/api/config/automation/config/leave", before)
	return f
}

func (h *writeHarness) writeLeave() verbTestEnvelope {
	h.t.Helper()
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "leave", "--file", draftFile(h.t, leaveDraft()))
	return env
}

func TestOtherAutomationsAreCheckedAsLoaded(t *testing.T) {
	h := newWriteHarness(t, watchHome())
	env := h.writeLeave()
	if env.Tier != "typed" || !slices.ContainsFunc(env.Members, func(m tierMember) bool { return m.EntityID == "lock.front" }) {
		t.Fatalf("the loaded consumer unlocks the door: tier %s members %+v reasons %v", env.Tier, env.Members, reasonCodes(env))
	}
	var stored []string
	for _, c := range h.log.all() {
		if path := fmt.Sprint(c.payload["path"]); strings.HasPrefix(path, "/api/config/automation/config/") {
			stored = append(stored, path)
		}
	}
	if slices.Sort(stored); !slices.Equal(slices.Compact(stored), []string{"/api/config/automation/config/leave"}) {
		t.Fatalf("stored-config reads %v, want only the edited automation's", stored)
	}
}

// Every read of another automation that fails or finds nothing ends typed
// and names it; none passes as checked.
func TestAnUnreadLoadedConfigIsTyped(t *testing.T) {
	for name, c := range map[string]struct {
		edit func(f *fakeHA)
		want string
	}{
		"a failed read": {func(f *fakeHA) { f.fail["automation/config automation.watch"] = true }, reasonScanFailed},
		"not loaded":    {func(f *fakeHA) { f.loaded = nil; delete(f.configs, "/api/config/automation/config/w1") }, reasonUnreadConfig},
		"not loaded, no id": {func(f *fakeHA) {
			f.loaded = nil
			f.states["automation.watch"]["attributes"] = map[string]any{}
		}, reasonYAMLNoID},
	} {
		t.Run(name, func(t *testing.T) {
			f := watchHome()
			c.edit(f)
			env := newWriteHarness(t, f).writeLeave()
			if env.Tier != "typed" || !strings.Contains(reasonCodes(env), c.want) {
				t.Fatalf("tier %s reasons %v, want typed with %s", env.Tier, reasonCodes(env), c.want)
			}
		})
	}
}

// The listener scan reads every enabled automation as loaded too; one it
// can't read makes the check typed.
func TestTheListenerScanReadsLoadedConfigs(t *testing.T) {
	f := appUpdateHA()
	f.events = []map[string]any{{"event": "call_service", "listener_count": 1.0}}
	f.automation("listen", "l1", map[string]any{"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service"}}, "actions": []any{step("light.turn_on", "light.kitchen")}})
	f.fail["/api/config/automation/config/l1"] = true // the stored view is never asked
	h := newWriteHarness(t, f)
	if planned := h.plan(appInstall...); strings.Contains(reasonCodes(planned), reasonScanFailed) || strings.Contains(reasonCodes(planned), reasonListenerUnexpl) {
		t.Fatalf("reasons %v: the loaded config explains the listener", reasonCodes(planned))
	}
	f.mu.Lock()
	f.fail["automation/config automation.listen"] = true
	f.mu.Unlock()
	if planned := h.plan(appInstall...); planned.Tier != "typed" || !strings.Contains(reasonCodes(planned), reasonScanFailed) {
		t.Fatalf("tier %s reasons %v, want typed with %s", planned.Tier, reasonCodes(planned), reasonScanFailed)
	}
}

// A cut plan made before this change kept stored-config reads. Its replay
// now asks for loaded configs that plan never read: drift, never applied.
func TestACutPlanWithStoredConfigReadsIsDrift(t *testing.T) {
	w := useWindow(t) // no window may open
	h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
	h.hangFirst("l05", 1)
	planned := h.planLeave()
	path := filepath.Join(planDir(h.paths), planned.Plan.ID+".json")
	stored, err := readPlanFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := newRelaySession(h.paths, verbOptions{})
	if err != nil || !planSealed(stored, s.transport.Credential) {
		t.Fatalf("setup: the plan's seal is not this credential's (%v)", err)
	}
	old, moved := map[string]bool{}, 0
	for key, answered := range stored.Reads {
		var payload map[string]any
		if body, isWS := strings.CutPrefix(key, "ws "); isWS && json.Unmarshal([]byte(body), &payload) == nil && payload["type"] == "automation/config" {
			attrs, _ := h.f.states[fmt.Sprint(payload["entity_id"])]["attributes"].(map[string]any)
			oldBody, _ := json.Marshal(map[string]any{"method": "GET", "path": "/api/config/automation/config/" + fmt.Sprint(attrs["id"])})
			key, moved = "core "+string(oldBody), moved+1
		}
		old[key] = answered
	}
	if moved == 0 || len(old) != len(stored.Reads) {
		t.Fatalf("setup: %d reads, %d moved", len(stored.Reads), moved)
	}
	stored.Reads = old
	stored.ChannelSeal = channelSeal(s.transport.Credential, stored) // sealed as the old version did
	if err := writePlanFile(path, stored); err != nil {
		t.Fatal(err)
	}
	h.hangFirst("l05", 1)
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != verbExitDrift || len(w.messages) != 0 || len(h.writes()) != 0 {
		t.Fatalf("exit %d windows %d writes %d say %q", exit, len(w.messages), len(h.writes()), env.Say)
	}
}

// A blueprint instance runs the substituted blueprint with the instance's
// own keys over it (Home Assistant's async_substitute, an automation's
// action, trigger, and condition counted as the plural on both sides): its own actions count, and its digest is
// the one of the config Home Assistant loads, so a batch that wrote it
// still recognizes it as loaded.
func TestABlueprintInstanceIsCheckedAsHomeAssistantRunsIt(t *testing.T) {
	for _, keys := range [][2]string{{"actions", "actions"}, {"actions", "action"}, {"action", "actions"}} {
		body, key := keys[0], keys[1] // the blueprint's key, the instance's
		t.Run(body+" "+key, func(t *testing.T) {
			f := newFakeHA()
			triggers := []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.door"}}
			f.substitute = map[string]any{"substituted_config": map[string]any{"trigger": triggers, body: []any{step("light.turn_on", "light.hall")}}}
			f.storeConfig("/api/config/automation/config/leave", map[string]any{"alias": "Leaving", "triggers": triggers, "actions": []any{}})
			instance := map[string]any{"alias": "Leaving", "use_blueprint": map[string]any{"path": "leave.yaml", "input": map[string]any{}}, key: []any{step("lock.unlock", "lock.front")}}
			h := newWriteHarness(t, f)
			_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "leave", "--file", draftFile(t, instance))
			if env.Tier != "typed" || !slices.ContainsFunc(env.Members, func(m tierMember) bool { return m.EntityID == "lock.front" }) {
				t.Fatalf("the instance's own actions unlock the door: tier %s members %+v", env.Tier, env.Members)
			}
			s, err := newRelaySession(h.paths, verbOptions{})
			if err != nil {
				t.Fatal(err)
			}
			draft, _ := json.Marshal(instance)
			loaded, _ := json.Marshal(map[string]any{"alias": "Leaving", "triggers": triggers, "actions": []any{step("lock.unlock", "lock.front")}})
			for range 20 { // one key per field: never a digest of whichever spelling a map yields first
				e := newTierEngine(s)
				e.parseConfig("draft", "automation", "draft", draft, "draft")
				e.parseConfig("loaded", "automation", "loaded", loaded, "draft")
				if e.basis["automation:draft"] == "" || e.basis["automation:draft"] != e.basis["automation:loaded"] {
					t.Fatalf("draft digest %v, loaded digest %v", e.basis["automation:draft"], e.basis["automation:loaded"])
				}
			}
		})
	}
}
