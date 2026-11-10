package main

import (
	"net/http"
	"strings"
	"testing"
)

// Images and videos in /config/www (docs/work/2026-11-10-www-media-delete-spec.md):
// the Relay keeps the copy, so a plan asks for no backups save and reads no
// text; it checks the name, the App, the file, and the room before anyone
// types a code, and the apply names the undo.

const wwwDoorDelete = `{"endpoint":"files","payload":{"action":"delete_file","path":"/config/www/snapshots/door.jpg"}}`

func wwwRelay(t *testing.T, extra map[string]func(map[string]any) any) (runtimePaths, func() []string) {
	t.Helper()
	handlers := map[string]func(map[string]any) any{
		"files list_deleted": func(map[string]any) any {
			return map[string]any{"copies": []any{map[string]any{"copy": "0123456789abcdef0123456789abcdef", "path": "/config/www/snapshots/old.jpg"}},
				"used_bytes": 1 << 20, "limit_bytes": 100 << 20, "file_limit_bytes": 25 << 20}
		},
		"files list_dir": func(map[string]any) any {
			return map[string]any{"entries": []any{
				map[string]any{"name": "door.jpg", "type": "file", "size": 5},
				map[string]any{"name": "huge.mp4", "type": "file", "size": 30 << 20},
				map[string]any{"name": "link.png", "type": "symlink", "size": nil},
			}}
		},
		"files delete_file": func(map[string]any) any {
			return map[string]any{"deleted": true, "copy": "fedcba9876543210fedcba9876543210", "kept_until": "2026-12-10T12:00:00.000Z"}
		},
	}
	for key, handle := range extra {
		handlers[key] = handle
	}
	paths, sent := rawRelay(t, handlers)
	return paths, func() []string {
		var changes []string
		for _, key := range sent() {
			if key != "files list_deleted" && key != "files list_dir" {
				changes = append(changes, key)
			}
		}
		return changes
	}
}

func TestPlanRawWWWDeleteNeedsNoSaveAndNamesTheUndo(t *testing.T) {
	paths, sent := wwwRelay(t, nil)
	exit, env := planRaw(t, paths, "["+wwwDoorDelete+"]")
	if exit != verbExitConfirmation || env.Plan == nil || env.Plan.Code == "" {
		t.Fatalf("a www delete is typed and needs no copy: exit %d %+v", exit, env.Error)
	}
	exit, applied := applyRaw(t, paths, env)
	if exit != 0 || !strings.Contains(applied.Say, "keeps a copy") ||
		!strings.Contains(applied.NextStep, `{"endpoint":"files","payload":{"action":"restore_file","copy":"fedcba9876543210fedcba9876543210","path":"/config/www/snapshots/door.jpg"}}`) {
		t.Fatalf("exit %d say %q next %q %+v", exit, applied.Say, applied.NextStep, applied.Error)
	}
	if got := strings.Join(sent(), " | "); got != "files delete_file" {
		t.Fatalf("sent %s", got)
	}
}

func TestPlanRawWWWRefusesBeforeAnyoneConfirms(t *testing.T) {
	paths, sent := wwwRelay(t, nil)
	full, _ := wwwRelay(t, map[string]func(map[string]any) any{"files list_deleted": func(map[string]any) any {
		return map[string]any{"copies": []any{}, "used_bytes": 100<<20 - 2, "limit_bytes": 100 << 20, "file_limit_bytes": 25 << 20}
	}})
	old, _ := wwwRelay(t, map[string]func(map[string]any) any{"files list_deleted": func(map[string]any) any {
		return fakeRelayStatus{http.StatusBadRequest, "VALIDATION_ERROR", "action must be one of: list_dir, read_file, write_file, delete_file"}
	}})
	item := func(action, file, copy string) string {
		return `[{"endpoint":"files","payload":{"action":"` + action + `","path":"` + file + `"` + map[bool]string{true: `,"copy":"` + copy + `"`}[copy != ""] + `}}]`
	}
	for _, tc := range []struct {
		paths       runtimePaths
		items, want string
	}{
		{paths, item("delete_file", "/config/www/card.js", ""), "deletes only images and videos"},
		{paths, item("delete_file", "/config/www/community/card/logo.png", ""), "never changes /config/www/community"},
		{paths, item("delete_file", "/config/www/Community/card/logo.png", ""), "never changes /config/www/community"},
		{paths, item("delete_file", "/config/www/snapshots/huge.mp4", ""), "larger than the 25 MiB"},
		{paths, item("delete_file", "/config/www/snapshots/link.png", ""), "not a plain file"},
		{paths, item("delete_file", "/config/www/snapshots/gone.jpg", ""), "there is no file /config/www/snapshots/gone.jpg"},
		{full, item("delete_file", "/config/www/snapshots/door.jpg", ""), "are full"},
		{old, item("delete_file", "/config/www/snapshots/door.jpg", ""), "update the NOVA App"},
		{paths, item("restore_file", "/config/www/snapshots/door.jpg", "0123456789abcdef0123456789abcdef"), "keeps no copy"},
		{paths, item("restore_file", "/config/packages/a.jpg", "0123456789abcdef0123456789abcdef"), "puts back only images and videos it deleted from /config/www"},
	} {
		exit, env := planRaw(t, tc.paths, tc.items)
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message+" "+env.Error.Next, tc.want) {
			t.Errorf("%s: exit %d %+v", tc.items, exit, env.Error)
		}
	}
	if exit, env := planRaw(t, paths, item("restore_file", "/config/www/snapshots/old.jpg", "0123456789abcdef0123456789abcdef")); exit != verbExitConfirmation || env.Plan == nil || env.Plan.Code == "" {
		t.Errorf("a restore of a kept copy is typed: exit %d %+v", exit, env.Error)
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("planning sent %v", got)
	}
}
