package main

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fileRelay is a raw relay with the YAML file /config/packages/lights.yaml and its
// .bak; change
// swaps its content, as another editor would, and extra adds handlers.
func fileRelay(t *testing.T, extra map[string]func(map[string]any) any) (paths runtimePaths, sent func() []string, change func(content string)) {
	t.Helper()
	var mu sync.Mutex
	content := "light:\n  - platform: group\n"
	handlers := map[string]func(map[string]any) any{"files read_file": func(p map[string]any) any {
		mu.Lock()
		defer mu.Unlock()
		switch p["path"] {
		case "/config/packages/lights.yaml":
			return map[string]any{"content": content, "size": len(content)}
		}
		return fakeRelayFailure{"FILE_NOT_FOUND", "not found"}
	}}
	for key, handle := range extra {
		handlers[key] = handle
	}
	paths, sent = rawRelay(t, handlers)
	return paths, sent, func(c string) { mu.Lock(); defer mu.Unlock(); content = c }
}

const (
	lightsWrite = `{"endpoint":"files","payload":{"action":"write_file","path":"/config/packages/lights.yaml","content":"light: []\n"}}`
	lightsCopy  = `{"endpoint":"backups","payload":{"action":"save","category":"yaml","name":"auto-config-packages-lights-yaml","data":{"path":"/config/packages/lights.yaml","content":"light:\n  - platform: group\n"}}}`
)

// A write that replaces an existing YAML file's content needs a copy of
// exactly that content first in the same plan; the refusal names the copy.
// A new file, the same content, or a write after its copy (the config
// check's rollback too: a .bak may be older than the file) is a preview; a
// copy under another category doesn't count. A
// file the Relay can't read plans nothing, and a plan writes a file once.
func TestPlanRawYAMLOverwriteNeedsACopyFirst(t *testing.T) {
	paths, sent, _ := fileRelay(t, nil)
	stale := strings.Replace(lightsCopy, "group", "template", 1)
	rollback := `{"endpoint":"files","payload":{"action":"write_file","path":"/config/packages/lights.yaml","content":"light: []\n","backup":false}}`
	otherKind := strings.Replace(lightsCopy, `"category":"yaml"`, `"category":"scenes"`, 1)
	for _, items := range []string{lightsWrite, stale + "," + lightsWrite, rollback, otherKind + "," + lightsWrite} {
		exit, env := planRaw(t, paths, "["+items+"]")
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "writing the file /config/packages/lights.yaml would replace its current content") ||
			!strings.Contains(env.Error.Next, `"category":"yaml","name":"auto-config-packages-lights-yaml","data":{"path":"/config/packages/lights.yaml","content":`) {
			t.Errorf("%s: exit %d %+v", items, exit, env.Error)
		}
	}
	for _, items := range []string{
		lightsCopy + "," + lightsWrite,
		lightsCopy + "," + rollback,
		`{"endpoint":"files","payload":{"action":"write_file","path":"/config/packages/new.yaml","content":"a: 1"}}`,
		`{"endpoint":"files","payload":{"action":"write_file","path":"/config/packages/lights.yaml","content":"light:\n  - platform: group\n"}}`,
		`{"endpoint":"files","payload":{"action":"write_file","path":"/config/packages/lights.txt","content":"x"}}`,
	} {
		if exit, env := planRaw(t, paths, "["+items+"]"); exit != verbExitConfirmation {
			t.Errorf("%s: exit %d %+v", items, exit, env.Error)
		}
	}
	if exit, env := planRaw(t, paths, "["+lightsCopy+","+lightsWrite+","+lightsWrite+"]"); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "writes the file /config/packages/lights.yaml once") {
		t.Errorf("two writes: exit %d %+v", exit, env.Error)
	}
	off, _ := rawRelay(t, map[string]func(map[string]any) any{"files read_file": func(map[string]any) any { return fakeRelayFailure{"FILE_ACCESS_DISABLED", "File access is disabled."} }})
	if exit, env := planRaw(t, off, "["+lightsWrite+"]"); exit != verbExitError || env.Plan != nil || env.Error == nil || !strings.Contains(env.Error.Message, "cannot read the file /config/packages/lights.yaml") {
		t.Errorf("unreadable: exit %d %+v", exit, env.Error)
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning sent %v", got)
	}
}

// The file's content is the plan's basis: one edited since the preview no
// longer matches its copy and is refused, one edited while the copy saved
// is refused unsent, and the copy
// can't be skipped: a failed one answers SNAPSHOT_REQUIRED and refuses
// --no-snapshot.
func TestPlanRawYAMLOverwriteRechecksAndKeepsItsCopy(t *testing.T) {
	paths, sent, change := fileRelay(t, nil)
	_, env := planRaw(t, paths, "["+lightsCopy+","+lightsWrite+"]")
	change("light: [edited]\n")
	if exit, applied := applyRaw(t, paths, env); applied.Error == nil || applied.Error.Code != "REFUSED" || !strings.Contains(applied.Error.Message, "would replace its current content") || len(sent()) != 0 {
		t.Fatalf("edited since the preview: exit %d %+v %v", exit, applied.Error, sent())
	}
	var edit func(string)
	var full atomic.Bool
	paths, sent, edit = fileRelay(t, map[string]func(map[string]any) any{"backups save": func(map[string]any) any {
		if full.Load() {
			return fakeRelayFailure{"SNAPSHOT_STORE_FULL", "the snapshot store is full"}
		}
		edit("light: [edited]\n")
		return map[string]any{"file": "yaml/auto-config-packages-lights-yaml.json.gz"}
	}})
	_, env = planRaw(t, paths, "["+lightsCopy+","+lightsWrite+"]")
	exit, applied := applyRaw(t, paths, env)
	if outcomes := rawOutcomes(t, applied); exit == 0 || statuses(outcomes) != "done,refused" || !strings.Contains(outcomes[1].Error, "the file /config/packages/lights.yaml changed since the preview") ||
		strings.Join(sent(), " | ") != "backups save" {
		t.Fatalf("edited while the copy saved: exit %d %+v %s %v", exit, applied.Error, applied.Result, sent())
	}
	full.Store(true)
	paths, _, _ = fileRelay(t, map[string]func(map[string]any) any{"backups save": func(map[string]any) any { return fakeRelayFailure{"SNAPSHOT_STORE_FULL", "full"} }})
	_, env = planRaw(t, paths, "["+lightsCopy+","+lightsWrite+"]")
	if exit, failed := applyRaw(t, paths, env); failed.Error == nil || failed.Error.Code != "SNAPSHOT_REQUIRED" || !strings.Contains(failed.Error.Next, "the current content of a YAML file") {
		t.Fatalf("copy failed: exit %d %+v", exit, failed.Error)
	}
	if exit, skipped := applyRaw(t, paths, env, "--no-snapshot"); skipped.Error == nil || skipped.Error.Code != "NO_SNAPSHOT_NOT_OFFERED" {
		t.Fatalf("--no-snapshot: exit %d %+v", exit, skipped.Error)
	}
}
