package main

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// sceneRelay is a raw relay with the stored scene "evening" (the desk and
// hall lamps); change swaps its config, as another client would. "night"
// does not exist yet.
func sceneRelay(t *testing.T) (paths runtimePaths, sent func() []string, change func(map[string]any)) {
	t.Helper()
	var mu sync.Mutex
	scene := map[string]any{"id": "evening", "name": "Evening", "entities": map[string]any{
		"light.desk": map[string]any{"state": "on", "brightness": 128}, "light.hall": "off"}}
	paths, sent = rawRelay(t, map[string]func(map[string]any) any{
		"GET /api/config/scene/config/evening": func(map[string]any) any {
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"status": 200, "body": scene}
		},
		"GET /api/config/scene/config/night": func(map[string]any) any {
			return map[string]any{"status": 404, "body": map[string]any{"message": "Resource not found"}}
		},
	})
	return paths, sent, func(next map[string]any) { mu.Lock(); defer mu.Unlock(); scene = next }
}

const (
	sceneSaveWithoutHall = `{"endpoint":"core","payload":{"method":"POST","path":"/api/config/scene/config/evening","body":{"id":"evening","name":"Evening","entities":{"light.desk":"on","light.lamp":"on"}}}}`
	sceneSaveKeepingAll  = `{"endpoint":"core","payload":{"method":"POST","path":"/api/config/scene/config/evening","body":{"id":"evening","name":"Evening","entities":{"light.desk":"on","light.hall":"off","light.lamp":"on"}}}}`
)

// A scene save replaces the whole scene: one that leaves out a member is
// refused unless a copy of the scene's live config is saved first in the
// same plan; the refusal says which members would go and how to keep them.
func TestPlanRawSceneSaveThatDropsMembersNeedsACopyFirst(t *testing.T) {
	paths, sent, _ := sceneRelay(t)
	exit, env := planRaw(t, paths, "["+sceneSaveWithoutHall+"]")
	if exit != verbExitRefused || env.Error == nil || env.Error.Code != "REFUSED" || !strings.Contains(env.Error.Message, "light.hall") ||
		!strings.Contains(env.Error.Next, "would remove light.hall from the scene") || !strings.Contains(env.Error.Next, "send every member") ||
		!strings.Contains(env.Error.Next, `"action":"save","category":"scenes","name":"auto-evening"`) {
		t.Fatalf("a save that drops a member: exit %d %+v", exit, env.Error)
	}
	staleCopy := `{"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-evening","data":{"id":"evening","name":"Evening","entities":{"light.desk":"on"}}}}`
	if exit, env := planRaw(t, paths, "["+staleCopy+","+sceneSaveWithoutHall+"]"); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Next, "light.hall") {
		t.Fatalf("a copy of another config counted: exit %d %+v", exit, env.Error)
	}
	liveCopyThenPrune := `{"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-evening","data":{"entities":{"light.hall":"off","light.desk":{"brightness":128.0,"state":"on"}},"name":"Evening","id":"evening"}}},` +
		`{"endpoint":"backups","payload":{"action":"prune","keep_named":false}}`
	if exit, env := planRaw(t, paths, "["+liveCopyThenPrune+","+sceneSaveWithoutHall+"]"); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Next, "light.hall") {
		t.Fatalf("a copy a prune may take counted: exit %d %+v", exit, env.Error)
	}
	liveCopy := `{"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-evening","data":{"entities":{"light.hall":"off","light.desk":{"brightness":128.0,"state":"on"}},"name":"Evening","id":"evening"}}}`
	exit, env = planRaw(t, paths, "["+liveCopy+","+sceneSaveWithoutHall+"]")
	if exit != verbExitConfirmation || env.Tier != "preview" || len(sent()) != 0 {
		t.Fatalf("a save after a copy of the live config: exit %d %+v %v", exit, env.Error, sent())
	}
	if exit, env = applyRaw(t, paths, env); exit != 0 || statuses(rawOutcomes(t, env)) != "done,done" {
		t.Fatalf("apply: exit %d %+v %s", exit, env.Error, env.Result)
	}
	backToLive := `{"endpoint":"core","payload":{"method":"POST","path":"/api/config/scene/config/evening","body":{"id":"evening","name":"Evening","entities":{"light.desk":"on","light.hall":"off"}}}}`
	if exit, env := planRaw(t, paths, "["+sceneSaveKeepingAll+","+backToLive+"]"); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "saves the scene evening once") {
		t.Fatalf("a second save that drops what the first added: exit %d %+v", exit, env.Error)
	}
	if got := strings.Join(sent(), " | "); got != "backups save | POST /api/config/scene/config/evening" {
		t.Fatalf("sent %s", got)
	}
}

// The scene's live config is the plan's basis: a member edited in Home
// Assistant since the preview is drift, and one added there would be
// dropped, so the save is refused; either way nothing is overwritten.
func TestPlanRawSceneSaveDriftsWhenTheSceneChanged(t *testing.T) {
	for _, tc := range []struct {
		entities   map[string]any
		code, says string
	}{
		{map[string]any{"light.desk": map[string]any{"state": "on", "brightness": 255}, "light.hall": "off"}, "PLAN_DRIFT", "nothing was sent"},
		{map[string]any{"light.desk": "on", "light.hall": "off", "light.porch": "on"}, "REFUSED", "would remove light.porch"},
	} {
		paths, sent, change := sceneRelay(t)
		exit, env := planRaw(t, paths, "["+sceneSaveKeepingAll+"]")
		if exit != verbExitConfirmation || env.Tier != "preview" {
			t.Fatalf("a save that keeps every member: exit %d %+v", exit, env.Error)
		}
		change(map[string]any{"id": "evening", "name": "Evening", "entities": tc.entities})
		exit, env = applyRaw(t, paths, env)
		if env.Error == nil || env.Error.Code != tc.code || !strings.Contains(env.Error.Message, tc.says) || len(sent()) != 0 {
			t.Fatalf("apply after the scene changed: exit %d %+v %v", exit, env.Error, sent())
		}
	}
}

// A new scene has nothing to drop; a scene whose config can't be read is
// never saved over.
func TestPlanRawSceneSaveNewAndUnreadable(t *testing.T) {
	paths, sent, _ := sceneRelay(t)
	exit, env := planRaw(t, paths, `[{"endpoint":"core","payload":{"method":"POST","path":"/api/config/scene/config/night","body":{"id":"night","name":"Night","entities":{"light.hall":"off"}}}}]`)
	if exit != verbExitConfirmation || env.Tier != "preview" {
		t.Fatalf("a new scene: exit %d %+v", exit, env.Error)
	}
	unreadable, _ := rawRelay(t, map[string]func(map[string]any) any{
		"GET /api/config/scene/config/evening": func(map[string]any) any { return fakeRelayFailure{"UPSTREAM_TIMEOUT", "no answer"} },
	})
	if exit, env := planRaw(t, unreadable, "["+sceneSaveKeepingAll+"]"); exit != verbExitError || env.Plan != nil {
		t.Fatalf("an unreadable scene: exit %d %+v", exit, env.Error)
	}
	if len(sent()) != 0 {
		t.Fatalf("planning sent %v", sent())
	}
}

// Home Assistant stores the body's id: a save whose body names another id,
// even in another case, would rename the scene, so it is refused; a body
// without an id keeps the path's.
func TestPlanRawSceneSaveKeepsItsID(t *testing.T) {
	paths, sent, _ := sceneRelay(t)
	renamed := strings.Replace(sceneSaveKeepingAll, `"id":"evening"`, `"id":"Evening"`, 1)
	if exit, env := planRaw(t, paths, "["+renamed+"]"); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "is not the scene id evening") {
		t.Fatalf("a body id in another case: exit %d %+v", exit, env.Error)
	}
	noID := strings.Replace(sceneSaveKeepingAll, `"id":"evening",`, "", 1)
	if exit, env := planRaw(t, paths, "["+noID+"]"); exit != verbExitConfirmation || env.Tier != "preview" {
		t.Fatalf("a body without an id: exit %d %+v", exit, env.Error)
	}
	if len(sent()) != 0 {
		t.Fatalf("planning sent %v", sent())
	}
}

// A save after its copy says which members it removes, in the home's
// language. That copy can't be left out: when it fails, apply offers no
// --no-snapshot and refuses one, and goes on once the copy is saved.
func TestPlanRawSceneSaveSaysWhatGoesAndKeepsItsCopy(t *testing.T) {
	var full atomic.Bool
	full.Store(true)
	scene := map[string]any{"id": "evening", "name": "Evening", "entities": map[string]any{"light.desk": "on", "light.hall": "off"}}
	relay := func(lang string) (runtimePaths, func() []string) {
		return rawRelay(t, map[string]func(map[string]any) any{
			"GET /api/config": func(map[string]any) any {
				return map[string]any{"status": 200, "body": map[string]any{"language": lang}}
			},
			"GET /api/config/scene/config/evening": func(map[string]any) any { return map[string]any{"status": 200, "body": scene} },
			"backups save": func(map[string]any) any {
				if full.Load() {
					return fakeRelayFailure{"SNAPSHOT_STORE_FULL", "the snapshot store is full"}
				}
				return map[string]any{"file": "scenes/auto-evening.json.gz"}
			},
		})
	}
	items := `[{"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-evening","data":{"id":"evening","name":"Evening","entities":{"light.desk":"on","light.hall":"off"}}}},` + sceneSaveWithoutHall + "]"
	german, _ := relay("de")
	if _, env := planRaw(t, german, items); !strings.Contains(env.Say, "Entfernt „light.hall“ aus der Szene.") {
		t.Fatalf("German say %q", env.Say)
	}
	paths, sent := relay("en")
	exit, env := planRaw(t, paths, items)
	if exit != verbExitConfirmation || !strings.Contains(env.Say, "Removes “light.hall” from the scene.") {
		t.Fatalf("plan: exit %d say %q %+v", exit, env.Say, env.Error)
	}
	exit, failed := applyRaw(t, paths, env)
	if exit != verbExitError || failed.Error == nil || failed.Error.Code != "SNAPSHOT_REQUIRED" || !strings.Contains(failed.Error.Next, "never with --no-snapshot") ||
		formsStep(map[string]any{"error": map[string]any{"code": failed.Error.Code}, "plan": map[string]any{"id": env.Plan.ID}}, "step", false) != "step" {
		t.Fatalf("the copy failed: exit %d %+v", exit, failed.Error)
	}
	if exit, skipped := applyRaw(t, paths, env, "--no-snapshot"); exit != verbExitRefused || skipped.Error == nil || skipped.Error.Code != "NO_SNAPSHOT_NOT_OFFERED" ||
		!strings.Contains(skipped.Error.Message, "removes members") {
		t.Fatalf("--no-snapshot: exit %d %+v", exit, skipped.Error)
	}
	full.Store(false)
	if exit, done := applyRaw(t, paths, env); exit != 0 || statuses(rawOutcomes(t, done)) != "done,done" {
		t.Fatalf("apply once the copy saves: exit %d %+v %s", exit, done.Error, done.Result)
	}
	if got := strings.Join(sent(), " | "); got != "backups save | backups save | POST /api/config/scene/config/evening" {
		t.Fatalf("sent %s", got)
	}
}

// A scene save is checked against the live scene again right before it is
// sent: another client may change it while an earlier step runs.
func TestPlanRawSceneSaveRechecksTheSceneBeforeItsSave(t *testing.T) {
	var mu sync.Mutex
	scene := map[string]any{"id": "evening", "name": "Evening", "entities": map[string]any{"light.desk": "on", "light.hall": "off"}}
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"GET /api/config/scene/config/evening": func(map[string]any) any {
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"status": 200, "body": scene}
		},
		"backups save": func(map[string]any) any {
			mu.Lock()
			defer mu.Unlock()
			scene = map[string]any{"id": "evening", "name": "Evening", "entities": map[string]any{"light.desk": "on", "light.hall": "off", "light.porch": "on"}}
			return map[string]any{"file": "scenes/auto-evening.json.gz"}
		},
	})
	exit, env := planRaw(t, paths, `[{"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-evening","data":{"id":"evening","name":"Evening","entities":{"light.desk":"on","light.hall":"off"}}}},`+sceneSaveWithoutHall+"]")
	if exit != verbExitConfirmation {
		t.Fatalf("plan: exit %d %+v", exit, env.Error)
	}
	exit, applied := applyRaw(t, paths, env)
	outcomes := rawOutcomes(t, applied)
	if exit == 0 || statuses(outcomes) != "done,refused" || !strings.Contains(outcomes[1].Error, "changed in Home Assistant since the preview") {
		t.Fatalf("apply: exit %d %+v %s", exit, applied.Error, applied.Result)
	}
	if got := strings.Join(sent(), " | "); got != "backups save" {
		t.Fatalf("sent %s", got)
	}
}
