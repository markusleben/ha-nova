package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The skills teach `--server <name>` (it works in every shell; a
// `HA_NOVA_SERVER=<name> cmd` prefix is POSIX-only). So every command they
// chain or name with a server must accept the flag and reach that server.
// The demo tour runs each of them with --server demo; apply stays bare.
func TestEveryServerCommandTheSkillsNameTakesServerDemo(t *testing.T) {
	fakeDemo(t, "")
	resetServerProfileSelection(t)
	t.Cleanup(func() { activeDemo = false })
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if exit, out := runTry(t, paths); exit != 0 {
		t.Fatalf("try exit=%d out=%s", exit, out)
	}
	for _, args := range [][]string{
		{"check-update", "--quiet", "--server", "demo"},
		{"relay", "health", "--server", "demo"},
		{"relay", "ws", "--server", "demo", "-d", `{"type":"ping"}`},
		{"relay", "core", "--server", "demo", "--method", "GET", "--path", "/api/"},
		{"find", "bed light", "--server", "demo"},
		{"get", "light.bed_light", "--server", "demo"},
		{"history", "light.bed_light", "--server", "demo"},
		{"map", "--entity", "light.bed_light", "--server", "demo"},
		{"plan", "call", "lock.unlock", "--match", "entrance door", "--server", "demo"},
		{"plan", "delete", "automation.nightly_lock", "--server", "demo"},
		{"plan", "revert", "--target", "nightly_lock", "--server", "demo"},
		{"trace", "latest", "automation.nightly_lock", "--json", "--server", "demo"},
		{"trace", "list", "--server=demo", "automation.nightly_lock"},
		{"trace", "--server", "demo", "get", "automation.nightly_lock", "run1"},
		{"snapshot", "show", "--target", "nightly_lock", "--server=demo"},
	} {
		serverSelectionOverride, activeDemo, activeServerProfileName = "", false, defaultServerProfileName
		exit := 0
		out := captureStdout(t, func() {
			os.Stdout.WriteString(captureStderr(t, func() { exit = dispatch(paths, "ha-nova", args) }))
		})
		if strings.Contains(out, "not defined") || strings.Contains(out, "unknown") || !activeServerIsDemo() {
			t.Errorf("%v must select the demo house (exit=%d demo=%v):\n%s", args, exit, activeServerIsDemo(), out)
		}
	}

	// snapshot save reads no config: the flag must still tag a demo record,
	// or a real home could later revert to it.
	serverSelectionOverride = ""
	record := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(record, []byte(`{"op":"update","domain":"input_boolean","target_id":"guest","before_config":{"name":"a"},"expected_after":{"name":"b"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	exit := 0
	out := captureStdout(t, func() {
		exit = dispatch(paths, "ha-nova", []string{"snapshot", "save", "--data-file", record, "--server", "demo"})
	})
	if exit != 0 {
		t.Fatalf("snapshot save --server demo exit=%d out=%s", exit, out)
	}
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil || len(stack.Snapshots) != 1 || stack.Snapshots[0].Server != demoProfileName {
		t.Fatalf("snapshot save --server demo must tag the record: %+v %v", stack, err)
	}
}

func TestServerFlagNeedsAName(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"check-update", "--server="},
		{"doctor", "--server", " "},
		{"trace", "latest", "automation.x", "--server"},
		{"trace", "latest", "automation.x", "--server="},
		{"trace", "latest", "automation.x", "--server", "--json"},
		{"snapshot", "save", "--server="},
	} {
		exit := 0
		out := captureStdout(t, func() {
			os.Stdout.WriteString(captureStderr(t, func() { exit = dispatch(paths, "ha-nova", args) }))
		})
		if exit == 0 || !strings.Contains(out, "--server requires a non-empty profile name") {
			t.Errorf("%v: exit=%d out=%s", args, exit, out)
		}
	}
}

func TestTakeServerArgKeepsOtherArgs(t *testing.T) {
	resetServerProfileSelection(t)
	rest, err := takeServerArg([]string{"latest", "-server", "cabin", "automation.x", "--json"})
	if err != nil || strings.Join(rest, " ") != "latest automation.x --json" || serverSelectionOverride != "cabin" {
		t.Fatalf("rest=%v err=%v override=%q", rest, err, serverSelectionOverride)
	}
}

// A named server: an explicit selection must exist, and snapshot save tags
// the record with it (an untagged record matches every server's revert).
func TestServerFlagOnANamedServer(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	run := func(args ...string) (int, string) {
		serverSelectionOverride = ""
		exit := 0
		out := captureStdout(t, func() {
			os.Stdout.WriteString(captureStderr(t, func() { exit = dispatch(paths, "ha-nova", args) }))
		})
		return exit, out
	}
	if exit, out := run("check-update", "--quiet", "--server", "cabn"); exit == 0 || !strings.Contains(out, "unknown server profile") {
		t.Fatalf("a mistyped server must not count as checked: exit=%d out=%s", exit, out)
	}
	record := filepath.Join(t.TempDir(), "record.json")
	write := func(server string) {
		body := `{"op":"update","domain":"input_boolean","target_id":"guest",` + server + `"before_config":{"name":"a"},"expected_after":{"name":"b"}}`
		if err := os.WriteFile(record, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("")
	if exit, out := run("snapshot", "save", "--data-file", record, "--server", "cabn"); exit == 0 || !strings.Contains(out, "no snapshot was written") {
		t.Fatalf("snapshot save to a mistyped server: exit=%d out=%s", exit, out)
	}
	if exit, out := run("snapshot", "save", "--data-file", record, "--server", "cabin"); exit != 0 {
		t.Fatalf("snapshot save --server cabin: exit=%d out=%s", exit, out)
	}
	write(`"server":"default",`)
	if exit, out := run("snapshot", "save", "--data-file", record, "--server", "cabin"); exit == 0 || !strings.Contains(out, `the record is for server "default", not "cabin"`) {
		t.Fatalf("a record for another server: exit=%d out=%s", exit, out)
	}
	stack, err := loadUndoSnapshotStack(paths)
	if err != nil || len(stack.Snapshots) != 1 || stack.Snapshots[0].Server != "cabin" {
		t.Fatalf("snapshot save --server cabin must tag the record: %+v %v", stack, err)
	}
}

func TestMCPRunIDIsNeverAFlag(t *testing.T) {
	if runIDPattern.MatchString("--server") || !runIDPattern.MatchString("1a2b.c:d-e_f") {
		t.Fatal("run_id must not start with a dash")
	}
}

// "--server on every command but apply": the server-independent commands
// the skills name (update, diff, relay jq) accept it and ignore it.
func TestLocalCommandsAcceptServer(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HOME", t.TempDir())
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "a.json")
	if err := os.WriteFile(file, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"update", "--server", "cabin", "--help"},
		{"diff", "--before", file, "--after", file, "--server", "cabin"},
		{"relay", "jq", "--file", file, ".a", "--server", "cabin"},
		{"relay", "jq", "--server=cabin", "--file", file, ".a"},
	} {
		exit := 0
		out := captureStdout(t, func() {
			os.Stdout.WriteString(captureStderr(t, func() { exit = dispatch(paths, "ha-nova", args) }))
		})
		if exit != 0 {
			t.Errorf("%v: exit=%d out=%s", args, exit, out)
		}
	}
}
