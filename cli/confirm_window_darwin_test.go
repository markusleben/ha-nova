package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDarwinWindowAnswers(t *testing.T) {
	exited := func(code string) error { return exec.Command("/bin/sh", "-c", "exit "+code).Run() }
	for _, c := range []struct {
		out     string
		err     error
		ended   bool
		showing bool
		want    windowAnswer
	}{
		{"button Confirm", nil, false, true, windowConfirmed},
		{"cancel", nil, false, true, windowCancelled},
		{"timeout", nil, false, true, windowNoAnswer},
		{"error -1712", nil, false, true, windowNoAnswer},
		{"error -1713", nil, false, true, windowNoAnswer}, // any error once the script reached the window
		{"error -50", nil, false, true, windowNoAnswer},   // any other error once the script reached the window
		{"", exited("1"), false, false, windowFailed},     // did not compile (sandbox, -2740)
		{"", exited("1"), false, true, windowNoAnswer},    // interrupted after reaching the window
		{"", errors.New("exec: no such file"), false, false, windowFailed},
		{"button Confirm", nil, true, true, windowNoAnswer},                                 // our own deadline ended it
		{"", exec.Command("/bin/sh", "-c", "kill -9 $$").Run(), false, false, windowClosed}, // killed from outside (lane D9)
		{"button Cancel", nil, false, true, windowNoAnswer},
		{"button Confirm extra", nil, false, true, windowNoAnswer},
	} {
		if got := darwinAnswer(c.out, c.err, c.ended, c.showing); got != c.want {
			t.Errorf("darwinAnswer(%q, %v, %v, %v) = %v, want %v", c.out, c.err, c.ended, c.showing, got, c.want)
		}
	}
}

// The window script compiles (one that doesn't never shows: windowFailed),
// takes the title, the text, and the labels only as arguments, adds Cancel
// first (the default: Return) and Confirm without a key, scrolls with a
// scroller that stays visible, and says it shows only right before it does.
func TestDarwinWindowScript(t *testing.T) {
	args := []string{"-l", "JavaScript", "-o", filepath.Join(t.TempDir(), "window.scpt")}
	for _, line := range windowScript {
		args = append(args, "-e", line)
	}
	if out, err := exec.Command("/usr/bin/osacompile", args...).CombinedOutput(); err != nil {
		t.Fatalf("the window script doesn't compile: %v %s", err, out)
	}
	marker := exec.Command("/usr/bin/osascript", "-l", "JavaScript", "-e", "console.log('"+windowShowing+"');")
	var stderr strings.Builder
	marker.Stderr = &stderr
	if out, err := marker.Output(); err != nil || len(out) > 0 || !strings.Contains(stderr.String(), windowShowing) { // nativeWindow reads it from stderr
		t.Fatalf("console.log: stdout %q stderr %q %v", out, stderr.String(), err)
	}
	script := strings.Join(windowScript, "\n")
	order := []string{"$.NSScreen.mainScreen", "try {", "= argv;", "wrappingLabelWithString($(title))", "text.string = $(shown);", "view.scrollerStyle = 0;", "alert.messageText = $(title);",
		"addButtonWithTitle($(cancel));", "addButtonWithTitle($(confirm)).keyEquivalent = $('');", "console.log('" + windowShowing + "')", "alert.runModal", "== 1001) return 'button Confirm'", "} catch (e) { return 'error ' + e; }"}
	at := -1
	for _, part := range order {
		next := strings.Index(script, part)
		if next <= at || strings.Count(script, part) != 1 {
			t.Fatalf("%q out of order or not once in:\n%s", part, script)
		}
		at = next
	}
	if strings.Count(script, "argv") != 2 || strings.Contains(script, "eval") || strings.Contains(script, "doShellScript") {
		t.Fatalf("arguments are data only:\n%s", script)
	}
}

// TestRealWindow opens the real window for two seconds and lets it time out
// (HA_NOVA_REAL_WINDOW=timeout), withdraws it after one second as when the
// phone answered first (withdrawn: it must close at once), or expects it to
// fail inside a sandbox (failed). Manual check only; never in CI.
func TestRealWindow(t *testing.T) {
	mode := os.Getenv("HA_NOVA_REAL_WINDOW")
	want := map[string]windowAnswer{"timeout": windowNoAnswer, "withdrawn": windowNoAnswer, "failed": windowFailed}
	expected, set := want[mode]
	if !set {
		t.Skip("manual: HA_NOVA_REAL_WINDOW=timeout|withdrawn|failed")
	}
	t.Logf("available: %v", nativeWindowAvailable())
	limit, wait := 10*time.Second, 2*time.Second
	if mode == "withdrawn" {
		limit, wait = time.Second, 30*time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	start := time.Now()
	if got := nativeWindow(ctx, planWindow("en", "HA NOVA test — nothing happens. Unlock “Front Door”?\n\nI'm about to unlock “Front Door”.\n\nThis unlocks a door.", true, "Unlock"), wait); got != expected || time.Since(start) > limit+2*time.Second {
		t.Fatalf("real window: %v after %v, want %v", got, time.Since(start), expected)
	}
	if out, _ := exec.Command("/usr/bin/pgrep", "-x", "osascript").Output(); mode == "withdrawn" && len(out) > 0 {
		t.Logf("an osascript still runs (maybe not ours): %s", out)
	}
}
